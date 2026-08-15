.PHONY: help demo dev-be dev-fe test test-be lint build build-be build-fe docker clean

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

demo: ## Run the whole stack with no API keys (backend :8080, frontend :5173)
	@echo "Starting MarketMate in demo mode — no API keys required."
	@echo "Backend  → http://localhost:8080"
	@echo "Frontend → http://localhost:5173"
	@trap 'kill 0' EXIT; \
	(cd market-mate-be && DEMO_MODE=true go run ./cmd) & \
	(cd market-mate-fe && npm run dev) & \
	wait

dev-be: ## Run the backend only
	cd market-mate-be && go run ./cmd

dev-fe: ## Run the frontend only
	cd market-mate-fe && npm run dev

test: test-be ## Run all tests

test-be: ## Run backend tests (no keys or network required)
	cd market-mate-be && go test ./... -race -cover

lint: ## Vet the backend and type-check the frontend
	cd market-mate-be && go vet ./...
	cd market-mate-fe && npx tsc --noEmit

build: build-be build-fe ## Build both

build-be: ## Build the backend binary
	cd market-mate-be && CGO_ENABLED=0 go build -ldflags="-s -w" -o ../bin/market-mate ./cmd

build-fe: ## Build the frontend bundle
	cd market-mate-fe && npm ci && npm run build

docker: ## Build and run both containers
	docker compose up --build

clean:
	rm -rf bin market-mate-fe/dist
