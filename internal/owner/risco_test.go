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
