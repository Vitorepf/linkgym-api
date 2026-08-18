package main

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Vitorepf/linkgym-api/internal/today"
)

func (a *api) todayGet(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	got, err := a.today.Today(r.Context(), sess.Person.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "erro")
		return
	}
	writeJSON(w, http.StatusOK, got)
}

func (a *api) comebackComplete(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	err := a.today.CompleteComeback(r.Context(), sess.Person.ID, r.PathValue("id"))
	if err != nil {
		if errors.Is(err, today.ErrNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "erro")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type prontidaoReq struct {
	Energy   int `json:"energy"`
	Soreness int `json:"soreness"`
	Sleep    int `json:"sleep"`
}

func (a *api) todayProntidaoPut(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	var req prontidaoReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "json_invalido")
		return
	}
	got, err := a.today.PutProntidao(r.Context(), sess.Person.ID, req.Energy, req.Soreness, req.Sleep)
	if err != nil {
		if errors.Is(err, today.ErrProntidaoInvalid) {
			writeError(w, http.StatusBadRequest, "invalido")
			return
		}
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
