package migrate_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Vitorepf/linkgym-api/internal/config"
	"github.com/Vitorepf/linkgym-api/internal/db"
	"github.com/Vitorepf/linkgym-api/internal/migrate"
)

func TestMain(m *testing.M) {
	loadRepoEnv()
	os.Exit(m.Run())
}

func loadRepoEnv() {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return
	}
	dir := filepath.Dir(file)
	for i := 0; i < 5; i++ {
		candidate := filepath.Join(dir, ".env")
		if _, err := os.Stat(candidate); err == nil {
			config.LoadDotEnv(candidate)
			return
		}
		dir = filepath.Dir(dir)
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	database, err := db.Open(url)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := migrate.Up(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return database
}

func TestBatchPrescriptionsKeepDifferentLoads(t *testing.T) {
	database := openTestDB(t)
	tx, err := database.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	var personalID, studentA, studentB, studioID, modelID, exerciseID, rxA, rxB string

	mustExec(t, tx, `INSERT INTO people (phone, name) VALUES ('+5511990000001', 'Personal') RETURNING id`, &personalID)
	mustExec(t, tx, `INSERT INTO people (phone, name) VALUES ('+5511990000002', 'Aluna A') RETURNING id`, &studentA)
	mustExec(t, tx, `INSERT INTO people (phone, name) VALUES ('+5511990000003', 'Aluna B') RETURNING id`, &studentB)
	mustExec(t, tx, `INSERT INTO studios (owner_person_id, name, accent_color) VALUES ($1, 'Studio X', '#ec3013') RETURNING id`, &studioID, personalID)
	mustExec(t, tx, `INSERT INTO bonds (person_id, studio_id, role, status) VALUES ($1, $2, 'owner', 'active')`, nil, personalID, studioID)
	mustExec(t, tx, `INSERT INTO bonds (person_id, studio_id, role, status) VALUES ($1, $2, 'student', 'active')`, nil, studentA, studioID)
	mustExec(t, tx, `INSERT INTO bonds (person_id, studio_id, role, status) VALUES ($1, $2, 'student', 'active')`, nil, studentB, studioID)
	mustExec(t, tx, `INSERT INTO exercises (studio_id, name) VALUES ($1, 'Supino') RETURNING id`, &exerciseID, studioID)
	mustExec(t, tx, `INSERT INTO models (studio_id, name, created_by_person_id) VALUES ($1, 'Peito A', $2) RETURNING id`, &modelID, studioID, personalID)
	mustExec(t, tx, `INSERT INTO model_items (model_id, exercise_id, position, planned_sets, planned_reps, starter_load_kg) VALUES ($1, $2, 1, 3, '8-12', 20)`, nil, modelID, exerciseID)
	mustExec(t, tx, `INSERT INTO prescriptions (studio_id, person_id, model_id, for_date, status, published_at) VALUES ($1, $2, $3, '2026-08-18', 'published', now()) RETURNING id`, &rxA, studioID, studentA, modelID)
	mustExec(t, tx, `INSERT INTO prescriptions (studio_id, person_id, model_id, for_date, status, published_at) VALUES ($1, $2, $3, '2026-08-18', 'published', now()) RETURNING id`, &rxB, studioID, studentB, modelID)
	mustExec(t, tx, `INSERT INTO prescription_items (prescription_id, exercise_id, position, planned_sets, planned_reps, load_kg, load_source) VALUES ($1, $2, 1, 3, '8-12', 40, 'history')`, nil, rxA, exerciseID)
	mustExec(t, tx, `INSERT INTO prescription_items (prescription_id, exercise_id, position, planned_sets, planned_reps, load_kg, load_source) VALUES ($1, $2, 1, 3, '8-12', 22.5, 'history')`, nil, rxB, exerciseID)

	var loadA, loadB float64
	if err := tx.QueryRow(`SELECT load_kg FROM prescription_items WHERE prescription_id = $1`, rxA).Scan(&loadA); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(`SELECT load_kg FROM prescription_items WHERE prescription_id = $1`, rxB).Scan(&loadB); err != nil {
		t.Fatal(err)
	}
	if loadA == loadB {
		t.Fatalf("lote copiou carga: A=%v B=%v", loadA, loadB)
	}
	if loadA != 40 || loadB != 22.5 {
		t.Fatalf("cargas = %v e %v", loadA, loadB)
	}
}

func TestPhoneIsUniqueToThePerson(t *testing.T) {
	database := openTestDB(t)
	tx, err := database.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	mustExec(t, tx, `INSERT INTO people (phone, name) VALUES ('+5511990000099', 'Um')`, nil)
	_, err = tx.Exec(`INSERT INTO people (phone, name) VALUES ('+5511990000099', 'Dois')`)
	if err == nil {
		t.Fatal("telefone duplicado deveria falhar")
	}
}

func TestPRBelongsToPersonNotStudio(t *testing.T) {
	database := openTestDB(t)
	tx, err := database.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	var personID, studioID, exerciseID string
	mustExec(t, tx, `INSERT INTO people (phone, name) VALUES ('+5511990000088', 'Pessoa') RETURNING id`, &personID)
	mustExec(t, tx, `INSERT INTO studios (owner_person_id, name) VALUES ($1, 'A') RETURNING id`, &studioID, personID)
	mustExec(t, tx, `INSERT INTO exercises (studio_id, name) VALUES ($1, 'Terra') RETURNING id`, &exerciseID, studioID)
	mustExec(t, tx, `INSERT INTO personal_records (person_id, exercise_id, load_kg, reps) VALUES ($1, $2, 100, 5)`, nil, personID, exerciseID)

	var studioCol int
	err = tx.QueryRow(`
		SELECT count(*) FROM information_schema.columns
		WHERE table_name = 'personal_records' AND column_name = 'studio_id'
	`).Scan(&studioCol)
	if err != nil {
		t.Fatal(err)
	}
	if studioCol != 0 {
		t.Fatal("PR não pode ter studio_id — o recorde é da pessoa")
	}
}

func TestStreakLivesOnTheBond(t *testing.T) {
	database := openTestDB(t)
	var column string
	err := database.QueryRow(`
		SELECT column_name FROM information_schema.columns
		WHERE table_name = 'streaks' AND column_name = 'bond_id'
	`).Scan(&column)
	if err != nil {
		t.Fatalf("ofensiva tem que viver no vínculo: %v", err)
	}
}

func TestV1TablesExist(t *testing.T) {
	database := openTestDB(t)
	need := []string{
		"people", "studios", "bonds", "invites", "login_codes", "auth_sessions",
		"media_objects", "exercises", "models", "model_items",
		"prescriptions", "prescription_items", "workout_sessions", "workout_sets",
		"personal_records", "streaks", "badges", "attention_items",
		"xp_ledger", "readiness_logs", "push_devices", "jobs",
		"progress_photos", "session_alerts", "comebacks",
	}
	for _, name := range need {
		var found string
		err := database.QueryRow(`SELECT to_regclass('public.' || $1)`, name).Scan(&found)
		if err != nil || found == "" {
			t.Errorf("faltou tabela %s: %v", name, err)
		}
	}
	var view string
	if err := database.QueryRow(`SELECT to_regclass('public.fio_days')`).Scan(&view); err != nil || view == "" {
		t.Fatalf("faltou view fio_days: %v", err)
	}
}

func mustExec(t *testing.T, tx *sql.Tx, query string, dest *string, args ...any) {
	t.Helper()
	if dest != nil {
		if err := tx.QueryRow(query, args...).Scan(dest); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		return
	}
	if _, err := tx.Exec(query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}
