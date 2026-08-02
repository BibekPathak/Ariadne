.PHONY: run test demo migrate lint tidy build sandbox-image

build:
	go build -o bin/kubeai ./cmd/api-gateway

sandbox-image:
	docker build -t kubeai-sandbox:local -f deploy/sandbox.Dockerfile .

run: build
	./bin/kubeai

test:
	go test ./...

migrate:
	go run ./cmd/api-gateway migrate

up:
	docker compose up -d

down:
	docker compose down

demo: up build
	./scripts/demo.sh

lint:
	go vet ./...
	gofmt -l -w .
