package migrate

import (
	"database/sql"

	"github.com/pressly/goose/v3"

	"github.com/Vitorepf/linkgym-api/migrations"
)

func Up(db *sql.DB) error {
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.Up(db, ".")
}
