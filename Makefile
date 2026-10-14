# Developer tasks. CI runs the same commands (see .github/workflows/ci.yml).
GO ?= go
GOLANGCI_LINT_VERSION := v2.14.0
GOVULNCHECK_VERSION := v1.8.0
GORELEASER_VERSION := v2.18.2

.PHONY: build test cover lint fmt vuln hygiene snapshot check

build:
	$(GO) build -o kemory ./cmd/kemory

test:
	$(GO) test -race ./...

cover:
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out | tail -1

lint:
	$(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run ./...

fmt:
	$(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) fmt ./...

vuln:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

hygiene:
	scripts/check-public-hygiene.sh

snapshot:
	$(GO) run github.com/goreleaser/goreleaser/v2@$(GORELEASER_VERSION) release --snapshot --clean

check: hygiene lint test vuln
