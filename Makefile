.DEFAULT_GOAL := help

.PHONY: help setup start api dev down logs test tidy docker doctor migrate

help:
	@echo "LinkGym API"
	@echo "  make setup    Go + Docker + .env + Postgres + MinIO + migrations + testes"
	@echo "  make start    API em http://localhost:8080"
	@echo "  make migrate  só as migrations (Postgres já no ar)"
	@echo "  make down   para Postgres e MinIO"
	@echo "  make test   só os testes"
	@echo "  make docker sobe tudo no Compose, inclusive a API"

setup:
	bash scripts/setup.sh

start api:
	go run ./cmd/api

dev: setup
	go run ./cmd/api

down:
	docker compose down

logs:
	docker compose logs -f postgres minio

migrate:
	go run ./cmd/migrate

test:
	set -a; [ -f .env ] && . ./.env; set +a; go test ./...

tidy:
	go mod tidy

docker:
	@test -f .env || cp .env.example .env
	docker compose up --build

doctor: setup
