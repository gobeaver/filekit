# Makefile for filekit (multi-module repo)
#
# filekit is a multi-module Go repo: the root module + filevalidator + 7 driver
# sub-modules. Every target below fans out across all modules so that running
# `make test` actually exercises every module rather than only the root.
#
# Releases are tagged atomically across every module — see `make release`.

GOCMD     = go
GOTEST    = $(GOCMD) test
GOVET     = $(GOCMD) vet
GOMOD     = $(GOCMD) mod
PKGS      = ./...
MODULE    = github.com/gobeaver/filekit

# Every module path in the repo, relative to the repo root.
MODULES = \
	. \
	filevalidator \
	driver/local \
	driver/memory \
	driver/s3 \
	driver/gcs \
	driver/azure \
	driver/sftp \
	driver/zip

# Colors
GREEN  = \033[0;32m
YELLOW = \033[0;33m
RED    = \033[0;31m
NC     = \033[0m

.DEFAULT_GOAL := help

# ─── Code quality ───────────────────────────────────────────────────────────

.PHONY: lint
lint: ## Run golangci-lint across every module
	@command -v golangci-lint >/dev/null 2>&1 || { echo "$(YELLOW)Install with: brew install golangci-lint$(NC)"; exit 1; }
	@set -e; for d in $(MODULES); do \
		echo "$(GREEN)→ lint $$d$(NC)"; \
		(cd "$$d" && golangci-lint run); \
	done

.PHONY: fmt
fmt: ## Format every module
	@set -e; for d in $(MODULES); do \
		(cd "$$d" && $(GOCMD) fmt $(PKGS)); \
	done

.PHONY: vet
vet: ## go vet every module
	@set -e; for d in $(MODULES); do \
		echo "$(GREEN)→ vet $$d$(NC)"; \
		(cd "$$d" && $(GOVET) $(PKGS)); \
	done

# ─── Tests ──────────────────────────────────────────────────────────────────

.PHONY: test
test: ## Run all tests in every module
	@set -e; for d in $(MODULES); do \
		echo "$(GREEN)→ test $$d$(NC)"; \
		(cd "$$d" && $(GOTEST) $(PKGS)); \
	done

.PHONY: test-verbose
test-verbose: ## Run all tests verbosely in every module
	@set -e; for d in $(MODULES); do \
		echo "$(GREEN)→ test -v $$d$(NC)"; \
		(cd "$$d" && $(GOTEST) -v $(PKGS)); \
	done

.PHONY: test-coverage
test-coverage: ## Run tests with coverage in every module
	@set -e; for d in $(MODULES); do \
		echo "$(GREEN)→ coverage $$d$(NC)"; \
		(cd "$$d" && $(GOTEST) -coverprofile=coverage.out $(PKGS) && $(GOCMD) tool cover -func=coverage.out | tail -1); \
	done

.PHONY: test-race
test-race: ## Run tests with race detector in every module
	@set -e; for d in $(MODULES); do \
		echo "$(GREEN)→ test -race $$d$(NC)"; \
		(cd "$$d" && $(GOTEST) -race $(PKGS)); \
	done

.PHONY: test-package
test-package: ## Test specific package (usage: make test-package PKG=./filevalidator)
	@$(GOTEST) -v $(PKG)

.PHONY: bench
bench: ## Run benchmarks across every module
	@set -e; for d in $(MODULES); do \
		echo "$(GREEN)→ bench $$d$(NC)"; \
		(cd "$$d" && $(GOTEST) -bench=. -benchmem $(PKGS)); \
	done

# ─── Modules & deps ─────────────────────────────────────────────────────────

.PHONY: tidy
tidy: ## go mod tidy every module
	@set -e; for d in $(MODULES); do \
		echo "$(GREEN)→ tidy $$d$(NC)"; \
		(cd "$$d" && $(GOMOD) tidy); \
	done

.PHONY: deps
deps: ## Download dependencies for every module
	@set -e; for d in $(MODULES); do \
		(cd "$$d" && $(GOMOD) download); \
	done

# ─── Security ───────────────────────────────────────────────────────────────
#
# Tools are pinned via `go run` so contributors don't need them installed.
STATICCHECK_VERSION = 2024.1.1
GOVULNCHECK_VERSION = latest
GOSEC_VERSION       = v2.21.4

.PHONY: vuln
vuln: ## govulncheck across every module
	@set -e; for d in $(MODULES); do \
		echo "$(GREEN)→ vuln $$d$(NC)"; \
		(cd "$$d" && $(GOCMD) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) $(PKGS)); \
	done

.PHONY: gosec
gosec: ## gosec across every module
	@set -e; for d in $(MODULES); do \
		echo "$(GREEN)→ gosec $$d$(NC)"; \
		(cd "$$d" && $(GOCMD) run github.com/securego/gosec/v2/cmd/gosec@$(GOSEC_VERSION) -quiet $(PKGS)); \
	done

.PHONY: sec
sec: vuln gosec ## Run all security checks (vuln + gosec)

# ─── Release-readiness validation ───────────────────────────────────────────
#
# This catches the multi-module publishing bug that broke filekit v0.0.1–v0.0.3:
# go.mod files containing v0.0.0 placeholder versions for sibling modules.
# When `replace` directives mask these placeholders locally, builds work but
# the published module is unimportable from outside the repo.
#
# `verify-release-mods` runs as part of `check` and again inside `release`.

.PHONY: verify-release-mods
verify-release-mods: ## Validate every go.mod is publishable (no v0.0.0 placeholders)
	@set -e; \
	bad=0; \
	for d in $(MODULES); do \
		mod="$$d/go.mod"; \
		if grep -E 'github.com/gobeaver/filekit[^ ]* v0\.0\.0( |$$)' "$$mod" >/dev/null 2>&1; then \
			echo "$(RED)✗ $$mod has placeholder v0.0.0 references — release would be broken$(NC)"; \
			grep -nE 'github.com/gobeaver/filekit[^ ]* v0\.0\.0( |$$)' "$$mod" | sed 's/^/    /'; \
			bad=1; \
		fi; \
		if grep -E 'github.com/gobeaver/filekit[^ ]* v0\.0\.0-' "$$mod" >/dev/null 2>&1; then \
			echo "$(RED)✗ $$mod has pseudo-version (v0.0.0-...) for sibling — release would be broken$(NC)"; \
			grep -nE 'github.com/gobeaver/filekit[^ ]* v0\.0\.0-' "$$mod" | sed 's/^/    /'; \
			bad=1; \
		fi; \
	done; \
	if [ "$$bad" = "1" ]; then \
		echo ""; \
		echo "Fix: edit each flagged go.mod to require its sibling modules at"; \
		echo "the next release version (e.g. github.com/gobeaver/filekit v0.0.5)"; \
		echo "before tagging. The replace directives keep local builds working."; \
		exit 1; \
	fi; \
	echo "$(GREEN)✓ all go.mod files are release-ready$(NC)"

# ─── Aggregate gates ────────────────────────────────────────────────────────

.PHONY: check
check: vet test verify-release-mods ## Run vet, tests, and the release-mod gate

.PHONY: check-full
check-full: vet lint test-race sec verify-release-mods ## Run EVERYTHING (slow)

.PHONY: ci
ci: deps vet test-race verify-release-mods ## CI pipeline

# ─── Release ────────────────────────────────────────────────────────────────
#
# Tags every module at HEAD as <module>/<version>. Refuses to proceed if:
#   - working tree is dirty
#   - not on main
#   - any go.mod still has placeholder versions
#   - any module fails its check
#   - any of the proposed tags already exist
#
# Workflow:
#   1. Bump every go.mod that requires a sibling module to require the
#      NEXT version you're about to tag (e.g. v0.0.5).
#   2. Build and test locally — replace directives keep it working.
#   3. Commit the bumps.
#   4. `make release`, type the version, confirm.

.PHONY: release
release: ## Interactive: tag every module at HEAD and push atomically
	@set -e; \
	if [ -n "$$(git status --porcelain)" ]; then \
		echo "$(RED)✗ working tree is dirty — commit or stash first$(NC)"; exit 1; \
	fi; \
	branch=$$(git rev-parse --abbrev-ref HEAD); \
	if [ "$$branch" != "main" ]; then \
		echo "$(RED)✗ not on main (currently on '$$branch')$(NC)"; exit 1; \
	fi; \
	last=$$(git tag --list 'v*' --sort=-v:refname | head -1); \
	[ -z "$$last" ] && last="(none)"; \
	echo "Module:       $(MODULE)"; \
	echo "Sub-modules:  $(words $(MODULES)) total"; \
	echo "Last tag:     $$last"; \
	echo ""; \
	printf "New version (e.g. v0.0.5): "; read new; \
	case "$$new" in \
		v[0-9]*.[0-9]*.[0-9]*) ;; \
		*) echo "$(RED)✗ '$$new' is not a valid semver tag (must look like vX.Y.Z)$(NC)"; exit 1 ;; \
	esac; \
	echo ""; \
	echo "Proposed tags (all at HEAD):"; \
	for d in $(MODULES); do \
		if [ "$$d" = "." ]; then tag="$$new"; else tag="$$d/$$new"; fi; \
		if git rev-parse "$$tag" >/dev/null 2>&1; then \
			echo "$(RED)  ✗ $$tag (already exists)$(NC)"; exit 1; \
		fi; \
		echo "  + $$tag"; \
	done; \
	echo ""; \
	printf "Release message: "; read msg; \
	if [ -z "$$msg" ]; then echo "$(RED)✗ message is required$(NC)"; exit 1; fi; \
	echo ""; \
	echo "Will run pre-release checks, then tag and push everything."; \
	printf "Proceed? [y/N] "; read confirm; \
	case "$$confirm" in y|Y|yes) ;; *) echo "aborted"; exit 1 ;; esac; \
	$(MAKE) check; \
	echo ""; \
	echo "$(GREEN)→ creating tags$(NC)"; \
	for d in $(MODULES); do \
		if [ "$$d" = "." ]; then tag="$$new"; else tag="$$d/$$new"; fi; \
		git tag -a "$$tag" -m "$$msg"; \
		echo "  ✓ $$tag"; \
	done; \
	echo ""; \
	echo "$(GREEN)→ pushing tags$(NC)"; \
	for d in $(MODULES); do \
		if [ "$$d" = "." ]; then tag="$$new"; else tag="$$d/$$new"; fi; \
		git push origin "$$tag" >/dev/null 2>&1; \
		echo "  ✓ pushed $$tag"; \
	done; \
	echo ""; \
	echo "$(GREEN)✓ released $$new across all $(words $(MODULES)) modules$(NC)"; \
	echo "  go get $(MODULE)@$$new"

# ─── Housekeeping ───────────────────────────────────────────────────────────

.PHONY: clean
clean: ## Clean build artifacts in every module
	@for d in $(MODULES); do rm -f "$$d/coverage.out" "$$d/coverage.html"; done

.PHONY: help
help: ## Display this help
	@echo "filekit Makefile (multi-module: $(words $(MODULES)) modules)"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  $(GREEN)%-20s$(NC) %s\n", $$1, $$2}'
