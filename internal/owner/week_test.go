package owner

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/Vitorepf/linkgym-api/internal/seed"
)

func TestWeekApprovePublishesTomorrowWithOwnLoads(t *testing.T) {
	database := openSeeded(t)
	var day time.Time
	if err := database.QueryRow(`SELECT current_date`).Scan(&day); err != nil {
		t.Fatal(err)
	}
	now := time.Date(day.Year(), day.Month(), day.Day(), 12, 0, 0, 0, time.UTC)
	svc := New(database, func() time.Time { return now })
	fredID := personIDByPhone(t, database, seed.PhoneFred)
	vitorID := personIDByPhone(t, database, seed.PhoneVitor)
	huanID := personIDByPhone(t, database, seed.PhoneHuan)
	tomorrow := now.AddDate(0, 0, 1).Format("2006-01-02")

	if _, err := database.Exec(`
		DELETE FROM prescriptions
		WHERE person_id IN ($1, $2) AND for_date = $3::date`,
		vitorID, huanID, tomorrow,
	); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = database.Exec(`
			DELETE FROM prescriptions
			WHERE person_id IN ($1, $2) AND for_date = $3::date`,
			vitorID, huanID, tomorrow,
		)
	})

	n, err := svc.Approve(context.Background(), fredID, []string{vitorID, huanID})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("count %d", n)
	}

	loadOf := func(personID string) float64 {
		t.Helper()
		var load float64
		if err := database.QueryRow(`
			SELECT pi.load_kg
			FROM prescriptions pr
			JOIN prescription_items pi ON pi.prescription_id = pr.id AND pi.position = 1
			WHERE pr.person_id = $1 AND pr.for_date = $2::date AND pr.status = 'published'`,
			personID, tomorrow,
		).Scan(&load); err != nil {
			t.Fatal(err)
		}
		return load
	}
	if v := loadOf(vitorID); v != 40 {
		t.Fatalf("vitor %v want 40", v)
	}
	if h := loadOf(huanID); h != 22.5 {
		t.Fatalf("huan %v want 22.5", h)
	}
}

func TestWeekListsStudentsSelectedWithDefaultManter(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, time.Now)
	fredID := personIDByPhone(t, database, seed.PhoneFred)

	items, err := svc.Week(context.Background(), fredID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("items %d", len(items))
	}
	byName := map[string]WeekItem{}
	for _, it := range items {
		if !it.Selected {
			t.Fatalf("unselected %+v", it)
		}
		if len(it.Adherence) < 3 || it.Adherence[len(it.Adherence)-2:] != "/3" {
			t.Fatalf("adherence %q", it.Adherence)
		}
		byName[it.Name] = it
	}
	if byName["Vitor"].PersonID == "" || byName["Huan"].PersonID == "" || byName["Jose"].PersonID == "" {
		t.Fatalf("names %+v", items)
	}
	if byName["Huan"].Suggested != "manter" && byName["Huan"].Suggested != "versão curta" {
		t.Fatalf("huan suggested %q", byName["Huan"].Suggested)
	}
}

func TestWeekSuggestsBumpWhenLastEffortEasy(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, time.Now)
	fredID := personIDByPhone(t, database, seed.PhoneFred)
	huanID := personIDByPhone(t, database, seed.PhoneHuan)
	insertFinishedSession(t, database, huanID, 1)

	items, err := svc.Week(context.Background(), fredID, "")
	if err != nil {
		t.Fatal(err)
	}
	got := itemByName(t, items, "Huan")
	if got.Suggested != "+2,5 kg no supino" {
		t.Fatalf("suggested %q", got.Suggested)
	}
}

func TestWeekSuggestsManterWhenLastEffortHard(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, time.Now)
	fredID := personIDByPhone(t, database, seed.PhoneFred)
	huanID := personIDByPhone(t, database, seed.PhoneHuan)
	insertFinishedSession(t, database, huanID, 3)

	items, err := svc.Week(context.Background(), fredID, "")
	if err != nil {
		t.Fatal(err)
	}
	got := itemByName(t, items, "Huan")
	if got.Suggested != "manter" {
		t.Fatalf("suggested %q", got.Suggested)
	}
}

func TestWeekSuggestsShortWhenMissingTwoDays(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, time.Now)
	fredID := personIDByPhone(t, database, seed.PhoneFred)
	huanID := personIDByPhone(t, database, seed.PhoneHuan)
	if _, err := database.Exec(`DELETE FROM workout_sessions WHERE person_id = $1`, huanID); err != nil {
		t.Fatal(err)
	}

	var day time.Time
	if err := database.QueryRow(`SELECT current_date`).Scan(&day); err != nil {
		t.Fatal(err)
	}
	now := time.Date(day.Year(), day.Month(), day.Day(), 12, 0, 0, 0, time.UTC)
	mon, _ := isoWeekBounds(now)
	extra := mon
	if extra.Format("2006-01-02") == now.Format("2006-01-02") {
		extra = mon.AddDate(0, 0, 1)
	}
	clonePublishedOnDate(t, database, huanID, extra.Format("2006-01-02"))

	items, err := svc.Week(context.Background(), fredID, "")
	if err != nil {
		t.Fatal(err)
	}
	got := itemByName(t, items, "Huan")
	if got.Suggested != "versão curta" {
		t.Fatalf("suggested %q", got.Suggested)
	}
}

func TestStudentCardKeepForVitor(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, time.Now)
	fredID := personIDByPhone(t, database, seed.PhoneFred)
	vitorID := personIDByPhone(t, database, seed.PhoneVitor)

	got, err := svc.Student(context.Background(), fredID, vitorID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Vitor" || got.Suggested != "keep" {
		t.Fatalf("%+v", got)
	}
	if len(got.LastLoads) == 0 || got.LastLoads[0].ExerciseName != "Supino" || got.LastLoads[0].LoadKg != 40 {
		t.Fatalf("loads %+v", got.LastLoads)
	}
}

func TestStudentCardNudgeForJose(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, time.Now)
	fredID := personIDByPhone(t, database, seed.PhoneFred)
	joseID := personIDByPhone(t, database, seed.PhoneJose)

	got, err := svc.Student(context.Background(), fredID, joseID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Jose" || got.Suggested != "nudge" {
		t.Fatalf("%+v", got)
	}
}

func TestStudentCardNotFoundOutsideStudio(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, time.Now)
	fredID := personIDByPhone(t, database, seed.PhoneFred)
	err := func() error {
		_, e := svc.Student(context.Background(), fredID, "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee")
		return e
	}()
	if err != ErrNotFound {
		t.Fatalf("got %v want %v", err, ErrNotFound)
	}
}

func TestStudentForbiddenAsStudentCaller(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, time.Now)
	vitorID := personIDByPhone(t, database, seed.PhoneVitor)
	_, err := svc.Student(context.Background(), vitorID, vitorID)
	if err != ErrForbidden {
		t.Fatalf("got %v want %v", err, ErrForbidden)
	}
}

func TestWeekForbiddenAsStudent(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, time.Now)
	vitorID := personIDByPhone(t, database, seed.PhoneVitor)
	_, err := svc.Week(context.Background(), vitorID, "")
	if err != ErrForbidden {
		t.Fatalf("got %v want %v", err, ErrForbidden)
	}
}

func itemByName(t *testing.T, items []WeekItem, name string) WeekItem {
	t.Helper()
	for _, it := range items {
		if it.Name == name {
			return it
		}
	}
	t.Fatalf("missing %s in %+v", name, items)
	return WeekItem{}
}

func insertFinishedSession(t *testing.T, database *sql.DB, personID string, effort int) {
	t.Helper()
	if _, err := database.Exec(`
		INSERT INTO workout_sessions (
			person_id, studio_id, prescription_id, client_id, started_at, finished_at, effort
		)
		SELECT person_id, studio_id, id, gen_random_uuid(), now() - interval '1 hour', now(), $2
		FROM prescriptions
		WHERE person_id = $1 AND for_date = current_date AND status = 'published'`,
		personID, effort,
	); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = database.Exec(`DELETE FROM workout_sessions WHERE person_id = $1`, personID)
	})
}

func clonePublishedOnDate(t *testing.T, database *sql.DB, personID, forDate string) {
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
	t.Cleanup(func() {
		_, _ = database.Exec(`
			DELETE FROM prescriptions
			WHERE person_id = $1 AND for_date = $2::date`,
			personID, forDate,
		)
	})
}
