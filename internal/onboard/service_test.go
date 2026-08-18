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
