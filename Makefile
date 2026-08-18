.DEFAULT_GOAL := help

.PHONY: help setup start api dev down logs test tidy docker doctor

help:
	@echo "LinkGym API"
	@echo "  make setup  Go + Docker + .env + Postgres + MinIO + testes"
	@echo "  make start  API em http://localhost:8080"
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

test:
	go test ./...

tidy:
	go mod tidy

docker:
	@test -f .env || cp .env.example .env
	docker compose up --build

doctor: setup
