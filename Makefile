.PHONY: up down logs api tidy test

up:
	docker compose up -d postgres minio createbucket

down:
	docker compose down

logs:
	docker compose logs -f postgres minio

api:
	go run ./cmd/api

tidy:
	go mod tidy

test:
	go test ./...
