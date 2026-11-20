.PHONY: help build api sim admin trace test vet lint seed up down logs smoke tidy

help: ## Show help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

tidy: ## Tidy go modules
	go mod tidy

build: ## Build backend binaries into ./bin
	go build -o bin/ctc-api ./apps/api/cmd/ctc-api
	go build -o bin/ctc-sim ./simulator/ctc-sim

api: ## Run API locally (needs compose deps)
	go run ./apps/api/cmd/ctc-api

sim: ## Run device simulator locally
	go run ./simulator/ctc-sim

admin: ## Run admin web dev server
	cd apps/admin-web && npm run dev

trace: ## Run trace web dev server
	cd apps/trace-web && npm run dev

test: ## Run Go tests
	go test ./...

vet: ## Go vet
	go vet ./...

seed: ## Seed demo data
	go run ./apps/api/cmd/ctc-api -mode seed

up: ## Start full stack with docker compose
	docker compose -f deployments/docker/docker-compose.yml up -d --build

down: ## Stop full stack
	docker compose -f deployments/docker/docker-compose.yml down

logs: ## Tail API logs
	docker compose -f deployments/docker/docker-compose.yml logs -f api

smoke: ## Run end-to-end smoke test against localhost
	bash tests/e2e/smoke.sh
