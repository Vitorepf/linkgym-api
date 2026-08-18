# linkgym-api

API do LinkGym. Go + Postgres. App: [linkgym-app](https://github.com/Vitorepf/linkgym-app).

## Subir (back)

Na máquina, só isto:

1. [Go 1.26+](https://go.dev/dl/)
2. [Docker Desktop](https://www.docker.com/products/docker-desktop/) ou [OrbStack](https://orbstack.dev/) — aberto, não só instalado
3. Acesso a este repo (é privado — peça convite no GitHub)

```bash
git clone https://github.com/Vitorepf/linkgym-api.git
cd linkgym-api
make setup
make start
```

`make setup` confere Go e Docker, cria o `.env`, sobe Postgres + MinIO e roda os testes.

`make start` sobe a API. Pronto quando [http://localhost:8080/health](http://localhost:8080/health) responder `{"status":"ok","service":"linkgym-api"}`.

| | |
| --- | --- |
| API | http://localhost:8080 |
| Postgres | `localhost:5436` — usuário/senha/db `linkgym` |
| MinIO S3 | http://localhost:9000 |
| MinIO console | http://localhost:9001 — `linkgym` / `linkgymsecret` |

A porta **5436** é de propósito (5432 costuma estar ocupada). Dentro do Compose a API fala com `postgres:5432`.

## Dia a dia

```bash
make start   # API (Docker já no ar)
make down    # para Postgres e MinIO
make test
```

Tudo no Docker, inclusive a API: `make docker`

## Travas (não mexer)

| | |
| --- | --- |
| Linguagem | Go 1.26 |
| Banco | Postgres 16 |
| Arquivos | MinIO no dev, R2/S3 em prod |
| Fila | Postgres (ainda não ligada) |
| App | Expo, fora deste repo |

Spec: [`docs/superpowers/specs/2026-08-18-linkgym-v1-design.md`](docs/superpowers/specs/2026-08-18-linkgym-v1-design.md)
