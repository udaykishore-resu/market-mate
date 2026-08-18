.PHONY: help demo dev-be dev-fe test test-be lint build build-be build-fe docker clean deps preflight stop

# Override to run beside something already using these ports:
#   make demo API_PORT=8081 WEB_PORT=5174
API_PORT ?= 8080
WEB_PORT ?= 5173

# The goal the user actually typed, so the port-conflict hint can suggest
# rerunning *that* rather than a hardcoded example. Falls back to `demo`.
FIRST_GOAL := $(if $(MAKECMDGOALS),$(firstword $(MAKECMDGOALS)),demo)

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

deps: market-mate-fe/node_modules ## Install frontend dependencies if they are missing

# A directory target with the lockfile as its prerequisite: npm ci runs on a
# fresh clone and whenever the lockfile changes, and is skipped otherwise. This
# is what makes `make demo` work straight after `git clone`; without it the
# first run fails with an opaque "Cannot find package 'vite'".
market-mate-fe/node_modules: market-mate-fe/package-lock.json market-mate-fe/package.json
	@echo "installing frontend dependencies (this runs once)"
	cd market-mate-fe && npm ci
	@touch market-mate-fe/node_modules

preflight: ## Fail early and legibly if a port is already taken
	@busy=""; \
	for p in $(API_PORT) $(WEB_PORT); do \
		if command -v lsof >/dev/null 2>&1 && lsof -ti tcp:$$p >/dev/null 2>&1; then \
			busy="$$busy $$p"; \
		fi; \
	done; \
	if [ -n "$$busy" ]; then \
		for p in $$busy; do \
			echo ""; \
			echo "  Port $$p is already in use:"; \
			lsof -i tcp:$$p | sed 's/^/    /'; \
		done; \
		freeapi=$$(p=$(API_PORT); while lsof -ti tcp:$$p >/dev/null 2>&1; do p=$$((p+1)); done; echo $$p); \
		freeweb=$$(p=$(WEB_PORT); while [ "$$p" = "$$freeapi" ] || lsof -ti tcp:$$p >/dev/null 2>&1; do p=$$((p+1)); done; echo $$p); \
		echo ""; \
		echo "  Free them:   $(MAKE) stop API_PORT=$(API_PORT) WEB_PORT=$(WEB_PORT)"; \
		echo "  Or move on:  $(MAKE) $(FIRST_GOAL) API_PORT=$$freeapi WEB_PORT=$$freeweb"; \
		echo ""; \
		exit 1; \
	fi

stop: ## Stop anything this project left listening on its ports
	@for p in $(API_PORT) $(WEB_PORT); do \
		pids=$$(lsof -ti tcp:$$p 2>/dev/null); \
		if [ -n "$$pids" ]; then \
			echo "stopping $$pids on port $$p"; \
			kill $$pids 2>/dev/null || true; \
		fi; \
	done; \
	echo "ports clear"

demo: preflight deps ## Run the whole stack with no API keys
	@echo ""
	@echo "  MarketMate → http://localhost:$(WEB_PORT)"
	@echo "  API        → http://localhost:$(API_PORT)/api/health"
	@echo "  GraphiQL   → http://localhost:$(API_PORT)/graphiql"
	@echo "  No API keys required — every provider falls back to a labelled fixture."
	@echo ""
	@trap 'kill 0' EXIT; \
	(cd market-mate-be && PORT=$(API_PORT) USE_FIXTURES=true MM_GRAPHIQL=true go run ./cmd) & \
	(cd market-mate-fe && API_PORT=$(API_PORT) WEB_PORT=$(WEB_PORT) npm run dev) & \
	wait

dev-be: ## Run the backend only
	cd market-mate-be && PORT=$(API_PORT) \
		ALLOWED_ORIGINS=http://localhost:$(WEB_PORT),http://127.0.0.1:$(WEB_PORT) \
		go run ./cmd

dev-fe: deps ## Run the frontend only
	cd market-mate-fe && API_PORT=$(API_PORT) WEB_PORT=$(WEB_PORT) npm run dev

test: test-be ## Run all tests

test-be: ## Run backend tests (no keys or network required)
	cd market-mate-be && go test ./... -race -cover

lint: deps ## Vet the backend and type-check the frontend
	cd market-mate-be && go vet ./...
	cd market-mate-fe && npx tsc --noEmit

build: build-be build-fe ## Build both

build-be: ## Build the backend binary
	cd market-mate-be && CGO_ENABLED=0 go build -ldflags="-s -w" -o ../bin/market-mate ./cmd

build-fe: deps ## Build the frontend bundle
	cd market-mate-fe && npm run build

docker: ## Build and run both containers
	docker compose up --build

clean:
	rm -rf bin market-mate-fe/dist
