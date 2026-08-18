package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Vitorepf/linkgym-api/internal/seed"
)

func TestModelsForbiddenAsStudent(t *testing.T) {
	a := testAPI(t)
	token := loginToken(t, a, seed.PhoneVitor)
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	a.withPerson(a.modelsList)(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"error":"nao_autorizado"`)) {
		t.Fatalf("body %s", rec.Body.String())
	}
}

func TestDraftFromLastForbiddenAsStudent(t *testing.T) {
	a := testAPI(t)
	token := loginToken(t, a, seed.PhoneVitor)
	req := httptest.NewRequest(http.MethodPost, "/v1/models/aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee/draft-from-last", bytes.NewBufferString(`{"person_id":"x"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.SetPathValue("id", "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee")
	rec := httptest.NewRecorder()
	a.withPerson(a.draftFromLast)(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestPublishForbiddenAsStudentHTTP(t *testing.T) {
	a := testAPI(t)
	token := loginToken(t, a, seed.PhoneVitor)
	req := httptest.NewRequest(http.MethodPost, "/v1/publish", bytes.NewBufferString(`{"prescription_id":"x","also_person_ids":[]}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	a.withPerson(a.publishPost)(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestPatchPublishedItemHTTPInvalid(t *testing.T) {
	a := testAPI(t)
	token := loginToken(t, a, seed.PhoneFred)
	vitorID := personIDByPhone(t, a.db, seed.PhoneVitor)

	var prID, itemID string
	if err := a.db.QueryRow(`
		SELECT pr.id::text, pi.id::text
		FROM prescriptions pr
		JOIN prescription_items pi ON pi.prescription_id = pr.id AND pi.position = 1
		WHERE pr.person_id = $1 AND pr.for_date = current_date AND pr.status = 'published'`,
		vitorID,
	).Scan(&prID, &itemID); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]any{
		"load_kg": 42.5, "planned_sets": 3, "planned_reps": "8-12",
	})
	req := httptest.NewRequest(http.MethodPatch, "/v1/prescriptions/"+prID+"/items/"+itemID, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.SetPathValue("id", prID)
	req.SetPathValue("item_id", itemID)
	rec := httptest.NewRecorder()
	a.withPerson(a.patchPrescriptionItem)(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"error":"invalido"`)) {
		t.Fatalf("body %s", rec.Body.String())
	}
}

func TestModelsListHTTP(t *testing.T) {
	a := testAPI(t)
	token := loginToken(t, a, seed.PhoneFred)
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	a.withPerson(a.modelsList)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"name":"Treino A"`)) {
		t.Fatalf("body %s", rec.Body.String())
	}
}

func personIDByPhone(t *testing.T, database *sql.DB, phone string) string {
	t.Helper()
	var id string
	if err := database.QueryRow(`SELECT id FROM people WHERE phone = $1`, phone).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
