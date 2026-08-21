package owner

import (
	"context"
	"database/sql"
	"testing"

	"github.com/Vitorepf/linkgym-api/internal/seed"
)

// O REGISTRO DO QUE ELE FEZ, e o placar que sai dele. É a única estatística agregada do
// produto, e ela é sobre o comportamento DELE — não uma classificação da turma.
func TestToqueEPlacar(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, nil)
	ctx := context.Background()
	fred := personIDByPhone(t, database, seed.PhoneFred)
	studio := studioDoFred(t, database, fred)

	if _, err := database.Exec(`DELETE FROM toques WHERE studio_id = $1`, studio); err != nil {
		t.Fatal(err)
	}
	op, err := svc.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	if op.Placar.Tocados != 0 {
		t.Fatalf("placar nasce em %d", op.Placar.Tocados)
	}
	if len(op.Risco) == 0 {
		t.Fatal("preciso de alguem na fila")
	}
	alvo := op.Risco[0]

	if err := svc.RegistrarToque(ctx, fred, alvo.PersonID, alvo.Sinal); err != nil {
		t.Fatal(err)
	}
	comToque, err := svc.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	if comToque.Placar.Tocados != 1 || comToque.Placar.Voltaram != 0 {
		t.Fatalf("placar %+v", comToque.Placar)
	}

	// Ela VOLTOU: uma sessão fechada DEPOIS do toque. É o que fecha o laço e permite, um dia,
	// perguntar se a fila acerta. A sessão sai no fim: ela muda os dias parados de quem está
	// no cenário fixo dos testes vizinhos.
	var sessao string
	if err := database.QueryRow(`
		INSERT INTO workout_sessions (person_id, studio_id, client_id, started_at, finished_at)
		VALUES ($1, $2, gen_random_uuid(), now(), now() + interval '1 minute')
		RETURNING id::text`,
		alvo.PersonID, studio,
	).Scan(&sessao); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = database.Exec(`DELETE FROM workout_sessions WHERE id = $1`, sessao)
	})
	voltou, err := svc.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	if voltou.Placar.Voltaram != 1 {
		t.Fatalf("ela voltou e o placar nao viu: %+v", voltou.Placar)
	}

	// Tocar duas vezes na mesma pessoa conta UMA: o placar é de pessoas, não de mensagens.
	if err := svc.RegistrarToque(ctx, fred, alvo.PersonID, "sumico"); err != nil {
		t.Fatal(err)
	}
	dedup, err := svc.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	if dedup.Placar.Tocados != 1 {
		t.Fatalf("tocou duas vezes e contou %d", dedup.Placar.Tocados)
	}

	// Não vaza entre estúdios, e o motivo é obrigatório.
	if err := svc.RegistrarToque(ctx, fred, alvo.PersonID, ""); err != ErrInvalid {
		t.Fatalf("motivo vazio aceito: %v", err)
	}
	var deOutro string
	if err := database.QueryRow(
		`SELECT p.id::text FROM people p WHERE p.phone = '+5511999990002'`).Scan(&deOutro); err == nil {
		if err := svc.RegistrarToque(ctx, fred, deOutro, "sumico"); err != ErrNotFound {
			t.Fatalf("tocou em pessoa de outro estudio: %v", err)
		}
	}
	if _, err := database.Exec(`DELETE FROM toques WHERE studio_id = $1`, studio); err != nil {
		t.Fatal(err)
	}
}

func studioDoFred(t *testing.T, database *sql.DB, ownerID string) string {
	t.Helper()
	var id string
	if err := database.QueryRow(
		`SELECT id::text FROM studios WHERE owner_person_id = $1`, ownerID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
