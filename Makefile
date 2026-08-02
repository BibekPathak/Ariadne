.PHONY: run test demo demo-memory demo-distributed migrate lint tidy build sandbox-image

build:
	go build -o bin/kubeai ./cmd/api-gateway
	go build -o bin/kubeai-worker ./cmd/worker

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

demo-memory: up build
	./scripts/demo-memory.sh

demo-distributed: up build
	./scripts/demo-distributed.sh

lint:
	go vet ./...
	gofmt -l -w .
