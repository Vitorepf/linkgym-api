package owner

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Vitorepf/linkgym-api/internal/relogio"
)

// QUEM VAI SUMIR — e por quê, numa frase que o personal confere contra a própria memória.
//
// A regra antiga era `dias_parado >= 7` (ou 3 com mensalidade em aberto) e tratava igual
// duas pessoas opostas: quem treinava 4× por semana e sumiu há 5 dias (perdeu), e quem
// treina 1× por semana e faltou uma (não perdeu nada). O limiar absoluto é o defeito: o
// denominador certo é a LINHA DA PRÓPRIA PESSOA.
//
// E a saída NÃO é um número. "78% de chance de cancelar" é infalsificável para quem lê: o
// personal não tem como saber que está errado, então ou obedece sem julgar ou ignora — e
// ignorar é o que acontece. Três fatos conferíveis ("ela treinava 4×, fez 1 nas últimas
// duas") ele checa contra a memória em dois segundos, descobre que ela viajou, e a fila
// continua merecendo confiança no dia seguinte. Assertivo é PRECISÃO com fato conferível,
// não recall com percentual.
//
// Teto de TRÊS, pela letra da doutrina da casa: o trabalho é tocar em três hoje. O quarto
// nome não aumenta o trabalho feito, aumenta a culpa.
type RiscoItem struct {
	PersonID string `json:"person_id"`
	BondID   string `json:"bond_id"`
	Name     string `json:"name"`
	Phone    string `json:"phone"`
	// A FRASE, pronta e conferível. Nunca um score, nunca um percentual, nunca uma faixa.
	Motivo string `json:"motivo"`
	// O que o dedo faz aqui: "recebi" | "pix" | "mandar" | "publicar" | "abrir".
	Acao string `json:"acao"`
	// O código do sinal, para o toque registrar POR QUE o personal foi chamado. É o que
	// permite, daqui a alguns meses, perguntar qual sinal de fato antecedeu uma saída.
	Sinal string `json:"sinal"`
	// Só quando o motivo é dinheiro: a linha mostra o valor e oferece [Recebi].
	AmountCents int `json:"amount_cents"`
}

// Os limiares, num lugar só e com o porquê de cada um.
const (
	// Uma semana sem aparecer já é a Retomada batendo na porta.
	sumicoDias = 7
	// Cinco dias de atraso é atraso; um ou dois é a vida.
	atrasoDias = 5
	// Três semanas sem o personal publicar nada. É o único sinal que aponta o dedo para
	// ELE — e num produto cuja migration 00010 diz que a presença dele é a única coisa
	// que se vende, é o sinal mais honesto da lista.
	silencioDias = 21
	// A janela recente e a linha de base: 14 dias contra as 8 semanas anteriores.
	janelaDias = 14
	baseDias   = 70
	// Sem um mínimo de sessões, "metade da média" é ruído: quem fez 2 e fez 0 não caiu,
	// só é irregular. Seis sessões em oito semanas é a menor amostra que carrega ritmo.
	baseMinima = 6
	// Estreante é outro fenômeno, com outro remédio: não é quem sumiu, é quem nunca
	// começou — e o remédio é o personal publicar, não mandar mensagem cobrando presença.
	estreiaDias    = 60
	estreiaSessoes = 3
	// Uma semana antes de chamar alguém de estreante parado. "Estreou há 1 dia. Fez 0."
	// não é risco, é terça-feira — e uma fila que começa acusando quem acabou de entrar é
	// uma fila que o personal aprende a ignorar na primeira semana de uso.
	estreiaMinima = 7
	// O trabalho é tocar em três hoje.
	TetoDoRisco = 3
	// A ESPERA DEPOIS DO TOQUE, e ela é `sumicoDias` de propósito, não um número novo: o
	// produto já chama sete dias de silêncio de "sumiu". Sete dias de silêncio DEPOIS de
	// uma mensagem é um fato do mesmo tamanho — e é a única coisa que ele não sabia ontem.
	// Voltar antes disso não é informação, é a mesma linha outra vez.
	esperaDoToque = sumicoDias
)

type fatos struct {
	bondID, personID, name, phone string
	desde                         int // dias desde o vínculo
	ultima                        sql.NullInt64
	total                         int
	recentes                      int
	base                          int
	abandonada                    sql.NullInt64
	semPublicar                   sql.NullInt64
	amountCents                   sql.NullInt64
	// competencias em aberto. O valor da divida e amountCents * mesesAbertos.
	mesesAbertos sql.NullInt64
	// dias de atraso da competência em aberto mais antiga. NULL = em dia.
	vencidoHa sql.NullInt64
	// dias desde o último toque. NULL = nunca foi tocada.
	tocadaHa sql.NullInt64
	// ELA JÁ RESPONDEU: disse que pagou o mês corrente, e o personal ainda não conferiu.
	// A fila não pode acusar quem já respondeu — era o defeito mais caro que restava nela,
	// porque a mesma pessoa aparecia na mesma dobra dizendo duas coisas contrárias: "R$ 350
	// em aberto há 9 dias" na fila, e "disse que pagou" na lista logo abaixo.
	dizQueJaPagou bool
}

// Risco é a ÚNICA computação de risco do produto. A Operação e a fila de atenção chamam
// esta função — se as duas contagens divergirem uma vez, o personal para de confiar nas
// duas, e a credibilidade da fila é o ativo inteiro do lado dele.
func (s *Service) Risco(ctx context.Context, studioID string) ([]RiscoItem, error) {
	hoje := relogio.Dia(s.now())
	mes := relogio.Competencia(s.now())

	// Uma varredura, agregados com FILTER. O `current_date` do SQL não aparece aqui de
	// propósito: ele é a data no fuso do SERVIDOR Postgres, e a competência é civil
	// brasileira — dois relógios davam "venceu há 9 dias" e "9 sem treinar" contados de
	// dias diferentes, três horas por noite.
	//
	// ponytail: uma subconsulta correlacionada por aluno (a última publicação) e outra
	// para o pagamento. Com 20–40 alunos por estúdio isso é irrelevante; acima de ~500
	// vira LATERAL ou uma tabela derivada. O teto está nomeado aqui para não custar uma
	// investigação depois.
	rows, err := s.db.QueryContext(ctx, `
		SELECT b.id::text, p.id::text, p.name, p.phone,
		       ($2::date - b.created_at::date),
		       ($2::date - GREATEST(max(ws.finished_at)::date, max(st.last_fulfilled_on))),
		       count(ws.id) FILTER (WHERE ws.finished_at IS NOT NULL),
		       count(ws.id) FILTER (WHERE ws.finished_at::date > $2::date - $4::int),
		       count(ws.id) FILTER (WHERE ws.finished_at::date <= $2::date - $4::int
		                              AND ws.finished_at::date >  $2::date - $5::int),
		       -- A sessão de HOJE não está abandonada: ela está acontecendo. O app grava
		       -- started_at na primeira série e só fecha no fim, então durante o treino
		       -- inteiro existe uma linha com finished_at NULL — sem este corte, a fila de
		       -- "quem vai sumir" enche das pessoas que estão treinando na frente dele.
		       ($2::date - max(ws.started_at) FILTER (
		            WHERE ws.finished_at IS NULL
		              AND ws.started_at::date <  $2::date
		              AND ws.started_at::date > $2::date - $6::int)::date),
		       ($2::date - (SELECT max(pr.published_at)::date FROM prescriptions pr
		                     WHERE pr.person_id = p.id AND pr.studio_id = b.studio_id
		                       AND pr.status = 'published')),
		       m.amount_cents,
		       ab.meses,
		       -- O ATRASO, em dias, da competência em aberto MAIS ANTIGA — a mesma
		       -- derivação de operacao.go, e pelos mesmos dois motivos: a conta antiga
		       -- (hoje.Day() menos due_day) zerava na virada do mês, bem quando a dívida
		       -- dobra, e nunca alcançava 5 dias para due_day 27 ou 28. NULL = em dia.
		       -- LEAST IGNORA NULL, e as duas pontas desta conta podem ser NULL: quem
		       -- nao tem combinado (m.due_day) e quem PAGOU TUDO (min(g.mes)). Nos dois
		       -- casos LEAST devolvia o segundo argumento — a idade do vinculo — e o teto
		       -- virava o VALOR: quem esta em dia ha 119 dias lia "R$ 350 em aberto ha 119
		       -- dias" no topo da fila, com [Recebi]. O CASE devolve o NULL que a conta tem.
		       CASE WHEN ab.mes_aberto IS NULL OR m.due_day IS NULL THEN NULL ELSE
		         LEAST($2::date - (ab.mes_aberto + (m.due_day - 1)),
		               $2::date - b.created_at::date)
		       END,
		       -- O QUE ELE JÁ FEZ. A tabela toques era escrita e nunca lida: segunda ele mandava
		       -- mensagem para a Marina, terça ela estava no topo de novo com o mesmo
		       -- motivo. Uma fila que não responde ao trabalho dele é uma fila que ele
		       -- para de rolar.
		       ($2::date - (SELECT max(t.created_at)::date FROM toques t
		                     WHERE t.person_id = p.id AND t.studio_id = b.studio_id)),
		       -- O QUE ELA JÁ DISSE. Mesma fonte da lista de em aberto (ja_paguei), ou as
		       -- duas leituras do mesmo mês discordam na mesma tela.
		       EXISTS (SELECT 1 FROM ja_paguei j WHERE j.bond_id = b.id AND j.month = $3::date)
		FROM bonds b
		JOIN people p ON p.id = b.person_id
		LEFT JOIN mensalidades m ON m.bond_id = b.id
		-- A PRESENÇA tem duas fontes e a maior das duas é a verdade: a sessão fechada e
		-- o Cumprimento que a Ofensiva registra (streaks.last_fulfilled_on). Contar só
		-- sessão fazia sumir quem treina e cumpre o dia por outro caminho — e streaks é
		-- PK por bond, então não multiplica linha.
		LEFT JOIN workout_sessions ws
		       ON ws.person_id = p.id AND ws.studio_id = b.studio_id
		LEFT JOIN streaks st ON st.bond_id = b.id
		-- AS COMPETENCIAS EM ABERTO, uma vez por vinculo. ESPELHO da LATERAL de
		-- operacao.go: MESMA janela (GREATEST vinculo/combinado) e MESMA contagem, ou a fila
		-- e a lista logo abaixo dela dizem dois valores para a mesma divida da mesma pessoa.
		LEFT JOIN LATERAL (
		    SELECT count(*) AS meses, min(g.mes)::date AS mes_aberto
		      FROM generate_series(GREATEST(date_trunc('month', b.created_at),
		                                    date_trunc('month', m.created_at))::date,
		                           $3::date, interval '1 month') g(mes)
		     WHERE NOT EXISTS (SELECT 1 FROM mensalidade_pagamentos pg
		                        WHERE pg.bond_id = b.id AND pg.month = g.mes::date)
		) ab ON true
		WHERE b.studio_id = $1 AND b.role = 'student' AND b.status = 'active'
		GROUP BY b.id, p.id, p.name, p.phone, b.created_at, m.amount_cents, m.due_day, ab.mes_aberto, ab.meses`,
		studioID, hoje, mes, janelaDias, baseDias, silencioDias,
	)
	if err != nil {
		return nil, fmt.Errorf("risco: %w", err)
	}
	defer rows.Close()

	// Cada aluno rende no máximo UM item: o sinal mais forte. Uma pessoa que aparece duas
	// vezes na mesma fila de três gasta duas das três vagas do dia.
	var achados []achado
	for rows.Next() {
		var f fatos
		if err := rows.Scan(
			&f.bondID, &f.personID, &f.name, &f.phone,
			&f.desde, &f.ultima, &f.total, &f.recentes, &f.base,
			&f.abandonada, &f.semPublicar, &f.amountCents, &f.mesesAbertos, &f.vencidoHa,
			&f.tocadaHa, &f.dizQueJaPagou,
		); err != nil {
			return nil, fmt.Errorf("risco scan: %w", err)
		}
		if a, ok := avaliar(f); ok && !jaTocada(f, a) {
			achados = append(achados, a)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("risco rows: %w", err)
	}

	// Prioridade primeiro, depois gravidade dentro do mesmo sinal: entre dois que sumiram,
	// quem sumiu há mais tempo.
	sortAchados(achados)

	out := make([]RiscoItem, 0, TetoDoRisco)
	for i, a := range achados {
		if i == TetoDoRisco {
			break
		}
		out = append(out, a.item)
	}
	return out, nil
}

type achado struct {
	item       RiscoItem
	prioridade int
	gravidade  int
}

// A ORDEM DOS SINAIS é a decisão de produto inteira: o primeiro que casar vira a frase, e
// os outros não aparecem. Do mais caro de ignorar para o menos.
func avaliar(f fatos) (achado, bool) {
	nome := f.name
	base := func(prio, grav int, motivo, acao, sinal string, cents int) (achado, bool) {
		return achado{
			item: RiscoItem{
				PersonID: f.personID, BondID: f.bondID, Name: nome, Phone: f.phone,
				Motivo: motivo, Acao: acao, Sinal: sinal, AmountCents: cents,
			},
			prioridade: prio,
			gravidade:  grav,
		}, true
	}

	parado := -1
	if f.ultima.Valid {
		parado = int(f.ultima.Int64)
	}
	vencidoHa := -1
	if f.vencidoHa.Valid {
		vencidoHa = int(f.vencidoHa.Int64)
	}
	// O TAMANHO DA DIVIDA, e nao um mes: a mesma pessoa lia "R$ 350 em aberto" aqui e
	// "5 meses · R$ 1.750" na lista 200pt abaixo, na mesma rolagem. Um ponto so, para o
	// motivo e o AmountCents do item nao poderem divergir.
	valor := 0
	if f.amountCents.Valid && f.mesesAbertos.Valid {
		valor = int(f.amountCents.Int64) * int(f.mesesAbertos.Int64)
	}

	// (1) DINHEIRO + SUMIÇO. Quem deve E sumiu é quem cancela — as outras cinco linhas são
	// prevenção, esta é a que já está acontecendo. Ação: [Recebi], porque a conversa é dele
	// e o app não escreve no lugar de ninguém.
	//
	// QUEM JÁ RESPONDEU NÃO É ACUSADA. Se ela tocou "já paguei", a frase muda de dona: não
	// é mais "ela não pagou há 9 dias", é "ele ainda não conferiu". Na foto de hoje a mesma
	// Marina aparece na dobra duas vezes dizendo coisas contrárias — "R$ 350 em aberto há 9
	// dias" aqui em cima, "disse que pagou" 275pt abaixo — e é a fila que está errada, não
	// a lista: `ja_paguei` é o dado mais novo sobre aquele mês, e a fila não o lia.
	//
	// Ela CONTINUA na fila, e é isso que separa esta correção de esconder o problema: ela
	// sumiu há 12 dias, e isso não deixou de ser verdade porque ela respondeu sobre o
	// dinheiro. O que muda é a frase e o dedo — [Conferir], que é o mesmo verbo que a linha
	// de baixo já usa para ela.
	if vencidoHa >= atrasoDias && parado >= sumicoDias {
		if f.dizQueJaPagou {
			return base(1, vencidoHa+parado,
				fmt.Sprintf("Disse que pagou R$ %s, e está há %s sem treinar.",
					reaisCurto(valor), dias(parado)),
				"recebi", "disse_que_pagou_e_sumico", valor)
		}
		return base(1, vencidoHa+parado,
			fmt.Sprintf("R$ %s em aberto há %s, e %s sem treinar.",
				reaisCurto(valor), dias(vencidoHa), dias(parado)),
			"recebi", "dinheiro_e_sumico", valor)
	}

	// (2) QUEDA CONTRA A LINHA DELA. O sinal que o limiar absoluto não enxerga: quem
	// treinava 4× por semana e caiu para 1 não disparou nenhum alarme de "7 dias parado",
	// porque nunca ficou 7 dias parado — foi embora devagar.
	esperadas := float64(f.base) * float64(janelaDias) / float64(baseDias-janelaDias)
	if f.base >= baseMinima && float64(f.recentes) < esperadas/2 {
		porSemana := float64(f.base) * 7 / float64(baseDias-janelaDias)
		return base(2, int(esperadas*10)-f.recentes,
			fmt.Sprintf("Treinava %s por semana. Fez %s nas últimas duas.",
				vezes(porSemana), sessoes(f.recentes)),
			"mandar", "queda_contra_a_linha", 0)
	}

	// (3) O SILÊNCIO DELE. O único sinal que aponta o dedo para o personal — e por isso
	// mesmo o mais honesto da lista. Ação: publicar, não mandar mensagem.
	//
	// Nunca publicou conta do VÍNCULO: sem isso, quem nunca recebeu uma ficha ficava fora
	// do sinal para sempre (a data era NULL), e o único aluno que o app nunca cobra do
	// personal era justamente o que ele nunca atendeu. E contar do vínculo dá o silêncio
	// de graça no caso novo: quem entrou ontem não aparece, quem entrou há um mês sim.
	semPublicar := f.desde
	if f.semPublicar.Valid {
		semPublicar = int(f.semPublicar.Int64)
	}
	if semPublicar >= silencioDias {
		frase := fmt.Sprintf("Você não publica para %s há %s.", primeiroNome(nome), dias(semPublicar))
		if !f.semPublicar.Valid {
			frase = fmt.Sprintf("Você ainda não publicou nada para %s.", primeiroNome(nome))
		}
		return base(3, semPublicar, frase, "publicar", "silencio_do_personal", 0)
	}

	// (4) COMEÇOU E NÃO TERMINOU. Sessão aberta e nunca fechada: é abandono no meio do
	// treino, e o motivo quase nunca é preguiça — é dor, máquina ocupada, ou uma carga que
	// não subia. Abre a pessoa, porque a resposta depende de ler o que aconteceu.
	if f.abandonada.Valid {
		return base(4, silencioDias-int(f.abandonada.Int64),
			fmt.Sprintf("Começou e não terminou, %s.", haQuantoTempo(int(f.abandonada.Int64))),
			"abrir", "sessao_abandonada", 0)
	}

	// (5) SUMIÇO PURO. Exige uma última sessão REAL: quem nunca treinou não "sumiu", e
	// jogar a turma inteira aqui na primeira semana de uso é como a regra antiga
	// transformava um estúdio recém-migrado do papel em 28 nomes fugindo.
	if parado >= sumicoDias {
		return base(5, parado, fmt.Sprintf("Sumiu faz %s.", dias(parado)), "mandar", "sumico", 0)
	}

	// (6) ESTREANTE QUE NÃO ENGATOU. Vínculo novo e quase nenhuma sessão. Outro fenômeno,
	// outro remédio: o que falta é ficha, não cobrança.
	if f.desde >= estreiaMinima && f.desde <= estreiaDias && f.total < estreiaSessoes {
		return base(6, estreiaDias-f.desde,
			fmt.Sprintf("Estreou há %s. Fez %s.", dias(f.desde), sessoes(f.total)),
			"publicar", "estreante_parado", 0)
	}

	return achado{}, false
}

// A FILA RESPONDE AO TRABALHO DELE. Suprime só o que é PEDIDO REPETIDO — as linhas de
// ação "mandar", que são as duas únicas que gravam toque (Operacao.tsx: `case "mandar"`).
//
// SUMIR, e não aparecer marcada: o teto é TRÊS, e um nome riscado ocupa uma das três vagas
// sem produzir trabalho — que é a definição de culpa da doutrina desta casa. O registro de
// que ele trabalhou já tem lugar: o placar do rodapé, "você tocou em 7 este mês".
//
// E DINHEIRO NÃO ESPERA. `dinheiro_e_sumico` não é pedido, é fato a registrar — a ação é
// [Recebi], que ninguém manda para ninguém. Esconder dinheiro por causa de uma mensagem
// enviada é o mesmo defeito ao contrário. É assim que o sinal muda o prazo: pela AÇÃO, sem
// segundo limiar.
func jaTocada(f fatos, a achado) bool {
	return a.item.Acao == "mandar" && f.tocadaHa.Valid && int(f.tocadaHa.Int64) < esperaDoToque
}

func sortAchados(a []achado) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0; j-- {
			menor := a[j].prioridade < a[j-1].prioridade ||
				(a[j].prioridade == a[j-1].prioridade && a[j].gravidade > a[j-1].gravidade)
			if !menor {
				break
			}
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}

// --- prosa. O português é parte do produto: "1 dias" e "fez 1 sessões" fazem a fila
// parecer um relatório gerado, e relatório gerado o personal não lê.

func dias(n int) string {
	if n == 1 {
		return "1 dia"
	}
	return fmt.Sprintf("%d dias", n)
}

func sessoes(n int) string {
	if n == 1 {
		return "1"
	}
	return fmt.Sprintf("%d", n)
}

func vezes(porSemana float64) string {
	n := int(porSemana + 0.5)
	if n < 1 {
		n = 1
	}
	return fmt.Sprintf("%d×", n)
}

func haQuantoTempo(diasAtras int) string {
	switch diasAtras {
	case 0:
		return "hoje"
	case 1:
		return "ontem"
	default:
		return fmt.Sprintf("há %s", dias(diasAtras))
	}
}

// Dinheiro na prosa da linha: inteiro limpo, milhar com ponto.
func reaisCurto(cents int) string {
	inteiro := cents / 100
	if inteiro < 1000 {
		return fmt.Sprintf("%d", inteiro)
	}
	return fmt.Sprintf("%d.%03d", inteiro/1000, inteiro%1000)
}

func primeiroNome(n string) string {
	for i, r := range n {
		if r == ' ' {
			return n[:i]
		}
	}
	return n
}
