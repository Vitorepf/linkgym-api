package main

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Vitorepf/linkgym-api/internal/auth"
)

type codeReq struct {
	Phone      string `json:"phone"`
	InviteCode string `json:"invite_code"`
}

type verifyReq struct {
	Phone      string `json:"phone"`
	Code       string `json:"code"`
	InviteCode string `json:"invite_code"`
}

func (a *api) requestCode(w http.ResponseWriter, r *http.Request) {
	var req codeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "json_invalido")
		return
	}
	devCode, studio, err := a.auth.RequestCode(r.Context(), req.Phone, req.InviteCode)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	out := map[string]any{"ok": true}
	if devCode != "" {
		out["dev_code"] = devCode
	}
	if studio != nil {
		out["studio"] = studio
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *api) verify(w http.ResponseWriter, r *http.Request) {
	var req verifyReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "json_invalido")
		return
	}
	session, err := a.auth.Verify(r.Context(), req.Phone, req.Code, req.InviteCode)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token":  session.Token,
		"person": session.Person,
		"studio": session.Studio,
	})
}

func (a *api) me(w http.ResponseWriter, r *http.Request) {
	session, err := a.auth.Me(r.Context(), bearer(r))
	if err != nil {
		writeAuthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"person":              session.Person,
		"studio":              session.Studio,
		"onboarding_complete": session.OnboardingComplete,
		"commitment_complete": session.CommitmentComplete,
		"debut":               session.Debut,
	})
}

func (a *api) logout(w http.ResponseWriter, r *http.Request) {
	_ = a.auth.Logout(r.Context(), bearer(r))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func writeAuthError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrUnauthorized):
		writeError(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, auth.ErrPhoneInvalid),
		errors.Is(err, auth.ErrInviteRequired),
		errors.Is(err, auth.ErrInviteInvalid),
		errors.Is(err, auth.ErrCodeInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "erro")
	}
}
