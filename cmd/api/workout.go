package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/Vitorepf/linkgym-api/internal/workout"
)

type startReq struct {
	ClientID       string `json:"client_id"`
	PrescriptionID string `json:"prescription_id"`
}

type setReq struct {
	ClientSetID           string  `json:"client_set_id"`
	PrescriptionItemID    string  `json:"prescription_item_id"`
	ExerciseID            string  `json:"exercise_id"`
	SwappedFromExerciseID *string `json:"swapped_from_exercise_id"`
	SetIndex              int     `json:"set_index"`
	Reps                  int     `json:"reps"`
	LoadKg                float64 `json:"load_kg"`
	RestSeconds           int     `json:"rest_seconds"`
	Effort                *int    `json:"effort"`
	PerformedAt           string  `json:"performed_at"`
}

type finishReq struct {
	Effort int `json:"effort"`
}

func (a *api) sessionStart(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	var req startReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "json_invalido")
		return
	}
	got, err := a.workout.Start(r.Context(), sess.Person.ID, req.ClientID, req.PrescriptionID)
	if err != nil {
		writeWorkoutError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, got)
}

func (a *api) sessionAddSet(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	var req setReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "json_invalido")
		return
	}
	performed := time.Time{}
	if req.PerformedAt != "" {
		t, err := time.Parse(time.RFC3339, req.PerformedAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalido")
			return
		}
		performed = t
	}
	got, err := a.workout.AddSet(r.Context(), sess.Person.ID, r.PathValue("id"), workout.SetInput{
		ClientSetID:           req.ClientSetID,
		PrescriptionItemID:    req.PrescriptionItemID,
		ExerciseID:            req.ExerciseID,
		SwappedFromExerciseID: req.SwappedFromExerciseID,
		SetIndex:              req.SetIndex,
		Reps:                  req.Reps,
		LoadKg:                req.LoadKg,
		RestSeconds:           req.RestSeconds,
		Effort:                req.Effort,
		PerformedAt:           performed,
	})
	if err != nil {
		writeWorkoutError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, got)
}

func (a *api) sessionFinish(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	var req finishReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "json_invalido")
		return
	}
	got, err := a.workout.Finish(r.Context(), sess.Person.ID, r.PathValue("id"), req.Effort)
	if err != nil {
		writeWorkoutError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, got)
}

func writeWorkoutError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, workout.ErrForbidden):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, workout.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, workout.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "erro")
	}
}
