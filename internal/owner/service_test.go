package owner

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
