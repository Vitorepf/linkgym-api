# linkgym-api

Go 1.26. After Go and Docker are installed once: `make setup` then `make start` (about a minute). Docs: `README.md`. Domain language: `CONTEXT.md`.

Postgres on the host is port **5436**. Do not switch it to 5432 to “match production”.

Schema changes: a new numbered file in `migrations/`. Never auto-migrate from Go structs.

Dev seed (Fred + Vitor, Huan, Jose): `make seed`. Never call it from the API process.
