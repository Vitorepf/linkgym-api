package today

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

func TestReadinessScore(t *testing.T) {
	if g := score(4, 2, 4); g != 80 {
		t.Fatalf("%d", g)
	}
	if label(80) != "Pode ir com carga" {
		t.Fatalf("%s", label(80))
	}
	if label(34) != "Versão leve" {
		t.Fatal(label(34))
	}
}

func TestReadinessUpsert(t *testing.T) {
	db := openSeeded(t)
	svc := New(db, time.Now)
	vitorID := personIDByPhone(t, db, "+5511900000002")

	got, err := svc.PutReadiness(context.Background(), vitorID, 4, 2, 4)
	if err != nil {
		t.Fatal(err)
	}
	if got.Score != 80 || got.Energy != 4 || got.Soreness != 2 || got.Sleep != 4 {
		t.Fatalf("%+v", got)
	}
	if got.Label != "Pode ir com carga" {
		t.Fatalf("%s", got.Label)
	}

	today, err := svc.Today(context.Background(), vitorID)
	if err != nil {
		t.Fatal(err)
	}
	if today.Readiness.Score != 80 || today.Readiness.Label != "Pode ir com carga" {
		t.Fatalf("today after save %+v", today.Readiness)
	}

	got, err = svc.PutReadiness(context.Background(), vitorID, 1, 5, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Score != 20 || got.Label != "Versão leve" {
		t.Fatalf("second put %+v", got)
	}
}

func TestReadinessUpsertRejectsOutOfRange(t *testing.T) {
	db := openSeeded(t)
	svc := New(db, time.Now)
	vitorID := personIDByPhone(t, db, "+5511900000002")
	if _, err := svc.PutReadiness(context.Background(), vitorID, 0, 3, 3); err != ErrReadinessInvalid {
		t.Fatalf("got %v", err)
	}
	if _, err := svc.PutReadiness(context.Background(), vitorID, 6, 3, 3); err != ErrReadinessInvalid {
		t.Fatalf("got %v", err)
	}
}

func TestTodayLoadIsThePersons(t *testing.T) {
	db := openSeeded(t)
	svc := New(db, time.Now)
	vitorID := personIDByPhone(t, db, "+5511900000002")
	got, err := svc.Today(context.Background(), vitorID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Prescription == nil || got.Prescription.Items[0].LoadKg != 40 {
		t.Fatalf("vitor load %+v", got.Prescription)
	}
}

func pgToday(t *testing.T, database *sql.DB) time.Time {
	t.Helper()
	var d time.Time
	if err := database.QueryRow(`SELECT current_date`).Scan(&d); err != nil {
		t.Fatal(err)
	}
	return time.Date(d.Year(), d.Month(), d.Day(), 12, 0, 0, 0, time.UTC)
}

func bondIDOf(t *testing.T, database *sql.DB, personID string) string {
	t.Helper()
	var id string
	if err := database.QueryRow(`SELECT active_bond_id::text FROM people WHERE id = $1`, personID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func restoreVitorStreak(t *testing.T, database *sql.DB, vitorID string) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = database.Exec(`DELETE FROM comebacks WHERE bond_id = (SELECT active_bond_id FROM people WHERE id = $1)`, vitorID)
		_, _ = database.Exec(`
			DELETE FROM prescriptions
			WHERE person_id = $1 AND for_date <> current_date`,
			vitorID,
		)
		_ = seed.Dev(context.Background(), database)
		_, _ = database.Exec(`
			UPDATE streaks
			SET current_count = 0, protector_available = true,
			    protector_spent_at = NULL, last_fulfilled_on = NULL, updated_at = now()
			WHERE bond_id = (SELECT active_bond_id FROM people WHERE id = $1)`,
			vitorID,
		)
	})
}

func clonePublishedOn(t *testing.T, database *sql.DB, personID, forDate string) {
	t.Helper()
	var srcID string
	if err := database.QueryRow(`
		SELECT id::text FROM prescriptions
		WHERE person_id = $1 AND status = 'published'
		ORDER BY for_date DESC LIMIT 1`,
		personID,
	).Scan(&srcID); err != nil {
		t.Fatal(err)
	}
	var dstID string
	if err := database.QueryRow(`
		INSERT INTO prescriptions (studio_id, person_id, model_id, for_date, status, published_at)
		SELECT studio_id, person_id, model_id, $2::date, 'published', now()
		FROM prescriptions WHERE id = $1
		ON CONFLICT (person_id, studio_id, for_date) WHERE status = 'published'
		DO UPDATE SET updated_at = now()
		RETURNING id::text`,
		srcID, forDate,
	).Scan(&dstID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO prescription_items (
			prescription_id, exercise_id, position, planned_sets, planned_reps,
			load_kg, rest_seconds, notes, load_source
		)
		SELECT $1, exercise_id, position, planned_sets, planned_reps,
		       load_kg, rest_seconds, notes, load_source
		FROM prescription_items WHERE prescription_id = $2
		ON CONFLICT (prescription_id, position) DO NOTHING`,
		dstID, srcID,
	); err != nil {
		t.Fatal(err)
	}
}

func TestFirstMissSpendsProtector(t *testing.T) {
	database := openSeeded(t)
	vitorID := personIDByPhone(t, database, seed.PhoneVitor)
	restoreVitorStreak(t, database, vitorID)
	bondID := bondIDOf(t, database, vitorID)

	day := pgToday(t, database)
	clock := day
	svc := New(database, func() time.Time { return clock })

	if _, err := database.Exec(`
		UPDATE streaks
		SET current_count = 4, protector_available = true,
		    protector_spent_at = NULL, last_fulfilled_on = $2::date, updated_at = now()
		WHERE bond_id = $1`,
		bondID, day.AddDate(0, 0, -1).Format("2006-01-02"),
	); err != nil {
		t.Fatal(err)
	}
	clonePublishedOn(t, database, vitorID, day.AddDate(0, 0, -1).Format("2006-01-02"))

	got, err := svc.Today(context.Background(), vitorID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Streak.ProtectorAvailable || got.Streak.CurrentCount != 4 {
		t.Fatalf("first today %+v", got.Streak)
	}

	again, err := svc.Today(context.Background(), vitorID)
	if err != nil {
		t.Fatal(err)
	}
	if again.Streak.ProtectorAvailable || again.Streak.CurrentCount != 4 {
		t.Fatalf("second today same day %+v", again.Streak)
	}

	clock = day.AddDate(0, 0, 1)
	if _, err := database.Exec(`
		DELETE FROM prescriptions
		WHERE person_id = $1 AND for_date = $2::date AND status = 'published'`,
		vitorID, day.Format("2006-01-02"),
	); err != nil {
		t.Fatal(err)
	}
	next, err := svc.Today(context.Background(), vitorID)
	if err != nil {
		t.Fatal(err)
	}
	if next.Streak.ProtectorAvailable || next.Streak.CurrentCount != 4 {
		t.Fatalf("day after first miss %+v", next.Streak)
	}
	twice, err := svc.Today(context.Background(), vitorID)
	if err != nil {
		t.Fatal(err)
	}
	if twice.Streak.ProtectorAvailable || twice.Streak.CurrentCount != 4 {
		t.Fatalf("day after first miss twice %+v", twice.Streak)
	}
}

func TestSecondMissZerosStreak(t *testing.T) {
	database := openSeeded(t)
	vitorID := personIDByPhone(t, database, seed.PhoneVitor)
	restoreVitorStreak(t, database, vitorID)
	bondID := bondIDOf(t, database, vitorID)

	day := pgToday(t, database)
	clock := day
	svc := New(database, func() time.Time { return clock })

	if _, err := database.Exec(`
		UPDATE streaks
		SET current_count = 4, protector_available = true,
		    protector_spent_at = NULL, last_fulfilled_on = $2::date, updated_at = now()
		WHERE bond_id = $1`,
		bondID, day.AddDate(0, 0, -1).Format("2006-01-02"),
	); err != nil {
		t.Fatal(err)
	}
	clonePublishedOn(t, database, vitorID, day.AddDate(0, 0, -1).Format("2006-01-02"))

	if _, err := svc.Today(context.Background(), vitorID); err != nil {
		t.Fatal(err)
	}

	clock = day.AddDate(0, 0, 1)
	got, err := svc.Today(context.Background(), vitorID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Streak.CurrentCount != 0 || got.Streak.ProtectorAvailable {
		t.Fatalf("second miss %+v", got.Streak)
	}
}

func TestD11OpensComebackWithoutWipingPR(t *testing.T) {
	database := openSeeded(t)
	vitorID := personIDByPhone(t, database, seed.PhoneVitor)
	restoreVitorStreak(t, database, vitorID)
	bondID := bondIDOf(t, database, vitorID)

	day := pgToday(t, database)
	svc := New(database, func() time.Time { return day })

	if _, err := database.Exec(`
		UPDATE streaks
		SET current_count = 4, protector_available = true,
		    protector_spent_at = NULL, last_fulfilled_on = $2::date, updated_at = now()
		WHERE bond_id = $1`,
		bondID, day.AddDate(0, 0, -11).Format("2006-01-02"),
	); err != nil {
		t.Fatal(err)
	}
	clonePublishedOn(t, database, vitorID, day.AddDate(0, 0, -1).Format("2006-01-02"))

	if _, err := database.Exec(`
		INSERT INTO personal_records (person_id, exercise_id, load_kg, reps)
		SELECT $1, e.id, 40, 8
		FROM exercises e
		JOIN studios s ON s.id = e.studio_id
		JOIN people owner ON owner.id = s.owner_person_id
		WHERE owner.phone = $2 AND e.name = 'Supino'
		ON CONFLICT (person_id, exercise_id) DO UPDATE SET load_kg = EXCLUDED.load_kg`,
		vitorID, seed.PhoneFred,
	); err != nil {
		t.Fatal(err)
	}

	got, err := svc.Today(context.Background(), vitorID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Comeback == nil {
		t.Fatal("comeback missing")
	}
	if got.Comeback.Minutes != 9 {
		t.Fatalf("minutes %d", got.Comeback.Minutes)
	}
	if got.Comeback.CoachLine != "Sem culpa. Nove minutos e você está de volta." {
		t.Fatalf("coach_line %q", got.Comeback.CoachLine)
	}

	var prs int
	if err := database.QueryRow(`SELECT count(*) FROM personal_records WHERE person_id = $1`, vitorID).Scan(&prs); err != nil {
		t.Fatal(err)
	}
	if prs < 1 {
		t.Fatal("personal_records wiped")
	}
}
