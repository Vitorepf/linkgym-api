package db

import (
	"database/sql"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func Open(url string) (*sql.DB, error) {
	database, err := sql.Open("pgx", url)
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(10)
	return database, database.Ping()
}
