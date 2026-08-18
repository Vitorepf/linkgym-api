.DEFAULT_GOAL := help

.PHONY: help setup start api dev down logs test tidy docker doctor migrate seed

help:
	@echo "LinkGym API"
	@echo "  make setup    1 comando: deps Go + Postgres + MinIO + schema + seed + testes"
	@echo "  make start    API em http://localhost:8080"
	@echo "  make migrate  só as migrations"
	@echo "  make seed     Fred + Vitor, Huan, Jose (dev)"
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

seed:
	go run ./cmd/seed

test:
	set -a; [ -f .env ] && . ./.env; set +a; go test ./...

tidy:
	go mod tidy

docker:
	@test -f .env || cp .env.example .env
	docker compose up --build

doctor: setup
