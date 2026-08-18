package main

import (
	"log"
	"os"

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
	log.Print("migrations ok")
}
