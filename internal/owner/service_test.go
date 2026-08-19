package owner

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Vitorepf/linkgym-api/internal/config"
	"github.com/Vitorepf/linkgym-api/internal/db"
	"github.com/Vitorepf/linkgym-api/internal/migrate"
	"github.com/Vitorepf/linkgym-api/internal/seed"
	"github.com/Vitorepf/linkgym-api/internal/workout"
)

func TestMain(m *testing.M) {
	_, file, _, ok := runtime.Caller(0)
	if ok {
		dir := filepath.Dir(file)
		for i := 0; i < 5; i++ {
			candidate := filepath.Join(dir, ".env")
			if _, err := os.Stat(candidate); err == nil {
				config.LoadDotEnv(candidate)
				break
			}
			dir = filepath.Dir(dir)
		}
	}
	os.Exit(m.Run())
}

func openSeeded(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	database, err := db.Open(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := migrate.Up(database); err != nil {
		t.Fatal(err)
	}
	if err := seed.Dev(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	return database
}

func personIDByPhone(t *testing.T, database *sql.DB, phone string) string {
	t.Helper()
	var id string
	if err := database.QueryRow(`SELECT id FROM people WHERE phone = $1`, phone).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestOwnerHomeListsStoppedStudent(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, time.Now)
	fredID := personIDByPhone(t, database, "+5511900000001")
	got, err := svc.Home(context.Background(), fredID)
	if err != nil {
		t.Fatal(err)
	}
	if got.StudentCount != 28 {
		t.Fatalf("students %d", got.StudentCount)
	}
	if got.Greeting != "Bom dia, Fred" {
		t.Fatalf("greeting %q", got.Greeting)
	}
	if len(got.Attention) != 3 || got.Attention[0].Name != "Jose" {
		t.Fatalf("attention %+v", got.Attention)
	}
	if got.Attention[0].Decision != "Mandar a retomada de 9 minutos" {
		t.Fatalf("decision %q", got.Attention[0].Decision)
	}
	if got.Fio.Prescribed != 3 {
		t.Fatalf("fio prescribed %d", got.Fio.Prescribed)
	}
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func finishVitorSession(t *testing.T, database *sql.DB) string {
	t.Helper()
	ctx := context.Background()
	vitorID := personIDByPhone(t, database, seed.PhoneVitor)
	if _, err := database.Exec(`DELETE FROM personal_records WHERE person_id = $1`, vitorID); err != nil {
		t.Fatal(err)
	}
	var prescriptionID, itemID, exerciseID string
	if err := database.QueryRow(`
		SELECT pr.id::text, pi.id::text, pi.exercise_id::text
		FROM prescriptions pr
		JOIN prescription_items pi ON pi.prescription_id = pr.id AND pi.position = 1
		WHERE pr.person_id = $1 AND pr.for_date = current_date AND pr.status = 'published'`,
		vitorID,
	).Scan(&prescriptionID, &itemID, &exerciseID); err != nil {
		t.Fatal(err)
	}
	wk := workout.New(database, time.Now)
	started, err := wk.Start(ctx, vitorID, newUUID(), prescriptionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wk.AddSet(ctx, vitorID, started.ID, workout.SetInput{
		LocalID:            newUUID(),
		PrescriptionItemID: itemID,
		ExerciseID:         exerciseID,
		SetIndex:           1,
		Reps:               10,
		LoadKg:             40,
		RestSeconds:        90,
		PerformedAt:        time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := wk.Finish(ctx, vitorID, started.ID, 2); err != nil {
		t.Fatal(err)
	}
	var alertID string
	if err := database.QueryRow(`
		SELECT id::text FROM session_alerts
		WHERE session_id = $1 AND kind = 'session_synced'`,
		started.ID,
	).Scan(&alertID); err != nil {
		t.Fatal(err)
	}
	return alertID
}

func TestOwnerReturnsListsUnreadSessionSynced(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, time.Now)
	alertID := finishVitorSession(t, database)
	fredID := personIDByPhone(t, database, seed.PhoneFred)
	vitorID := personIDByPhone(t, database, seed.PhoneVitor)

	got, err := svc.Returns(context.Background(), fredID)
	if err != nil {
		t.Fatal(err)
	}
	var hit *ReturnItem
	for i := range got {
		if got[i].AlertID == alertID {
			hit = &got[i]
			break
		}
	}
	if hit == nil {
		t.Fatalf("missing alert %s in %+v", alertID, got)
	}
	if hit.PersonID != vitorID || hit.Name != "Vitor" || hit.Effort != 2 {
		t.Fatalf("%+v", hit)
	}
	if len(hit.Records) != 1 || hit.Records[0].ExerciseName != "Supino" {
		t.Fatalf("records %+v", hit.Records)
	}

	home, err := svc.Home(context.Background(), fredID)
	if err != nil {
		t.Fatal(err)
	}
	if home.UnreadReturns < 1 {
		t.Fatalf("unread_returns %d", home.UnreadReturns)
	}
}

func TestOwnerApplyBumpsTomorrowDraft(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, time.Now)
	alertID := finishVitorSession(t, database)
	fredID := personIDByPhone(t, database, seed.PhoneFred)
	vitorID := personIDByPhone(t, database, seed.PhoneVitor)

	if _, err := database.Exec(`
		DELETE FROM prescriptions
		WHERE person_id = $1 AND for_date = current_date + 1 AND status = 'draft'`,
		vitorID,
	); err != nil {
		t.Fatal(err)
	}

	if err := svc.ApplyReturn(context.Background(), fredID, alertID, 2.5); err != nil {
		t.Fatal(err)
	}

	var load float64
	var source string
	if err := database.QueryRow(`
		SELECT pi.load_kg, pi.load_source
		FROM prescriptions pr
		JOIN prescription_items pi ON pi.prescription_id = pr.id AND pi.position = 1
		WHERE pr.person_id = $1 AND pr.for_date = current_date + 1 AND pr.status = 'draft'`,
		vitorID,
	).Scan(&load, &source); err != nil {
		t.Fatal(err)
	}
	if load != 42.5 {
		t.Fatalf("load %v want 42.5", load)
	}
	if source != "manual" {
		t.Fatalf("source %s", source)
	}

	var readAt sql.NullTime
	if err := database.QueryRow(`SELECT read_at FROM session_alerts WHERE id = $1`, alertID).Scan(&readAt); err != nil {
		t.Fatal(err)
	}
	if !readAt.Valid {
		t.Fatal("read_at empty")
	}

	got, err := svc.Returns(context.Background(), fredID)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range got {
		if it.AlertID == alertID {
			t.Fatal("applied alert still listed")
		}
	}
}

func TestOwnerApplyMissingAlertNotFound(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, time.Now)
	fredID := personIDByPhone(t, database, seed.PhoneFred)
	err := svc.ApplyReturn(context.Background(), fredID, newUUID(), 0)
	if err != ErrNotFound {
		t.Fatalf("got %v want %v", err, ErrNotFound)
	}
}

func TestOwnerApplyBumpsExistingTomorrowDraft(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, time.Now)
	fredID := personIDByPhone(t, database, seed.PhoneFred)
	vitorID := personIDByPhone(t, database, seed.PhoneVitor)

	var todayID string
	if err := database.QueryRow(`
		SELECT id::text FROM prescriptions
		WHERE person_id = $1 AND for_date = current_date AND status = 'published'`,
		vitorID,
	).Scan(&todayID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		DELETE FROM prescriptions
		WHERE person_id = $1 AND for_date = current_date + 1 AND status = 'draft'`,
		vitorID,
	); err != nil {
		t.Fatal(err)
	}
	var draftID string
	if err := database.QueryRow(`
		INSERT INTO prescriptions (studio_id, person_id, model_id, for_date, status)
		SELECT studio_id, person_id, model_id, current_date + 1, 'draft'
		FROM prescriptions WHERE id = $1
		RETURNING id::text`,
		todayID,
	).Scan(&draftID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO prescription_items (
			prescription_id, exercise_id, position, planned_sets, planned_reps, load_kg, load_source
		)
		SELECT $1, exercise_id, position, planned_sets, planned_reps, 30, 'history'
		FROM prescription_items WHERE prescription_id = $2`,
		draftID, todayID,
	); err != nil {
		t.Fatal(err)
	}

	alertID := finishVitorSession(t, database)
	if err := svc.ApplyReturn(context.Background(), fredID, alertID, 2.5); err != nil {
		t.Fatal(err)
	}

	var load float64
	var n int
	if err := database.QueryRow(`
		SELECT pi.load_kg, (SELECT count(*) FROM prescriptions p2
			WHERE p2.person_id = $1 AND p2.for_date = current_date + 1 AND p2.status = 'draft')
		FROM prescription_items pi
		WHERE pi.prescription_id = $2 AND pi.position = 1`,
		vitorID, draftID,
	).Scan(&load, &n); err != nil {
		t.Fatal(err)
	}
	if load != 32.5 {
		t.Fatalf("load %v want 32.5", load)
	}
	if n != 1 {
		t.Fatalf("drafts %d want 1", n)
	}
}

func restoreJoseAttention(t *testing.T, database *sql.DB) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = database.Exec(`
			INSERT INTO attention_items (studio_id, person_id, for_date, reason, rank)
			SELECT s.id, p.id, current_date, 'student_stopped', 1
			FROM people p
			JOIN people owner ON owner.phone = $1
			JOIN studios s ON s.owner_person_id = owner.id
			WHERE p.phone = $2
			ON CONFLICT (studio_id, person_id, for_date) DO UPDATE
			SET reason = EXCLUDED.reason, rank = EXCLUDED.rank`,
			seed.PhoneFred, seed.PhoneJose,
		)
		_, _ = database.Exec(`
			DELETE FROM comebacks
			WHERE bond_id = (SELECT active_bond_id FROM people WHERE phone = $1)`,
			seed.PhoneJose,
		)
	})
}

func TestApplyStoppedDeletesAttentionAndOpensComeback(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, time.Now)
	restoreJoseAttention(t, database)
	fredID := personIDByPhone(t, database, seed.PhoneFred)
	joseID := personIDByPhone(t, database, seed.PhoneJose)

	home, err := svc.Home(context.Background(), fredID)
	if err != nil {
		t.Fatal(err)
	}
	if len(home.Attention) != 3 || home.Attention[0].Name != "Jose" {
		t.Fatalf("seed attention %+v", home.Attention)
	}
	id := home.Attention[0].ID

	if err := svc.Apply(context.Background(), fredID, id); err != nil {
		t.Fatal(err)
	}

	after, err := svc.Home(context.Background(), fredID)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range after.Attention {
		if it.PersonID == joseID {
			t.Fatalf("attention still there %+v", after.Attention)
		}
	}

	var n int
	if err := database.QueryRow(`
		SELECT count(*) FROM attention_items
		WHERE person_id = $1 AND for_date = current_date`,
		joseID,
	).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("attention rows %d", n)
	}

	var comebacks int
	if err := database.QueryRow(`
		SELECT count(*) FROM comebacks
		WHERE bond_id = (SELECT active_bond_id FROM people WHERE id = $1)
		  AND completed_at IS NULL`,
		joseID,
	).Scan(&comebacks); err != nil {
		t.Fatal(err)
	}
	if comebacks != 1 {
		t.Fatalf("comebacks %d", comebacks)
	}
}

// O bump entra em UM item. A copia carimbava a linha inteira com 'manual' e fazia a
// Prescricao de amanha declarar decisao do personal onde o numero veio do corpo ou do
// chute do Modelo. A invariante e comparada contra a origem, nao contra o seed.
func TestOwnerApplyKeepsSourceOfItemsWithoutBump(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, time.Now)
	alertID := finishVitorSession(t, database)
	fredID := personIDByPhone(t, database, seed.PhoneFred)
	vitorID := personIDByPhone(t, database, seed.PhoneVitor)

	origem := loadSources(t, database, `
		SELECT pi.position, pi.load_source, pi.load_kg
		FROM prescriptions pr
		JOIN prescription_items pi ON pi.prescription_id = pr.id
		WHERE pr.person_id = $1 AND pr.for_date = current_date AND pr.status = 'published'`,
		vitorID)
	if len(origem) < 2 {
		t.Fatalf("origem com %d itens, precisa de mais de um", len(origem))
	}
	if origem[2].source == "manual" {
		t.Fatal("origem ja e manual na posicao 2; o teste nao mede nada")
	}

	if _, err := database.Exec(`
		DELETE FROM prescriptions
		WHERE person_id = $1 AND for_date = current_date + 1 AND status = 'draft'`,
		vitorID,
	); err != nil {
		t.Fatal(err)
	}
	if err := svc.ApplyReturn(context.Background(), fredID, alertID, 2.5); err != nil {
		t.Fatal(err)
	}

	copia := loadSources(t, database, `
		SELECT pi.position, pi.load_source, pi.load_kg
		FROM prescriptions pr
		JOIN prescription_items pi ON pi.prescription_id = pr.id
		WHERE pr.person_id = $1 AND pr.for_date = current_date + 1 AND pr.status = 'draft'`,
		vitorID)
	if len(copia) != len(origem) {
		t.Fatalf("copia com %d itens, origem com %d", len(copia), len(origem))
	}
	for position, was := range origem {
		got := copia[position]
		if position == 1 {
			if got.source != "manual" || got.load != was.load+2.5 {
				t.Fatalf("position 1: %s %v want manual %v", got.source, got.load, was.load+2.5)
			}
			continue
		}
		if got.source != was.source || got.load != was.load {
			t.Fatalf("position %d: %s %v want %s %v", position, got.source, got.load, was.source, was.load)
		}
	}
}

type itemSource struct {
	source string
	load   float64
}

func loadSources(t *testing.T, database *sql.DB, query, personID string) map[int]itemSource {
	t.Helper()
	rows, err := database.Query(query, personID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[int]itemSource{}
	for rows.Next() {
		var position int
		var it itemSource
		if err := rows.Scan(&position, &it.source, &it.load); err != nil {
			t.Fatal(err)
		}
		out[position] = it
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// A troca e da Sessao daquele dia, e o personal le a Sessao no Retorno. Sem isto o aviso
// era gravado e nunca lido por ninguem.
func TestOwnerReturnsCarriesSwap(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, time.Now)
	ctx := context.Background()
	fredID := personIDByPhone(t, database, seed.PhoneFred)
	vitorID := personIDByPhone(t, database, seed.PhoneVitor)

	var prescriptionID, itemID, exerciseID, studioID string
	if err := database.QueryRow(`
		SELECT pr.id::text, pi.id::text, pi.exercise_id::text, pr.studio_id::text
		FROM prescriptions pr
		JOIN prescription_items pi ON pi.prescription_id = pr.id AND pi.position = 1
		WHERE pr.person_id = $1 AND pr.for_date = current_date AND pr.status = 'published'`,
		vitorID,
	).Scan(&prescriptionID, &itemID, &exerciseID, &studioID); err != nil {
		t.Fatal(err)
	}
	var remadaID string
	if err := database.QueryRow(
		`SELECT id::text FROM exercises WHERE studio_id = $1 AND name = 'Remada'`,
		studioID,
	).Scan(&remadaID); err != nil {
		t.Fatal(err)
	}

	wk := workout.New(database, time.Now)
	started, err := wk.Start(ctx, vitorID, newUUID(), prescriptionID)
	if err != nil {
		t.Fatal(err)
	}
	if err := wk.Swap(ctx, vitorID, started.ID, exerciseID, remadaID); err != nil {
		t.Fatal(err)
	}
	if _, err := wk.AddSet(ctx, vitorID, started.ID, workout.SetInput{
		LocalID:               newUUID(),
		PrescriptionItemID:    itemID,
		ExerciseID:            remadaID,
		SwappedFromExerciseID: &exerciseID,
		SetIndex:              1,
		Reps:                  10,
		LoadKg:                30,
		RestSeconds:           90,
		PerformedAt:           time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := wk.Finish(ctx, vitorID, started.ID, 2); err != nil {
		t.Fatal(err)
	}

	var alertID string
	if err := database.QueryRow(`
		SELECT id::text FROM session_alerts
		WHERE session_id = $1 AND kind = 'session_synced'`,
		started.ID,
	).Scan(&alertID); err != nil {
		t.Fatal(err)
	}

	got, err := svc.Returns(ctx, fredID)
	if err != nil {
		t.Fatal(err)
	}
	var hit *ReturnItem
	for i := range got {
		if got[i].AlertID == alertID {
			hit = &got[i]
			break
		}
	}
	if hit == nil {
		t.Fatalf("missing alert %s in %+v", alertID, got)
	}
	if len(hit.Swaps) != 1 {
		t.Fatalf("swaps %+v", hit.Swaps)
	}
	if hit.Swaps[0].From != "Supino" || hit.Swaps[0].To != "Remada" {
		t.Fatalf("swap %+v", hit.Swaps[0])
	}

	if err := svc.ApplyReturn(ctx, fredID, alertID, 0); err != nil {
		t.Fatal(err)
	}
	var readAt sql.NullTime
	if err := database.QueryRow(`
		SELECT read_at FROM session_alerts
		WHERE session_id = $1 AND kind = 'exercise_swap'`,
		started.ID,
	).Scan(&readAt); err != nil {
		t.Fatal(err)
	}
	if !readAt.Valid {
		t.Fatal("aviso de troca lido e nao marcado")
	}
}
