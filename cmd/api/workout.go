package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/Vitorepf/linkgym-api/internal/workout"
)

type startReq struct {
	LocalID        string `json:"local_id"`
	PrescriptionID string `json:"prescription_id"`
}

type setReq struct {
	LocalID               string  `json:"local_id"`
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
	got, err := a.workout.Start(r.Context(), sess.Person.ID, req.LocalID, req.PrescriptionID)
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
		LocalID:               req.LocalID,
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

type swapReq struct {
	FromExerciseID string `json:"from_exercise_id"`
	ToExerciseID   string `json:"to_exercise_id"`
}

func (a *api) sessionSwap(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	var req swapReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "json_invalido")
		return
	}
	err := a.workout.Swap(r.Context(), sess.Person.ID, r.PathValue("id"), req.FromExerciseID, req.ToExerciseID)
	if err != nil {
		writeWorkoutError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
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
	case errors.Is(err, workout.ErrInvalid), errors.Is(err, workout.ErrEmptySession):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "erro")
	}
}
