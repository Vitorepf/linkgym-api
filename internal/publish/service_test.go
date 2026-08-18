package publish

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Vitorepf/linkgym-api/internal/config"
	"github.com/Vitorepf/linkgym-api/internal/db"
	"github.com/Vitorepf/linkgym-api/internal/migrate"
	"github.com/Vitorepf/linkgym-api/internal/seed"
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

func treinoA(t *testing.T, database *sql.DB) string {
	t.Helper()
	var id string
	if err := database.QueryRow(`
		SELECT m.id::text FROM models m
		JOIN studios s ON s.id = m.studio_id
		JOIN people owner ON owner.id = s.owner_person_id
		WHERE owner.phone = $1 AND m.name = 'Treino A'`,
		seed.PhoneFred,
	).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func clockOnDBDay(t *testing.T, database *sql.DB) (time.Time, string) {
	t.Helper()
	var day time.Time
	if err := database.QueryRow(`SELECT current_date`).Scan(&day); err != nil {
		t.Fatal(err)
	}
	now := time.Date(day.Year(), day.Month(), day.Day(), 12, 0, 0, 0, time.UTC)
	tomorrow := now.AddDate(0, 0, 1).Format("2006-01-02")
	return now, tomorrow
}

func clearTomorrow(t *testing.T, database *sql.DB, personIDs []string, tomorrow string) {
	t.Helper()
	for _, id := range personIDs {
		if _, err := database.Exec(`
			DELETE FROM prescriptions
			WHERE person_id = $1 AND for_date = $2::date`,
			id, tomorrow,
		); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, id := range personIDs {
			_, _ = database.Exec(`
				DELETE FROM prescriptions
				WHERE person_id = $1 AND for_date = $2::date`,
				id, tomorrow,
			)
		}
	})
}

func firstLoad(t *testing.T, database *sql.DB, personID, forDate, status string) float64 {
	t.Helper()
	var load float64
	if err := database.QueryRow(`
		SELECT pi.load_kg
		FROM prescriptions pr
		JOIN prescription_items pi ON pi.prescription_id = pr.id AND pi.position = 1
		WHERE pr.person_id = $1 AND pr.for_date = $2::date AND pr.status = $3`,
		personID, forDate, status,
	).Scan(&load); err != nil {
		t.Fatal(err)
	}
	return load
}

func TestPublishLoteNeverCopiesLoad(t *testing.T) {
	database := openSeeded(t)
	now, tomorrow := clockOnDBDay(t, database)
	svc := New(database, func() time.Time { return now })
	fredID := personIDByPhone(t, database, seed.PhoneFred)
	vitorID := personIDByPhone(t, database, seed.PhoneVitor)
	huanID := personIDByPhone(t, database, seed.PhoneHuan)
	modelID := treinoA(t, database)
	clearTomorrow(t, database, []string{vitorID, huanID}, tomorrow)

	draft, err := svc.DraftFromLast(context.Background(), fredID, modelID, vitorID, "last")
	if err != nil {
		t.Fatal(err)
	}
	if draft.DraftID == "" || len(draft.Items) == 0 {
		t.Fatalf("draft %+v", draft)
	}
	if draft.Items[0].LoadKg != 40 {
		t.Fatalf("vitor draft %v want 40", draft.Items[0].LoadKg)
	}

	if err := svc.Publish(context.Background(), fredID, draft.DraftID, []string{huanID}); err != nil {
		t.Fatal(err)
	}

	if v := firstLoad(t, database, vitorID, tomorrow, "published"); v != 40 {
		t.Fatalf("vitor %v want 40", v)
	}
	if h := firstLoad(t, database, huanID, tomorrow, "published"); h != 22.5 {
		t.Fatalf("huan %v want 22.5 (never 40)", h)
	}
}

func TestManualLoadSource(t *testing.T) {
	database := openSeeded(t)
	now, tomorrow := clockOnDBDay(t, database)
	svc := New(database, func() time.Time { return now })
	fredID := personIDByPhone(t, database, seed.PhoneFred)
	vitorID := personIDByPhone(t, database, seed.PhoneVitor)
	modelID := treinoA(t, database)
	clearTomorrow(t, database, []string{vitorID}, tomorrow)

	draft, err := svc.DraftFromLast(context.Background(), fredID, modelID, vitorID, "last")
	if err != nil {
		t.Fatal(err)
	}
	itemID := draft.Items[0].ID
	if itemID == "" {
		t.Fatal("missing item id")
	}

	if err := svc.PatchItem(context.Background(), fredID, draft.DraftID, itemID, ItemPatch{
		LoadKg:      42.5,
		PlannedSets: draft.Items[0].PlannedSets,
		PlannedReps: draft.Items[0].PlannedReps,
	}); err != nil {
		t.Fatal(err)
	}

	var load float64
	var source string
	if err := database.QueryRow(`
		SELECT load_kg, load_source FROM prescription_items WHERE id = $1`,
		itemID,
	).Scan(&load, &source); err != nil {
		t.Fatal(err)
	}
	if load != 42.5 {
		t.Fatalf("load %v want 42.5", load)
	}
	if source != "manual" {
		t.Fatalf("source %s want manual", source)
	}
}

func TestListModelsIncludesTreinoA(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, time.Now)
	fredID := personIDByPhone(t, database, seed.PhoneFred)

	got, err := svc.ListModels(context.Background(), fredID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range got {
		if m.Name == "Treino A" && m.ID != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("models %+v", got)
	}
}

func TestGetModelItemsHaveStarterLoad(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, time.Now)
	fredID := personIDByPhone(t, database, seed.PhoneFred)
	modelID := treinoA(t, database)

	got, err := svc.GetModel(context.Background(), fredID, modelID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Treino A" || len(got.Items) != 3 {
		t.Fatalf("%+v", got)
	}
	if got.Items[0].Name != "Supino" || got.Items[0].StarterLoadKg != 20 || got.Items[0].PlannedSets != 3 {
		t.Fatalf("first %+v", got.Items[0])
	}
}

func TestDraftFromModelUsesStarter(t *testing.T) {
	database := openSeeded(t)
	now, tomorrow := clockOnDBDay(t, database)
	svc := New(database, func() time.Time { return now })
	fredID := personIDByPhone(t, database, seed.PhoneFred)
	vitorID := personIDByPhone(t, database, seed.PhoneVitor)
	modelID := treinoA(t, database)
	clearTomorrow(t, database, []string{vitorID}, tomorrow)

	draft, err := svc.DraftFromLast(context.Background(), fredID, modelID, vitorID, "model")
	if err != nil {
		t.Fatal(err)
	}
	if draft.Items[0].LoadKg != 20 || draft.Items[0].LoadSource != "starter" {
		t.Fatalf("starter %+v", draft.Items[0])
	}
}

func TestPatchPublishedIsInvalid(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, time.Now)
	fredID := personIDByPhone(t, database, seed.PhoneFred)
	vitorID := personIDByPhone(t, database, seed.PhoneVitor)

	var prID, itemID string
	if err := database.QueryRow(`
		SELECT pr.id::text, pi.id::text
		FROM prescriptions pr
		JOIN prescription_items pi ON pi.prescription_id = pr.id AND pi.position = 1
		WHERE pr.person_id = $1 AND pr.for_date = current_date AND pr.status = 'published'`,
		vitorID,
	).Scan(&prID, &itemID); err != nil {
		t.Fatal(err)
	}

	err := svc.PatchItem(context.Background(), fredID, prID, itemID, ItemPatch{
		LoadKg: 42.5, PlannedSets: 3, PlannedReps: "8-12",
	})
	if err != ErrInvalid {
		t.Fatalf("got %v want %v", err, ErrInvalid)
	}
}

func TestPublishForbiddenAsStudent(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, time.Now)
	vitorID := personIDByPhone(t, database, seed.PhoneVitor)
	modelID := treinoA(t, database)

	_, err := svc.ListModels(context.Background(), vitorID)
	if err != ErrForbidden {
		t.Fatalf("list %v", err)
	}
	_, err = svc.DraftFromLast(context.Background(), vitorID, modelID, vitorID, "last")
	if err != ErrForbidden {
		t.Fatalf("draft %v", err)
	}
	err = svc.Publish(context.Background(), vitorID, "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", nil)
	if err != ErrForbidden {
		t.Fatalf("publish %v", err)
	}
}
