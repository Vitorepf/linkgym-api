#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

if ! command -v go >/dev/null 2>&1; then
  echo "Instale Go 1.26+: https://go.dev/dl/"
  exit 1
fi

go_minor="$(go env GOVERSION | sed -E 's/^go1\.([0-9]+).*/\1/')"
if ! [[ "$go_minor" =~ ^[0-9]+$ ]] || [ "$go_minor" -lt 26 ]; then
  echo "Go $(go env GOVERSION) é velho demais. Precisa 1.26+."
  echo "https://go.dev/dl/"
  exit 1
fi
echo "Go $(go env GOVERSION) ok"

if ! command -v docker >/dev/null 2>&1; then
  echo "Instale Docker Desktop ou OrbStack."
  exit 1
fi

if ! docker info >/dev/null 2>&1; then
  echo "Docker instalado, mas parado. Abra o Docker Desktop ou o OrbStack e rode de novo."
  exit 1
fi
echo "Docker ok"

if [ ! -f .env ]; then
  cp .env.example .env
  echo "criei .env a partir do .env.example"
else
  echo ".env já existe — não mexi"
fi

docker compose up -d postgres minio createbucket

echo "esperando Postgres..."
ok=0
for _ in $(seq 1 40); do
  if docker compose exec -T postgres pg_isready -U linkgym -d linkgym >/dev/null 2>&1; then
    ok=1
    break
  fi
  sleep 1
done
if [ "$ok" -ne 1 ]; then
  echo "Postgres não ficou pronto. Veja: docker compose logs postgres"
  exit 1
fi
echo "Postgres :5436 ok"

echo "esperando MinIO..."
ok=0
for _ in $(seq 1 40); do
  if curl -sf http://localhost:9000/minio/health/live >/dev/null; then
    ok=1
    break
  fi
  sleep 1
done
if [ "$ok" -ne 1 ]; then
  echo "MinIO não ficou pronto. Veja: docker compose logs minio"
  exit 1
fi
echo "MinIO :9000 ok"

echo "migrations..."
set -a
# shellcheck disable=SC1091
. ./.env
set +a
go run ./cmd/migrate

echo "seed (Fred + Vitor, Huan, Jose)..."
go run ./cmd/seed

go test ./...

echo ""
echo "Ambiente pronto. Próximo: make start"
echo "Health: http://localhost:8080/health"
echo "MinIO console: http://localhost:9001  (linkgym / linkgymsecret)"
echo "Seed: Fred (personal) + alunos Vitor, Huan, Jose — telefones no README"
