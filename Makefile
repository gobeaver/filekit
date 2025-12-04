# Makefile for filekit

GOCMD=go
GOTEST=$(GOCMD) test
GOVET=$(GOCMD) vet
GOMOD=$(GOCMD) mod
ALL_PACKAGES=./...

# Colors
GREEN=\033[0;32m
YELLOW=\033[0;33m
NC=\033[0m

.DEFAULT_GOAL := help

.PHONY: lint
lint: ## Run linter
	@echo "$(GREEN)Running linter...$(NC)"
	@golangci-lint run || (echo "$(YELLOW)Install with: brew install golangci-lint$(NC)" && exit 1)

.PHONY: fmt
fmt: ## Format code
	@go fmt ./...

.PHONY: vet
vet: ## Run go vet
	@$(GOVET) $(ALL_PACKAGES)

.PHONY: test
test: ## Run all tests
	@$(GOTEST) -v $(ALL_PACKAGES)

.PHONY: test-coverage
test-coverage: ## Run tests with coverage
	@$(GOTEST) -v -coverprofile=coverage.out $(ALL_PACKAGES)
	@$(GOCMD) tool cover -html=coverage.out -o coverage.html

.PHONY: test-race
test-race: ## Run tests with race detector
	@$(GOTEST) -race $(ALL_PACKAGES)

.PHONY: test-package
test-package: ## Test specific package (usage: make test-package PKG=./filevalidator)
	@$(GOTEST) -v $(PKG)

.PHONY: bench
bench: ## Run benchmarks
	@$(GOTEST) -bench=. -benchmem $(ALL_PACKAGES)

.PHONY: clean
clean: ## Clean build artifacts
	@rm -f coverage.out coverage.html

.PHONY: tidy
tidy: ## Tidy all go.mod files (multi-module)
	@$(GOMOD) tidy
	@cd filevalidator && $(GOMOD) tidy
	@for dir in driver/*/; do cd "$$dir" && $(GOMOD) tidy && cd ../..; done

.PHONY: deps
deps: ## Download dependencies
	@$(GOMOD) download

.PHONY: check
check: lint test ## Run lint and tests

.PHONY: ci
ci: deps lint test-race ## Run CI pipeline

.PHONY: help
help: ## Display this help
	@echo "filekit Makefile"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-16s %s\n", $$1, $$2}'
