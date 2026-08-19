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
	_, _, err := svc.RequestCode(context.Background(), "+5511999888777", "")
	if err != ErrInviteRequired {
		t.Fatalf("err = %v", err)
	}
}

func TestVitorLogsInWithDevCode(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	code, _, err := svc.RequestCode(ctx, seed.PhoneVitor, "")
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
	if session.Time.Name != seed.TimeName {
		t.Fatalf("time = %s", session.Time.Name)
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
	if _, _, err := svc.RequestCode(ctx, seed.PhoneFred, ""); err != nil {
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
	if _, _, err := svc.RequestCode(ctx, seed.PhoneFred, ""); err != nil {
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

// O TELEFONE É O CONVITE.
//
// O personal digita o número do aluno e manda a mensagem pelo WhatsApp dele. A partir
// desse instante o número está autorizado, e o aluno entra digitando o próprio número —
// sem código para decorar, ditar na porta da academia ou perder na rolagem da conversa.
//
// Este teste é o contrato dessa promessa. Se alguém voltar a exigir o código de quem já
// foi chamado pelo nome, ele quebra aqui e não na mão de um aluno.
func TestNumeroChamadoEntraSemCodigo(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	const fone = "+5511955512345"

	limpar := func() {
		_, _ = svc.db.ExecContext(ctx, `UPDATE people SET active_bond_id = NULL WHERE phone = $1`, fone)
		_, _ = svc.db.ExecContext(ctx, `DELETE FROM auth_sessions WHERE person_id IN (SELECT id FROM people WHERE phone = $1)`, fone)
		_, _ = svc.db.ExecContext(ctx, `DELETE FROM bonds WHERE person_id IN (SELECT id FROM people WHERE phone = $1)`, fone)
		_, _ = svc.db.ExecContext(ctx, `DELETE FROM login_codes WHERE phone = $1`, fone)
		_, _ = svc.db.ExecContext(ctx, `DELETE FROM invites WHERE phone = $1`, fone)
		_, _ = svc.db.ExecContext(ctx, `DELETE FROM people WHERE phone = $1`, fone)
	}
	limpar()
	t.Cleanup(limpar)

	var studioID, fredID string
	if err := svc.db.QueryRowContext(ctx, `SELECT id::text FROM studios WHERE name = $1`, seed.TimeName).Scan(&studioID); err != nil {
		t.Fatal(err)
	}
	if err := svc.db.QueryRowContext(ctx, `SELECT id::text FROM people WHERE phone = $1`, seed.PhoneFred).Scan(&fredID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.ExecContext(ctx, `
		INSERT INTO invites (studio_id, created_by_person_id, phone, code, expires_at)
		VALUES ($1, $2, $3, $4, now() + interval '1 year')`,
		studioID, fredID, fone, "TST-SEMCODIGO",
	); err != nil {
		t.Fatal(err)
	}

	// Convite em branco, de propósito: é assim que a tela de entrada chama a API quando o
	// aluno só digitou o número dele. E a marca do estúdio tem que voltar JUNTO — é a
	// primeira coisa que ele vê do personal, antes de existir sessão.
	code, tm, err := svc.RequestCode(ctx, fone, "")
	if err != nil {
		t.Fatalf("número já chamado ainda exigiu convite: %v", err)
	}
	if tm == nil || tm.Name != seed.TimeName {
		t.Fatalf("a marca do estúdio não veio na porta: %+v", tm)
	}

	session, err := svc.Verify(ctx, fone, code, "")
	if err != nil {
		t.Fatalf("verify sem convite: %v", err)
	}
	if session.Time.Name != seed.TimeName {
		t.Fatalf("time = %s", session.Time.Name)
	}
	if session.Person.Role != "student" {
		t.Fatalf("role = %s", session.Person.Role)
	}
}
