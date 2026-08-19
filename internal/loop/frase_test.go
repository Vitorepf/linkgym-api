package loop

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Vitorepf/linkgym-api/internal/publish"
	"github.com/Vitorepf/linkgym-api/internal/seed"
	"github.com/Vitorepf/linkgym-api/internal/today"
)

// A FRASE DO PERSONAL, do teclado dele ate o olho do aluno.
//
// O defeito que estes testes prendem: /v1/today montava a coach_line por template
// ("<exercicio> em <carga>. Tecnica, nao ego.") e a tela do aluno a desenhava embaixo do
// rosto e do nome do personal. O app assinava, em nome de uma pessoa real, uma frase que
// ela nao escreveu. Agora ou a frase e dele, literal, ou nao existe frase.

func modeloTreinoA(t *testing.T, database *sql.DB) string {
	t.Helper()
	var id string
	if err := database.QueryRow(`
		SELECT m.id::text FROM models m
		JOIN studios s ON s.id = m.studio_id
		JOIN people dono ON dono.id = s.owner_person_id
		WHERE dono.phone = $1 AND m.name = 'Treino A'`,
		seed.PhoneFred,
	).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// O rascunho nasce sempre para amanha (DraftFromLast), entao o relogio do /today anda um
// dia para a frente para ver o que foi publicado. Sobra do dia seguinte de uma rodada
// anterior sai da frente antes de comecar.
func rascunhoDeAmanha(t *testing.T, database *sql.DB, pessoas ...string) (hoje time.Time, amanha time.Time, draftID string) {
	t.Helper()
	hoje = pgToday(t, database)
	amanha = hoje.AddDate(0, 0, 1)
	for _, id := range pessoas {
		restoreVitorOfensiva(t, database, id)
		if _, err := database.Exec(`
			DELETE FROM prescriptions
			WHERE person_id = $1 AND for_date = $2::date`,
			id, amanha.Format("2006-01-02"),
		); err != nil {
			t.Fatal(err)
		}
	}
	svc := publish.New(database, func() time.Time { return hoje })
	draft, err := svc.DraftFromLast(context.Background(),
		personIDByPhone(t, database, seed.PhoneFred),
		modeloTreinoA(t, database), pessoas[0], "last")
	if err != nil {
		t.Fatal(err)
	}
	return hoje, amanha, draft.DraftID
}

func TestFraseDoPersonalChegaComoEleEscreveu(t *testing.T) {
	database := openSeeded(t)
	ctx := context.Background()
	fredID := personIDByPhone(t, database, seed.PhoneFred)
	vitorID := personIDByPhone(t, database, seed.PhoneVitor)
	huanID := personIDByPhone(t, database, seed.PhoneHuan)
	hoje, amanha, draftID := rascunhoDeAmanha(t, database, vitorID, huanID)

	const frase = "Hoje é técnica. Deixa o peso esperar."
	svc := publish.New(database, func() time.Time { return hoje })
	if err := svc.Publish(ctx, fredID, draftID, []string{huanID}, "  "+frase+"  "); err != nil {
		t.Fatal(err)
	}

	dia := today.New(database, func() time.Time { return amanha })
	got, err := dia.Today(ctx, vitorID)
	if err != nil {
		t.Fatal(err)
	}
	if got.CoachLine != frase {
		t.Fatalf("coach_line %q want %q", got.CoachLine, frase)
	}
	// O lote leva o MESMO recado: uma frase por publicacao, e publicar em lote e um ato so.
	// A carga de cada corpo continua sendo a dele — isso outro teste ja prende.
	outro, err := dia.Today(ctx, huanID)
	if err != nil {
		t.Fatal(err)
	}
	if outro.CoachLine != frase {
		t.Fatalf("coach_line do lote %q want %q", outro.CoachLine, frase)
	}
}

func TestSemFraseOAppNaoAssinaNoLugarDele(t *testing.T) {
	database := openSeeded(t)
	ctx := context.Background()
	fredID := personIDByPhone(t, database, seed.PhoneFred)
	vitorID := personIDByPhone(t, database, seed.PhoneVitor)
	hoje, amanha, draftID := rascunhoDeAmanha(t, database, vitorID)

	svc := publish.New(database, func() time.Time { return hoje })
	if err := svc.Publish(ctx, fredID, draftID, nil, ""); err != nil {
		t.Fatal(err)
	}

	got, err := today.New(database, func() time.Time { return amanha }).Today(ctx, vitorID)
	if err != nil {
		t.Fatal(err)
	}
	if got.CoachLine != "" {
		t.Fatalf("coach_line %q: sem frase escrita, ninguem fala pelo personal", got.CoachLine)
	}
	// O silencio e SO da frase: a Prescricao do dia chega inteira. Se este campo sumisse
	// junto, o conserto teria custado o produto.
	if got.Prescription == nil {
		t.Fatal("prescricao ausente: o dia tem que chegar mesmo sem frase")
	}

	var gravada sql.NullString
	if err := database.QueryRow(`SELECT coach_line FROM prescriptions WHERE id = $1`, draftID).Scan(&gravada); err != nil {
		t.Fatal(err)
	}
	if gravada.Valid {
		t.Fatalf("coach_line gravada como %q: ausencia se escreve NULL", gravada.String)
	}
}

func TestFraseComLinkOuTelefoneRecusada(t *testing.T) {
	database := openSeeded(t)
	ctx := context.Background()
	fredID := personIDByPhone(t, database, seed.PhoneFred)
	vitorID := personIDByPhone(t, database, seed.PhoneVitor)
	hoje, _, draftID := rascunhoDeAmanha(t, database, vitorID)
	svc := publish.New(database, func() time.Time { return hoje })

	// A MESMA regra de boas_vindas e retomada, pelo MESMO caminho (internal/voz): o slot
	// que existe para soar como pessoa nao vira canal de venda.
	for _, frase := range []string{
		"Bora! www.meusite.com",
		"Chama em https://exemplo.test/vendas",
		"Me liga: +5511999998888",
		"Whats 11999998888",
		"Fecha o pacote no wa.me/5511999998888",
	} {
		err := svc.Publish(ctx, fredID, draftID, nil, frase)
		if !errors.Is(err, publish.ErrInvalid) {
			t.Fatalf("frase %q: err %v want ErrInvalid", frase, err)
		}
	}

	// E nada foi ao ar no caminho: a recusa acontece antes de qualquer escrita.
	var status string
	if err := database.QueryRow(`SELECT status FROM prescriptions WHERE id = $1`, draftID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "draft" {
		t.Fatalf("status %q: frase recusada nao pode publicar a ficha", status)
	}
}

func TestFraseAcimaDoLimiteRecusadaENoLimitePassa(t *testing.T) {
	database := openSeeded(t)
	ctx := context.Background()
	fredID := personIDByPhone(t, database, seed.PhoneFred)
	vitorID := personIDByPhone(t, database, seed.PhoneVitor)
	hoje, amanha, draftID := rascunhoDeAmanha(t, database, vitorID)
	svc := publish.New(database, func() time.Time { return hoje })

	// Conta RUNA, nao byte: a frase e em portugues, e "ã" custa dois bytes.
	demais := strings.Repeat("ã", publish.LimiteDaFrase+1)
	if err := svc.Publish(ctx, fredID, draftID, nil, demais); !errors.Is(err, publish.ErrInvalid) {
		t.Fatalf("%d runas: err %v want ErrInvalid", publish.LimiteDaFrase+1, err)
	}

	cabe := strings.Repeat("ã", publish.LimiteDaFrase)
	if err := svc.Publish(ctx, fredID, draftID, nil, cabe); err != nil {
		t.Fatalf("%d runas: %v", publish.LimiteDaFrase, err)
	}
	got, err := today.New(database, func() time.Time { return amanha }).Today(ctx, vitorID)
	if err != nil {
		t.Fatal(err)
	}
	if got.CoachLine != cabe {
		t.Fatalf("coach_line com %d runas nao voltou inteira", publish.LimiteDaFrase)
	}
}
