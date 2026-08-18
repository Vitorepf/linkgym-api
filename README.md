# linkgym-api

API do LinkGym. Go + Postgres. O app (`linkgym-app`) consome este contrato.

Depois do login, a marca LinkGym some: nome, logo e cor vêm do estúdio do personal.

## Subir o ambiente (sócio / máquina nova)

```bash
cp .env.example .env
make up          # Postgres 5436 + MinIO 9000 (console 9001)
make api         # API em :8080
```

Health: [http://localhost:8080/health](http://localhost:8080/health)

Postgres no host usa a porta **5436** (5432 já está ocupada nesta máquina). Dentro do Compose a API continua falando com `postgres:5432`.

MinIO console: [http://localhost:9001](http://localhost:9001) — `linkgym` / `linkgymsecret`

Tudo no Docker, incluindo a API:

```bash
docker compose up --build
```

## Stack

- Go
- Postgres 16
- Object storage: MinIO no dev, R2/S3 em prod
- Fila: no Postgres (ainda não ligada)

Spec: `docs/superpowers/specs/2026-08-18-linkgym-v1-design.md`

App: https://github.com/Vitorepf/linkgym-app
