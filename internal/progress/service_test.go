package progress

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
	if err := database.QueryRow(`SELECT id::text FROM people WHERE phone = $1`, phone).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func bondIDByPhone(t *testing.T, database *sql.DB, phone string) string {
	t.Helper()
	var id string
	if err := database.QueryRow(`
		SELECT b.id::text
		FROM bonds b
		JOIN people p ON p.id = b.person_id
		WHERE p.phone = $1 AND p.active_bond_id = b.id`,
		phone,
	).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestLeagueXPStaysOnTheBond(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, time.Now)
	ctx := context.Background()

	vitorID := personIDByPhone(t, database, seed.PhoneVitor)
	huanID := personIDByPhone(t, database, seed.PhoneHuan)
	vitorBond := bondIDByPhone(t, database, seed.PhoneVitor)
	huanBond := bondIDByPhone(t, database, seed.PhoneHuan)
	joseBond := bondIDByPhone(t, database, seed.PhoneJose)

	if _, err := database.Exec(`DELETE FROM xp_ledger WHERE bond_id IN ($1, $2, $3)`, vitorBond, huanBond, joseBond); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO xp_ledger (bond_id, amount, reason) VALUES ($1, 100, 'session')`, vitorBond); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO xp_ledger (bond_id, amount, reason) VALUES ($1, 40, 'session')`, huanBond); err != nil {
		t.Fatal(err)
	}

	vitor, err := svc.Get(ctx, vitorID)
	if err != nil {
		t.Fatal(err)
	}
	if vitor.XPTotal != 100 {
		t.Fatalf("vitor xp_total %d want 100", vitor.XPTotal)
	}

	huan, err := svc.Get(ctx, huanID)
	if err != nil {
		t.Fatal(err)
	}
	if huan.XPTotal != 40 {
		t.Fatalf("huan xp_total %d want 40 (vitor's 100 must stay on vitor's bond)", huan.XPTotal)
	}

	byName := map[string]LeagueRow{}
	var meCount int
	for _, row := range vitor.League {
		byName[row.Name] = row
		if row.Me {
			meCount++
		}
	}
	if len(vitor.League) != 10 {
		t.Fatalf("league len %d want 10 (turma inteira, liga corta em 10)", len(vitor.League))
	}
	if meCount != 1 || !byName["Vitor"].Me {
		t.Fatalf("league me %+v", vitor.League)
	}
	if byName["Vitor"].XPTotal != 100 {
		t.Fatalf("vitor league xp %d", byName["Vitor"].XPTotal)
	}
	if byName["Huan"].XPTotal != 40 {
		t.Fatalf("huan league xp %d want 40, not vitor+huan", byName["Huan"].XPTotal)
	}
	if _, ok := byName["Fred"]; ok {
		t.Fatal("owner must not appear in the student liga")
	}
	if vitor.League[0].Name != "Vitor" || vitor.League[1].Name != "Huan" {
		t.Fatalf("order %+v", vitor.League)
	}
	if vitor.League[2].XPTotal != 0 {
		t.Fatalf("terceiro %+v want 0 XP (quem tem zero entra na liga)", vitor.League[2])
	}
}
