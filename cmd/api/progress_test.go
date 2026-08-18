package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Vitorepf/linkgym-api/internal/seed"
	"github.com/Vitorepf/linkgym-api/internal/workout"
)

func TestPRSurvivesTimeNameChange(t *testing.T) {
	a := testAPI(t)
	ctx := context.Background()
	token := loginToken(t, a, seed.PhoneVitor)

	var personID, prescriptionID, itemID, exerciseID string
	if err := a.db.QueryRow(`
		SELECT p.id::text, pr.id::text, pi.id::text, pi.exercise_id::text
		FROM people p
		JOIN prescriptions pr ON pr.person_id = p.id
		JOIN prescription_items pi ON pi.prescription_id = pr.id AND pi.position = 1
		WHERE p.phone = $1 AND pr.for_date = current_date AND pr.status = 'published'`,
		seed.PhoneVitor,
	).Scan(&personID, &prescriptionID, &itemID, &exerciseID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`DELETE FROM personal_records WHERE person_id = $1`, personID); err != nil {
		t.Fatal(err)
	}

	started, err := a.workout.Start(ctx, personID, fmt.Sprintf("aaaaaaaa-bbbb-4ccc-8ddd-%012d", time.Now().UnixNano()%1e12), prescriptionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.workout.AddSet(ctx, personID, started.ID, workout.SetInput{
		LocalID:            fmt.Sprintf("bbbbbbbb-cccc-4ddd-8eee-%012d", time.Now().UnixNano()%1e12),
		PrescriptionItemID: itemID,
		ExerciseID:         exerciseID,
		SetIndex:           1,
		Reps:               10,
		LoadKg:             40,
		RestSeconds:        90,
		PerformedAt:        time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.workout.Finish(ctx, personID, started.ID, 2); err != nil {
		t.Fatal(err)
	}

	if _, err := a.db.Exec(`
		UPDATE studios SET name = 'OutroNome'
		FROM people owner
		WHERE studios.owner_person_id = owner.id AND owner.phone = $1`,
		seed.PhoneFred,
	); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/records", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	a.withPerson(a.recordsGet)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("OutroNome")) {
		t.Fatalf("records leaked time name: %s", rec.Body.String())
	}

	var got struct {
		Items []struct {
			ExerciseName string  `json:"exercise_name"`
			LoadKg       float64 `json:"load_kg"`
			Reps         int     `json:"reps"`
			History      []struct {
				LoadKg float64 `json:"load_kg"`
			} `json:"history"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 || got.Items[0].ExerciseName != "Supino" || got.Items[0].LoadKg != 40 || got.Items[0].Reps != 10 {
		t.Fatalf("items %+v", got.Items)
	}
	if len(got.Items[0].History) != 1 || got.Items[0].History[0].LoadKg != 40 {
		t.Fatalf("history %+v", got.Items[0].History)
	}

	var badge string
	if err := a.db.QueryRow(`
		SELECT badge_key FROM badges
		WHERE person_id = $1 AND badge_key = 'primeiro_pr'`,
		personID,
	).Scan(&badge); err != nil {
		t.Fatalf("primeiro_pr: %v", err)
	}
}
