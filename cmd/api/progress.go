package main

import (
	"net/http"
)

func (a *api) progressGet(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	got, err := a.progress.Get(r.Context(), sess.Person.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "erro")
		return
	}
	writeJSON(w, http.StatusOK, got)
}

func (a *api) recordsGet(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	got, err := a.progress.Records(r.Context(), sess.Person.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "erro")
		return
	}
	writeJSON(w, http.StatusOK, got)
}
