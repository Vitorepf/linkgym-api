package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Vitorepf/linkgym-api/internal/auth"
)

type ctxKey int

const sessionKey ctxKey = 1

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (a *api) withPerson(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, err := a.auth.Me(r.Context(), bearer(r))
		if err != nil {
			writeAuthError(w, err)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), sessionKey, session)))
	}
}

func sessionFrom(r *http.Request) *auth.Session {
	s, _ := r.Context().Value(sessionKey).(*auth.Session)
	return s
}
