package owner

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// O CATÁLOGO: o que o personal vende, com nome e preço digitados UMA vez.
//
// Quatro tipos, fechados, e eles não são taxonomia — são os três comportamentos que o
// dinheiro tem, mais a entrega:
//
//	assinatura — repete todo mês (marmita, consultoria online)
//	pacote     — carrega saldo contável (10 sessões, 4 avaliações)
//	avulso     — acontece uma vez (avaliação física, aula experimental)
//	fisico     — precisa chegar na mão dela (whey, camiseta, garrafa)
//
// O que NÃO existe aqui, e a ausência é a decisão: categoria, foto, variante, cupom, frete,
// carrinho e busca. Cada um deles é um campo entre o personal e o dinheiro, e nenhum é uma
// escolha que ele saiba fazer às 22h. Estoque também não: quem tem quatro potes de whey no
// porta-malas resolve pausando o produto num toque, e `ativo` é o estoque dele — quantidade
// exigiria decremento atômico e a pergunta "o que acontece quando dois compram o último".
type Produto struct {
	ID         string `json:"id"`
	Tipo       string `json:"tipo"`
	Nome       string `json:"nome"`
	PrecoCents int    `json:"preco_cents"`
	Sessoes    *int   `json:"sessoes"`
	Ativo      bool   `json:"ativo"`
	// Quantas vendas em aberto e quanto elas somam. É o número que abre a lista de nomes —
	// nenhum agregado deste produto viaja sem o que o compõe.
	Vendidos    int `json:"vendidos"`
	AbertoCents int `json:"aberto_cents"`
	RecebidoMes int `json:"recebido_mes_cents"`
}

// O CARDÁPIO DE MODELOS: o personal não cria produto do zero, ele escolhe uma linha e digita
// o preço. É a diferença entre quatro toques e um formulário em branco às 22h — e um
// formulário em branco é onde a maioria desiste.
type Modelo struct {
	Tipo    string `json:"tipo"`
	Nome    string `json:"nome"`
	Sessoes *int   `json:"sessoes"`
	// o que a linha diz embaixo do nome, na hora de escolher.
	Nota string `json:"nota"`
}

func dez() *int { n := 10; return &n }

// Os modelos saem do que um personal brasileiro de fato vende. Não é lista de tudo que
// existe: é a lista que faz o dedo dele parar de digitar.
func Modelos() []Modelo {
	return []Modelo{
		{"assinatura", "Consultoria online", nil, "Repete todo mês"},
		{"assinatura", "Marmita fitness", nil, "Repete todo mês"},
		{"pacote", "Pacote de 10 sessões", dez(), "Carrega saldo, você marca cada uso"},
		{"avulso", "Avaliação física", nil, "Acontece uma vez"},
		{"avulso", "Aula experimental", nil, "Acontece uma vez"},
		{"fisico", "Whey protein", nil, "Você entrega na mão"},
		{"fisico", "Camiseta do time", nil, "Você entrega na mão"},
	}
}

const maxNomeProduto = 60

func (s *Service) CriarProduto(ctx context.Context, ownerID, tipo, nome string, precoCents int, sessoes *int) (*Produto, error) {
	nome = strings.TrimSpace(nome)
	switch tipo {
	case "assinatura", "pacote", "avulso", "fisico":
	default:
		return nil, ErrInvalid
	}
	if nome == "" || len([]rune(nome)) > maxNomeProduto || precoCents <= 0 {
		return nil, ErrInvalid
	}
	// O pacote é o único que carrega saldo, e ele NÃO existe sem o número: um "pacote de
	// sessões" sem quantas é um produto que ninguém sabe consumir.
	if (tipo == "pacote") != (sessoes != nil && *sessoes > 0) {
		return nil, ErrInvalid
	}
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return nil, err
	}

	var p Produto
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO produtos (studio_id, tipo, nome, preco_cents, sessoes)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id::text, tipo, nome, preco_cents, sessoes, ativo`,
		studioID, tipo, nome, precoCents, sessoes,
	).Scan(&p.ID, &p.Tipo, &p.Nome, &p.PrecoCents, &p.Sessoes, &p.Ativo)
	if err != nil {
		// O índice único é por nome ativo dentro do time: dois "Whey protein" no mesmo
		// cardápio é o personal se perdendo, não dois produtos.
		if strings.Contains(err.Error(), "produtos_nome_do_time") {
			return nil, ErrInvalid
		}
		return nil, fmt.Errorf("criar produto: %w", err)
	}
	return &p, nil
}

// Pausar/despausar: o estoque do personal em um toque. Pausado some da loja da aluna e do
// cardápio de venda, e as vendas antigas ficam inteiras.
func (s *Service) PausarProduto(ctx context.Context, ownerID, produtoID string, ativo bool) error {
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE produtos SET ativo = $3, updated_at = now()
		WHERE id = $1 AND studio_id = $2`,
		produtoID, studioID, ativo,
	)
	if err != nil {
		return fmt.Errorf("pausar produto: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// O cardápio do personal, com o dinheiro que cada linha já fez. Cada número aqui abre uma
// lista de nomes na tela — é a regra que vale para todo agregado deste produto.
func (s *Service) Produtos(ctx context.Context, studioID, mes string) ([]Produto, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT pr.id::text, pr.tipo, pr.nome, pr.preco_cents, pr.sessoes, pr.ativo,
		       count(e.id),
		       COALESCE(SUM(e.valor_cents) FILTER (WHERE e.recebida_em IS NULL), 0),
		       COALESCE(SUM(e.valor_cents) FILTER (
		           WHERE e.recebida_em IS NOT NULL
		             AND e.recebida_em >= $2::date), 0)
		FROM produtos pr
		LEFT JOIN extras e ON e.produto_id = pr.id
		WHERE pr.studio_id = $1
		GROUP BY pr.id
		ORDER BY pr.ativo DESC, pr.nome`,
		studioID, mes,
	)
	if err != nil {
		return nil, fmt.Errorf("produtos: %w", err)
	}
	defer rows.Close()

	out := []Produto{}
	for rows.Next() {
		var p Produto
		if err := rows.Scan(&p.ID, &p.Tipo, &p.Nome, &p.PrecoCents, &p.Sessoes, &p.Ativo,
			&p.Vendidos, &p.AbertoCents, &p.RecebidoMes); err != nil {
			return nil, fmt.Errorf("produtos scan: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// VENDER: o produto do cardápio para UMA pessoa. A venda continua nominal — não existe
// prateleira que se navegue sozinha, e é o personal escolhendo alguém que faz vender ser,
// por construção, mais um motivo para ele tocar num aluno hoje.
//
// Nome e preço CONGELAM na linha: subir o whey de R$219 para R$239 não reescreve o que ela
// pagou mês passado.
func (s *Service) VenderProduto(ctx context.Context, ownerID, produtoID, bondID string) (*Extra, error) {
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	// ASSINATURA NÃO É VENDA: é um combinado que repete. Vender uma marmita como avulsa
	// criava UMA linha, uma vez, e no mês seguinte não havia nada — o cardápio prometia
	// "repete todo mês" e nada repetia. Quem assina passa por `Assinar`.
	var tipo string
	if err := s.db.QueryRowContext(ctx,
		`SELECT tipo FROM produtos WHERE id = $1 AND studio_id = $2`, produtoID, studioID,
	).Scan(&tipo); err == nil && tipo == "assinatura" {
		return nil, ErrInvalid
	}
	var out Extra
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO extras (bond_id, descricao, valor_cents, produto_id)
		SELECT b.id, pr.nome, pr.preco_cents, pr.id
		FROM bonds b
		JOIN produtos pr ON pr.id = $2 AND pr.studio_id = $3 AND pr.ativo
		WHERE b.id = $1 AND b.studio_id = $3 AND b.role = 'student' AND b.status = 'active'
		RETURNING id::text, bond_id::text, descricao, valor_cents, criada_em::date::text`,
		bondID, produtoID, studioID,
	).Scan(&out.ID, &out.BondID, &out.Descricao, &out.ValorCents, &out.CriadaEm)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("vender produto: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, `
		SELECT p.id::text, p.name FROM bonds b JOIN people p ON p.id = b.person_id
		WHERE b.id = $1`, out.BondID,
	).Scan(&out.PersonID, &out.Name); err != nil {
		return nil, fmt.Errorf("vender produto pessoa: %w", err)
	}
	out.CopiaECola = s.recebedorDe(ctx, studioID).codigo(out.ValorCents)
	return &out, nil
}

// GASTAR UMA SESSÃO do pacote. O saldo é `sessoes - count(usos)`, nunca uma coluna que
// decrementa: contador que anda sozinho diverge em silêncio e ninguém sabe dizer quando
// começou. Uma por dia, pelo índice único — o mesmo dia duas vezes é dedo duplo.
func (s *Service) GastarSessao(ctx context.Context, ownerID, extraID, dia string) error {
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO usos_do_pacote (extra_id, usado_em)
		SELECT e.id, $3::date
		FROM extras e
		JOIN bonds b ON b.id = e.bond_id
		JOIN produtos pr ON pr.id = e.produto_id
		WHERE e.id = $1 AND b.studio_id = $2 AND pr.tipo = 'pacote'
		  AND (SELECT count(*) FROM usos_do_pacote u WHERE u.extra_id = e.id) < pr.sessoes
		ON CONFLICT (extra_id, usado_em) DO NOTHING`,
		extraID, studioID, dia,
	)
	if err != nil {
		return fmt.Errorf("gastar sessao: %w", err)
	}
	// Zero linhas: ou não é deste time, ou o saldo acabou, ou o dia já estava marcado. As
	// três são "não há o que fazer", e nenhuma delas é erro para o dedo do personal — menos
	// a primeira, que não pode nem revelar que a linha existe.
	if n, _ := res.RowsAffected(); n == 0 {
		var existe bool
		if err := s.db.QueryRowContext(ctx, `
			SELECT EXISTS (SELECT 1 FROM extras e JOIN bonds b ON b.id = e.bond_id
			                WHERE e.id = $1 AND b.studio_id = $2)`, extraID, studioID,
		).Scan(&existe); err != nil {
			return fmt.Errorf("gastar sessao conferencia: %w", err)
		}
		if !existe {
			return ErrNotFound
		}
	}
	return nil
}
