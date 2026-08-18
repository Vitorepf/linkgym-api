package main

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Vitorepf/linkgym-api/internal/publish"
)

func (a *api) requireOwner(w http.ResponseWriter, r *http.Request) bool {
	if sessionFrom(r).Person.Role != "owner" {
		writeError(w, http.StatusForbidden, "nao_autorizado")
		return false
	}
	return true
}

func (a *api) modelsList(w http.ResponseWriter, r *http.Request) {
	if !a.requireOwner(w, r) {
		return
	}
	items, err := a.publish.ListModels(r.Context(), sessionFrom(r).Person.ID)
	if err != nil {
		writePublishError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *api) modelGet(w http.ResponseWriter, r *http.Request) {
	if !a.requireOwner(w, r) {
		return
	}
	got, err := a.publish.GetModel(r.Context(), sessionFrom(r).Person.ID, r.PathValue("id"))
	if err != nil {
		writePublishError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, got)
}

type draftFromLastReq struct {
	PersonID string `json:"person_id"`
	From     string `json:"from"`
}

func (a *api) draftFromLast(w http.ResponseWriter, r *http.Request) {
	if !a.requireOwner(w, r) {
		return
	}
	var req draftFromLastReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "json_invalido")
		return
	}
	got, err := a.publish.DraftFromLast(r.Context(), sessionFrom(r).Person.ID, r.PathValue("id"), req.PersonID, req.From)
	if err != nil {
		writePublishError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, got)
}

func (a *api) patchPrescriptionItem(w http.ResponseWriter, r *http.Request) {
	if !a.requireOwner(w, r) {
		return
	}
	var req publish.ItemPatch
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "json_invalido")
		return
	}
	err := a.publish.PatchItem(r.Context(), sessionFrom(r).Person.ID, r.PathValue("id"), r.PathValue("item_id"), req)
	if err != nil {
		writePublishError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type publishReq struct {
	PrescriptionID string   `json:"prescription_id"`
	AlsoPersonIDs  []string `json:"also_person_ids"`
}

func (a *api) publishPost(w http.ResponseWriter, r *http.Request) {
	if !a.requireOwner(w, r) {
		return
	}
	var req publishReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "json_invalido")
		return
	}
	if req.AlsoPersonIDs == nil {
		req.AlsoPersonIDs = []string{}
	}
	err := a.publish.Publish(r.Context(), sessionFrom(r).Person.ID, req.PrescriptionID, req.AlsoPersonIDs)
	if err != nil {
		writePublishError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func writePublishError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, publish.ErrForbidden):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, publish.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, publish.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "erro")
	}
}
