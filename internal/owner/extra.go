package owner

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/Vitorepf/linkgym-api/internal/pix"
)

// O QUE O PERSONAL VENDE FORA DA MENSALIDADE.
//
// O pedido era "criar produtos em pouquíssimos cliques, do jeito mais simples possível", com
// loja de suplemento e assinatura de marmita como exemplos. A resposta NÃO é um catálogo:
// "qualquer tipo de produto" é o pedido de uma Shopify, e uma Shopify dentro de um app de
// treino é como o produto morre. Categoria, foto, estoque, variante, frete e cupom são seis
// campos entre o personal e o dinheiro, e cada um é uma decisão que ele não sabe tomar às
// 22h — enquanto a venda que ele de fato faz é "avaliação física, 150" para UMA pessoa.
//
// Então: duas colunas de conteúdo, descrição livre e valor. Isso cobre avaliação, whey,
// marmita, aula avulsa, camiseta e o que mais ele inventar, sem nenhuma taxonomia para
// manter. Quando ele digitar a mesma descrição três meses seguidos, aí a coisa virou
// produto e ganha cadastro — com evidência, e não por antecipação.
//
// E ela nasce NOMINAL: não existe prateleira. Toda venda começa com o personal escolhendo
// uma pessoa, o que faz vender ser, por construção, mais um motivo para ele tocar em alguém
// hoje — e mantém o personal DENTRO da transação, que é o oposto do que uma vitrine faz.
type Extra struct {
	ID         string  `json:"id"`
	BondID     string  `json:"bond_id"`
	PersonID   string  `json:"person_id"`
	Name       string  `json:"name"`
	Descricao  string  `json:"descricao"`
	ValorCents int     `json:"valor_cents"`
	CriadaEm   string  `json:"criada_em"`
	RecebidaEm *string `json:"recebida_em"`
	CopiaECola string  `json:"copia_e_cola"`
}

const maxDescricao = 60

// Criar: duas entradas, e o retorno já traz o Pix pronto — a folha mostra o código sem uma
// segunda ida à rede, porque o próximo gesto dele é mandar para a aluna.
func (s *Service) CriarExtra(ctx context.Context, ownerID, bondID, descricao string, valorCents int) (*Extra, error) {
	descricao = strings.TrimSpace(descricao)
	if descricao == "" || len([]rune(descricao)) > maxDescricao || valorCents <= 0 {
		return nil, ErrInvalid
	}
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return nil, err
	}

	var out Extra
	// O INSERT confere o dono na PRÓPRIA escrita: um SELECT antes e um INSERT depois é uma
	// janela onde o vínculo pode mudar de estúdio entre os dois.
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO extras (bond_id, descricao, valor_cents)
		SELECT b.id, $2, $3 FROM bonds b
		WHERE b.id = $1 AND b.studio_id = $4 AND b.role = 'student' AND b.status = 'active'
		RETURNING id::text, bond_id::text, descricao, valor_cents, criada_em::date::text`,
		bondID, descricao, valorCents, studioID,
	).Scan(&out.ID, &out.BondID, &out.Descricao, &out.ValorCents, &out.CriadaEm)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("criar extra: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, `
		SELECT p.id::text, p.name FROM bonds b JOIN people p ON p.id = b.person_id
		WHERE b.id = $1`, out.BondID,
	).Scan(&out.PersonID, &out.Name); err != nil {
		return nil, fmt.Errorf("criar extra pessoa: %w", err)
	}
	out.CopiaECola = s.recebedorDe(ctx, studioID).codigo(out.ValorCents)
	return &out, nil
}

// As extras em aberto do estúdio — "A ENTREGAR" na tela. Fechadas somem: o que já foi
// recebido não é trabalho, é histórico, e histórico não disputa a dobra com trabalho.
func (s *Service) ExtrasAbertos(ctx context.Context, studioID string) ([]Extra, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id::text, c.bond_id::text, p.id::text, p.name, c.descricao, c.valor_cents,
		       c.criada_em::date::text
		FROM extras c
		JOIN bonds b ON b.id = c.bond_id
		JOIN people p ON p.id = b.person_id
		WHERE b.studio_id = $1 AND b.status = 'active' AND c.recebida_em IS NULL
		ORDER BY c.criada_em`,
		studioID,
	)
	if err != nil {
		return nil, fmt.Errorf("extras abertas: %w", err)
	}
	defer rows.Close()

	out := []Extra{}
	for rows.Next() {
		var c Extra
		if err := rows.Scan(&c.ID, &c.BondID, &c.PersonID, &c.Name, &c.Descricao,
			&c.ValorCents, &c.CriadaEm); err != nil {
			return nil, fmt.Errorf("extras abertas scan: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("extras abertas rows: %w", err)
	}
	quem := s.recebedorDe(ctx, studioID)
	for i := range out {
		out[i].CopiaECola = quem.codigo(out[i].ValorCents)
	}
	return out, nil
}

// Recebi / desfazer: o mesmo par da mensalidade, e pelo mesmo motivo. `meio` fica travado em
// 'mao' porque é a única escrita que a fase 1 conhece; o desfazer também, para que o dia em
// que existir provedor o fato dele não some por toque errado.
func (s *Service) ReceberExtra(ctx context.Context, ownerID, extraID string, desfazer bool) error {
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return err
	}
	consulta := `
		UPDATE extras c
		SET recebida_em = now(), meio = 'mao'
		FROM bonds b
		WHERE c.bond_id = b.id AND c.id = $1 AND b.studio_id = $2 AND c.recebida_em IS NULL`
	if desfazer {
		consulta = `
		UPDATE extras c
		SET recebida_em = NULL, meio = NULL
		FROM bonds b
		WHERE c.bond_id = b.id AND c.id = $1 AND b.studio_id = $2 AND c.meio = 'mao'`
	}
	res, err := s.db.ExecContext(ctx, consulta, extraID, studioID)
	if err != nil {
		return fmt.Errorf("receber extra: %w", err)
	}
	// Idempotente: repetir o toque não é erro, é a mesma verdade. Só reclama quando a
	// extra não é deste estúdio — e sem revelar que ela existe em algum lugar.
	if n, _ := res.RowsAffected(); n == 0 {
		var existe bool
		if err := s.db.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM extras c JOIN bonds b ON b.id = c.bond_id
				WHERE c.id = $1 AND b.studio_id = $2)`, extraID, studioID,
		).Scan(&existe); err != nil {
			return fmt.Errorf("receber extra conferencia: %w", err)
		}
		if !existe {
			return ErrNotFound
		}
	}
	return nil
}

// O RECEBEDOR, lido UMA vez por requisição. A primeira versão relia a linha do estúdio para
// cada extra — a mesma consulta N vezes para um dado que não muda dentro do pedido.
type recebedor struct {
	chave, nome, cidade string
}

func (r recebedor) configurado() bool {
	return r.chave != "" && r.nome != "" && r.cidade != ""
}

// O código daquele valor, ou vazio quando não há chave. Erro de montagem não derruba nada:
// sem código a linha perde o atalho e mantém o [Recebi]. Dinheiro é o assunto; o atalho é
// conveniência.
func (r recebedor) codigo(cents int) string {
	if !r.configurado() {
		return ""
	}
	codigo, err := pix.BRCode(r.chave, r.nome, r.cidade, cents)
	if err != nil {
		return ""
	}
	return codigo
}

func (s *Service) recebedorDe(ctx context.Context, studioID string) recebedor {
	var chave, nome, cidade sql.NullString
	if err := s.db.QueryRowContext(ctx,
		`SELECT chave_pix, nome_recebedor, cidade_recebedor FROM studios WHERE id = $1`,
		studioID,
	).Scan(&chave, &nome, &cidade); err != nil {
		return recebedor{}
	}
	return recebedor{chave.String, nome.String, cidade.String}
}
