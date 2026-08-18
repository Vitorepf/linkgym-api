package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Vitorepf/linkgym-api/internal/seed"
)

func TestCodeWithInviteReturnsTimeNameFred(t *testing.T) {
	a := testAPI(t)
	const phone = "+5511900000099"
	const invite = "FRED-NOVO"

	if _, err := a.db.Exec(`
		INSERT INTO invites (studio_id, created_by_person_id, phone, code, expires_at)
		SELECT s.id, owner.id, $1, $2, now() + interval '1 day'
		FROM people owner
		JOIN studios s ON s.owner_person_id = owner.id
		WHERE owner.phone = $3
		ON CONFLICT (code) DO UPDATE
		SET phone = EXCLUDED.phone,
		    expires_at = EXCLUDED.expires_at,
		    accepted_at = NULL,
		    accepted_person_id = NULL`,
		phone, invite, seed.PhoneFred,
	); err != nil {
		t.Fatal(err)
	}

	body := bytes.NewBufferString(`{"phone":"+5511900000099","invite_code":"FRED-NOVO"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/code", body)
	rec := httptest.NewRecorder()
	a.requestCode(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}

	var out struct {
		OK   bool `json:"ok"`
		Time *struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Accent string `json:"accent_color"`
		} `json:"time"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Time == nil {
		t.Fatalf("missing time: %s", rec.Body.String())
	}
	if out.Time.Name != "Fred" {
		t.Fatalf("time.name = %q, want Fred", out.Time.Name)
	}
	if out.Time.ID == "" || out.Time.Accent == "" {
		t.Fatalf("time = %+v", out.Time)
	}
}

type meFlags struct {
	OnboardingComplete bool `json:"onboarding_complete"`
	CommitmentComplete bool `json:"commitment_complete"`
	Debut              bool `json:"debut"`
}

func getMeFlags(t *testing.T, a *api, token string) meFlags {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	a.me(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("me status %d body %s", rec.Code, rec.Body.String())
	}
	var got meFlags
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestMeFlagsFromBondAndSessions(t *testing.T) {
	a := testAPI(t)
	if _, err := a.db.Exec(`
		UPDATE bonds SET onboarding = '{}'::jsonb, commitment_at = NULL
		FROM people p WHERE bonds.person_id = p.id AND p.phone = $1`,
		seed.PhoneVitor,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`
		DELETE FROM workout_sessions
		WHERE person_id = (SELECT id FROM people WHERE phone = $1)`,
		seed.PhoneVitor,
	); err != nil {
		t.Fatal(err)
	}

	token := loginToken(t, a, seed.PhoneVitor)
	got := getMeFlags(t, a, token)
	if got.OnboardingComplete {
		t.Fatal("empty onboarding must not be complete")
	}
	if got.CommitmentComplete {
		t.Fatal("null commitment_at must not be complete")
	}
	if !got.Debut {
		t.Fatalf("debut = false, want true with zero finished sessions: %+v", got)
	}

	if _, err := a.db.Exec(`
		UPDATE bonds SET onboarding = '{"experience":"training"}'::jsonb
		FROM people p WHERE bonds.person_id = p.id AND p.phone = $1`,
		seed.PhoneVitor,
	); err != nil {
		t.Fatal(err)
	}
	got = getMeFlags(t, a, token)
	if !got.OnboardingComplete {
		t.Fatal("onboarding with experience key must be complete")
	}

	if _, err := a.db.Exec(`
		UPDATE bonds SET commitment_at = now()
		FROM people p WHERE bonds.person_id = p.id AND p.phone = $1`,
		seed.PhoneVitor,
	); err != nil {
		t.Fatal(err)
	}
	got = getMeFlags(t, a, token)
	if !got.CommitmentComplete {
		t.Fatal("commitment_at set must be complete")
	}
}

func TestOnboardingPutWritesJSON(t *testing.T) {
	a := testAPI(t)
	token := loginToken(t, a, seed.PhoneVitor)
	body := bytes.NewBufferString(`{"experience":"never","days_per_week":4,"pain":false}`)
	req := httptest.NewRequest(http.MethodPut, "/v1/onboarding", body)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	a.withPerson(a.onboardingPut)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}

	var experience string
	var days int
	if err := a.db.QueryRow(`
		SELECT b.onboarding->>'experience', (b.onboarding->>'days_per_week')::int
		FROM bonds b
		JOIN people p ON p.id = b.person_id
		WHERE p.phone = $1 AND p.active_bond_id = b.id`,
		seed.PhoneVitor,
	).Scan(&experience, &days); err != nil {
		t.Fatal(err)
	}
	if experience != "never" || days != 4 {
		t.Fatalf("onboarding experience=%s days=%d", experience, days)
	}

	got := getMeFlags(t, a, token)
	if !got.OnboardingComplete {
		t.Fatal("PUT onboarding must set onboarding_complete")
	}
}

func TestCommitmentLivesOnBond(t *testing.T) {
	a := testAPI(t)
	token := loginToken(t, a, seed.PhoneVitor)
	t.Cleanup(func() {
		_, _ = a.db.Exec(`
			UPDATE bonds SET
				commitment_text = NULL,
				commitment_at = NULL,
				onboarding = '{"experience":"training","days_per_week":3,"pain":false}'::jsonb
			FROM people p
			WHERE bonds.person_id = p.id AND p.phone = $1`,
			seed.PhoneVitor,
		)
	})

	bad := bytes.NewBufferString(`{"days_per_week":1}`)
	badReq := httptest.NewRequest(http.MethodPut, "/v1/commitment", bad)
	badReq.Header.Set("Authorization", "Bearer "+token)
	badRec := httptest.NewRecorder()
	a.withPerson(a.commitmentPut)(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("invalid status %d body %s", badRec.Code, badRec.Body.String())
	}
	if !bytes.Contains(badRec.Body.Bytes(), []byte(`"error":"invalido"`)) {
		t.Fatalf("invalid body %s", badRec.Body.String())
	}

	body := bytes.NewBufferString(`{"days_per_week":3}`)
	req := httptest.NewRequest(http.MethodPut, "/v1/commitment", body)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	a.withPerson(a.commitmentPut)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}

	var text string
	if err := a.db.QueryRow(`
		SELECT b.commitment_text
		FROM bonds b
		JOIN people p ON p.id = b.person_id
		WHERE p.phone = $1 AND p.active_bond_id = b.id`,
		seed.PhoneVitor,
	).Scan(&text); err != nil {
		t.Fatal(err)
	}
	if text != "3 dias" {
		t.Fatalf("commitment_text = %q, want 3 dias", text)
	}

	got := getMeFlags(t, a, token)
	if !got.CommitmentComplete {
		t.Fatal("PUT commitment must set commitment_complete")
	}
}
