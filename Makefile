BIN := $(CURDIR)/bin
export PATH := $(BIN):$(PATH)
export GOTOOLCHAIN := local

GOLANGCI_LINT_VERSION := 2.4.0
MODULES := $(shell go list -m -f '{{.Dir}}' 2>/dev/null)

.DEFAULT_GOAL := help

.PHONY: help tools proto sqlc lint test build up down migrate new-service rename-service

help: ## list targets
	@grep -hE '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-16s %s\n", $$1, $$2}'

tools: ## install pinned buf, protoc plugins, goose, sqlc and golangci-lint into ./bin
	mkdir -p $(BIN)
	cd tools && GOWORK=off GOBIN=$(BIN) go install tool
	GOWORK=off GOBIN=$(BIN) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v$(GOLANGCI_LINT_VERSION)

proto: ## buf lint + buf generate into gen/go and web/src/gen
	$(BIN)/buf lint
	$(BIN)/buf generate

sqlc: ## generate internal/store/db in each service
	@echo "not yet: needs service skeletons (P2.3) and sqlc.yaml (P4.4)"

lint: ## golangci-lint on every module, buf lint
	@for m in $(MODULES); do echo "==> lint $$m"; (cd $$m && $(BIN)/golangci-lint run ./...) || exit 1; done
	$(BIN)/buf lint

test: ## go test -race on every module
	@for m in $(MODULES); do echo "==> test $$m"; (cd $$m && go test -race ./...) || exit 1; done

build: ## build every service binary into ./dist
	@found=0; for d in services/*/cmd/*; do \
		[ -d "$$d" ] || continue; found=1; \
		echo "==> build $$d"; mkdir -p dist; \
		(cd $$(dirname $$(dirname $$d)) && go build -o $(CURDIR)/dist/$$(basename $$d) ./cmd/$$(basename $$d)) || exit 1; \
	done; [ $$found = 1 ] || echo "not yet: no services (P2.3)"

up: ## docker compose local stack
	@echo "not yet: compose stack arrives in P3.3"

down: ## stop the local stack
	@echo "not yet: compose stack arrives in P3.3"

migrate: ## apply all service migrations to the local database
	@echo "not yet: migrations arrive in P2.1 and P3.3"

new-service: ## generate a service skeleton: make new-service NAME=<name>
	@scripts/new-service.sh "$(NAME)"

rename-service: ## rename a service everywhere: make rename-service OLD=a NEW=b
	@scripts/rename-service.sh "$(OLD)" "$(NEW)"
