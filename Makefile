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

.PHONY: build build-binary run doctor version install clean build-web fmt lint lint-go lint-web generate setup-hooks desktop-icons desktop-sidecar desktop-dev desktop-build desktop-clean

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

build-web: generate
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

# ─── Desktop app (Tauri) ───
# The desktop app embeds the same jcode binary as a sidecar: Tauri renders the
# UI and provides native system integration, while the Go server (with the web
# UI baked in) runs on a loopback port. See docs/desktop.md.
DESKTOP_DIR  := desktop
SIDECAR_DIR  := $(DESKTOP_DIR)/src-tauri/binaries
RUST_TARGET  := $(shell rustc -vV 2>/dev/null | sed -n 's/^host: //p')
# Tauri's externalBin resolver requires the OS executable suffix, so Windows
# sidecars must be jcode-<triple>.exe.
SIDECAR_EXE  := $(if $(findstring windows,$(RUST_TARGET)),.exe,)

# Regenerate the app icon set from the brand mark.
desktop-icons:
	cd $(DESKTOP_DIR) && npx --yes @tauri-apps/cli@2 icon ../web/public/icon.svg -o src-tauri/icons

# Build the sidecar binary for the desktop shell. The desktop app serves the
# page itself (Tauri's built-in frontend), so the sidecar is built with the
# `jcode_headless` tag: it omits the embedded SPA (no dist/ needed, smaller
# binary) and exposes only the REST + WebSocket API on a loopback port. The
# frontend is built separately and bundled by the Tauri dev/build targets.
desktop-sidecar: generate
	@echo "Building jcode sidecar for $(RUST_TARGET)..."
	@mkdir -p $(SIDECAR_DIR)
	go build -tags jcode_headless -ldflags "$(LDFLAGS)" -o $(SIDECAR_DIR)/jcode-$(RUST_TARGET)$(SIDECAR_EXE) $(PKG)

# Run the desktop app in development (hot window; rebuilds the sidecar first).
desktop-dev: desktop-sidecar
	cd $(DESKTOP_DIR) && (pnpm install 2>/dev/null || npm install) && pnpm tauri dev

# Produce a distributable bundle (.app/.dmg on macOS, .msi on Windows, etc.).
desktop-build: desktop-sidecar
	cd $(DESKTOP_DIR) && (pnpm install 2>/dev/null || npm install) && pnpm tauri build

desktop-clean:
	rm -rf $(SIDECAR_DIR) $(DESKTOP_DIR)/src-tauri/target
