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
	if got.StudentCount != 3 {
		t.Fatalf("students %d", got.StudentCount)
	}
	if got.Greeting != "Bom dia, Fred" {
		t.Fatalf("greeting %q", got.Greeting)
	}
	if len(got.Attention) != 1 || got.Attention[0].Name != "Jose" {
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
		ClientSetID:        newUUID(),
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
