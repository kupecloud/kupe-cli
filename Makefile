# kupe-cli Makefile

# Local gates run on the Go CI installs: setup-go reads go.mod's toolchain line,
# else its go line. A GOTOOLCHAIN the caller set wins (setup-go and kupe-runner set local).
ifndef GOTOOLCHAIN
GO_MOD_TOOLCHAIN := $(shell awk '$$1=="toolchain"{t=$$2} $$1=="go"&&g==""{g="go"$$2} END{v=(t!=""?t:g); sub(/\r$$/,"",v); print v}' go.mod)
ifeq ($(shell echo '$(GO_MOD_TOOLCHAIN)' | grep -Ex 'go1\.[0-9]+(\.[0-9]+|rc[0-9]+)'),)
$(error go.mod names no Go release to run ('$(GO_MOD_TOOLCHAIN)'): set its go line to one, e.g. go mod edit -go=1.N.P)
endif
export GOTOOLCHAIN := $(GO_MOD_TOOLCHAIN)
endif

# Variables
VERSION ?= dev
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

# Go settings
GO := go
# Local targets ignore a vendor/ left behind by `make vendor` (it goes stale and
# fails with "inconsistent vendoring"); only the image build uses -mod=vendor.
export GOFLAGS := -mod=readonly
GOCACHE ?= $(PWD)/.tmp/go-build
# golangci-lint's cache stays in this checkout: from v2.12 its keys ignore the
# checkout path, so a shared cache reports another worktree's files, even a
# removed one. A caller value wins (CI's gates job sets and caches its own).
export GOLANGCI_LINT_CACHE ?= $(CURDIR)/.tmp/golangci-lint
GORELEASER_VERSION := v2.15.2

# ldflags inject into internal/build.
LDFLAGS := -s -w \
           -X github.com/kupecloud/kupe-cli/internal/build.Version=$(VERSION) \
           -X github.com/kupecloud/kupe-cli/internal/build.Commit=$(GIT_COMMIT) \
           -X github.com/kupecloud/kupe-cli/internal/build.Date=$(BUILD_DATE)

.PHONY: all build build-local test test-coverage test-update test-live vet-live lint fmt gosec govulncheck clean vendor tidy version snapshot release-check manpages help go-env

all: build

## Build

build: ## Build the binary into bin/kupe for the target OS/arch
	GOCACHE="$(GOCACHE)" CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build -ldflags "$(LDFLAGS)" -o bin/kupe ./cmd/kupe

build-local: ## Build for local OS/arch (alias for build)
	$(MAKE) build

version: ## Display version information that would be injected
	@echo "Version:    $(VERSION)"
	@echo "Commit:     $(GIT_COMMIT)"
	@echo "Build Date: $(BUILD_DATE)"

## Dependencies

tidy: ## Run go mod tidy
	GOCACHE="$(GOCACHE)" $(GO) mod tidy

vendor: tidy ## Vendor dependencies
	GOCACHE="$(GOCACHE)" $(GO) mod vendor

## Testing

test: ## Run unit tests
	GOCACHE="$(GOCACHE)" $(GO) test -v ./internal/...

test-coverage: ## Run tests with coverage report
	GOCACHE="$(GOCACHE)" $(GO) test -v -coverprofile=coverage.out ./internal/...
	GOCACHE="$(GOCACHE)" $(GO) tool cover -html=coverage.out -o coverage.html

test-update: ## Update golden files
	GOCACHE="$(GOCACHE)" $(GO) test ./internal/printer -update

test-live: ## Run live tests against deployed kupe-api. Requires KUPE_API_TOKEN and KUPE_API_URL. See test/live/suite_test.go header.
	GOCACHE="$(GOCACHE)" $(GO) test -tags=live -timeout=45m -v ./test/live/...

vet-live: ## Compile-check the live suite without running it (no credentials needed); CI runs this on every PR
	GOCACHE="$(GOCACHE)" $(GO) vet -tags=live ./test/live/...

## Linting

lint: ## Run linter
	golangci-lint run ./...

fmt: ## Format code
	GOCACHE="$(GOCACHE)" $(GO) fmt ./...
	gofmt -s -w .

gosec: ## Run gosec against the codebase
	GOCACHE="$(GOCACHE)" GOWORK=off $(GO) run github.com/securego/gosec/v2/cmd/gosec@v2.25.0 -exclude-generated ./...

govulncheck: ## Run govulncheck against the codebase
	GOCACHE="$(GOCACHE)" GOWORK=off $(GO) run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...

# The pre-commit hooks that run Go read this through .github/hooks/go-env.sh,
# so they run on the Go and lint cache make uses. One NAME=value per line.
go-env: ## Print the Go environment the pre-commit Go hooks run with
	@printf 'GOTOOLCHAIN=%s\nGOLANGCI_LINT_CACHE=%s\n' "$$GOTOOLCHAIN" "$$GOLANGCI_LINT_CACHE"

## Release

snapshot: ## Build a local goreleaser snapshot (no publish, no sign/sbom)
	GOCACHE="$(GOCACHE)" GOWORK=off $(GO) run github.com/goreleaser/goreleaser/v2@$(GORELEASER_VERSION) release --snapshot --clean --skip=publish,sign,sbom

release-check: ## Validate .goreleaser.yaml
	GOCACHE="$(GOCACHE)" GOWORK=off $(GO) run github.com/goreleaser/goreleaser/v2@$(GORELEASER_VERSION) check

manpages: ## Generate shell completions + man(1) pages into ./completions/ and ./manpages/ — same script the goreleaser before-hook runs
	VERSION="$(VERSION)" COMMIT="$(GIT_COMMIT)" DATE="$(BUILD_DATE)" ./scripts/completions-and-manpages.sh

## Cleanup

clean: ## Clean build artifacts
	rm -rf bin/ dist/ .tmp/
	rm -f coverage.out coverage.html

## Help

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'
