package owner

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Vitorepf/linkgym-api/internal/relogio"
)

// A LOJA DA ALUNA — e o que ela NÃO é.
//
// Não é prateleira: não tem busca, filtro, categoria, ordenação nem carrinho. É a lista do
// que o personal DELA vende, com um verbo por linha, no rodapé do Perfil — longe do ritual
// de treino, que é o eixo que o produto não negocia. Vinte e oito alunos não são um
// marketplace, e uma vitrine dentro de um app de treino não é navegada.
//
// E o verbo dela NÃO É COMPRAR: é "quero". Ela não paga aqui, não escolhe forma de
// pagamento e não recebe cobrança nenhuma — ela levanta a mão, e isso vira um nome na tela
// do personal com o produto do lado. Quem fecha a venda é ele, com a frase dele. É a mesma
// lei do "já paguei": o app dá voz à aluna e mantém a caneta com o personal.
//
// O ganho comercial disso é maior que o de uma vitrine, e não menor: a venda continua sendo
// um motivo para ele tocar em alguém hoje, em vez de uma transação que acontece pelas
// costas dele.
type ItemDaLoja struct {
	ProdutoID  string `json:"produto_id"`
	Tipo       string `json:"tipo"`
	Nome       string `json:"nome"`
	PrecoCents int    `json:"preco_cents"`
	Sessoes    *int   `json:"sessoes"`
	// Ela já pediu, e ainda não foi atendida. O botão vira a confirmação.
	JaPedi bool `json:"ja_pedi"`
}

func (s *Service) LojaDoAluno(ctx context.Context, personID string) ([]ItemDaLoja, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT pr.id::text, pr.tipo, pr.nome, pr.preco_cents, pr.sessoes,
		       CASE WHEN pr.tipo = 'assinatura'
		            THEN EXISTS (SELECT 1 FROM assinaturas a
		                          WHERE a.bond_id = b.id AND a.produto_id = pr.id
		                            AND a.ate IS NULL)
		            ELSE EXISTS (SELECT 1 FROM extras e
		                          WHERE e.bond_id = b.id AND e.produto_id = pr.id
		                            AND e.recebida_em IS NULL)
		       END
		FROM bonds b
		JOIN produtos pr ON pr.studio_id = b.studio_id AND pr.ativo
		WHERE b.person_id = $1 AND b.role = 'student' AND b.status = 'active'
		ORDER BY pr.nome`,
		personID,
	)
	if err != nil {
		return nil, fmt.Errorf("loja do aluno: %w", err)
	}
	defer rows.Close()

	out := []ItemDaLoja{}
	for rows.Next() {
		var it ItemDaLoja
		if err := rows.Scan(&it.ProdutoID, &it.Tipo, &it.Nome, &it.PrecoCents,
			&it.Sessoes, &it.JaPedi); err != nil {
			return nil, fmt.Errorf("loja scan: %w", err)
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// "QUERO": ela levanta a mão. Isto NÃO cria dívida e NÃO cobra nada — grava a venda em
// aberto, que aparece na lista dele com o nome dela e o produto, para o dedo dele confirmar
// ou desfazer. O preço congela aqui, como em toda venda.
//
// Idempotente por construção: pedir duas vezes o mesmo produto com um pedido ainda aberto
// não empilha linha. Dedo duplo não é dois wheys.
func (s *Service) QueroEsse(ctx context.Context, personID, produtoID string) error {
	// ASSINATURA É OUTRO VERBO. Para um produto que repete todo mês, "quero" significa
	// ASSINAR — e criar uma venda avulsa aqui daria a ela uma marmita única que nunca
	// repetiria, exatamente o defeito que o tipo `assinatura` existia para consertar.
	//
	// Ela assina a si mesma, e isso não compromete o personal com nada: quem se compromete é
	// ela, com o mês. A primeira competência nasce em aberto e ele recebe como recebe
	// qualquer outra — o app continua sem cobrar ninguém.
	var tipo string
	if err := s.db.QueryRowContext(ctx, `
		SELECT pr.tipo FROM produtos pr
		JOIN bonds b ON b.studio_id = pr.studio_id
		WHERE pr.id = $2 AND pr.ativo
		  AND b.person_id = $1 AND b.role = 'student' AND b.status = 'active'`,
		personID, produtoID,
	).Scan(&tipo); err != nil {
		if err == sql.ErrNoRows {
			return ErrNotFound
		}
		return fmt.Errorf("quero esse tipo: %w", err)
	}
	if tipo == "assinatura" {
		return s.assinarComoAluna(ctx, personID, produtoID)
	}

	res, err := s.db.ExecContext(ctx, `
		INSERT INTO extras (bond_id, descricao, valor_cents, produto_id)
		SELECT b.id, pr.nome, pr.preco_cents, pr.id
		FROM bonds b
		JOIN produtos pr ON pr.id = $2 AND pr.studio_id = b.studio_id AND pr.ativo
		WHERE b.person_id = $1 AND b.role = 'student' AND b.status = 'active'
		  AND NOT EXISTS (
		      SELECT 1 FROM extras e
		      WHERE e.bond_id = b.id AND e.produto_id = pr.id AND e.recebida_em IS NULL)`,
		personID, produtoID,
	)
	if err != nil {
		return fmt.Errorf("quero esse: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// Ou já pediu (idempotente, e não é erro), ou o produto não é do time dela.
		var existe bool
		if err := s.db.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM bonds b
				JOIN produtos pr ON pr.id = $2 AND pr.studio_id = b.studio_id
				WHERE b.person_id = $1 AND b.role = 'student' AND b.status = 'active')`,
			personID, produtoID,
		).Scan(&existe); err != nil {
			return fmt.Errorf("quero esse conferencia: %w", err)
		}
		if !existe {
			return ErrNotFound
		}
	}
	return nil
}

// A ALUNA SE ASSINA. Idempotente pelo índice parcial (bond_id, produto_id) WHERE ate IS NULL:
// tocar duas vezes é a mesma verdade, não duas marmitas.
func (s *Service) assinarComoAluna(ctx context.Context, personID, produtoID string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO assinaturas (bond_id, produto_id, valor_cents, desde)
		SELECT b.id, pr.id, pr.preco_cents, $3::date
		FROM bonds b
		JOIN produtos pr ON pr.id = $2 AND pr.studio_id = b.studio_id
		                AND pr.ativo AND pr.tipo = 'assinatura'
		WHERE b.person_id = $1 AND b.role = 'student' AND b.status = 'active'
		ON CONFLICT DO NOTHING`,
		personID, produtoID, relogio.Competencia(s.now()),
	)
	if err != nil {
		return fmt.Errorf("assinar como aluna: %w", err)
	}
	return nil
}

// O que ela pediu e ainda não foi atendido, para a tela dela poder dizer "o Fred já sabe".
func (s *Service) PedidosDoAluno(ctx context.Context, personID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.descricao
		FROM extras e
		JOIN bonds b ON b.id = e.bond_id
		WHERE b.person_id = $1 AND b.status = 'active' AND e.recebida_em IS NULL
		  AND e.produto_id IS NOT NULL
		ORDER BY e.criada_em DESC`,
		personID,
	)
	if err != nil {
		return nil, fmt.Errorf("pedidos do aluno: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, fmt.Errorf("pedidos scan: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// DESISTIR: ela desfaz o próprio toque, e só o dela.
//
// A porta era de mão única. "Quero" numa marmita criava uma dívida MENSAL RECORRENTE com um
// toque, sem confirmação — e o botão virava "Assinado" e acabava. Um dedo errado no rodapé
// do Perfil comprometia ela com um valor todo mês, e o único caminho de volta era pedir
// para o personal fazer alguma coisa que a tela dele também não oferecia.
//
// A casa recusa diálogo de confirmação — "confirmação em toda ação é o que faz app parecer
// formulário" — e a saída que ela prescreve é a mesma de todo lugar onde este produto grava
// um fato: o DESFAZER é a confirmação. Então a linha volta a ser tocável, e o toque
// desfaz.
//
// O LIMITE É O DINHEIRO, e ele não é negociável: desfazer só enquanto NADA foi recebido. No
// minuto em que o personal marcou um mês, o que existe é um fato de caixa, e apagar fato de
// caixa pela tela de quem paga seria o app reescrevendo o livro dele. A partir daí quem
// encerra é ele — e é por isso que a linha dele ganhou "Encerrar" no mesmo lote.
//
// Idempotente: desistir do que já não existe é a mesma verdade, não um erro.
func (s *Service) DesistirDoPedido(ctx context.Context, personID, produtoID string) error {
	var tipo string
	if err := s.db.QueryRowContext(ctx, `
		SELECT pr.tipo FROM produtos pr
		JOIN bonds b ON b.studio_id = pr.studio_id
		WHERE pr.id = $2
		  AND b.person_id = $1 AND b.role = 'student' AND b.status = 'active'`,
		personID, produtoID,
	).Scan(&tipo); err != nil {
		if err == sql.ErrNoRows {
			return ErrNotFound
		}
		return fmt.Errorf("desistir tipo: %w", err)
	}

	if tipo == "assinatura" {
		// DELETE e não `ate = hoje`: nenhum mês foi recebido, então não houve assinatura —
		// houve um toque. Cancelar com data deixaria na história dela uma assinatura que
		// nunca existiu, e o `ate` é o registro de uma coisa que aconteceu.
		_, err := s.db.ExecContext(ctx, `
			DELETE FROM assinaturas a
			USING bonds b
			WHERE a.bond_id = b.id AND a.produto_id = $2 AND a.ate IS NULL
			  AND b.person_id = $1 AND b.role = 'student' AND b.status = 'active'
			  AND NOT EXISTS (
			      SELECT 1 FROM extras e
			      WHERE e.bond_id = a.bond_id AND e.produto_id = a.produto_id
			        AND e.recebida_em IS NOT NULL)`,
			personID, produtoID,
		)
		if err != nil {
			return fmt.Errorf("desistir da assinatura: %w", err)
		}
		return nil
	}

	// A venda avulsa em aberto: some inteira. O `recebida_em IS NULL` é o mesmo limite.
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM extras e
		USING bonds b
		WHERE e.bond_id = b.id AND e.produto_id = $2 AND e.recebida_em IS NULL
		  AND b.person_id = $1 AND b.role = 'student' AND b.status = 'active'`,
		personID, produtoID,
	)
	if err != nil {
		return fmt.Errorf("desistir do pedido: %w", err)
	}
	return nil
}
