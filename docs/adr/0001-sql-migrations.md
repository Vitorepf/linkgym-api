# Versioned SQL migrations, English tables

The domain language is Portuguese (Pessoa, Estúdio, Vínculo). The Postgres schema uses English identifiers (`people`, `studios`, `bonds`) so Go, SQL tooling, and the AI stay on one spelling. Mapping lives in `CONTEXT.md` and in comments on each table.

Schema changes go through numbered SQL in `migrations/`, applied by goose. Auto-migrate from structs is out: the invariants (lote never copies load, PRs belong to the person, ofensiva belongs to the bond) have to be visible in SQL and reviewable.
