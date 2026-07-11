GO ?= go
GOBIN ?= $(shell $(GO) env GOPATH)/bin

# Pin the linter version so local runs and CI stay in lockstep. Bump both
# this value and the version in .github/workflows/go.yml together.
GOLANGCI_LINT_VERSION := v2.1.6
GOLANGCI_LINT := $(GOBIN)/golangci-lint

.PHONY: help
help:
	@echo "Targets:"
	@echo "  make build          - go build ./..."
	@echo "  make test           - run tests with the race detector"
	@echo "  make vet            - run go vet ./..."
	@echo "  make fmt            - run gofmt (writes in place)"
	@echo "  make lint           - run golangci-lint"
	@echo "  make lint-fix       - run golangci-lint with --fix"
	@echo "  make check          - vet + lint + test"
	@echo "  make install-tools  - install pinned golangci-lint"

.PHONY: build
build:
	$(GO) build ./...

.PHONY: test
test:
	$(GO) test -race -count=1 ./...

.PHONY: vet
vet:
	$(GO) vet ./...

.PHONY: fmt
fmt:
	gofmt -s -w .

.PHONY: lint
lint: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run ./...

.PHONY: lint-fix
lint-fix: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run --fix ./...

.PHONY: check
check: vet lint test

.PHONY: install-tools
install-tools:
	$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

# Install the linter on demand if the pinned binary is missing. Avoids
# breaking `make lint` on a fresh clone.
$(GOLANGCI_LINT):
	$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
