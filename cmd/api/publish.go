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

func (a *api) modelCreate(w http.ResponseWriter, r *http.Request) {
	if !a.requireOwner(w, r) {
		return
	}
	var in publish.NewModel
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "json_invalido")
		return
	}
	model, err := a.publish.CreateModel(r.Context(), sessionFrom(r).Person.ID, in)
	if err != nil {
		writePublishError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, model)
}

func (a *api) exercisesList(w http.ResponseWriter, r *http.Request) {
	if !a.requireOwner(w, r) {
		return
	}
	items, err := a.publish.ListExercises(r.Context(), sessionFrom(r).Person.ID)
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

type swapItemReq struct {
	ExerciseID string `json:"exercise_id"`
}

func (a *api) swapPrescriptionItem(w http.ResponseWriter, r *http.Request) {
	if !a.requireOwner(w, r) {
		return
	}
	var req swapItemReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "json_invalido")
		return
	}
	err := a.publish.SwapItem(r.Context(), sessionFrom(r).Person.ID,
		r.PathValue("id"), r.PathValue("item_id"), req.ExerciseID)
	if err != nil {
		writePublishError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type publishReq struct {
	PrescriptionID string   `json:"prescription_id"`
	AlsoPersonIDs  []string `json:"also_person_ids"`
	// A frase do personal para o dia, na voz dele. Opcional: ausente ou vazia, o
	// /v1/today devolve vazio e a tela do aluno nao desenha bloco nenhum.
	CoachLine string `json:"coach_line"`
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
	err := a.publish.Publish(r.Context(), sessionFrom(r).Person.ID, req.PrescriptionID, req.AlsoPersonIDs, req.CoachLine)
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

// nomeReq: os dois PATCH de nome mandam a mesma coisa, entao e um struct so.
type nomeReq struct {
	Name string `json:"name"`
}

func (a *api) exerciseCreate(w http.ResponseWriter, r *http.Request) {
	if !a.requireOwner(w, r) {
		return
	}
	var req nomeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "json_invalido")
		return
	}
	got, err := a.publish.CreateExercise(r.Context(), sessionFrom(r).Person.ID, req.Name)
	if err != nil {
		writePublishError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, got)
}

func (a *api) exerciseRename(w http.ResponseWriter, r *http.Request) {
	if !a.requireOwner(w, r) {
		return
	}
	var req nomeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "json_invalido")
		return
	}
	err := a.publish.RenameExercise(r.Context(), sessionFrom(r).Person.ID, r.PathValue("id"), req.Name)
	if err != nil {
		writePublishError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type modelItemsReq struct {
	Items []publish.NewModelItem `json:"items"`
}

func (a *api) modelItemsPut(w http.ResponseWriter, r *http.Request) {
	if !a.requireOwner(w, r) {
		return
	}
	var req modelItemsReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "json_invalido")
		return
	}
	got, err := a.publish.ReplaceModelItems(r.Context(), sessionFrom(r).Person.ID, r.PathValue("id"), req.Items)
	if err != nil {
		writePublishError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, got)
}

func (a *api) modelRename(w http.ResponseWriter, r *http.Request) {
	if !a.requireOwner(w, r) {
		return
	}
	var req nomeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "json_invalido")
		return
	}
	err := a.publish.RenameModel(r.Context(), sessionFrom(r).Person.ID, r.PathValue("id"), req.Name)
	if err != nil {
		writePublishError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// modelDelete arquiva. O verbo do HTTP e DELETE porque e o que o personal fez; o que
// acontece com a Prescricao de quem ja treinou esta em publish.ArchiveModel.
func (a *api) modelDelete(w http.ResponseWriter, r *http.Request) {
	if !a.requireOwner(w, r) {
		return
	}
	if err := a.publish.ArchiveModel(r.Context(), sessionFrom(r).Person.ID, r.PathValue("id")); err != nil {
		writePublishError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
