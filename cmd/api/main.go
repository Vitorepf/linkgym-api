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
		db:   database,
		auth: auth.New(database, config.Getenv("AUTH_PEPPER", ""), dev),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", api.health)
	mux.HandleFunc("POST /v1/auth/code", api.requestCode)
	mux.HandleFunc("POST /v1/auth/verify", api.verify)
	mux.HandleFunc("GET /v1/me", api.me)
	mux.HandleFunc("POST /v1/auth/logout", api.logout)

	server := &http.Server{
		Addr:              addr,
		Handler:           withCORS(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("linkgym-api listening on %s", addr)
	log.Fatal(server.ListenAndServe())
}

type api struct {
	db   *sql.DB
	auth *auth.Service
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
