.PHONY: run test demo demo-memory demo-distributed demo-firecracker eval dashboard migrate lint tidy build sandbox-image firecracker-rootfs

build:
	go build -o bin/kubeai ./cmd/api-gateway
	go build -o bin/kubeai-worker ./cmd/worker
	go build -o bin/kubeai-eval ./cmd/eval

sandbox-image:
	docker build -t kubeai-sandbox:local -f deploy/sandbox.Dockerfile .

firecracker-rootfs:
	./scripts/build-firecracker-rootfs.sh

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

demo-firecracker: build
	./scripts/demo-firecracker.sh

eval: up build
	./scripts/demo-eval.sh

dashboard:
	cd dashboard && npm install && npm run dev

lint:
	go vet ./...
	gofmt -l -w .
