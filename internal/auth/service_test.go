package auth

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Vitorepf/linkgym-api/internal/config"
	"github.com/Vitorepf/linkgym-api/internal/db"
	"github.com/Vitorepf/linkgym-api/internal/migrate"
	"github.com/Vitorepf/linkgym-api/internal/seed"
)

func TestMain(m *testing.M) {
	_, file, _, ok := runtime.Caller(0)
	if ok {
		dir := filepath.Dir(file)
		for i := 0; i < 6; i++ {
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

func testService(t *testing.T) *Service {
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
	return New(database, "test-pepper", true)
}

func TestUnknownPhoneNeedsInvite(t *testing.T) {
	svc := testService(t)
	_, err := svc.RequestCode(context.Background(), "+5511999888777", "")
	if err != ErrInviteRequired {
		t.Fatalf("err = %v", err)
	}
}

func TestVitorLogsInWithDevCode(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	code, err := svc.RequestCode(ctx, seed.PhoneVitor, "")
	if err != nil {
		t.Fatal(err)
	}
	if code != DevCode {
		t.Fatalf("dev code = %s", code)
	}
	session, err := svc.Verify(ctx, seed.PhoneVitor, DevCode, "")
	if err != nil {
		t.Fatal(err)
	}
	if session.Person.Name != "Vitor" {
		t.Fatalf("name = %s", session.Person.Name)
	}
	if session.Studio.Name != seed.StudioName {
		t.Fatalf("studio = %s", session.Studio.Name)
	}
	if session.Token == "" {
		t.Fatal("missing token")
	}
	me, err := svc.Me(ctx, session.Token)
	if err != nil {
		t.Fatal(err)
	}
	if me.Person.Name != "Vitor" {
		t.Fatalf("me = %s", me.Person.Name)
	}
}

func TestWrongCodeFails(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	if _, err := svc.RequestCode(ctx, seed.PhoneFred, ""); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Verify(ctx, seed.PhoneFred, "9999", "")
	if err != ErrCodeInvalid {
		t.Fatalf("err = %v", err)
	}
}

func TestFredDoesNotNeedInvite(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	if _, err := svc.RequestCode(ctx, seed.PhoneFred, ""); err != nil {
		t.Fatal(err)
	}
	session, err := svc.Verify(ctx, seed.PhoneFred, DevCode, "")
	if err != nil {
		t.Fatal(err)
	}
	if session.Person.Role != "owner" {
		t.Fatalf("role = %s", session.Person.Role)
	}
}
