BIN := $(CURDIR)/bin
export PATH := $(BIN):$(PATH)
export GOTOOLCHAIN := local

GOLANGCI_LINT_VERSION := 2.4.0
MODULES := $(shell go list -m -f '{{.Dir}}' 2>/dev/null)

# Local stack: use .env when present, otherwise the dev placeholders.
COMPOSE_ENV := $(if $(wildcard .env),.env,.env.example)
COMPOSE := docker compose --env-file $(COMPOSE_ENV) -f deploy/compose/compose.yaml

.DEFAULT_GOAL := help

.PHONY: help tools proto sqlc lint test drills build up down up-observability migrate new-service rename-service

help: ## list targets
	@grep -hE '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-16s %s\n", $$1, $$2}'

tools: ## install pinned buf, protoc plugins, goose, sqlc and golangci-lint into ./bin
	mkdir -p $(BIN)
	cd tools && GOWORK=off GOBIN=$(BIN) go install tool
	GOWORK=off GOBIN=$(BIN) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v$(GOLANGCI_LINT_VERSION)

proto: ## buf lint + buf generate into gen/go and web/src/gen
	$(BIN)/buf lint
	$(BIN)/buf generate
	$(BIN)/buf generate --template buf.gen.connect.yaml --path proto/shogun/api
	@if [ -x web/node_modules/.bin/protoc-gen-es ]; then \
		echo "==> buf generate (web)"; \
		$(BIN)/buf generate --template buf.gen.web.yaml --path proto/shogun/api || exit 1; \
	else \
		echo "not yet: web/node_modules missing, run 'cd web && npm ci' to generate the TS client"; \
	fi

sqlc: ## generate internal/store/db in each service
	@for f in services/*/sqlc.yaml; do \
		[ -f "$$f" ] || continue; \
		echo "==> sqlc $$(dirname $$f)"; \
		(cd $$(dirname $$f) && $(BIN)/sqlc generate) || exit 1; \
	done

lint: ## golangci-lint on every module, buf lint
	@for m in $(MODULES); do echo "==> lint $$m"; (cd $$m && $(BIN)/golangci-lint run ./...) || exit 1; done
	$(BIN)/buf lint

test: ## go test -race on every module
	@for m in $(MODULES); do echo "==> test $$m"; (cd $$m && go test -race ./...) || exit 1; done

# Modules that hold failure drills (TestDrill...): each kills or fails something
# and checks nothing is lost or done twice.
DRILL_MODULES := pkg services/soroban services/tsubame

drills: ## run the failure drills (needs Docker for Postgres)
	@for m in $(DRILL_MODULES); do echo "==> drills $$m"; (cd $$m && go test -race -count=1 -run '^TestDrill' ./...) || exit 1; done

build: ## build every service binary into ./dist (SERVICE=<name> limits it to one service)
	@found=0; for d in services/$(if $(SERVICE),$(SERVICE),*)/cmd/*; do \
		[ -d "$$d" ] || continue; found=1; \
		echo "==> build $$d"; mkdir -p dist; \
		(cd $$(dirname $$(dirname $$d)) && go build -o $(CURDIR)/dist/$$(basename $$d) ./cmd/$$(basename $$d)) || exit 1; \
	done; [ $$found = 1 ] || echo "not yet: no services (P2.3)"

up: ## build and start the local stack, waiting until every container is healthy
	$(COMPOSE) up -d --build --wait

up-observability: ## local stack plus Jaeger, Prometheus, Alertmanager and Grafana (see compose.obs.yaml)
	OTEL_EXPORTER_OTLP_ENDPOINT=http://jaeger:4317 $(COMPOSE) -f deploy/compose/compose.obs.yaml up -d --build --wait

down: ## stop the local stack (keeps the database volume)
	$(COMPOSE) -f deploy/compose/compose.obs.yaml down --remove-orphans

migrate: ## apply pending migrations: services migrate on start, so recreate them
	$(COMPOSE) up -d --build --force-recreate --wait

new-service: ## generate a service skeleton: make new-service NAME=<name>
	@scripts/new-service.sh "$(NAME)"

rename-service: ## rename a service everywhere: make rename-service OLD=a NEW=b
	@scripts/rename-service.sh "$(OLD)" "$(NEW)"
