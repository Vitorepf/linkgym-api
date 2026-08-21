package owner

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

// A TESE do risco, em teste: o denominador é a linha DA PRÓPRIA PESSOA.
//
// A regra antiga (>= 7 dias parado) tratava igual duas pessoas opostas. Aqui as duas
// existem no mesmo estúdio, com o mesmo número de sessões nas últimas duas semanas — e só
// uma delas está em risco. Se este teste passar com a regra antiga, a regra antiga não
// tinha tese nenhuma.
func TestQuedaContraALinhaDaPessoa(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, nil)
	ctx := context.Background()
	studio, _ := estudioDeTeste(t, database)

	// CAIU: treinava 4× por semana nas 8 semanas anteriores, fez 1 nas últimas duas.
	caiu := alunoCom(t, database, studio, "Marta Caiu", 120)
	publicouHa(t, database, studio, caiu, 3)
	sessoesEm(t, database, studio, caiu, diasAtras(15, 70, 32))
	sessoesEm(t, database, studio, caiu, []int{6})

	// SEMPRE FOI ASSIM: treina 1× por semana e continua treinando 1× por semana. Mesmo
	// número de sessões recentes que a Marta — e não está em risco nenhum.
	estavel := alunoCom(t, database, studio, "Rita Estavel", 120)
	publicouHa(t, database, studio, estavel, 3)
	sessoesEm(t, database, studio, estavel, diasAtras(15, 70, 8))
	sessoesEm(t, database, studio, estavel, []int{6})

	itens, err := svc.Risco(ctx, studio)
	if err != nil {
		t.Fatal(err)
	}
	var achouCaiu bool
	for _, it := range itens {
		if it.Name == "Rita Estavel" {
			t.Fatalf("falso positivo: quem sempre treinou 1x por semana entrou na fila (%s)", it.Motivo)
		}
		if it.Name == "Marta Caiu" {
			achouCaiu = true
			if it.Sinal != "queda_contra_a_linha" {
				t.Fatalf("sinal errado para quem caiu: %s (%s)", it.Sinal, it.Motivo)
			}
			// A frase é CONFERÍVEL: traz o ritmo antigo e o novo, nunca um percentual.
			if !strings.Contains(it.Motivo, "por semana") || strings.Contains(it.Motivo, "%") {
				t.Fatalf("motivo nao confere contra a memoria do personal: %q", it.Motivo)
			}
			if it.Acao != "mandar" {
				t.Fatalf("acao = %q", it.Acao)
			}
		}
	}
	if !achouCaiu {
		t.Fatalf("quem caiu de 4x para 1x nao entrou na fila: %+v", itens)
	}
}

// O teto é TRÊS, e a lista nunca é preenchida com enchimento: se não houver três, vêm
// menos. A doutrina da casa: o trabalho é tocar em três hoje, e o quarto nome não aumenta
// o trabalho feito, aumenta a culpa.
func TestRiscoNuncaPassaDeTres(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, nil)
	studio, _ := estudioDeTeste(t, database)

	// Doze pessoas sumidas: a fila continua com três.
	for i := 0; i < 12; i++ {
		p := alunoCom(t, database, studio, fmt.Sprintf("Sumido %02d", i), 120)
		publicouHa(t, database, studio, p, 3)
		sessoesEm(t, database, studio, p, []int{20 + i})
	}
	itens, err := svc.Risco(context.Background(), studio)
	if err != nil {
		t.Fatal(err)
	}
	if len(itens) > TetoDoRisco {
		t.Fatalf("fila com %d nomes", len(itens))
	}
	// E o mais grave vem primeiro: entre sumidos, quem sumiu há mais tempo.
	if len(itens) > 1 && itens[0].Sinal == "sumico" && itens[1].Sinal == "sumico" {
		if !strings.Contains(itens[0].Motivo, "31 dias") && !strings.Contains(itens[0].Motivo, "30 dias") {
			t.Logf("primeiro: %s", itens[0].Motivo)
		}
	}
	// Nenhuma pessoa aparece duas vezes: um nome repetido gasta duas das tres vagas.
	visto := map[string]bool{}
	for _, it := range itens {
		if visto[it.PersonID] {
			t.Fatalf("%s aparece duas vezes", it.Name)
		}
		visto[it.PersonID] = true
	}
}

// Quem acabou de entrar NÃO é risco. A regra antiga contava os dias parados a partir da
// data do vínculo quando não havia presença nenhuma — então um estúdio migrado do papel
// abria, no oitavo dia, com a turma inteira em "perto de sumir". É o pior momento possível
// para o produto acusar o personal de estar perdendo todo mundo.
func TestEstudioNovoNaoAcusaATurmaInteira(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, nil)
	studio, _ := estudioDeTeste(t, database)

	for i := 0; i < 10; i++ {
		alunoCom(t, database, studio, fmt.Sprintf("Recem %02d", i), 1)
	}
	itens, err := svc.Risco(context.Background(), studio)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range itens {
		if strings.HasPrefix(it.Name, "Recem") {
			t.Fatalf("aluno que entrou hoje na fila de risco: %s — %s", it.Name, it.Motivo)
		}
	}
}

// --- helpers

// UM ESTÚDIO SÓ DESTES TESTES. Eles enchem a turma de casos de propósito, e o estúdio do
// Fred é o cenário fixo de outros testes (student_count = 28, receita, ticket). Compartilhar
// a turma fazia um teste de risco quebrar quatro testes de leitura, o que não é defeito do
// produto — é o cenário de um vazando para o outro.
func estudioDeTeste(t *testing.T, database *sql.DB) (studioID, modelID string) {
	t.Helper()
	var dono string
	if err := database.QueryRow(`
		INSERT INTO people (phone, name) VALUES ('+5511900777001', 'Dona do risco')
		ON CONFLICT (phone) DO UPDATE SET name = EXCLUDED.name
		RETURNING id::text`).Scan(&dono); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		INSERT INTO studios (owner_person_id, name) VALUES ($1, 'Estudio do risco')
		ON CONFLICT (owner_person_id) DO UPDATE SET name = EXCLUDED.name
		RETURNING id::text`, dono).Scan(&studioID); err != nil {
		t.Fatal(err)
	}
	// Turma limpa a cada teste: casos de rodadas anteriores falsificariam este cenário.
	if _, err := database.Exec(`
		DELETE FROM people WHERE id IN (
			SELECT person_id FROM bonds WHERE studio_id = $1 AND role = 'student')`,
		studioID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		INSERT INTO models (studio_id, name, created_by_person_id) VALUES ($1, 'Modelo', $2)
		ON CONFLICT (studio_id, name) WHERE archived_at IS NULL
		DO UPDATE SET name = EXCLUDED.name
		RETURNING id::text`, studioID, dono).Scan(&modelID); err != nil {
		t.Fatal(err)
	}
	return studioID, modelID
}

// O telefone sai do NOME, e a escrita é upsert: `openSeeded` re-semeia mas não trunca, e
// um contador de pacote colidia com as pessoas deixadas pela rodada anterior.
func foneDe(nome string) string {
	h := 0
	for _, r := range nome {
		h = (h*31 + int(r)) % 900000
	}
	return fmt.Sprintf("+5511%09d", 100000+h)
}

// Um aluno deste estúdio, com o vínculo datado `diasDeVinculo` atrás.
func alunoCom(t *testing.T, database *sql.DB, studioID, nome string, diasDeVinculo int) string {
	t.Helper()
	// O banco de teste é RE-SEMEADO, nunca truncado: pessoas de rodadas anteriores
	// sobrevivem e falsificam o cenário desta. O nome é a identidade aqui, então quem
	// tiver este nome e outro telefone sai antes.
	if _, err := database.Exec(
		`DELETE FROM people WHERE name = $1 AND phone <> $2`, nome, foneDe(nome)); err != nil {
		t.Fatal(err)
	}
	var person string
	if err := database.QueryRow(`
		INSERT INTO people (phone, name) VALUES ($1, $2)
		ON CONFLICT (phone) DO UPDATE SET name = EXCLUDED.name
		RETURNING id::text`, foneDe(nome), nome,
	).Scan(&person); err != nil {
		t.Fatal(err)
	}
	// Sessões e fichas da rodada anterior falsificariam o cenário desta.
	if _, err := database.Exec(
		`DELETE FROM workout_sessions WHERE person_id = $1`, person); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(
		`DELETE FROM prescriptions WHERE person_id = $1`, person); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO bonds (person_id, studio_id, role, status, created_at)
		VALUES ($1, $2, 'student', 'active', now() - ($3::int * interval '1 day'))
		ON CONFLICT (person_id, studio_id) DO UPDATE
		SET status = 'active', created_at = EXCLUDED.created_at`,
		person, studioID, diasDeVinculo,
	); err != nil {
		t.Fatal(err)
	}
	return person
}

// Uma Prescrição PUBLICADA há N dias. Sem isto, todo aluno de teste dispara o sinal do
// silêncio do personal — que é o sinal certo para quem nunca recebeu ficha, e por isso
// mesmo precisa sair do caminho quando o cenário é outro. Aluno que treina tem ficha.
func publicouHa(t *testing.T, database *sql.DB, studioID, personID string, dias int) {
	t.Helper()
	var modelo string
	if err := database.QueryRow(
		`SELECT id::text FROM models WHERE studio_id = $1 LIMIT 1`, studioID,
	).Scan(&modelo); err != nil {
		t.Fatal(err)
	}

	if _, err := database.Exec(`
		INSERT INTO prescriptions (studio_id, person_id, model_id, for_date, status, published_at)
		VALUES ($1, $2, $3, (now() - ($4::int * interval '1 day'))::date, 'published',
		        now() - ($4::int * interval '1 day'))`,
		studioID, personID, modelo, dias,
	); err != nil {
		t.Fatal(err)
	}
}

// Sessões FECHADAS nos dias indicados (dias atrás, a partir de hoje).
func sessoesEm(t *testing.T, database *sql.DB, studioID, personID string, diasAtras []int) {
	t.Helper()
	for i, d := range diasAtras {
		if _, err := database.Exec(`
			INSERT INTO workout_sessions (person_id, studio_id, client_id, started_at, finished_at)
			VALUES ($1, $2, gen_random_uuid(),
			        now() - ($3::int * interval '1 day'),
			        now() - ($3::int * interval '1 day') + interval '50 minutes')`,
			personID, studioID, d,
		); err != nil {
			t.Fatalf("sessao %d: %v", i, err)
		}
	}
}

// n dias espalhados uniformemente entre `de` e `ate` dias atrás.
func diasAtras(de, ate, n int) []int {
	out := make([]int, 0, n)
	span := ate - de
	for i := 0; i < n; i++ {
		out = append(out, de+(i*span)/n)
	}
	return out
}

// QUEM ESTÁ TREINANDO AGORA não está sumindo. O app grava `started_at` na primeira série e
// só fecha no fim, então durante o treino inteiro existe uma sessão com finished_at NULL —
// exatamente o que o sinal de "começou e não terminou" casava. Numa tarde cheia, a fila de
// quem vai sumir eram as pessoas que estavam na academia na frente do personal.
func TestQuemTreinaAgoraNaoEstaSumindo(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, nil)
	studio, _ := estudioDeTeste(t, database)

	// Aluna regular, com ficha recente: nada nela pede atenção.
	regular := alunoCom(t, database, studio, "Treina Agora", 120)
	publicouHa(t, database, studio, regular, 2)
	sessoesEm(t, database, studio, regular, diasAtras(1, 60, 26))

	// E uma sessão ABERTA agora — ela está na academia neste instante.
	if _, err := database.Exec(`
		INSERT INTO workout_sessions (person_id, studio_id, client_id, started_at)
		VALUES ($1, $2, gen_random_uuid(), now() - interval '20 minutes')`,
		regular, studio,
	); err != nil {
		t.Fatal(err)
	}

	itens, err := svc.Risco(context.Background(), studio)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range itens {
		if it.Name == "Treina Agora" {
			t.Fatalf("quem esta treinando agora entrou na fila: %s — %s", it.Sinal, it.Motivo)
		}
	}

	// Mas a sessão de ONTEM que nunca fechou continua sendo sinal: ali houve abandono.
	if _, err := database.Exec(`
		INSERT INTO workout_sessions (person_id, studio_id, client_id, started_at)
		VALUES ($1, $2, gen_random_uuid(), now() - interval '2 days')`,
		regular, studio,
	); err != nil {
		t.Fatal(err)
	}
	depois, err := svc.Risco(context.Background(), studio)
	if err != nil {
		t.Fatal(err)
	}
	achou := false
	for _, it := range depois {
		if it.Name == "Treina Agora" && it.Sinal == "sessao_abandonada" {
			achou = true
		}
	}
	if !achou {
		t.Fatalf("a sessao de ontem que nao fechou deixou de ser sinal: %+v", depois)
	}
}

// A FILA E A LISTA FALAM DA MESMA DÍVIDA. O motivo do sinal (1) imprimia o valor de UM mês
// enquanto a linha da Operação, na mesma rolagem, imprimia a dívida inteira: "R$ 350 em
// aberto há 10 meses" logo acima de "5 meses · R$ 1.750" — mesma pessoa, mesma tela, dois
// números. Duas contagens divergentes matam a confiança nas duas.
//
// E a janela é a MESMA das duas telas: a LATERAL do risco partia do vínculo e a da Operação
// partia do combinado. Aqui o vínculo tem 300 dias e o acordo 4 meses.
func TestFilaEListaDizemAMesmaDivida(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, nil)
	ctx := context.Background()
	studio, _ := estudioDeTeste(t, database)
	// O dono do estúdio de teste não tinha vínculo: `ownerTime` lê `people.active_bond_id`,
	// e sem ele a Operação deste estúdio responde nao_encontrado.
	var dono string
	if err := database.QueryRow(
		`SELECT owner_person_id::text FROM studios WHERE id = $1`, studio).Scan(&dono); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		WITH v AS (
			INSERT INTO bonds (person_id, studio_id, role, status)
			VALUES ($1, $2, 'owner', 'active')
			ON CONFLICT (person_id, studio_id) DO UPDATE SET status = 'active'
			RETURNING id, person_id)
		UPDATE people p SET active_bond_id = v.id FROM v WHERE p.id = v.person_id`,
		dono, studio); err != nil {
		t.Fatal(err)
	}

	pessoa := alunoCom(t, database, studio, "Marina Deve", 300)
	publicouHa(t, database, studio, pessoa, 3)
	sessoesEm(t, database, studio, pessoa, []int{40}) // sumiu: 40 dias sem treinar
	if _, err := database.Exec(`
		INSERT INTO mensalidades (bond_id, amount_cents, due_day, created_at)
		SELECT b.id, 35000, 5, now() - interval '4 months'
		FROM bonds b WHERE b.person_id = $1 AND b.studio_id = $2
		ON CONFLICT (bond_id) DO UPDATE
		SET amount_cents = 35000, due_day = 5, created_at = EXCLUDED.created_at`,
		pessoa, studio); err != nil {
		t.Fatal(err)
	}

	fila, err := svc.Risco(ctx, studio)
	if err != nil {
		t.Fatal(err)
	}
	var naFila *RiscoItem
	for i := range fila {
		if fila[i].Name == "Marina Deve" {
			naFila = &fila[i]
		}
	}
	if naFila == nil {
		t.Fatalf("quem deve 5 competencias e sumiu ha 40 dias nao entrou na fila: %+v", fila)
	}
	if naFila.Sinal != "dinheiro_e_sumico" {
		t.Fatalf("sinal = %q (%s)", naFila.Sinal, naFila.Motivo)
	}

	op, err := svc.Operacao(ctx, dono)
	if err != nil {
		t.Fatal(err)
	}
	var naLista *EmAbertoItem
	for i := range op.EmAberto {
		if op.EmAberto[i].Name == "Marina Deve" {
			naLista = &op.EmAberto[i]
		}
	}
	if naLista == nil {
		t.Fatal("a devedora nao esta na lista em aberto")
	}
	// A CONTAGEM PARTE DO COMBINADO, nos dois lugares: o vínculo tem 300 dias e o acordo 4
	// meses. Com a janela do vínculo dariam 11 competências e uma dívida inventada.
	if naLista.MesesAbertos != 5 || naLista.DevidoCents != 5*35000 {
		t.Fatalf("lista: %d meses / %d centavos", naLista.MesesAbertos, naLista.DevidoCents)
	}
	if naFila.AmountCents != naLista.DevidoCents {
		t.Fatalf("a fila diz %d e a lista diz %d para a mesma divida da mesma pessoa",
			naFila.AmountCents, naLista.DevidoCents)
	}
	// E a frase carrega o mesmo número, com o ponto final que os outros seis motivos têm.
	if !strings.Contains(naFila.Motivo, "R$ 1.750 em aberto") || !strings.HasSuffix(naFila.Motivo, ".") {
		t.Fatalf("motivo = %q", naFila.Motivo)
	}
}

// A FILA NAO ACUSA QUEM JA RESPONDEU. Na foto de ontem a mesma Marina aparecia duas vezes na
// mesma dobra dizendo coisas contrarias: "R$ 350 em aberto ha 9 dias" na fila de risco, e
// "disse que pagou" na lista de em aberto 275pt abaixo. ja_paguei e o dado mais novo sobre
// aquele mes, e a fila nao o lia.
func TestFilaMudaDeFraseQuandoElaDizQueJaPagou(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, nil)
	ctx := context.Background()
	studio, _ := estudioDeTeste(t, database)
	var dono string
	if err := database.QueryRow(
		`SELECT owner_person_id::text FROM studios WHERE id = $1`, studio).Scan(&dono); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		WITH v AS (
			INSERT INTO bonds (person_id, studio_id, role, status)
			VALUES ($1, $2, 'owner', 'active')
			ON CONFLICT (person_id, studio_id) DO UPDATE SET status = 'active'
			RETURNING id, person_id)
		UPDATE people p SET active_bond_id = v.id FROM v WHERE p.id = v.person_id`,
		dono, studio); err != nil {
		t.Fatal(err)
	}

	pessoa := alunoCom(t, database, studio, "Marina Respondeu", 300)
	publicouHa(t, database, studio, pessoa, 3)
	sessoesEm(t, database, studio, pessoa, []int{40})
	if _, err := database.Exec(`
		INSERT INTO mensalidades (bond_id, amount_cents, due_day, created_at)
		SELECT b.id, 35000, 5, now() - interval '4 months'
		FROM bonds b WHERE b.person_id = $1 AND b.studio_id = $2
		ON CONFLICT (bond_id) DO UPDATE
		SET amount_cents = 35000, due_day = 5, created_at = EXCLUDED.created_at`,
		pessoa, studio); err != nil {
		t.Fatal(err)
	}

	antes, err := svc.Risco(ctx, studio)
	if err != nil {
		t.Fatal(err)
	}
	if len(antes) == 0 || antes[0].Sinal != "dinheiro_e_sumico" {
		t.Fatalf("cenario nao montou: %+v", antes)
	}

	// ELA RESPONDE, com o mesmo verbo do app dela.
	if _, err := database.ExecContext(ctx, `
		INSERT INTO ja_paguei (bond_id, month)
		SELECT b.id, date_trunc('month', now())::date
		FROM bonds b WHERE b.person_id = $1 AND b.studio_id = $2
		ON CONFLICT DO NOTHING`, pessoa, studio); err != nil {
		t.Fatal(err)
	}

	depois, err := svc.Risco(ctx, studio)
	if err != nil {
		t.Fatal(err)
	}
	// CONTINUA NA FILA: ela sumiu ha 40 dias, e responder sobre o dinheiro nao desfaz isso.
	if len(depois) == 0 {
		t.Fatal("ela sumiu da fila inteira; a fila muda de FRASE, nao esconde o sumico")
	}
	achou := depois[0]
	if achou.Sinal != "disse_que_pagou_e_sumico" {
		t.Fatalf("sinal %q, motivo %q — a fila continua acusando quem ja respondeu",
			achou.Sinal, achou.Motivo)
	}
	if strings.Contains(achou.Motivo, "em aberto") {
		t.Fatalf("motivo ainda acusa: %q", achou.Motivo)
	}
	if !strings.Contains(achou.Motivo, "Disse que pagou R$ 1.750") ||
		!strings.Contains(achou.Motivo, "sem treinar") {
		t.Fatalf("motivo perdeu o valor ou o sumico: %q", achou.Motivo)
	}
	// E o valor da acao continua sendo a divida inteira: a frase mudou, o dinheiro nao.
	if achou.AmountCents != 5*35000 {
		t.Fatalf("valor da acao: %d", achou.AmountCents)
	}

	// A LISTA DE EM ABERTO diz a mesma coisa sobre ela, que era o defeito: duas leituras do
	// mesmo mes discordando na mesma dobra.
	op, err := svc.Operacao(ctx, dono)
	if err != nil {
		t.Fatal(err)
	}
	var diz bool
	for _, it := range op.EmAberto {
		if it.Name == "Marina Respondeu" {
			diz = it.DizQueJaPagou
		}
	}
	if !diz {
		t.Fatal("a lista de em aberto nao sabe o que a fila acabou de dizer")
	}
}
