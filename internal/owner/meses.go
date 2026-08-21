package owner

import (
	"context"
	"fmt"

	"github.com/Vitorepf/linkgym-api/internal/relogio"
)

// A FITA: uma coluna por mês FECHADO, com o que entrou.
//
// É o único elemento gráfico da Operação, e ele existe sob uma regra que a doutrina da casa
// escreve por extenso: nenhum número tem permissão de existir se tocá-lo não produzir uma
// lista de pessoas. A referência de mercado tem uma tela de retenção com quatro KPIs e cinco
// faixas coloridas, zero nomes e zero ações — e o documento da casa a condena como "a tela de
// números que o profissional olha uma vez por mês e fecha". A diferença entre aquilo e isto
// não é a tipografia: é que cada coluna daqui carrega os nomes que a compõem.
//
// SÓ MÊS FECHADO. O mês corrente já está desenhado inteiro na dobra, em três pedaços — repetir
// ele aqui como uma coluna pela metade seria a mesma informação em duas gramáticas, e a coluna
// de hoje cresceria durante o mês, fazendo a fita mentir sobre a forma da série.
//
// E só desenha a partir de DOIS meses: uma barra sozinha não tem com o que comparar, e um
// gráfico de um ponto é decoração. Não existe placeholder de série — não desenhar é a resposta
// certa, a mesma lei que src/ui/Baseline.tsx já aplica ("uma baseline falsa é PIOR que
// nenhuma").
type Mes struct {
	// dia 1 do mês, como o resto do produto conta competência.
	Month         string `json:"month"`
	RecebidoCents int    `json:"recebido_cents"`
	Quantos       int    `json:"quantos"`
	// OS NOMES por trás da coluna. Sem eles a fita é um relatório, e relatório é a forma
	// mais elegante de não fazer nada.
	Nomes []string `json:"nomes"`
}

// Seis meses cabem na largura de um telefone sem coluna virar risco. Mais que isso é uma
// série que ninguém lê no celular — e o personal que quiser o ano fecha o ano no contador.
const mesesDaFita = 6

func (s *Service) Meses(ctx context.Context, studioID string) ([]Mes, error) {
	// A competência de hoje é o CORTE: `pg.month < $2` deixa o mês corrente de fora, e é
	// isso que faz a fita só falar de mês fechado.
	rows, err := s.db.QueryContext(ctx, `
		SELECT pg.month::text,
		       SUM(pg.amount_cents),
		       count(*),
		       array_agg(p.name ORDER BY p.name)
		FROM mensalidade_pagamentos pg
		JOIN bonds b ON b.id = pg.bond_id
		JOIN people p ON p.id = b.person_id
		WHERE b.studio_id = $1 AND pg.month < $2::date
		GROUP BY pg.month
		ORDER BY pg.month DESC
		LIMIT $3`,
		studioID, relogio.Competencia(s.now()), mesesDaFita,
	)
	if err != nil {
		return nil, fmt.Errorf("meses: %w", err)
	}
	defer rows.Close()

	out := []Mes{}
	for rows.Next() {
		var m Mes
		if err := rows.Scan(&m.Month, &m.RecebidoCents, &m.Quantos, &nomes{&m.Nomes}); err != nil {
			return nil, fmt.Errorf("meses scan: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("meses rows: %w", err)
	}
	// Do mais antigo para o mais novo: a fita se lê da esquerda para a direita, como o tempo.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	// Uma coluna sozinha não é série. Devolver lista vazia é a resposta honesta, e a tela
	// simplesmente não desenha nada — sem esqueleto, sem eixo inventado.
	if len(out) < 2 {
		return []Mes{}, nil
	}
	return out, nil
}

// text[] do Postgres para []string, sem lib. `array_agg` devolve o literal `{a,b,"c,d"}` e o
// driver o entrega como bytes — são doze linhas de varredura contra promover uma dependência
// para um único tipo de coluna.
type nomes struct{ alvo *[]string }

func (n *nomes) Scan(v any) error {
	var s string
	switch t := v.(type) {
	case nil:
		*n.alvo = []string{}
		return nil
	case string:
		s = t
	case []byte:
		s = string(t)
	default:
		return fmt.Errorf("nomes: tipo inesperado %T", v)
	}
	if len(s) < 2 || s[0] != '{' || s[len(s)-1] != '}' {
		*n.alvo = []string{}
		return nil
	}
	s = s[1 : len(s)-1]
	out := []string{}
	var atual []rune
	dentro, escape := false, false
	for _, r := range s {
		switch {
		case escape:
			atual = append(atual, r)
			escape = false
		case r == '\\':
			escape = true
		case r == '"':
			dentro = !dentro
		case r == ',' && !dentro:
			out = append(out, string(atual))
			atual = nil
		default:
			atual = append(atual, r)
		}
	}
	if len(atual) > 0 || len(out) > 0 {
		out = append(out, string(atual))
	}
	*n.alvo = out
	return nil
}
