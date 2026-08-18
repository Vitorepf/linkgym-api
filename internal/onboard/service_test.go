package onboard

import (
	"context"
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

func TestOnboardingPainCreatesAttention(t *testing.T) {
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

	svc := New(database, time.Now)
	ctx := context.Background()

	var personID string
	if err := database.QueryRow(`SELECT id::text FROM people WHERE phone = $1`, seed.PhoneVitor).Scan(&personID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = database.Exec(`DELETE FROM attention_items WHERE person_id = $1 AND reason = 'pain_flag'`, personID)
	})

	if err := svc.Put(ctx, personID, Answers{
		Experience:  "before",
		DaysPerWeek: 3,
		Pain:        true,
	}); err != nil {
		t.Fatal(err)
	}

	var reason string
	var rank int
	if err := database.QueryRow(`
		SELECT reason, rank FROM attention_items
		WHERE person_id = $1 AND for_date = current_date`,
		personID,
	).Scan(&reason, &rank); err != nil {
		t.Fatal(err)
	}
	if reason != "pain_flag" {
		t.Fatalf("reason = %s, want pain_flag", reason)
	}
	if rank != 1 {
		t.Fatalf("rank = %d, want 1", rank)
	}
}

func TestOnboardingWritesBody(t *testing.T) {
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

	svc := New(database, time.Now)
	var personID string
	if err := database.QueryRow(`SELECT id::text FROM people WHERE phone = $1`, seed.PhoneHuan).Scan(&personID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = database.Exec(`
			UPDATE bonds SET onboarding = '{"experience":"training","days_per_week":3,"pain":false}'::jsonb
			FROM people p WHERE bonds.person_id = p.id AND p.phone = $1`,
			seed.PhoneHuan,
		)
	})

	if err := svc.Put(context.Background(), personID, Answers{
		Experience:  "training",
		DaysPerWeek: 4,
		Pain:        false,
		Sex:         "male",
		HeightCm:    178,
		WeightKg:    82,
	}); err != nil {
		t.Fatal(err)
	}

	var sex string
	var height int
	var weight float64
	if err := database.QueryRow(`
		SELECT onboarding->>'sex',
		       (onboarding->>'height_cm')::int,
		       (onboarding->>'weight_kg')::float
		FROM bonds WHERE person_id = $1 AND role = 'student'`,
		personID,
	).Scan(&sex, &height, &weight); err != nil {
		t.Fatal(err)
	}
	if sex != "male" || height != 178 || weight != 82 {
		t.Fatalf("body sex=%s height=%d weight=%v", sex, height, weight)
	}

	if err := svc.Put(context.Background(), personID, Answers{
		Experience:  "training",
		DaysPerWeek: 4,
		Pain:        false,
		Sex:         "male",
		HeightCm:    90,
		WeightKg:    82,
	}); err == nil {
		t.Fatal("invalid height must fail")
	}

	if err := svc.Put(context.Background(), personID, Answers{
		Experience:  "training",
		DaysPerWeek: 4,
		Pain:        false,
		Sex:         "alien",
		HeightCm:    178,
		WeightKg:    82,
	}); err == nil {
		t.Fatal("invalid sex must fail")
	}
}
