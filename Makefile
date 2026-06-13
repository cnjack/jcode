BIN := jcode
PKG := ./cmd/jcode/
LATEST_TAG := $(shell git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0")
COMPUTE_VERSION := $(shell echo "$(LATEST_TAG)" | sed 's/^v//' | awk -F. '{printf "v%s.%s.%d", $$1, $$2, $$3+1}')
VERSION ?= $(COMPUTE_VERSION)
BUILD_TIME ?= $(shell date +"%Y-%m-%dT%H:%M:%S%z")
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

LDFLAGS := -s -w \
	-X github.com/cnjack/jcode/internal/command.Version=$(VERSION) \
	-X github.com/cnjack/jcode/internal/command.BuildTime=$(BUILD_TIME) \
	-X github.com/cnjack/jcode/internal/command.GitCommit=$(GIT_COMMIT)

export GOFLAGS := -buildvcs=false

.PHONY: build build-binary run doctor version install clean build-web fmt lint lint-go lint-web generate setup-hooks

fmt:
	@echo "Formatting Go..."
	goimports -w .

lint: lint-go lint-web

lint-go:
	@echo "Linting Go..."
	golangci-lint run

lint-web:
	@echo "Linting frontend..."
	cd web && (pnpm install --frozen-lockfile 2>/dev/null || pnpm install)
	cd web && pnpm lint

generate:
	@echo "Generating code..."
	go generate ./internal/model/...
	go generate ./internal/theme/...

build-web:
	@echo "Building frontend..."
	cd web && (pnpm install --frozen-lockfile 2>/dev/null || pnpm install)
	cd web && npx vite build

build: generate build-web
	go build -ldflags "$(LDFLAGS)" -o $(BIN) $(PKG)

build-binary:
	go build -ldflags "$(LDFLAGS)" -o $(BIN) $(PKG)

install: generate build-web
	go install -ldflags "$(LDFLAGS)" $(PKG)

run:
	go run $(PKG)

doctor:
	go run $(PKG) --doctor

version:
	go run $(PKG) --version

clean:
	rm -f $(BIN)
	rm -rf internal/web/dist

setup-hooks:
	@git config core.hooksPath .githooks
	@echo "Git hooks installed (core.hooksPath = .githooks)"
