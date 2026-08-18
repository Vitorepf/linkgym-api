package workout

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
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
)

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

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

type seededLift struct {
	personID       string
	prescriptionID string
	itemID         string
	exerciseID     string
	exerciseName   string
	loadKg         float64
	huanLoadKg     float64
}

func vitorSupinoToday(t *testing.T, database *sql.DB) seededLift {
	t.Helper()
	out := seededLift{personID: personIDByPhone(t, database, seed.PhoneVitor)}
	err := database.QueryRow(`
		SELECT pr.id::text, pi.id::text, pi.exercise_id::text, e.name, pi.load_kg
		FROM prescriptions pr
		JOIN prescription_items pi ON pi.prescription_id = pr.id AND pi.position = 1
		JOIN exercises e ON e.id = pi.exercise_id
		WHERE pr.person_id = $1 AND pr.for_date = current_date AND pr.status = 'published'`,
		out.personID,
	).Scan(&out.prescriptionID, &out.itemID, &out.exerciseID, &out.exerciseName, &out.loadKg)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		SELECT pi.load_kg
		FROM prescriptions pr
		JOIN people p ON p.id = pr.person_id
		JOIN prescription_items pi ON pi.prescription_id = pr.id AND pi.position = 1
		WHERE p.phone = $1 AND pr.for_date = current_date AND pr.status = 'published'`,
		seed.PhoneHuan,
	).Scan(&out.huanLoadKg); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestFinishAwardsXPAndDoesNotCopyLoads(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, time.Now)
	lift := vitorSupinoToday(t, database)
	if lift.exerciseName != "Supino" || lift.loadKg != 40 {
		t.Fatalf("seed %+v", lift)
	}
	_, _ = database.Exec(`DELETE FROM personal_records WHERE person_id = $1`, lift.personID)

	ctx := context.Background()
	started, err := svc.Start(ctx, lift.personID, newUUID(), lift.prescriptionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddSet(ctx, lift.personID, started.ID, SetInput{
		LocalID:            newUUID(),
		PrescriptionItemID: lift.itemID,
		ExerciseID:         lift.exerciseID,
		SetIndex:           1,
		Reps:               10,
		LoadKg:             40,
		RestSeconds:        90,
		PerformedAt:        time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Finish(ctx, lift.personID, started.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got.XPGained != 35 {
		t.Fatalf("xp_gained %d want 35 (10 session + 25 pr)", got.XPGained)
	}
	if len(got.Records) != 1 || got.Records[0].ExerciseName != "Supino" || got.Records[0].LoadKg != 40 || got.Records[0].PreviousKg != 0 {
		t.Fatalf("records %+v", got.Records)
	}

	var sessionXP int
	if err := database.QueryRow(`
		SELECT amount FROM xp_ledger
		WHERE session_id = $1 AND reason = 'session'`,
		started.ID,
	).Scan(&sessionXP); err != nil {
		t.Fatal(err)
	}
	if sessionXP != 10 {
		t.Fatalf("session xp %d", sessionXP)
	}

	var pr float64
	if err := database.QueryRow(`
		SELECT load_kg FROM personal_records
		WHERE person_id = $1 AND exercise_id = $2`,
		lift.personID, lift.exerciseID,
	).Scan(&pr); err != nil {
		t.Fatal(err)
	}
	if pr != 40 {
		t.Fatalf("pr %v", pr)
	}

	var huan float64
	if err := database.QueryRow(`
		SELECT pi.load_kg
		FROM prescriptions pr
		JOIN people p ON p.id = pr.person_id
		JOIN prescription_items pi ON pi.prescription_id = pr.id AND pi.position = 1
		WHERE p.phone = $1 AND pr.for_date = current_date AND pr.status = 'published'`,
		seed.PhoneHuan,
	).Scan(&huan); err != nil {
		t.Fatal(err)
	}
	if huan != 22.5 {
		t.Fatalf("huan load copied %v", huan)
	}
}

func TestDuplicateLocalIDDoesNotDouble(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, time.Now)
	lift := vitorSupinoToday(t, database)
	ctx := context.Background()
	started, err := svc.Start(ctx, lift.personID, newUUID(), lift.prescriptionID)
	if err != nil {
		t.Fatal(err)
	}
	in := SetInput{
		LocalID:            newUUID(),
		PrescriptionItemID: lift.itemID,
		ExerciseID:         lift.exerciseID,
		SetIndex:           1,
		Reps:               10,
		LoadKg:             40,
		RestSeconds:        90,
		PerformedAt:        time.Now().UTC(),
	}
	if _, err := svc.AddSet(ctx, lift.personID, started.ID, in); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddSet(ctx, lift.personID, started.ID, in); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := database.QueryRow(`
		SELECT count(*) FROM workout_sets WHERE session_id = $1`,
		started.ID,
	).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("count=%d", n)
	}
}

func TestOfflineSetWinsIfServerLacksRow(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, time.Now)
	lift := vitorSupinoToday(t, database)
	ctx := context.Background()
	started, err := svc.Start(ctx, lift.personID, newUUID(), lift.prescriptionID)
	if err != nil {
		t.Fatal(err)
	}
	id := newUUID()
	first, err := svc.AddSet(ctx, lift.personID, started.ID, SetInput{
		LocalID:            id,
		PrescriptionItemID: lift.itemID,
		ExerciseID:         lift.exerciseID,
		SetIndex:           1,
		Reps:               10,
		LoadKg:             40,
		RestSeconds:        90,
		PerformedAt:        time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.AddSet(ctx, lift.personID, started.ID, SetInput{
		LocalID:            id,
		PrescriptionItemID: lift.itemID,
		ExerciseID:         lift.exerciseID,
		SetIndex:           1,
		Reps:               8,
		LoadKg:             35,
		RestSeconds:        60,
		PerformedAt:        time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Reps != 10 || first.Reps != 10 {
		t.Fatalf("first=%d second=%d", first.Reps, second.Reps)
	}
	var reps int
	if err := database.QueryRow(`
		SELECT reps FROM workout_sets
		WHERE session_id = $1 AND client_set_id = $2`,
		started.ID, id,
	).Scan(&reps); err != nil {
		t.Fatal(err)
	}
	if reps != 10 {
		t.Fatalf("stored reps %d", reps)
	}
}

func TestFinishTwiceDoesNotDoubleXP(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, time.Now)
	lift := vitorSupinoToday(t, database)
	ctx := context.Background()
	started, err := svc.Start(ctx, lift.personID, newUUID(), lift.prescriptionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddSet(ctx, lift.personID, started.ID, SetInput{
		LocalID:            newUUID(),
		PrescriptionItemID: lift.itemID,
		ExerciseID:         lift.exerciseID,
		SetIndex:           1,
		Reps:               10,
		LoadKg:             40,
		RestSeconds:        90,
		PerformedAt:        time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	first, err := svc.Finish(ctx, lift.personID, started.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Finish(ctx, lift.personID, started.ID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if first.XPGained != second.XPGained || first.XPTotal != second.XPTotal {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	var n int
	if err := database.QueryRow(`
		SELECT count(*) FROM xp_ledger WHERE session_id = $1 AND reason = 'session'`,
		started.ID,
	).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("session xp rows %d", n)
	}
}

func TestSecondFinishSameDayDoesNotDoubleOfensiva(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, time.Now)
	lift := vitorSupinoToday(t, database)
	ctx := context.Background()

	if _, err := database.Exec(`DELETE FROM personal_records WHERE person_id = $1`, lift.personID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		UPDATE streaks SET current_count = 0, last_fulfilled_on = NULL
		WHERE bond_id = (SELECT active_bond_id FROM people WHERE id = $1)`,
		lift.personID,
	); err != nil {
		t.Fatal(err)
	}

	finish := func(loadKg float64) *FinishResult {
		t.Helper()
		started, err := svc.Start(ctx, lift.personID, newUUID(), lift.prescriptionID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.AddSet(ctx, lift.personID, started.ID, SetInput{
			LocalID:            newUUID(),
			PrescriptionItemID: lift.itemID,
			ExerciseID:         lift.exerciseID,
			SetIndex:           1,
			Reps:               10,
			LoadKg:             loadKg,
			RestSeconds:        90,
			PerformedAt:        time.Now().UTC(),
		}); err != nil {
			t.Fatal(err)
		}
		got, err := svc.Finish(ctx, lift.personID, started.ID, 2)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	first := finish(40)
	if first.Ofensiva.CurrentCount != 1 {
		t.Fatalf("first ofensiva %d want 1", first.Ofensiva.CurrentCount)
	}
	if first.XPGained != 35 {
		t.Fatalf("first xp_gained %d want 35", first.XPGained)
	}

	second := finish(45)
	if second.Ofensiva.CurrentCount != 1 {
		t.Fatalf("second ofensiva %d want 1 (same for_date)", second.Ofensiva.CurrentCount)
	}
	if second.XPGained != 35 {
		t.Fatalf("second xp_gained %d want 35 (10 session + 25 pr)", second.XPGained)
	}
	if len(second.Records) != 1 || second.Records[0].LoadKg != 45 || second.Records[0].PreviousKg != 40 {
		t.Fatalf("second records %+v", second.Records)
	}
}

func TestFinishWithNoSetsRejected(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, time.Now)
	lift := vitorSupinoToday(t, database)
	ctx := context.Background()
	started, err := svc.Start(ctx, lift.personID, newUUID(), lift.prescriptionID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Finish(ctx, lift.personID, started.ID, 2)
	if err != ErrEmptySession {
		t.Fatalf("got %v want %v", err, ErrEmptySession)
	}
	var finished sql.NullTime
	if err := database.QueryRow(`SELECT finished_at FROM workout_sessions WHERE id = $1`, started.ID).Scan(&finished); err != nil {
		t.Fatal(err)
	}
	if finished.Valid {
		t.Fatal("empty session should stay open")
	}
	var n int
	if err := database.QueryRow(`SELECT count(*) FROM xp_ledger WHERE session_id = $1`, started.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("xp rows %d", n)
	}
}

func TestSwapWritesAlertAndKeepsOfensiva(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, time.Now)
	lift := vitorSupinoToday(t, database)
	remadaID := exerciseIDByName(t, database, "Remada")
	ctx := context.Background()

	if _, err := database.Exec(`
		UPDATE streaks SET current_count = 7
		WHERE bond_id = (SELECT active_bond_id FROM people WHERE id = $1)`,
		lift.personID,
	); err != nil {
		t.Fatal(err)
	}

	started, err := svc.Start(ctx, lift.personID, newUUID(), lift.prescriptionID)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Swap(ctx, lift.personID, started.ID, lift.exerciseID, remadaID); err != nil {
		t.Fatal(err)
	}

	var kind string
	var payload []byte
	if err := database.QueryRow(`
		SELECT kind, payload FROM session_alerts
		WHERE session_id = $1 AND kind = 'exercise_swap'`,
		started.ID,
	).Scan(&kind, &payload); err != nil {
		t.Fatal(err)
	}
	if kind != "exercise_swap" {
		t.Fatalf("kind %s", kind)
	}
	var got map[string]string
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatal(err)
	}
	if got["from"] != lift.exerciseID || got["to"] != remadaID || got["person"] != lift.personID {
		t.Fatalf("payload %s", payload)
	}

	var ofensiva int
	if err := database.QueryRow(`
		SELECT current_count FROM streaks
		WHERE bond_id = (SELECT active_bond_id FROM people WHERE id = $1)`,
		lift.personID,
	).Scan(&ofensiva); err != nil {
		t.Fatal(err)
	}
	if ofensiva != 7 {
		t.Fatalf("ofensiva %d want 7", ofensiva)
	}
}

func exerciseIDByName(t *testing.T, database *sql.DB, name string) string {
	t.Helper()
	var id string
	if err := database.QueryRow(`
		SELECT e.id::text FROM exercises e
		JOIN studios s ON s.id = e.studio_id
		JOIN people o ON o.id = s.owner_person_id
		WHERE o.phone = $1 AND e.name = $2`,
		seed.PhoneFred, name,
	).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestDebutBadgeOnFirstFinish(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, time.Now)
	personID := personIDByPhone(t, database, seed.PhoneJose)
	ctx := context.Background()

	if _, err := database.Exec(`DELETE FROM workout_sessions WHERE person_id = $1`, personID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`DELETE FROM badges WHERE person_id = $1 AND badge_key = 'estreia'`, personID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = database.Exec(`DELETE FROM workout_sessions WHERE person_id = $1`, personID)
		_, _ = database.Exec(`DELETE FROM badges WHERE person_id = $1 AND badge_key = 'estreia'`, personID)
	})

	var prescriptionID, itemID, exerciseID string
	var loadKg float64
	if err := database.QueryRow(`
		SELECT pr.id::text, pi.id::text, pi.exercise_id::text, pi.load_kg
		FROM prescriptions pr
		JOIN prescription_items pi ON pi.prescription_id = pr.id AND pi.position = 1
		WHERE pr.person_id = $1 AND pr.for_date = current_date AND pr.status = 'published'`,
		personID,
	).Scan(&prescriptionID, &itemID, &exerciseID, &loadKg); err != nil {
		t.Fatal(err)
	}

	finishOne := func() *FinishResult {
		t.Helper()
		started, err := svc.Start(ctx, personID, newUUID(), prescriptionID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.AddSet(ctx, personID, started.ID, SetInput{
			LocalID:            newUUID(),
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
		got, err := svc.Finish(ctx, personID, started.ID, 2)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	first := finishOne()
	if !hasBadge(first.BadgeKeys, "estreia") {
		t.Fatalf("first badge_keys %+v, want estreia", first.BadgeKeys)
	}
	var key string
	if err := database.QueryRow(`
		SELECT badge_key FROM badges
		WHERE person_id = $1 AND badge_key = 'estreia'`,
		personID,
	).Scan(&key); err != nil {
		t.Fatalf("estreia row: %v", err)
	}

	second := finishOne()
	if hasBadge(second.BadgeKeys, "estreia") {
		t.Fatalf("second finish must not re-award estreia: %+v", second.BadgeKeys)
	}
	var n int
	if err := database.QueryRow(`
		SELECT count(*) FROM badges
		WHERE person_id = $1 AND badge_key = 'estreia'`,
		personID,
	).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("estreia rows %d, want 1", n)
	}
}

func TestFinishAtOfensivaFourAwardsOfensiva4(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, time.Now)
	lift := vitorSupinoToday(t, database)
	ctx := context.Background()
	if _, err := database.Exec(`DELETE FROM badges WHERE person_id = $1 AND badge_key = 'ofensiva_4'`, lift.personID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = database.Exec(`DELETE FROM badges WHERE person_id = $1 AND badge_key = 'ofensiva_4'`, lift.personID)
		_, _ = database.Exec(`DELETE FROM workout_sessions WHERE person_id = $1`, lift.personID)
	})

	if _, err := database.Exec(`
		INSERT INTO streaks (bond_id, current_count, last_fulfilled_on, protector_available)
		SELECT active_bond_id, 3, current_date - 1, true
		FROM people WHERE id = $1
		ON CONFLICT (bond_id) DO UPDATE SET
			current_count = 3,
			last_fulfilled_on = current_date - 1,
			updated_at = now()`,
		lift.personID,
	); err != nil {
		t.Fatal(err)
	}

	started, err := svc.Start(ctx, lift.personID, newUUID(), lift.prescriptionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddSet(ctx, lift.personID, started.ID, SetInput{
		LocalID:            newUUID(),
		PrescriptionItemID: lift.itemID,
		ExerciseID:         lift.exerciseID,
		SetIndex:           1,
		Reps:               10,
		LoadKg:             lift.loadKg,
		RestSeconds:        90,
		PerformedAt:        time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Finish(ctx, lift.personID, started.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got.Ofensiva.CurrentCount < 4 {
		t.Fatalf("ofensiva %d want >= 4", got.Ofensiva.CurrentCount)
	}
	if !hasBadge(got.BadgeKeys, "ofensiva_4") {
		t.Fatalf("badge_keys %+v, want ofensiva_4", got.BadgeKeys)
	}
	var key string
	if err := database.QueryRow(`
		SELECT badge_key FROM badges
		WHERE person_id = $1 AND badge_key = 'ofensiva_4'`,
		lift.personID,
	).Scan(&key); err != nil {
		t.Fatalf("ofensiva_4 row: %v", err)
	}
}

func TestAddSetRejectedWhenFinished(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, time.Now)
	lift := vitorSupinoToday(t, database)
	ctx := context.Background()

	started, err := svc.Start(ctx, lift.personID, newUUID(), lift.prescriptionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddSet(ctx, lift.personID, started.ID, SetInput{
		LocalID:            newUUID(),
		PrescriptionItemID: lift.itemID,
		ExerciseID:         lift.exerciseID,
		SetIndex:           1,
		Reps:               10,
		LoadKg:             lift.loadKg,
		RestSeconds:        90,
		PerformedAt:        time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Finish(ctx, lift.personID, started.ID, 2); err != nil {
		t.Fatal(err)
	}
	_, err = svc.AddSet(ctx, lift.personID, started.ID, SetInput{
		LocalID:            newUUID(),
		PrescriptionItemID: lift.itemID,
		ExerciseID:         lift.exerciseID,
		SetIndex:           2,
		Reps:               8,
		LoadKg:             lift.loadKg,
		RestSeconds:        90,
		PerformedAt:        time.Now().UTC(),
	})
	if err != ErrInvalid {
		t.Fatalf("got %v want %v", err, ErrInvalid)
	}
	var n int
	if err := database.QueryRow(`SELECT count(*) FROM workout_sets WHERE session_id = $1`, started.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("sets %d want 1", n)
	}
}

func hasBadge(keys []string, want string) bool {
	for _, k := range keys {
		if k == want {
			return true
		}
	}
	return false
}

func TestFinishForbiddenForOtherPerson(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, time.Now)
	lift := vitorSupinoToday(t, database)
	huanID := personIDByPhone(t, database, seed.PhoneHuan)
	ctx := context.Background()
	started, err := svc.Start(ctx, lift.personID, newUUID(), lift.prescriptionID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Finish(ctx, huanID, started.ID, 2)
	if err != ErrForbidden {
		t.Fatalf("got %v", err)
	}
}
