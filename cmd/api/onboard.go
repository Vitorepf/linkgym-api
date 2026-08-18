package main

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Vitorepf/linkgym-api/internal/onboard"
)

func (a *api) onboardingPut(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	var req onboard.Answers
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "json_invalido")
		return
	}
	if err := a.onboard.Put(r.Context(), sess.Person.ID, req); err != nil {
		if errors.Is(err, onboard.ErrInvalid) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "erro")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
