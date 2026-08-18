package seed

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Vitorepf/linkgym-api/internal/config"
	"github.com/Vitorepf/linkgym-api/internal/db"
	"github.com/Vitorepf/linkgym-api/internal/migrate"
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

func TestDevSeedIsIdempotent(t *testing.T) {
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

	ctx := context.Background()
	if err := Dev(ctx, database); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	if err := Dev(ctx, database); err != nil {
		t.Fatalf("second seed: %v", err)
	}

	var people, students int
	if err := database.QueryRow(`
		SELECT count(*) FROM people
		WHERE phone IN ($1, $2, $3, $4)`,
		PhoneFred, PhoneVitor, PhoneHuan, PhoneJose,
	).Scan(&people); err != nil {
		t.Fatal(err)
	}
	if people != 4 {
		t.Fatalf("people = %d, want 4", people)
	}

	if err := database.QueryRow(`
		SELECT count(*) FROM bonds b
		JOIN people p ON p.id = b.person_id
		JOIN studios s ON s.id = b.studio_id
		WHERE s.name = $1 AND b.role = 'student' AND p.name IN ('Vitor', 'Huan', 'Jose')`,
		StudioName,
	).Scan(&students); err != nil {
		t.Fatal(err)
	}
	if students != 3 {
		t.Fatalf("students = %d, want 3", students)
	}

	var studio, owner string
	if err := database.QueryRow(`
		SELECT s.name, p.name
		FROM studios s
		JOIN people p ON p.id = s.owner_person_id
		WHERE p.phone = $1`, PhoneFred,
	).Scan(&studio, &owner); err != nil {
		t.Fatal(err)
	}
	if studio != StudioName || owner != "Fred" {
		t.Fatalf("studio=%s owner=%s", studio, owner)
	}

	var loads int
	if err := database.QueryRow(`
		SELECT count(DISTINCT pi.load_kg)
		FROM prescription_items pi
		JOIN prescriptions pr ON pr.id = pi.prescription_id
		JOIN people p ON p.id = pr.person_id
		WHERE p.phone IN ($1, $2, $3) AND pi.position = 1 AND pr.status = 'published'`,
		PhoneVitor, PhoneHuan, PhoneJose,
	).Scan(&loads); err != nil {
		t.Fatal(err)
	}
	if loads != 3 {
		t.Fatalf("lote tinha %d cargas distintas no supino, want 3", loads)
	}

	var onboarded int
	if err := database.QueryRow(`
		SELECT count(*) FROM bonds b
		JOIN people p ON p.id = b.person_id
		WHERE b.role = 'student'
		  AND p.phone IN ($1, $2, $3)
		  AND b.onboarding ? 'experience'`,
		PhoneVitor, PhoneHuan, PhoneJose,
	).Scan(&onboarded); err != nil {
		t.Fatal(err)
	}
	if onboarded != 3 {
		t.Fatalf("seed students with onboarding experience = %d, want 3", onboarded)
	}
}
