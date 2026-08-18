# linkgym-api

Go 1.26. Local stack: `make setup` then `make start`. Docs for humans: `README.md`. Domain language: `CONTEXT.md`.

Postgres on the host is port **5436**. Do not switch it to 5432 to “match production”.

Schema changes: a new numbered file in `migrations/`. Never auto-migrate from Go structs.
