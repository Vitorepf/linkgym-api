package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Vitorepf/linkgym-api/internal/seed"
)

func TestSessionUnauthorizedWithoutToken(t *testing.T) {
	a := testAPI(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/sessions", bytes.NewBufferString(`{}`))
	rec := httptest.NewRecorder()
	a.withPerson(a.sessionStart)(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestSessionForbiddenForOtherPerson(t *testing.T) {
	a := testAPI(t)
	vitor := loginToken(t, a, seed.PhoneVitor)
	huan := loginToken(t, a, seed.PhoneHuan)

	var prescriptionID, itemID, exerciseID string
	if err := a.db.QueryRow(`
		SELECT pr.id::text, pi.id::text, pi.exercise_id::text
		FROM prescriptions pr
		JOIN people p ON p.id = pr.person_id
		JOIN prescription_items pi ON pi.prescription_id = pr.id AND pi.position = 1
		WHERE p.phone = $1 AND pr.for_date = current_date AND pr.status = 'published'`,
		seed.PhoneVitor,
	).Scan(&prescriptionID, &itemID, &exerciseID); err != nil {
		t.Fatal(err)
	}

	clientID := fmt.Sprintf("aaaaaaaa-bbbb-4ccc-8ddd-%012d", time.Now().UnixNano()%1e12)
	startBody, _ := json.Marshal(map[string]string{
		"client_id":       clientID,
		"prescription_id": prescriptionID,
	})
	startReq := httptest.NewRequest(http.MethodPost, "/v1/sessions", bytes.NewBuffer(startBody))
	startReq.Header.Set("Authorization", "Bearer "+vitor)
	startRec := httptest.NewRecorder()
	a.withPerson(a.sessionStart)(startRec, startReq)
	if startRec.Code != http.StatusOK {
		t.Fatalf("start %d %s", startRec.Code, startRec.Body.String())
	}
	var started struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(startRec.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}

	finishReq := httptest.NewRequest(http.MethodPost, "/v1/sessions/"+started.ID+"/finish", bytes.NewBufferString(`{"effort":2}`))
	finishReq.Header.Set("Authorization", "Bearer "+huan)
	finishReq.SetPathValue("id", started.ID)
	finishRec := httptest.NewRecorder()
	a.withPerson(a.sessionFinish)(finishRec, finishReq)
	if finishRec.Code != http.StatusForbidden {
		t.Fatalf("status %d body %s", finishRec.Code, finishRec.Body.String())
	}
}
