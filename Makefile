.DEFAULT_GOAL := help

.PHONY: help setup up down logs api dev tidy test

help:
	@echo "LinkGym API"
	@echo "  make setup  copia .env (se faltar) e sobe Postgres + MinIO"
	@echo "  make api    sobe a API em http://localhost:8080"
	@echo "  make dev    setup + api (o que o sócio usa no dia a dia)"
	@echo "  make down   para o Docker"
	@echo "  make test   testes"

setup:
	@test -f .env || cp .env.example .env
	docker compose up -d postgres minio createbucket
	@echo ""
	@echo "Pronto. Postgres :5436  MinIO :9000  console :9001 (linkgym / linkgymsecret)"
	@echo "Agora: make api"

up: setup

down:
	docker compose down

logs:
	docker compose logs -f postgres minio

api:
	go run ./cmd/api

dev: setup
	go run ./cmd/api

tidy:
	go mod tidy

test:
	go test ./...
