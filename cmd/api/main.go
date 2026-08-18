package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/Vitorepf/linkgym-api/internal/auth"
	"github.com/Vitorepf/linkgym-api/internal/config"
	"github.com/Vitorepf/linkgym-api/internal/db"
	"github.com/Vitorepf/linkgym-api/internal/migrate"
	"github.com/Vitorepf/linkgym-api/internal/onboard"
	"github.com/Vitorepf/linkgym-api/internal/owner"
	"github.com/Vitorepf/linkgym-api/internal/progress"
	"github.com/Vitorepf/linkgym-api/internal/today"
	"github.com/Vitorepf/linkgym-api/internal/workout"
)

func main() {
	config.LoadDotEnv(".env")

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		log.Fatal("DATABASE_URL is required")
	}
	database, err := db.Open(url)
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()
	if err := migrate.Up(database); err != nil {
		log.Fatal(err)
	}

	addr := ":" + config.Getenv("PORT", "8080")
	dev := config.Getenv("ENV", "development") == "development"
	api := &api{
		db:       database,
		auth:     auth.New(database, config.Getenv("AUTH_PEPPER", ""), dev),
		today:    today.New(database, time.Now),
		owner:    owner.New(database, time.Now),
		progress: progress.New(database, time.Now),
		workout:  workout.New(database, time.Now),
		onboard:  onboard.New(database, time.Now),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", api.health)
	mux.HandleFunc("POST /v1/auth/code", api.requestCode)
	mux.HandleFunc("POST /v1/auth/verify", api.verify)
	mux.HandleFunc("GET /v1/me", api.me)
	mux.HandleFunc("POST /v1/auth/logout", api.logout)
	mux.HandleFunc("GET /v1/today", api.withPerson(api.todayGet))
	mux.HandleFunc("PUT /v1/today/readiness", api.withPerson(api.todayReadinessPut))
	mux.HandleFunc("GET /v1/owner/home", api.withPerson(api.ownerHome))
	mux.HandleFunc("GET /v1/owner/returns", api.withPerson(api.ownerReturns))
	mux.HandleFunc("POST /v1/owner/returns/{alert_id}/apply", api.withPerson(api.ownerApplyReturn))
	mux.HandleFunc("POST /v1/sessions", api.withPerson(api.sessionStart))
	mux.HandleFunc("POST /v1/sessions/{id}/sets", api.withPerson(api.sessionAddSet))
	mux.HandleFunc("POST /v1/sessions/{id}/swap", api.withPerson(api.sessionSwap))
	mux.HandleFunc("POST /v1/sessions/{id}/finish", api.withPerson(api.sessionFinish))
	mux.HandleFunc("GET /v1/progress", api.withPerson(api.progressGet))
	mux.HandleFunc("GET /v1/records", api.withPerson(api.recordsGet))
	mux.HandleFunc("PUT /v1/onboarding", api.withPerson(api.onboardingPut))

	server := &http.Server{
		Addr:              addr,
		Handler:           withCORS(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("linkgym-api listening on %s", addr)
	log.Fatal(server.ListenAndServe())
}

type api struct {
	db       *sql.DB
	auth     *auth.Service
	today    *today.Service
	owner    *owner.Service
	progress *progress.Service
	workout  *workout.Service
	onboard  *onboard.Service
}

func (a *api) health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	status := "ok"
	if a.db != nil {
		if err := a.db.Ping(); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status":  "down",
				"service": "linkgym-api",
				"db":      "down",
			})
			return
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  status,
		"service": "linkgym-api",
		"db":      "ok",
	})
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
