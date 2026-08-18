# linkgym-api

API do LinkGym. Go + Postgres. O app consome este contrato: [linkgym-app](https://github.com/Vitorepf/linkgym-app).

Depois do login, a marca LinkGym some. Nome, logo e cor vêm do estúdio do personal.

## Precisa na máquina

- [Go 1.26+](https://go.dev/dl/)
- [Docker Desktop](https://www.docker.com/products/docker-desktop/) (ou OrbStack)

## Subir

```bash
git clone https://github.com/Vitorepf/linkgym-api.git
cd linkgym-api
make dev
```

Isso cria o `.env`, sobe Postgres + MinIO e inicia a API.

Confirme: [http://localhost:8080/health](http://localhost:8080/health) → `{"status":"ok","service":"linkgym-api"}`

| Serviço | Onde |
| --- | --- |
| API | http://localhost:8080 |
| Postgres (host) | `localhost:5436` — usuário/senha/db `linkgym` |
| MinIO S3 | http://localhost:9000 |
| MinIO console | http://localhost:9001 — `linkgym` / `linkgymsecret` |

A porta **5436** é de propósito: 5432 costuma estar ocupada. Dentro do Compose a API fala com `postgres:5432`.

## Dia a dia

```bash
make api     # API (Docker já no ar)
make down    # para Postgres e MinIO
make test
```

Tudo no Docker, inclusive a API:

```bash
docker compose up --build
```

## Contrato

Spec do v1: [`docs/superpowers/specs/2026-08-18-linkgym-v1-design.md`](docs/superpowers/specs/2026-08-18-linkgym-v1-design.md)
