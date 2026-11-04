# Flint — Bubble Tea + PocketBase TUI scaffold
#
# The Makefile is the entry point of the developer workflow:
#
#   make run        start the TUI in the embedded mode
#   make build      build the standalone binary
#   make test       run the test suite
#   make lint       run go vet and golangci-lint
#   make pb-serve   run a local PocketBase server (useful for the remote mode)
#   make pb-migrate apply the PocketBase migrations to the embedded database

BINARY      := flint
CMD         := ./cmd/tui
BIN_DIR     := bin
DATA_DIR    ?= ./pb_data
GO          ?= go
GOFILES     := $(shell find . -name '*.go' -not -path './vendor/*')

# The Go toolchain version required by the project.
GO_VERSION  ?= 1.27.0

# The version of golangci-lint that `make lint-install` fetches. It is the
# one the CI runs: the action installs the latest release, and the linter
# changes its rules between the versions.
GOLANGCI_VERSION ?= 2.15.0

# The release artefacts are named after the platform, so the two parts of it
# are derived from the host. An unknown platform falls back to the linux one,
# which is the one the CI runs on.
GOLANGCI_OS   ?= $(shell $(GO) env GOOS)
GOLANGCI_ARCH ?= $(shell $(GO) env GOARCH)

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@echo "Flint — available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build the standalone binary into ./bin
	@mkdir -p $(BIN_DIR)
	$(GO) build -trimpath -ldflags "-s -w" -o $(BIN_DIR)/$(BINARY) $(CMD)
	@echo "built $(BIN_DIR)/$(BINARY)"

.PHONY: install
install: ## Install the binary into $GOPATH/bin
	$(GO) install -trimpath -ldflags "-s -w" $(CMD)

.PHONY: run
run: ## Run the TUI in the embedded mode
	$(GO) run $(CMD) --mode embedded --data-dir $(DATA_DIR)

.PHONY: run-remote
run-remote: ## Run the TUI against a remote PocketBase server
	$(GO) run $(CMD) --mode remote --remote-url $(REMOTE_URL)

.PHONY: test
test: ## Run the test suite
	$(GO) test ./...

.PHONY: test-race
test-race: ## Run the test suite with the race detector
	$(GO) test -race ./...

.PHONY: cover
cover: ## Run the test suite and report the coverage
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out | tail -1

.PHONY: lint
lint: vet golangci-lint ## Run all the linters

.PHONY: vet
vet: ## Run go vet
	$(GO) vet ./...

.PHONY: golangci-lint
golangci-lint: ## Run golangci-lint (the local ./bin copy first, then the PATH)
	@if [ -x "$(BIN_DIR)/golangci-lint" ]; then \
		$(BIN_DIR)/golangci-lint run; \
	elif command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint is not installed, skipping (see https://golangci-lint.run)"; \
		echo "install it with: make lint-install"; \
		exit 0; \
	fi

.PHONY: lint-install
lint-install: ## Install golangci-lint into ./bin
	@mkdir -p $(BIN_DIR)
	@archive="golangci-lint-$(GOLANGCI_VERSION)-$(GOLANGCI_OS)-$(GOLANGCI_ARCH).tar.gz"; \
	base="https://github.com/golangci/golangci-lint/releases/download/v$(GOLANGCI_VERSION)"; \
	if command -v curl >/dev/null 2>&1 && \
		curl -sSfL --max-time 300 -o "$(BIN_DIR)/$$archive" "$$base/$$archive" && \
		curl -sSfL --max-time 120 -o "$(BIN_DIR)/checksums.txt" \
			"$$base/golangci-lint-$(GOLANGCI_VERSION)-checksums.txt" && \
		cd $(BIN_DIR) && \
		grep " $$archive$$" checksums.txt | sha256sum -c - && \
		tar xzf "$$archive" && \
		mv "golangci-lint-$(GOLANGCI_VERSION)-$(GOLANGCI_OS)-$(GOLANGCI_ARCH)/golangci-lint" . && \
		rm -rf "golangci-lint-$(GOLANGCI_VERSION)-$(GOLANGCI_OS)-$(GOLANGCI_ARCH)" \
			"$$archive" checksums.txt; then \
		echo "installed the release archive"; \
	else \
		echo "the download failed, falling back to 'go install'"; \
		GOBIN="$(CURDIR)/$(BIN_DIR)" $(GO) install \
			github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v$(GOLANGCI_VERSION); \
	fi
	@chmod +x $(BIN_DIR)/golangci-lint
	@echo "installed $$( $(BIN_DIR)/golangci-lint --version )"

.PHONY: fmt
fmt: ## Format the sources
	gofmt -w $(GOFILES)

.PHONY: fmt-check
fmt-check: ## Verify that the sources are formatted
	@out=$$(gofmt -l $(GOFILES)); \
	if [ -n "$$out" ]; then echo "not formatted:"; echo "$$out"; exit 1; fi
	@echo "all files are formatted"

.PHONY: tidy
tidy: ## Tidy the go module
	$(GO) mod tidy

.PHONY: audit
audit: ## Scan the dependencies for known vulnerabilities
	@command -v govulncheck >/dev/null 2>&1 || { \
		echo "govulncheck is not installed"; \
		echo "install it with: go install golang.org/x/vuln/cmd/govulncheck@latest"; \
		exit 1; \
	}
	govulncheck ./...

.PHONY: pb-serve
pb-serve: ## Run a local PocketBase server (requires the pocketbase CLI)
	@command -v pocketbase >/dev/null 2>&1 || { \
		echo "the pocketbase CLI is not installed"; \
		echo "see https://pocketbase.io/docs/"; \
		exit 1; \
	}
	pocketbase serve --dir $(DATA_DIR) --http=127.0.0.1:8090

.PHONY: pb-migrate
pb-migrate: ## Apply the PocketBase migrations to the embedded database
	$(GO) run $(CMD) --mode embedded --data-dir $(DATA_DIR) --migrate-only
	@echo "the migrations have been applied to $(DATA_DIR)"

.PHONY: cross-build
cross-build: ## Build the binary for the supported platforms
	@mkdir -p $(BIN_DIR)
	@for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64; do \
		os=$${target%/*}; arch=$${target#*/}; \
		ext=""; if [ "$$os" = "windows" ]; then ext=".exe"; fi; \
		echo "building $$os/$$arch"; \
		GOOS=$$os GOARCH=$$arch $(GO) build -trimpath \
			-o $(BIN_DIR)/$(BINARY)-$$os-$$arch$$ext $(CMD) || exit 1; \
	done

.PHONY: clean
clean: ## Remove the build artifacts
	rm -rf $(BIN_DIR) coverage.out

.PHONY: ci
ci: fmt-check vet test ## Run the checks executed by the CI
