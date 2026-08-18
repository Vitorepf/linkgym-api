package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Vitorepf/linkgym-api/internal/auth"
	"github.com/Vitorepf/linkgym-api/internal/config"
	"github.com/Vitorepf/linkgym-api/internal/db"
	"github.com/Vitorepf/linkgym-api/internal/migrate"
	"github.com/Vitorepf/linkgym-api/internal/onboard"
	"github.com/Vitorepf/linkgym-api/internal/owner"
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

func testAPI(t *testing.T) *api {
	t.Helper()
	database := openSeeded(t)
	return &api{
		db:       database,
		auth:     auth.New(database, "test-pepper", true),
		today:    today.New(database, time.Now),
		owner:    owner.New(database, time.Now),
		progress: progress.New(database, time.Now),
		publish:  publish.New(database, time.Now),
		workout:  workout.New(database, time.Now),
		onboard:  onboard.New(database, time.Now),
	}
}

func loginToken(t *testing.T, a *api, phone string) string {
	t.Helper()
	ctx := context.Background()
	if _, _, err := a.auth.RequestCode(ctx, phone, ""); err != nil {
		t.Fatal(err)
	}
	session, err := a.auth.Verify(ctx, phone, auth.DevCode, "")
	if err != nil {
		t.Fatal(err)
	}
	return session.Token
}

func TestOwnerHomeForbiddenAsStudent(t *testing.T) {
	a := testAPI(t)
	token := loginToken(t, a, "+5511900000002")
	req := httptest.NewRequest(http.MethodGet, "/v1/owner/home", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	a.withPerson(a.ownerHome)(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"error":"nao_autorizado"`)) {
		t.Fatalf("body %s", rec.Body.String())
	}
}

func TestOwnerReturnsForbiddenAsStudent(t *testing.T) {
	a := testAPI(t)
	token := loginToken(t, a, seed.PhoneVitor)
	req := httptest.NewRequest(http.MethodGet, "/v1/owner/returns", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	a.withPerson(a.ownerReturns)(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestOwnerApplyReturnForbiddenAsStudent(t *testing.T) {
	a := testAPI(t)
	token := loginToken(t, a, seed.PhoneVitor)
	req := httptest.NewRequest(http.MethodPost, "/v1/owner/returns/aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee/apply", bytes.NewBufferString(`{"bump_kg":2.5}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.SetPathValue("alert_id", "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee")
	rec := httptest.NewRecorder()
	a.withPerson(a.ownerApplyReturn)(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestOwnerAttentionForbiddenAsStudent(t *testing.T) {
	a := testAPI(t)
	token := loginToken(t, a, seed.PhoneVitor)
	req := httptest.NewRequest(http.MethodGet, "/v1/owner/attention", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	a.withPerson(a.ownerAttention)(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestOwnerApplyAttentionForbiddenAsStudent(t *testing.T) {
	a := testAPI(t)
	token := loginToken(t, a, seed.PhoneVitor)
	req := httptest.NewRequest(http.MethodPost, "/v1/owner/attention/aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee/apply", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.SetPathValue("id", "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee")
	rec := httptest.NewRecorder()
	a.withPerson(a.ownerApplyAttention)(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestOwnerWeekForbiddenAsStudent(t *testing.T) {
	a := testAPI(t)
	token := loginToken(t, a, seed.PhoneVitor)
	req := httptest.NewRequest(http.MethodGet, "/v1/owner/week", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	a.withPerson(a.ownerWeek)(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"error":"nao_autorizado"`)) {
		t.Fatalf("body %s", rec.Body.String())
	}
}

func TestOwnerWeekApproveForbiddenAsStudent(t *testing.T) {
	a := testAPI(t)
	token := loginToken(t, a, seed.PhoneVitor)
	req := httptest.NewRequest(http.MethodPost, "/v1/owner/week/approve", bytes.NewBufferString(`{"person_ids":[]}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	a.withPerson(a.ownerWeekApprove)(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestOwnerStudentForbiddenAsStudent(t *testing.T) {
	a := testAPI(t)
	token := loginToken(t, a, seed.PhoneVitor)
	req := httptest.NewRequest(http.MethodGet, "/v1/owner/students/aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.SetPathValue("id", "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee")
	rec := httptest.NewRecorder()
	a.withPerson(a.ownerStudent)(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestOwnerStudentReturnsCommitmentTextAfterPut(t *testing.T) {
	a := testAPI(t)
	vitorTok := loginToken(t, a, seed.PhoneVitor)
	fredTok := loginToken(t, a, seed.PhoneFred)
	var vitorID string
	if err := a.db.QueryRow(`SELECT id::text FROM people WHERE phone = $1`, seed.PhoneVitor).Scan(&vitorID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = a.db.Exec(`
			UPDATE bonds SET commitment_text = NULL, commitment_at = NULL
			WHERE person_id = $1`,
			vitorID,
		)
	})

	put := httptest.NewRequest(http.MethodPut, "/v1/commitment", bytes.NewBufferString(`{"days_per_week":3}`))
	put.Header.Set("Authorization", "Bearer "+vitorTok)
	putRec := httptest.NewRecorder()
	a.withPerson(a.commitmentPut)(putRec, put)
	if putRec.Code != http.StatusOK {
		t.Fatalf("put status %d body %s", putRec.Code, putRec.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/owner/students/"+vitorID, nil)
	req.Header.Set("Authorization", "Bearer "+fredTok)
	req.SetPathValue("id", vitorID)
	rec := httptest.NewRecorder()
	a.withPerson(a.ownerStudent)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var got struct {
		CommitmentText *string `json:"commitment_text"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.CommitmentText == nil || *got.CommitmentText != "3 dias" {
		t.Fatalf("body %s", rec.Body.String())
	}
}

func TestOwnerStudentNotFound(t *testing.T) {
	a := testAPI(t)
	token := loginToken(t, a, seed.PhoneFred)
	req := httptest.NewRequest(http.MethodGet, "/v1/owner/students/aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.SetPathValue("id", "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee")
	rec := httptest.NewRecorder()
	a.withPerson(a.ownerStudent)(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestOwnerApplyReturnNotFound(t *testing.T) {
	a := testAPI(t)
	token := loginToken(t, a, seed.PhoneFred)
	req := httptest.NewRequest(http.MethodPost, "/v1/owner/returns/aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee/apply", bytes.NewBufferString(`{"bump_kg":0}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.SetPathValue("alert_id", "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee")
	rec := httptest.NewRecorder()
	a.withPerson(a.ownerApplyReturn)(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestTodayServesVitorLoad(t *testing.T) {
	a := testAPI(t)
	token := loginToken(t, a, "+5511900000002")
	req := httptest.NewRequest(http.MethodGet, "/v1/today", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	a.withPerson(a.todayGet)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Prescription *struct {
			Items []struct {
				LoadKg float64 `json:"load_kg"`
			} `json:"items"`
		} `json:"prescription"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Prescription == nil || len(got.Prescription.Items) == 0 || got.Prescription.Items[0].LoadKg != 40 {
		t.Fatalf("body %s", rec.Body.String())
	}
}

func TestTodayUnauthorizedWithoutToken(t *testing.T) {
	a := testAPI(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/today", nil)
	rec := httptest.NewRecorder()
	a.withPerson(a.todayGet)(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestReadinessPutUnauthorized(t *testing.T) {
	a := testAPI(t)
	req := httptest.NewRequest(http.MethodPut, "/v1/today/readiness", bytes.NewBufferString(`{"energy":4,"soreness":2,"sleep":4}`))
	rec := httptest.NewRecorder()
	a.withPerson(a.todayReadinessPut)(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestReadinessPutInvalidBody(t *testing.T) {
	a := testAPI(t)
	token := loginToken(t, a, "+5511900000002")
	body := bytes.NewBufferString(`{"energy":0,"soreness":3,"sleep":3}`)
	req := httptest.NewRequest(http.MethodPut, "/v1/today/readiness", body)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	a.withPerson(a.todayReadinessPut)(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestReadinessPutReturnsScore(t *testing.T) {
	a := testAPI(t)
	token := loginToken(t, a, "+5511900000002")
	body := bytes.NewBufferString(`{"energy":4,"soreness":2,"sleep":4}`)
	req := httptest.NewRequest(http.MethodPut, "/v1/today/readiness", body)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	a.withPerson(a.todayReadinessPut)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Score    int    `json:"score"`
		Energy   int    `json:"energy"`
		Soreness int    `json:"soreness"`
		Sleep    int    `json:"sleep"`
		Label    string `json:"label"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Score != 80 || got.Energy != 4 || got.Soreness != 2 || got.Sleep != 4 {
		t.Fatalf("%+v", got)
	}
	if got.Label != "Pode ir com carga" {
		t.Fatalf("%s", got.Label)
	}
}
