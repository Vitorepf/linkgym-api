package owner

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	"github.com/Vitorepf/linkgym-api/internal/relogio"
)

// A OPERAÇÃO do personal: as leituras da Mensalidade sobre a turma (CONTEXT.md), a lista
// de quem está em aberto — com a ÚNICA ação que o domínio permite, marcar pago — e a
// lista de quem está perto de sumir. O app nunca cobra o aluno: tudo aqui é para os
// olhos e o dedo do personal.
type Operacao struct {
	StudentCount   int    `json:"student_count"`
	ComMensalidade int    `json:"com_mensalidade"`
	ReceitaCents   int    `json:"receita_cents"`
	TicketCents    int    `json:"ticket_cents"`
	Month          string `json:"month"`
	// O MÊS EM TRÊS PEDAÇOS, e não um "receita" que mistura os três. `receita_cents` é o
	// COMBINADO (preço de tabela × turma de hoje) e não muda um centavo quando ninguém
	// paga — chamar isso de receita é o app afirmando que entrou dinheiro que não entrou.
	// Estes três somam o combinado e dizem onde ele está.
	RecebidoCents int `json:"recebido_cents"`
	AVencerCents  int `json:"a_vencer_cents"`
	VencidoCents  int `json:"vencido_cents"`
	// Quando o personal marcou alguma coisa pela última vez. É o que permite a tela se
	// recusar a mentir: sem isto, "ninguém pagou" e "ele parou de marcar" são o mesmo
	// silêncio, e o app acusa a turma inteira de calote por esquecimento dele.
	UltimaMarcacao *string        `json:"ultima_marcacao"`
	EmAberto       []EmAbertoItem `json:"em_aberto"`
	Risco          []RiscoItem    `json:"risco"`
	// QUEM não tem combinado. O rodapé da tela já escrevia o NÚMERO ("8 sem valor
	// combinado ainda") e mandava o personal digitar "na pessoa" — uma porta que não
	// existia em tela nenhuma. Número sem os nomes que o compõem não abre porta: é o
	// mesmo defeito que a doutrina da casa aponta na tela "Retenção" da referência.
	SemCombinado []SemCombinadoItem `json:"sem_combinado"`
	// AS MESMAS LINHAS que produzem `receita_cents` e `ticket_cents`, sem o SUM. A tela
	// desenha a distribuição do preço e a média deixa de ser a única coisa que ele sabe:
	// "ticket médio R$ 375" é o mesmo número para uma turma toda em 375 e para uma turma
	// metade em 250 e metade em 500 — e as duas pedem manhãs diferentes.
	//
	// Por serem as mesmas linhas, a soma daqui É `receita_cents` e a contagem É
	// `com_mensalidade`: se divergir, é bug, não arredondamento.
	Combinados []CombinadoItem `json:"combinados"`
	// A chave Pix do personal, só para ele saber se já configurou e para a tela oferecer a
	// porta quando não. NÃO viaja no payload do aluno: `Time` é compartilhado com o convite
	// e com /v1/me do aluno, e chave de recebimento não é assunto de quem paga.
	Pix PixDoTime `json:"pix"`
	// O que ele vendeu fora da mensalidade e ainda não recebeu: avaliação, whey, marmita.
	AEntregar []Extra `json:"a_entregar"`
	// A fita de meses fechados. Vazia até existirem dois — um ponto não é série.
	Meses  []Mes  `json:"meses"`
	Placar Placar `json:"placar"`
	// O CARDÁPIO: o que ele vende, com o dinheiro que cada linha já fez.
	Produtos []Produto `json:"produtos"`
	// O QUE REPETE TODO MÊS e ainda não foi recebido. É a mensalidade de novo, com outro
	// nome e outro produto — e pela mesma máquina: competência derivada, nunca gerada.
	Assinaturas []Assinatura `json:"assinaturas"`
	// A OPERAÇÃO INTEIRA — o pedido era "toda". Não é um segundo painel de vaidade: são os
	// números que o personal não tem em lugar nenhum e que mudam o que ele faz no mês
	// seguinte. Cada um deles abre uma lista de nomes na tela.
	Visao Visao `json:"visao"`
}

type Visao struct {
	// Quem entrou e quem saiu NESTE mês. Um estúdio que ganha 3 e perde 3 tem o mesmo
	// total do mês passado e está numa situação completamente diferente — e o total
	// sozinho esconde exatamente isso.
	Entraram int `json:"entraram"`
	Sairam   int `json:"sairam"`
	// Há quantos meses, em média, os alunos ATIVOS estão com ele. É o número mais próximo
	// de "quanto vale um aluno" que dá para calcular com honestidade aqui, e o personal
	// nunca o viu.
	PermanenciaMeses float64 `json:"permanencia_meses"`
	// Receita de produto no mês, separada da mensalidade: são dois negócios com margens
	// diferentes, e somá-los esconde qual dos dois está crescendo.
	ProdutoCents int `json:"produto_cents"`
	// O maior combinado da turma sobre o total: concentração. Se um aluno é 15% da
	// receita, perder ele é um evento — e o personal não costuma saber disso.
	MaiorFatiaBps int    `json:"maior_fatia_bps"`
	MaiorNome     string `json:"maior_nome"`
}

type PixDoTime struct {
	Configurado bool   `json:"configurado"`
	Chave       string `json:"chave"`
	Nome        string `json:"nome"`
	Cidade      string `json:"cidade"`
}

type SemCombinadoItem struct {
	BondID   string `json:"bond_id"`
	PersonID string `json:"person_id"`
	Name     string `json:"name"`
}

// Uma pessoa e o que ela paga. Sem SUM, sem faixa, sem média: a faixa é decisão de
// desenho e muda com a largura do telefone — o servidor manda o fato.
type CombinadoItem struct {
	BondID      string `json:"bond_id"`
	PersonID    string `json:"person_id"`
	Name        string `json:"name"`
	AmountCents int    `json:"amount_cents"`
	DueDay      int    `json:"due_day"`
}

type EmAbertoItem struct {
	BondID      string `json:"bond_id"`
	PersonID    string `json:"person_id"`
	Name        string `json:"name"`
	AmountCents int    `json:"amount_cents"`
	DueDay      int    `json:"due_day"`
	// dias desde o vencimento neste mês; negativo = ainda vence, 0 = vence hoje.
	VencidoHa int `json:"vencido_ha"`
	// QUANTAS COMPETÊNCIAS esta pessoa deve, e quanto isso soma.
	//
	// A lista é escopada pelo MÊS, e por isso o valor da linha era sempre o de um mês: quem
	// devia desde junho lia "venceu há 3 meses · R$ 350" quando o fato era R$ 1.050. O
	// atraso já atravessava a virada; o VALOR não.
	//
	// A contagem começa no mês em que o COMBINADO foi feito, não no do vínculo: cobrar por
	// meses anteriores ao acordo seria inventar dívida.
	MesesAbertos int `json:"meses_abertos"`
	DevidoCents  int `json:"devido_cents"`
	// A DATA em que a competência aberta MAIS ANTIGA venceu. O pedido escrito do dono era
	// "quem não pagou ainda E A DATA", e a tela inteira não tinha uma única data de
	// calendário: dizia "venceu há 9 dias", "há 3 semanas", "há 2 meses" — e para quem devia
	// mais de um mês não dizia nem isso. Quanto mais velha a dívida, mais vago o texto, que
	// é o inverso do que ele precisa para escrever a mensagem.
	VenceuEm string `json:"venceu_em"`
	// A ALUNA DISSE que já pagou, neste mês. Não é pagamento e não muda número nenhum: é um
	// nome para o dedo do personal confirmar, e a única fonte de verdade que não depende da
	// memória de uma pessoa ocupada.
	DizQueJaPagou bool `json:"diz_que_ja_pagou"`
	// O copia-e-cola do Pix COM O VALOR desta linha. Vazio quando o personal ainda não
	// configurou a chave. Viaja no mesmo payload de propósito: um segundo pedido por linha
	// seria N idas à rede para um dado que já está aqui.
	CopiaECola string `json:"copia_e_cola"`
}

// RiscoItem mora em risco.go: o cálculo saiu daqui para ter DOIS consumidores (esta tela
// e a fila de atenção) sem duas regras que possam divergir.

func (s *Service) Operacao(ctx context.Context, ownerID string) (*Operacao, error) {
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	// A competência é civil e brasileira. Montada com time.UTC sobre a hora da máquina, no
	// dia 31 às 21h de Brasília ela já era o mês seguinte: a turma inteira voltava a dever
	// e o mês certo ficava em aberto sem caminho de quitação. Ver internal/relogio.
	month := relogio.Competencia(now)
	hoje := relogio.Dia(now)

	out := Operacao{
		Month:        month,
		EmAberto:     []EmAbertoItem{},
		Risco:        []RiscoItem{},
		SemCombinado: []SemCombinadoItem{},
		Combinados:   []CombinadoItem{},
	}

	if err := s.db.QueryRowContext(ctx, `
		SELECT count(*),
		       count(m.id),
		       COALESCE(SUM(m.amount_cents), 0)
		FROM bonds b
		LEFT JOIN mensalidades m ON m.bond_id = b.id
		WHERE b.studio_id = $1 AND b.role = 'student' AND b.status = 'active'`,
		studioID,
	).Scan(&out.StudentCount, &out.ComMensalidade, &out.ReceitaCents); err != nil {
		return nil, fmt.Errorf("operacao leituras: %w", err)
	}
	if out.ComMensalidade > 0 {
		out.TicketCents = out.ReceitaCents / out.ComMensalidade
	}

	// O ATRASO SAI DE UMA DATA, e da competência EM ABERTO MAIS ANTIGA — nunca de
	// `hoje.Day() - due_day`, que era a conta anterior e tinha dois defeitos de raiz.
	//
	// (a) Ela zerava na virada. Quem devia desde julho lia "venceu há 20 dias" em 30/08 e
	//     "venceu há 1 dia" em 01/09 — a dívida DOBRA e o sinal enfraquece, todo mês.
	// (b) Ela era aritmética de dia do mês, e o schema trava due_day em 1..28: para
	//     `hoje.Day() - due_day >= 5`, um vencimento no dia 27 precisaria do dia 32. O
	//     vencimento mais natural do país — fim do mês — nunca alcançava o sinal forte.
	//
	// E é limitado pela idade do VÍNCULO: quem entrou dia 20 com vencimento dia 1 não
	// deve desde o dia 1, deve a partir do próximo dia 1. Sem isso, a turma inteira nasce
	// vencida no primeiro uso do produto, com o vermelho pintado na barra do mês.
	rows, err := s.db.QueryContext(ctx, `
		SELECT b.id::text, p.id::text, p.name, m.amount_cents, m.due_day,
		       EXISTS (SELECT 1 FROM ja_paguei j WHERE j.bond_id = b.id AND j.month = $2::date),
		       a.meses,
		       LEAST($3::date - (a.primeira + (m.due_day - 1)),
		             $3::date - b.created_at::date),
		       NOT EXISTS (SELECT 1 FROM mensalidade_pagamentos pg
		                    WHERE pg.bond_id = b.id AND pg.month = $2::date),
		       -- A MESMA data de onde sai o atraso acima: a competência aberta mais antiga,
		       -- no dia do vencimento dela. Um ponto só, ou o dia e o "há N dias" divergem.
		       (a.primeira + (m.due_day - 1))::text
		FROM bonds b
		JOIN people p ON p.id = b.person_id
		JOIN mensalidades m ON m.bond_id = b.id
		-- UMA JANELA, OS DOIS NÚMEROS. A generate_series estava escrita DUAS vezes nesta
		-- mesma query e as cópias divergiram: a contagem partia do COMBINADO e o atraso
		-- partia do VÍNCULO. Medido no banco: vínculo de jan/2026 com combinado de ago/2026
		-- devolvia meses_abertos=1 e vencido_ha=216 — "venceu há 7 meses · R$ 350" para quem
		-- deve UM mês há 14 dias. Espelho em risco.go: as duas têm de andar juntas.
		-- A janela nunca é vazia aqui (a.meses > 0 no WHERE), então a.primeira não é NULL
		-- e o LEAST não cai no truque de ignorar NULL que mordeu risco.go.
		CROSS JOIN LATERAL (
		    SELECT count(*) AS meses, min(g.mes)::date AS primeira
		      FROM generate_series(
		             GREATEST(date_trunc('month', b.created_at),
		                      date_trunc('month', m.created_at))::date,
		             $2::date, interval '1 month') g(mes)
		     WHERE NOT EXISTS (SELECT 1 FROM mensalidade_pagamentos pg
		                        WHERE pg.bond_id = b.id AND pg.month = g.mes::date)
		) a
		WHERE b.studio_id = $1 AND b.role = 'student' AND b.status = 'active'
		  -- A LISTA É DA DÍVIDA, e não do mês. Era NOT EXISTS pagamento do mês corrente:
		  -- quem devia 5 competências recebia UM toque em [Recebi], agosto era marcado, ela
		  -- SUMIA da tela e R$ 1.750 em quatro competências ficavam abertos para sempre, sem
		  -- nenhum caminho de quitação no produto. Medido no banco de dev.
		  AND a.meses > 0
		-- Quem DISSE que pagou vem primeiro: é a única linha da lista em que o trabalho é
		-- conferir, e não lembrar. Depois, por dia de vencimento.
		ORDER BY EXISTS (SELECT 1 FROM ja_paguei j WHERE j.bond_id = b.id AND j.month = $2::date) DESC,
		         m.due_day, p.name`,
		studioID, month, hoje,
	)
	if err != nil {
		return nil, fmt.Errorf("operacao em aberto: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var it EmAbertoItem
		var correnteAberta bool
		if err := rows.Scan(&it.BondID, &it.PersonID, &it.Name, &it.AmountCents, &it.DueDay,
			&it.DizQueJaPagou, &it.MesesAbertos, &it.VencidoHa, &correnteAberta,
			&it.VenceuEm); err != nil {
			return nil, fmt.Errorf("operacao em aberto scan: %w", err)
		}
		// VENCIDO e A VENCER são coisas diferentes e estavam somadas no mesmo número. No
		// dia 4 do mês, com vencimentos em 5/10/15/20, NINGUÉM venceu — e a tela dizia
		// "EM ABERTO 28 · somam R$ 10.500", colado num "RECEITA DO MÊS R$ 10.500". Dois
		// números idênticos com sentidos opostos, e inadimplência real zero.
		// O que ESTA PESSOA deve, somando as competências abertas. É do TAMANHO DELA, e não
		// do mês — a barra do mês continua sendo do mês, e as duas coisas dizem o que são.
		it.DevidoCents = it.MesesAbertos * it.AmountCents
		// A BARRA É DO MÊS, a LINHA é da DÍVIDA. Quem já pagou agosto e deve junho entra na
		// lista (a dívida existe) e NÃO entra nas somas do mês (agosto está quitado), senão
		// recebido+a_vencer+vencido para de fechar em receita_cents.
		if correnteAberta {
			if it.VencidoHa >= 0 {
				out.VencidoCents += it.AmountCents
			} else {
				out.AVencerCents += it.AmountCents
			}
		}
		out.EmAberto = append(out.EmAberto, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("operacao em aberto rows: %w", err)
	}

	// O recebedor: UMA leitura, e o mesmo código vale para todas as linhas.
	quem := s.recebedorDe(ctx, studioID)
	out.Pix = PixDoTime{
		Configurado: quem.configurado(),
		Chave:       quem.chave,
		Nome:        quem.nome,
		Cidade:      quem.cidade,
	}
	for i := range out.EmAberto {
		out.EmAberto[i].CopiaECola = quem.codigo(out.EmAberto[i].DevidoCents)
	}

	visao, err := s.visaoDoMes(ctx, studioID, month)
	if err != nil {
		return nil, err
	}
	out.Visao = visao

	assinaturas, err := s.AssinaturasAbertas(ctx, studioID, month)
	if err != nil {
		return nil, err
	}
	out.Assinaturas = assinaturas

	produtos, err := s.Produtos(ctx, studioID, month)
	if err != nil {
		return nil, err
	}
	out.Produtos = produtos

	// O placar do mês: o que ELE fez, e não uma classificação da turma.
	placar, err := s.PlacarDoMes(ctx, studioID, month)
	if err != nil {
		return nil, err
	}
	out.Placar = placar

	// A fita: mês fechado, com os nomes que compõem cada coluna.
	meses, err := s.Meses(ctx, studioID)
	if err != nil {
		return nil, err
	}
	out.Meses = meses

	// O que ele vende fora da mensalidade e ainda não recebeu.
	avulsas, err := s.ExtrasAbertos(ctx, studioID)
	if err != nil {
		return nil, err
	}
	out.AEntregar = avulsas

	// O QUE ENTROU, do valor CONGELADO no fato — não do combinado de hoje. Subir o preço
	// de R$300 para R$350 em maio reescrevia o que entrou em janeiro.
	var ultima sql.NullString
	if err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(pg.amount_cents) FILTER (WHERE pg.month = $2::date), 0),
		       MAX(pg.paid_at)::date::text
		FROM mensalidade_pagamentos pg
		JOIN bonds b ON b.id = pg.bond_id
		WHERE b.studio_id = $1 AND b.role = 'student'`,
		studioID, month,
	).Scan(&out.RecebidoCents, &ultima); err != nil {
		return nil, fmt.Errorf("operacao recebido: %w", err)
	}
	if ultima.Valid {
		out.UltimaMarcacao = &ultima.String
	}

	// Os nomes por trás de "N sem valor combinado". Mesma cláusula das outras: só aluno
	// ativo deste estúdio, e a ausência é ausência de linha em `mensalidades`.
	semRows, err := s.db.QueryContext(ctx, `
		SELECT b.id::text, p.id::text, p.name
		FROM bonds b
		JOIN people p ON p.id = b.person_id
		WHERE b.studio_id = $1 AND b.role = 'student' AND b.status = 'active'
		  AND NOT EXISTS (SELECT 1 FROM mensalidades m WHERE m.bond_id = b.id)
		ORDER BY p.name`,
		studioID,
	)
	if err != nil {
		return nil, fmt.Errorf("operacao sem combinado: %w", err)
	}
	defer semRows.Close()
	for semRows.Next() {
		var it SemCombinadoItem
		if err := semRows.Scan(&it.BondID, &it.PersonID, &it.Name); err != nil {
			return nil, fmt.Errorf("operacao sem combinado scan: %w", err)
		}
		out.SemCombinado = append(out.SemCombinado, it)
	}
	if err := semRows.Err(); err != nil {
		return nil, fmt.Errorf("operacao sem combinado rows: %w", err)
	}

	// JOIN e não LEFT JOIN: quem não tem combinado não é um ponto num eixo de preço — tem
	// porta própria logo acima. `mensalidades.bond_id` é UNIQUE desde a 00006, então o JOIN
	// não multiplica linha.
	combRows, err := s.db.QueryContext(ctx, `
		SELECT b.id::text, p.id::text, p.name, m.amount_cents, m.due_day
		FROM bonds b
		JOIN people p ON p.id = b.person_id
		JOIN mensalidades m ON m.bond_id = b.id
		WHERE b.studio_id = $1 AND b.role = 'student' AND b.status = 'active'
		ORDER BY m.amount_cents, p.name`,
		studioID,
	)
	if err != nil {
		return nil, fmt.Errorf("operacao combinados: %w", err)
	}
	defer combRows.Close()
	for combRows.Next() {
		var it CombinadoItem
		if err := combRows.Scan(&it.BondID, &it.PersonID, &it.Name, &it.AmountCents, &it.DueDay); err != nil {
			return nil, fmt.Errorf("operacao combinados scan: %w", err)
		}
		out.Combinados = append(out.Combinados, it)
	}
	if err := combRows.Err(); err != nil {
		return nil, fmt.Errorf("operacao combinados rows: %w", err)
	}

	// QUEM VAI SUMIR: uma chamada, e a regra mora em risco.go. Antes o cálculo estava
	// inline aqui e a fila de atenção do Painel tinha outra fonte — duas telas
	// respondendo à mesma pergunta com regras diferentes, uma delas alimentada por uma
	// tabela que nenhum processo de produção preenche.
	risco, err := s.Risco(ctx, studioID)
	if err != nil {
		return nil, err
	}
	out.Risco = risco

	return &out, nil
}

// Marca a mensalidade do Vínculo como PAGA no mês corrente. Fato, não estado: repetir o
// toque não duplica nem falha — ON CONFLICT DO NOTHING é o "já estava pago".
func (s *Service) PagarMensalidade(ctx context.Context, ownerID, bondID string) error {
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return err
	}
	// A mesma competência civil da leitura: escrever num mês e ler de outro é como o
	// pagamento das 21h do dia 31 sumia.
	ate := relogio.Competencia(s.now())

	// O TOQUE QUITA A COMPETÊNCIA ABERTA MAIS ANTIGA, e não a corrente. Cravar o mês
	// corrente era um estado que o produto sabia criar e não sabia desfazer: quem devia 5
	// competências recebia um [Recebi], agosto era marcado, ela sumia da lista e R$ 1.750
	// em junho/julho ficavam abertos para sempre, sem nenhum caminho de quitação.
	//
	// O VALOR VIAJA JUNTO, tirado de `mensalidades` no momento do fato. Sem isto o histórico
	// era recalculado do combinado de hoje, e corrigir o preço reescrevia o passado.
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO mensalidade_pagamentos (bond_id, month, amount_cents, meio)
		SELECT b.id, g.mes, m.amount_cents, 'mao'
		FROM bonds b
		JOIN mensalidades m ON m.bond_id = b.id
		CROSS JOIN LATERAL (
		    SELECT min(x.mes)::date AS mes
		      FROM generate_series(
		             GREATEST(date_trunc('month', b.created_at),
		                      date_trunc('month', m.created_at))::date,
		             $2::date, interval '1 month') x(mes)
		     WHERE NOT EXISTS (SELECT 1 FROM mensalidade_pagamentos pg
		                        WHERE pg.bond_id = b.id AND pg.month = x.mes::date)
		) g
		WHERE b.id = $1 AND b.studio_id = $3 AND b.role = 'student' AND b.status = 'active'
		  AND g.mes IS NOT NULL
		ON CONFLICT (bond_id, month) DO NOTHING`,
		bondID, ate, studioID,
	)
	if err != nil {
		return fmt.Errorf("pagar mensalidade: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// Zero linhas tem TRÊS causas, e a conferência antiga só conhecia duas: o vínculo
		// não é deste time, já estava tudo pago, OU o vínculo não está ativo. A pergunta
		// antiga perdia `role` e `status`, então um vínculo pausado casava aqui e a função
		// devolvia sucesso sem ter gravado nada — o personal via a linha sumir e acreditava
		// ter registrado um pagamento que não existe.
		//
		// E ela não pode mais perguntar pelo mês corrente: com o preenchimento mais-antigo
		// primeiro, o toque que não gravou nada é o toque de quem não tem NENHUMA
		// competência aberta. A pergunta é pelo vínculo cobrável — true = nada em aberto =
		// sucesso idempotente, false = ErrNotFound.
		var cobravel bool
		if err := s.db.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM bonds b
				JOIN mensalidades m ON m.bond_id = b.id
				WHERE b.id = $1 AND b.studio_id = $2
				  AND b.role = 'student' AND b.status = 'active'
			)`, bondID, studioID,
		).Scan(&cobravel); err != nil {
			return fmt.Errorf("pagar mensalidade conferencia: %w", err)
		}
		if !cobravel {
			return ErrNotFound
		}
	}
	return nil
}

// A OPERAÇÃO INTEIRA, numa varredura. São os números que não cabem na dobra e que o
// personal não tem em lugar nenhum — nem no caderno, nem na planilha, nem na referência de
// mercado, que mostra "engajamento 83%" e nenhum nome.
func (s *Service) visaoDoMes(ctx context.Context, studioID, month string) (Visao, error) {
	var v Visao
	// Entraram e saíram NESTE mês. `updated_at` é o carimbo de quando o vínculo mudou de
	// estado — é o que existe hoje, e é honesto enquanto encerrar for o único caminho que
	// mexe em status.
	if err := s.db.QueryRowContext(ctx, `
		SELECT count(*) FILTER (WHERE b.created_at >= $2::date AND b.role = 'student'),
		       count(*) FILTER (WHERE b.status = 'ended' AND b.updated_at >= $2::date
		                          AND b.role = 'student')
		FROM bonds b WHERE b.studio_id = $1`,
		studioID, month,
	).Scan(&v.Entraram, &v.Sairam); err != nil {
		return v, fmt.Errorf("visao entradas: %w", err)
	}

	// Permanência média dos ATIVOS, em meses. Média de quem está, não de quem passou: a
	// segunda mistura quem saiu na primeira semana com quem está há três anos.
	if err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(AVG(EXTRACT(EPOCH FROM (now() - b.created_at)) / 2592000.0), 0)
		FROM bonds b
		WHERE b.studio_id = $1 AND b.role = 'student' AND b.status = 'active'`,
		studioID,
	).Scan(&v.PermanenciaMeses); err != nil {
		return v, fmt.Errorf("visao permanencia: %w", err)
	}

	// O que entrou de PRODUTO no mês, separado da mensalidade.
	if err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(e.valor_cents), 0)
		FROM extras e JOIN bonds b ON b.id = e.bond_id
		WHERE b.studio_id = $1 AND e.recebida_em >= $2::date`,
		studioID, month,
	).Scan(&v.ProdutoCents); err != nil {
		return v, fmt.Errorf("visao produto: %w", err)
	}

	// CONCENTRAÇÃO: a maior fatia da receita numa pessoa só. Em pontos-base para não
	// arrastar float pelo payload — e a tela escreve por extenso.
	var maior, total sql.NullInt64
	var nome sql.NullString
	if err := s.db.QueryRowContext(ctx, `
		SELECT max(m.amount_cents), SUM(m.amount_cents),
		       (SELECT p.name FROM mensalidades m2
		         JOIN bonds b2 ON b2.id = m2.bond_id
		         JOIN people p ON p.id = b2.person_id
		        WHERE b2.studio_id = $1 AND b2.role = 'student' AND b2.status = 'active'
		        ORDER BY m2.amount_cents DESC LIMIT 1)
		FROM mensalidades m
		JOIN bonds b ON b.id = m.bond_id
		WHERE b.studio_id = $1 AND b.role = 'student' AND b.status = 'active'`,
		studioID,
	).Scan(&maior, &total, &nome); err != nil {
		return v, fmt.Errorf("visao concentracao: %w", err)
	}
	if total.Valid && total.Int64 > 0 && maior.Valid {
		v.MaiorFatiaBps = int(maior.Int64 * 10000 / total.Int64)
		v.MaiorNome = nome.String
	}
	return v, nil
}

// DESFAZER o "Recebi". Marcar é a única escrita de dinheiro do produto e era write-only:
// um Pix que estorna, um comprovante de outro mês, um "vou receber hoje à noite" que não
// vem — todos terminavam com o mês marcado pago para sempre, e não havia nem onde ver o
// que foi marcado.
//
// Travado em `meio = 'mao'`: quando existir provedor, o fato dele não some por toque
// errado. Um dedo errado não é um fato; um repasse confirmado é.
func (s *Service) DesfazerPagamento(ctx context.Context, ownerID, bondID string) error {
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM mensalidade_pagamentos pg
		USING bonds b
		WHERE pg.bond_id = b.id
		  AND pg.bond_id = $1 AND b.studio_id = $2 AND pg.meio = 'mao'
		  -- O ÚLTIMO FATO, e não uma competência fixa. O toque preenche a competência aberta
		  -- mais antiga, então o desfazer escopado pelo mês corrente procurava agosto,
		  -- não achava a linha de junho que o dedo acabou de criar, e devolvia sucesso
		  -- silencioso: o recibo sumia da tela e o pagamento continuava gravado.
		  AND pg.id = (SELECT id FROM mensalidade_pagamentos
		                WHERE bond_id = $1 AND meio = 'mao'
		                ORDER BY paid_at DESC, month DESC LIMIT 1)`,
		bondID, studioID,
	)
	if err != nil {
		return fmt.Errorf("desfazer pagamento: %w", err)
	}
	// Idempotente para frente: desfazer duas vezes não é erro, é a mesma verdade. Só
	// reclama quando o vínculo não é deste estúdio — e sem revelar que ele existe.
	if n, _ := res.RowsAffected(); n == 0 {
		var existe bool
		if err := s.db.QueryRowContext(ctx, `
			SELECT EXISTS (SELECT 1 FROM bonds b WHERE b.id = $1 AND b.studio_id = $2)`,
			bondID, studioID,
		).Scan(&existe); err != nil {
			return fmt.Errorf("desfazer pagamento conferencia: %w", err)
		}
		if !existe {
			return ErrNotFound
		}
	}
	return nil
}

// O combinado, digitado uma vez (e corrigível): valor e dia do vencimento do Vínculo.
func (s *Service) DefinirMensalidade(ctx context.Context, ownerID, bondID string, amountCents, dueDay int) error {
	if amountCents < 0 || dueDay < 1 || dueDay > 28 {
		return ErrInvalid
	}
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO mensalidades (bond_id, amount_cents, due_day)
		SELECT b.id, $2, $3 FROM bonds b
		WHERE b.id = $1 AND b.studio_id = $4 AND b.role = 'student' AND b.status = 'active'
		ON CONFLICT (bond_id) DO UPDATE
		SET amount_cents = EXCLUDED.amount_cents, due_day = EXCLUDED.due_day, updated_at = now()`,
		bondID, amountCents, dueDay, studioID,
	)
	if err != nil {
		return fmt.Errorf("definir mensalidade: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

var hexColor = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

var (
	soDigitos = regexp.MustCompile(`^\d+$`)
	emailPix  = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
	evpPix    = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

// A CHAVE PIX, normalizada e conferida antes de gravar.
//
// O DICT aceita cinco formatos e só cinco. Gravar qualquer string faria o BR Code sair
// sintaticamente válido e semanticamente morto: o aplicativo do banco abriria e diria
// "chave não encontrada" na frente da aluna, e o personal não teria como saber que o
// problema é um ponto a mais no CPF que ele digitou meses atrás.
//
// Normaliza antes de recusar, porque a forma como a pessoa digita não é erro dela: CPF e
// CNPJ vêm com ponto e traço, telefone vem sem o país, e-mail vem com maiúscula.
func normalizarChavePix(bruta string) (string, bool) {
	c := strings.TrimSpace(bruta)
	if c == "" {
		return "", true // vazio LIMPA a chave: é escolha válida.
	}
	if evpPix.MatchString(c) {
		return strings.ToLower(c), true
	}
	if strings.Contains(c, "@") {
		e := strings.ToLower(c)
		// 77 é o limite do DICT para e-mail.
		return e, emailPix.MatchString(e) && len(e) <= 77
	}
	so := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, c)
	if !soDigitos.MatchString(so) {
		return "", false
	}
	switch len(so) {
	case 11:
		// Ambíguo por construção: 11 dígitos é CPF, e telefone com DDD também. O DICT
		// distingue pelo "+55", e quem digita telefone quase sempre escreve o sinal ou os
		// parênteses. Sem nenhuma dessas marcas, o palpite seguro é CPF — que é a chave que
		// todo mundo tem.
		if strings.HasPrefix(c, "+") || strings.ContainsAny(c, "()") {
			return "+55" + so, true
		}
		return so, true
	case 10:
		return "+55" + so, true
	case 13:
		if strings.HasPrefix(so, "55") {
			return "+" + so, true
		}
		return "", false
	case 14:
		return so, true // CNPJ
	}
	return "", false
}

// O que o personal escreve sobre o próprio Time. Ponteiro nil = não veio no corpo e não se
// toca na coluna; string vazia, onde faz sentido, LIMPA.
type TimePatch struct {
	Name            *string
	Accent          *string
	LogoKey         *string
	ChavePix        *string
	NomeRecebedor   *string
	CidadeRecebedor *string
}

// Vazio: nada veio no corpo. O handler pergunta para não gravar por engano e para devolver
// 400 em vez de 200 silencioso.
func (p TimePatch) Vazio() bool {
	return p.Name == nil && p.Accent == nil && p.LogoKey == nil &&
		p.ChavePix == nil && p.NomeRecebedor == nil && p.CidadeRecebedor == nil
}

// White-label: nome, acento e LOGO são DO TIME, e é o personal quem os escreve. O schema já
// garante o hex; validar aqui devolve 400 em vez de 500. Logo com string vazia LIMPA
// (voltar às iniciais é escolha válida); a chave do logo tem que ser do prefixo do próprio
// time.
//
// A CHAVE PIX entra aqui pelo mesmo caminho, e pelo mesmo motivo: é a marca dele operando.
// Ela nunca vira conta da plataforma — o BR Code é montado a partir dela e o dinheiro do
// aluno cai direto na conta do personal.
func (s *Service) PatchTime(ctx context.Context, ownerID string, p TimePatch) error {
	if p.Vazio() {
		return ErrInvalid
	}
	if p.Name != nil && strings.TrimSpace(*p.Name) == "" {
		return ErrInvalid
	}
	if p.Accent != nil && !hexColor.MatchString(*p.Accent) {
		return ErrInvalid
	}
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return err
	}
	if p.LogoKey != nil && *p.LogoKey != "" && !strings.HasPrefix(*p.LogoKey, "logo/"+studioID+"/") {
		return ErrInvalid
	}

	// Coluna e valor, em pares. Eram seis blocos UPDATE idênticos a menos do nome da
	// coluna, e cada campo novo do white-label acrescentava mais um. Os nomes de coluna são
	// literais deste arquivo — nunca entrada de usuário —, então a interpolação é segura e o
	// valor continua indo por parâmetro.
	type par struct {
		coluna string
		valor  string
		limpa  bool // string vazia grava NULL
	}
	var campos []par
	if p.Name != nil {
		campos = append(campos, par{"name", strings.TrimSpace(*p.Name), false})
	}
	if p.Accent != nil {
		campos = append(campos, par{"accent_color", *p.Accent, false})
	}
	if p.LogoKey != nil {
		campos = append(campos, par{"logo_object_key", *p.LogoKey, true})
	}
	if p.ChavePix != nil {
		chave, ok := normalizarChavePix(*p.ChavePix)
		if !ok {
			return ErrInvalid
		}
		campos = append(campos, par{"chave_pix", chave, true})
	}
	if p.NomeRecebedor != nil {
		campos = append(campos, par{"nome_recebedor", strings.TrimSpace(*p.NomeRecebedor), true})
	}
	if p.CidadeRecebedor != nil {
		campos = append(campos, par{"cidade_recebedor", strings.TrimSpace(*p.CidadeRecebedor), true})
	}
	for _, c := range campos {
		valor := "$2"
		if c.limpa {
			valor = "NULLIF($2, '')"
		}
		if _, err := s.db.ExecContext(ctx,
			"UPDATE studios SET "+c.coluna+" = "+valor+", updated_at = now() WHERE id = $1",
			studioID, c.valor,
		); err != nil {
			return fmt.Errorf("patch time %s: %w", c.coluna, err)
		}
	}
	return nil
}
