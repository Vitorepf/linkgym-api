package owner

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Vitorepf/linkgym-api/internal/relogio"
)

// A ASSINATURA QUE DE FATO REPETE.
//
// O tipo `assinatura` existia e o cardápio dizia "repete todo mês" — e nada repetia: vender
// a marmita criava uma venda, uma vez. O rótulo prometia um comportamento que o produto não
// tinha, e essa é a pior espécie de defeito, porque quem lê a tela não tem como desconfiar.
//
// A recorrência aqui NÃO é um processo que roda no dia 1º. É a mesma máquina que a
// Mensalidade já usa há duas migrations: as competências abertas são DERIVADAS
// (generate_series desde o início da assinatura, menos as que já foram recebidas). Nada é
// gerado por antecipação, nada precisa de cron, e não existe o dia em que o job falhou às
// 3h e a turma inteira ficou sem cobrança sem ninguém perceber até o dia 5.
//
// A linha só é MATERIALIZADA quando o personal age sobre ela — e aí o índice único
// (bond_id, produto_id, competencia) faz a idempotência ser do schema, não do código.
type Assinatura struct {
	ID         string `json:"id"`
	BondID     string `json:"bond_id"`
	ProdutoID  string `json:"produto_id"`
	PersonID   string `json:"person_id"`
	Name       string `json:"name"`
	Nome       string `json:"nome"`
	ValorCents int    `json:"valor_cents"`
	Desde      string `json:"desde"`
	// Quantas competências estão em aberto e quanto somam — a mesma leitura da mensalidade.
	MesesAbertos int `json:"meses_abertos"`
	DevidoCents  int `json:"devido_cents"`
	// A competência aberta mais antiga: é ela que o [Recebi] quita.
	PrimeiraAberta string `json:"primeira_aberta"`
	CopiaECola     string `json:"copia_e_cola"`
}

// Assinar: o personal vende um produto do tipo `assinatura` para UMA pessoa. O valor congela
// na assinatura, não fica preso ao produto — subir a marmita depois não reescreve o que ela
// combinou.
func (s *Service) Assinar(ctx context.Context, ownerID, produtoID, bondID string) (*Assinatura, error) {
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	desde := relogio.Competencia(s.now())

	var a Assinatura
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO assinaturas (bond_id, produto_id, valor_cents, desde)
		SELECT b.id, pr.id, pr.preco_cents, $4::date
		FROM bonds b
		JOIN produtos pr ON pr.id = $2 AND pr.studio_id = $3
		                AND pr.ativo AND pr.tipo = 'assinatura'
		WHERE b.id = $1 AND b.studio_id = $3 AND b.role = 'student' AND b.status = 'active'
		ON CONFLICT DO NOTHING
		RETURNING id::text, bond_id::text, produto_id::text, valor_cents, desde::text`,
		bondID, produtoID, studioID, desde,
	).Scan(&a.ID, &a.BondID, &a.ProdutoID, &a.ValorCents, &a.Desde)
	if err == sql.ErrNoRows {
		// Ou o vínculo/produto não é deste time, ou ela JÁ assina — e assinar de novo é a
		// mesma verdade, não um erro. A pergunta separa as duas.
		var jaAssina bool
		if err := s.db.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM assinaturas a JOIN bonds b ON b.id = a.bond_id
				WHERE a.bond_id = $1 AND a.produto_id = $2 AND a.ate IS NULL
				  AND b.studio_id = $3)`,
			bondID, produtoID, studioID,
		).Scan(&jaAssina); err != nil {
			return nil, fmt.Errorf("assinar conferencia: %w", err)
		}
		if !jaAssina {
			return nil, ErrNotFound
		}
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("assinar: %w", err)
	}
	return &a, nil
}

// Cancelar: grava a última competência cobrada, nunca apaga. O que ela pagou continua sendo
// fato, e o mês corrente continua devido — cancelar no dia 20 não devolve o mês.
func (s *Service) CancelarAssinatura(ctx context.Context, ownerID, assinaturaID string) error {
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE assinaturas a SET ate = $3::date
		FROM bonds b
		WHERE a.bond_id = b.id AND a.id = $1 AND b.studio_id = $2 AND a.ate IS NULL`,
		assinaturaID, studioID, relogio.Competencia(s.now()),
	)
	if err != nil {
		return fmt.Errorf("cancelar assinatura: %w", err)
	}
	// Cancelar duas vezes é a mesma verdade.
	if n, _ := res.RowsAffected(); n == 0 {
		var existe bool
		if err := s.db.QueryRowContext(ctx, `
			SELECT EXISTS (SELECT 1 FROM assinaturas a JOIN bonds b ON b.id = a.bond_id
			                WHERE a.id = $1 AND b.studio_id = $2)`, assinaturaID, studioID,
		).Scan(&existe); err != nil {
			return fmt.Errorf("cancelar assinatura conferencia: %w", err)
		}
		if !existe {
			return ErrNotFound
		}
	}
	return nil
}

// As assinaturas do time com competência em aberto. A janela vai do início da assinatura até
// a competência corrente — ou até `ate`, quando cancelada —, menos o que já foi recebido.
func (s *Service) AssinaturasAbertas(ctx context.Context, studioID, mes string) ([]Assinatura, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.id::text, a.bond_id::text, a.produto_id::text, p.id::text, p.name,
		       pr.nome, a.valor_cents, a.desde::text,
		       ab.meses, ab.primeira::text
		FROM assinaturas a
		JOIN bonds b   ON b.id = a.bond_id
		JOIN people p  ON p.id = b.person_id
		JOIN produtos pr ON pr.id = a.produto_id
		CROSS JOIN LATERAL (
		    SELECT count(*) AS meses, min(g.mes)::date AS primeira
		      FROM generate_series(a.desde, LEAST(COALESCE(a.ate, $2::date), $2::date),
		                           interval '1 month') g(mes)
		     WHERE NOT EXISTS (
		         SELECT 1 FROM extras e
		          WHERE e.bond_id = a.bond_id AND e.produto_id = a.produto_id
		            AND e.competencia = g.mes::date AND e.recebida_em IS NOT NULL)
		) ab
		WHERE b.studio_id = $1 AND b.status = 'active' AND ab.meses > 0
		ORDER BY ab.primeira, p.name`,
		studioID, mes,
	)
	if err != nil {
		return nil, fmt.Errorf("assinaturas abertas: %w", err)
	}
	defer rows.Close()

	out := []Assinatura{}
	for rows.Next() {
		var a Assinatura
		if err := rows.Scan(&a.ID, &a.BondID, &a.ProdutoID, &a.PersonID, &a.Name,
			&a.Nome, &a.ValorCents, &a.Desde, &a.MesesAbertos, &a.PrimeiraAberta); err != nil {
			return nil, fmt.Errorf("assinaturas abertas scan: %w", err)
		}
		a.DevidoCents = a.MesesAbertos * a.ValorCents
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("assinaturas abertas rows: %w", err)
	}
	quem := s.recebedorDe(ctx, studioID)
	for i := range out {
		out[i].CopiaECola = quem.codigo(out[i].DevidoCents)
	}
	return out, nil
}

// RECEBER a competência aberta MAIS ANTIGA de uma assinatura. Materializa a linha em
// `extras` já recebida — a mesma lei da mensalidade, em que quitar começa pelo mais antigo,
// e a mesma idempotência: o índice único (bond_id, produto_id, competencia) impede a mesma
// competência de ser cobrada duas vezes, e o `ON CONFLICT DO NOTHING` faz o segundo toque
// ser a mesma verdade em vez de um erro.
func (s *Service) ReceberAssinatura(ctx context.Context, ownerID, assinaturaID string) error {
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return err
	}
	ate := relogio.Competencia(s.now())

	res, err := s.db.ExecContext(ctx, `
		INSERT INTO extras (bond_id, descricao, valor_cents, produto_id, competencia,
		                    recebida_em, meio)
		SELECT a.bond_id, pr.nome, a.valor_cents, a.produto_id, g.mes, now(), 'mao'
		FROM assinaturas a
		JOIN bonds b ON b.id = a.bond_id
		JOIN produtos pr ON pr.id = a.produto_id
		CROSS JOIN LATERAL (
		    SELECT min(x.mes)::date AS mes
		      FROM generate_series(a.desde, LEAST(COALESCE(a.ate, $3::date), $3::date),
		                           interval '1 month') x(mes)
		     WHERE NOT EXISTS (
		         SELECT 1 FROM extras e
		          WHERE e.bond_id = a.bond_id AND e.produto_id = a.produto_id
		            AND e.competencia = x.mes::date AND e.recebida_em IS NOT NULL)
		) g
		WHERE a.id = $1 AND b.studio_id = $2 AND g.mes IS NOT NULL
		ON CONFLICT DO NOTHING`,
		assinaturaID, studioID, ate,
	)
	if err != nil {
		return fmt.Errorf("receber assinatura: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var existe bool
		if err := s.db.QueryRowContext(ctx, `
			SELECT EXISTS (SELECT 1 FROM assinaturas a JOIN bonds b ON b.id = a.bond_id
			                WHERE a.id = $1 AND b.studio_id = $2)`, assinaturaID, studioID,
		).Scan(&existe); err != nil {
			return fmt.Errorf("receber assinatura conferencia: %w", err)
		}
		if !existe {
			return ErrNotFound
		}
	}
	return nil
}

// O QUE ELA ASSINA, na tela DELA — e a porta de saída junto.
//
// A assinatura entrava com um toque e não saía por lugar nenhum: `CancelarAssinatura` existia,
// a rota existia, e nenhuma tela chamava. Ela mora aqui e não na fila do mês de propósito: a
// lista "Todo mês" da Operação é o TRABALHO DE HOJE (quem deve o quê), e encerrar não é
// trabalho do mês — é uma mudança no combinado, e combinado é assunto da pessoa.
type AssinaturaDaPessoa struct {
	ID         string `json:"id"`
	ProdutoID  string `json:"produto_id"`
	Nome       string `json:"nome"`
	ValorCents int    `json:"valor_cents"`
	Desde      string `json:"desde"`
}

func (s *Service) AssinaturasDaPessoa(ctx context.Context, studioID, personID string) ([]AssinaturaDaPessoa, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.id::text, a.produto_id::text, pr.nome, a.valor_cents, a.desde::text
		FROM assinaturas a
		JOIN bonds b ON b.id = a.bond_id
		JOIN produtos pr ON pr.id = a.produto_id
		WHERE b.studio_id = $1 AND b.person_id = $2 AND b.role = 'student'
		  AND a.ate IS NULL
		ORDER BY a.desde, pr.nome`,
		studioID, personID,
	)
	if err != nil {
		return nil, fmt.Errorf("assinaturas da pessoa: %w", err)
	}
	defer rows.Close()
	out := []AssinaturaDaPessoa{}
	for rows.Next() {
		var a AssinaturaDaPessoa
		if err := rows.Scan(&a.ID, &a.ProdutoID, &a.Nome, &a.ValorCents, &a.Desde); err != nil {
			return nil, fmt.Errorf("assinaturas da pessoa scan: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
