package main

import "net/http"

func (a *api) todayGet(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	got, err := a.today.Today(r.Context(), sess.Person.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "erro")
		return
	}
	writeJSON(w, http.StatusOK, got)
}

func (a *api) ownerHome(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	if sess.Person.Role != "owner" {
		writeError(w, http.StatusForbidden, "nao_autorizado")
		return
	}
	got, err := a.owner.Home(r.Context(), sess.Person.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "erro")
		return
	}
	writeJSON(w, http.StatusOK, got)
}
