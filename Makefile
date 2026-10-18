# go-cli-tools — developer tasks. Requires a POSIX shell (Linux, macOS, Git Bash/WSL on Windows).

MODULE      := github.com/tonklasocute/go-cli-tools
BINARIES    := envcheck portscout jsonformat dirsize reqping
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT      ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE        ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS     := -s -w \
	-X $(MODULE)/internal/version.Version=$(VERSION) \
	-X $(MODULE)/internal/version.Commit=$(COMMIT) \
	-X $(MODULE)/internal/version.Date=$(DATE)
EXE         := $(if $(filter windows,$(shell go env GOOS)),.exe,)
COVER_MIN   ?= 80

GOLANGCI_LINT_VERSION := v2.14.0
GOLANGCI_LINT ?= $(shell command -v golangci-lint 2>/dev/null || echo go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION))
GORELEASER    ?= $(shell command -v goreleaser 2>/dev/null || echo go run github.com/goreleaser/goreleaser/v2@latest)

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z_-]+:.*## / {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.PHONY: build
build: ## Build all binaries into ./bin
	@mkdir -p bin
	@for b in $(BINARIES); do \
		echo "  build  bin/$$b$(EXE)"; \
		go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$$b$(EXE) ./cmd/$$b || exit 1; \
	done

.PHONY: install
install: ## Install all binaries into $GOBIN (or $GOPATH/bin)
	@for b in $(BINARIES); do \
		echo "  install $$b"; \
		go install -trimpath -ldflags "$(LDFLAGS)" ./cmd/$$b || exit 1; \
	done

.PHONY: test
test: ## Run all tests
	go test ./...

.PHONY: race
race: ## Run all tests with the race detector (needs cgo)
	go test -race ./...

.PHONY: cover
cover: ## Run tests with coverage, print the total and enforce COVER_MIN
	go test -covermode=atomic -coverpkg=./internal/... -coverprofile=coverage.out ./...
	@go tool cover -func=coverage.out | tail -n 1
	@go tool cover -func=coverage.out | awk -v min=$(COVER_MIN) '/^total:/ { sub("%","",$$3); if ($$3+0 < min) { printf "coverage %.1f%% is below %d%%\n", $$3, min; exit 1 } }'

.PHONY: cover-html
cover-html: cover ## Open the HTML coverage report
	go tool cover -html=coverage.out

.PHONY: fmt
fmt: ## Format the code
	gofmt -s -w .

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: lint
lint: ## Run golangci-lint (falls back to go run when not installed)
	$(GOLANGCI_LINT) run ./...

.PHONY: bench
bench: ## Run the dirsize and portscout benchmarks
	go test -run '^$$' -bench . -benchmem ./internal/dirsize/ ./internal/portscout/

.PHONY: demo
demo: build ## Render the demo GIFs in docs/demo with charmbracelet/vhs
	@command -v vhs >/dev/null 2>&1 || { echo "vhs not found: https://github.com/charmbracelet/vhs#installation"; exit 1; }
	@for t in docs/tapes/*.tape; do \
		echo "  vhs    $$t"; \
		PATH="$(CURDIR)/bin:$$PATH" vhs "$$t" || exit 1; \
	done

.PHONY: release-snapshot
release-snapshot: ## Build a local snapshot release into ./dist (no publish)
	$(GORELEASER) release --snapshot --clean

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf bin dist coverage.out
