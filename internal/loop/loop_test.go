package loop

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

	"github.com/Vitorepf/linkgym-api/internal/auth"
	"github.com/Vitorepf/linkgym-api/internal/config"
	"github.com/Vitorepf/linkgym-api/internal/db"
	"github.com/Vitorepf/linkgym-api/internal/migrate"
	"github.com/Vitorepf/linkgym-api/internal/progress"
	"github.com/Vitorepf/linkgym-api/internal/publish"
	"github.com/Vitorepf/linkgym-api/internal/seed"
	"github.com/Vitorepf/linkgym-api/internal/today"
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
	if err := database.QueryRow(`SELECT id::text FROM people WHERE phone = $1`, phone).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func uniquePhone(prefix int) string {
	return fmt.Sprintf("+55119814%01d%04d", prefix, time.Now().UnixNano()%1e4)
}

func TestStudentWithoutInviteDoesNotEnter(t *testing.T) {
	database := openSeeded(t)
	svc := auth.New(database, "test-pepper", true)
	phone := "+5511999888777"

	_, _, err := svc.RequestCode(context.Background(), phone, "")
	if err != auth.ErrInviteRequired {
		t.Fatalf("err = %v want %v", err, auth.ErrInviteRequired)
	}

	var n int
	if err := database.QueryRow(`SELECT count(*) FROM people WHERE phone = $1`, phone).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("unknown phone entered without invite")
	}
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

func TestLoteKeepsDifferentLoads(t *testing.T) {
	database := openSeeded(t)
	now, tomorrow := clockOnDBDay(t, database)
	svc := publish.New(database, func() time.Time { return now })
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
		t.Fatalf("huan %v want 22.5 (never copy 40)", h)
	}
}

func vitorSupinoToday(t *testing.T, database *sql.DB) (personID, prescriptionID, itemID, exerciseID string, loadKg float64) {
	t.Helper()
	personID = personIDByPhone(t, database, seed.PhoneVitor)
	if err := database.QueryRow(`
		SELECT pr.id::text, pi.id::text, pi.exercise_id::text, pi.load_kg
		FROM prescriptions pr
		JOIN prescription_items pi ON pi.prescription_id = pr.id AND pi.position = 1
		WHERE pr.person_id = $1 AND pr.for_date = current_date AND pr.status = 'published'`,
		personID,
	).Scan(&prescriptionID, &itemID, &exerciseID, &loadKg); err != nil {
		t.Fatal(err)
	}
	return personID, prescriptionID, itemID, exerciseID, loadKg
}

func TestOfflineSessionUpdatesFio(t *testing.T) {
	database := openSeeded(t)
	svc := workout.New(database, time.Now)
	personID, prescriptionID, itemID, exerciseID, loadKg := vitorSupinoToday(t, database)
	ctx := context.Background()
	clientID := newUUID()

	started, err := svc.Start(ctx, personID, clientID, prescriptionID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = database.Exec(`DELETE FROM xp_ledger WHERE session_id = $1`, started.ID)
		_, _ = database.Exec(`DELETE FROM session_alerts WHERE session_id = $1`, started.ID)
		_, _ = database.Exec(`UPDATE personal_records SET session_id = NULL WHERE session_id = $1`, started.ID)
		_, _ = database.Exec(`DELETE FROM workout_sets WHERE session_id = $1`, started.ID)
		_, _ = database.Exec(`DELETE FROM workout_sessions WHERE id = $1`, started.ID)
	})

	if _, err := svc.AddSet(ctx, personID, started.ID, workout.SetInput{
		ClientSetID:        newUUID(),
		PrescriptionItemID: itemID,
		ExerciseID:         exerciseID,
		SetIndex:           1,
		Reps:               10,
		LoadKg:             loadKg,
		RestSeconds:        90,
		PerformedAt:        time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Finish(ctx, personID, started.ID, 2); err != nil {
		t.Fatal(err)
	}

	var status, sessionID string
	if err := database.QueryRow(`
		SELECT status, session_id::text
		FROM fio_days
		WHERE prescription_id = $1`,
		prescriptionID,
	).Scan(&status, &sessionID); err != nil {
		t.Fatal(err)
	}
	if status != "done" {
		t.Fatalf("fio status %q want done", status)
	}
	if sessionID != started.ID {
		t.Fatalf("fio session %s want %s", sessionID, started.ID)
	}
}

func TestSwitchingStudioKeepsPRs(t *testing.T) {
	database := openSeeded(t)
	ctx := context.Background()
	vitorID := personIDByPhone(t, database, seed.PhoneVitor)

	var origBond string
	if err := database.QueryRow(`SELECT active_bond_id::text FROM people WHERE id = $1`, vitorID).Scan(&origBond); err != nil {
		t.Fatal(err)
	}

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

	var prsBefore int
	if err := database.QueryRow(`SELECT count(*) FROM personal_records WHERE person_id = $1`, vitorID).Scan(&prsBefore); err != nil {
		t.Fatal(err)
	}
	if prsBefore < 1 {
		t.Fatal("need a PR before switching studio")
	}

	ownerPhone := uniquePhone(2)
	studioName := fmt.Sprintf("Iron Loop %d", time.Now().UnixNano()%1e9)

	var ownerID, studioID, ownerBondID, newBondID string
	if err := database.QueryRow(`
		INSERT INTO people (phone, name) VALUES ($1, 'Loop Owner') RETURNING id::text`,
		ownerPhone,
	).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		INSERT INTO studios (owner_person_id, name, accent_color)
		VALUES ($1, $2, '#112233') RETURNING id::text`,
		ownerID, studioName,
	).Scan(&studioID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		INSERT INTO bonds (person_id, studio_id, role, status)
		VALUES ($1, $2, 'owner', 'active') RETURNING id::text`,
		ownerID, studioID,
	).Scan(&ownerBondID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE people SET active_bond_id = $2 WHERE id = $1`, ownerID, ownerBondID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		INSERT INTO bonds (person_id, studio_id, role, status)
		VALUES ($1, $2, 'student', 'active') RETURNING id::text`,
		vitorID, studioID,
	).Scan(&newBondID); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_, _ = database.Exec(`UPDATE people SET active_bond_id = $1 WHERE id = $2`, origBond, vitorID)
		_, _ = database.Exec(`UPDATE people SET active_bond_id = NULL WHERE id = $1`, ownerID)
		_, _ = database.Exec(`DELETE FROM streaks WHERE bond_id IN ($1, $2)`, newBondID, ownerBondID)
		_, _ = database.Exec(`DELETE FROM comebacks WHERE bond_id IN ($1, $2)`, newBondID, ownerBondID)
		_, _ = database.Exec(`DELETE FROM xp_ledger WHERE bond_id IN ($1, $2)`, newBondID, ownerBondID)
		_, _ = database.Exec(`DELETE FROM bonds WHERE studio_id = $1`, studioID)
		_, _ = database.Exec(`DELETE FROM studios WHERE id = $1`, studioID)
		_, _ = database.Exec(`DELETE FROM people WHERE id = $1`, ownerID)
	})

	if _, err := database.Exec(`UPDATE people SET active_bond_id = $2 WHERE id = $1`, vitorID, newBondID); err != nil {
		t.Fatal(err)
	}

	recs, err := progress.New(database, time.Now).Records(ctx, vitorID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range recs.Items {
		if item.ExerciseName == "Supino" && item.LoadKg == 40 {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("PR missing after studio switch: %+v", recs.Items)
	}

	got, err := today.New(database, time.Now).Today(ctx, vitorID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Studio.Name != studioName {
		t.Fatalf("today studio %q want %q", got.Studio.Name, studioName)
	}
	if got.Studio.Name == seed.StudioName {
		t.Fatal("today still showing Fred")
	}

	var prsAfter int
	if err := database.QueryRow(`SELECT count(*) FROM personal_records WHERE person_id = $1`, vitorID).Scan(&prsAfter); err != nil {
		t.Fatal(err)
	}
	if prsAfter != prsBefore {
		t.Fatalf("personal_records wiped: before %d after %d", prsBefore, prsAfter)
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

func TestComebackDoesNotWipeArchive(t *testing.T) {
	database := openSeeded(t)
	vitorID := personIDByPhone(t, database, seed.PhoneVitor)
	restoreVitorStreak(t, database, vitorID)
	bondID := bondIDOf(t, database, vitorID)

	day := pgToday(t, database)
	svc := today.New(database, func() time.Time { return day })

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

	var prs int
	if err := database.QueryRow(`SELECT count(*) FROM personal_records WHERE person_id = $1`, vitorID).Scan(&prs); err != nil {
		t.Fatal(err)
	}
	if prs < 1 {
		t.Fatal("personal_records wiped")
	}
}
