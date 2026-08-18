package main

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Vitorepf/linkgym-api/internal/owner"
)

func (a *api) ownerReturns(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	if sess.Person.Role != "owner" {
		writeError(w, http.StatusForbidden, "nao_autorizado")
		return
	}
	items, err := a.owner.Returns(r.Context(), sess.Person.ID)
	if err != nil {
		writeOwnerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *api) ownerAttention(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	if sess.Person.Role != "owner" {
		writeError(w, http.StatusForbidden, "nao_autorizado")
		return
	}
	items, err := a.owner.Attention(r.Context(), sess.Person.ID)
	if err != nil {
		writeOwnerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *api) ownerApplyAttention(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	if sess.Person.Role != "owner" {
		writeError(w, http.StatusForbidden, "nao_autorizado")
		return
	}
	err := a.owner.Apply(r.Context(), sess.Person.ID, r.PathValue("id"))
	if err != nil {
		writeOwnerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type applyReturnReq struct {
	BumpKg float64 `json:"bump_kg"`
}

func (a *api) ownerApplyReturn(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	if sess.Person.Role != "owner" {
		writeError(w, http.StatusForbidden, "nao_autorizado")
		return
	}
	var req applyReturnReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "json_invalido")
		return
	}
	err := a.owner.ApplyReturn(r.Context(), sess.Person.ID, r.PathValue("alert_id"), req.BumpKg)
	if err != nil {
		writeOwnerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func writeOwnerError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, owner.ErrForbidden):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, owner.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, owner.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "erro")
	}
}
