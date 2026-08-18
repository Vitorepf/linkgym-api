package main

import (
	"context"
	"log"
	"os"

	"github.com/Vitorepf/linkgym-api/internal/config"
	"github.com/Vitorepf/linkgym-api/internal/db"
	"github.com/Vitorepf/linkgym-api/internal/migrate"
	"github.com/Vitorepf/linkgym-api/internal/seed"
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
	if err := seed.Dev(context.Background(), database); err != nil {
		log.Fatal(err)
	}
	log.Print("seed ok: Fred + Vitor, Huan, Jose")
}
