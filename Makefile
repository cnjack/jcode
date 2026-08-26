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

.PHONY: build build-binary run doctor version install clean build-web fmt lint lint-go lint-web generate setup-hooks desktop-icons desktop-sidecar desktop-dev desktop-build desktop-clean build-ble build-computerd build-computerd-bundle

# Swift defaults its deployment target to the build host. That made helpers
# compiled on newer CI hosts unloadable on macOS 14/15. Keep the target explicit
# and map Go's architecture names to the spellings Swift accepts.
TARGET_GOOS := $(if $(GOOS),$(GOOS),$(shell go env GOOS))
TARGET_GOARCH := $(if $(GOARCH),$(GOARCH),$(shell go env GOARCH))
SWIFTC ?= swiftc
SWIFT_MACOS_MIN ?= 14.0
SWIFT_ARCH := $(if $(filter arm64,$(TARGET_GOARCH)),arm64,$(if $(filter amd64,$(TARGET_GOARCH)),x86_64,))
SWIFT_TARGET := $(SWIFT_ARCH)-apple-macos$(SWIFT_MACOS_MIN)

ifeq ($(TARGET_GOOS),darwin)
ifeq ($(SWIFT_ARCH),)
$(error unsupported macOS architecture $(TARGET_GOARCH) for jcode-computerd)
endif
GO_INSTALL_BIN_DIR := $(if $(strip $(GOBIN)),$(GOBIN),$(if $(strip $(GOPATH)),$(firstword $(subst :, ,$(GOPATH)))/bin,$(shell bin="$$(go env GOBIN)"; if [ -n "$$bin" ]; then printf '%s' "$$bin"; else path="$$(go env GOPATH)"; printf '%s/bin' "$${path%%:*}"; fi)))
endif

fmt:
	@echo "Formatting Go..."
	goimports -w .

lint: lint-go lint-web

lint-go:
	@echo "Linting Go..."
	golangci-lint run

lint-web:
	@echo "Type-checking React frontend + packages..."
	-pnpm install --frozen-lockfile 2>/dev/null || true
	cd packages/jcode-ui-core && npx tsc --noEmit -p tsconfig.json
	cd packages/jcode-ui && npx tsc --noEmit -p tsconfig.json
	cd web && npx tsc --noEmit -p tsconfig.app.json

generate:
	@echo "Generating code..."
	go generate ./internal/model/...
	go generate ./internal/theme/...

# Build the React product UI (web/) + jcode-ui packages into internal/web/dist
# for Go //go:embed and the Tauri shell.
#
# NOTE: pnpm's ERR_PNPM_IGNORED_BUILDS (esbuild/@parcel/watcher native scripts)
# can return non-zero even when deps are fully installed. Tolerate install exit.
build-web: generate
	@echo "Building React frontend + packages..."
	-pnpm install --frozen-lockfile 2>/dev/null || true
	cd packages/jcode-ui-core && pnpm build
	cd packages/jcode-ui && pnpm build
	cd web && npx vite build

# The main binary never links CoreBluetooth (whose eager init triggers the macOS
# Bluetooth permission prompt at startup). BLE runs in a separate `jcode-ble`
# helper the main binary spawns only when BLE is enabled in config — so BLE is a
# pure runtime toggle with zero prompt when off. Build the helper once with
# `make build-ble`; no recompile is needed to flip it on/off after that.
build: generate build-web build-computerd
	go build -ldflags "$(LDFLAGS)" -o $(BIN) $(PKG)

build-binary:
	go build -ldflags "$(LDFLAGS)" -o $(BIN) $(PKG)

# cgo is REQUIRED for BLE on macOS (CoreBluetooth via cbgo). Without it, `-tags
# ble` on darwin silently falls back to the spawner stub — a helper that would
# spawn itself. Linux (D-Bus) / Windows (WinRT) BLE is pure Go, so cgo off is
# fine (and avoids needing a C toolchain). So: 1 on darwin, 0 elsewhere.
BLE_CGO := $(if $(filter darwin,$(shell go env GOOS)),1,0)

# Build the jcode-ble helper next to the main binary to enable BLE at runtime.
# After this, toggle BLE via config — no rebuild needed.
build-ble:
	CGO_ENABLED=$(BLE_CGO) go build -tags ble -ldflags "$(LDFLAGS)" -o $(dir $(BIN))jcode-ble ./cmd/jcode-ble

# Build the native computer-use helper daemon (Swift, macOS only) next to the
# main binary. It reads accessibility trees, synthesizes input, and captures
# windows behind the socket protocol in internal/computer/proto.go. macOS-only:
# the helper is not implemented on other platforms. After this, computer use is
# available at runtime once enabled in settings — no rebuild needed.
build-computerd:
ifeq ($(TARGET_GOOS),darwin)
	$(SWIFTC) -O -target $(SWIFT_TARGET) -o "$(dir $(BIN))jcode-computerd-capture" ./cmd/jcode-computerd/WindowCaptureHelper.swift
	$(SWIFTC) -O -target $(SWIFT_TARGET) -o "$(dir $(BIN))jcode-computerd" ./cmd/jcode-computerd/main.swift
else
	@echo "jcode-computerd is macOS only; skipping on $(TARGET_GOOS)"
endif

# Assemble the full jcode-computerd.app bundle next to the main binary. The
# bundle is what gives the helpers their own TCC identity ("jcode Computer
# Use" with its own icon in System Settings) instead of per-binary rows; the
# runtime prefers it over the bare binaries when both exist (helper_dial.go).
# Includes the Rust onboarding UI when cargo is available.
build-computerd-bundle:
ifeq ($(TARGET_GOOS),darwin)
	script/build_computerd_bundle.sh $(SWIFT_TARGET) "$(dir $(BIN))"
else
	@echo "jcode-computerd is macOS only; skipping on $(TARGET_GOOS)"
endif

install: generate build-web
	go install -ldflags "$(LDFLAGS)" $(PKG)
ifeq ($(TARGET_GOOS),darwin)
	@echo "Installing jcode-computerd helpers to $(GO_INSTALL_BIN_DIR)..."
	$(SWIFTC) -O -target $(SWIFT_TARGET) -o "$(GO_INSTALL_BIN_DIR)/jcode-computerd-capture" ./cmd/jcode-computerd/WindowCaptureHelper.swift
	$(SWIFTC) -O -target $(SWIFT_TARGET) -o "$(GO_INSTALL_BIN_DIR)/jcode-computerd" ./cmd/jcode-computerd/main.swift
endif

run:
	go run $(PKG)

doctor:
	go run $(PKG) --doctor

version:
	go run $(PKG) --version

clean:
	rm -f $(BIN)
	rm -f "$(dir $(BIN))jcode-computerd" "$(dir $(BIN))jcode-computerd-capture"
	rm -rf "$(dir $(BIN))jcode-computerd.app"
	rm -rf internal/web/dist
	rm -rf packages/jcode-ui/dist packages/jcode-ui-core/dist

setup-hooks:
	@git config core.hooksPath .githooks
	@echo "Git hooks installed (core.hooksPath = .githooks):"
	@echo "  pre-commit  fast gofmt/goimports gate on staged Go files"
	@echo "  pre-push    CI mirror: build + vet + golangci-lint (main: full; branches: new issues) + test"
	@echo "Bypass with --no-verify; skip only tests via 'SKIP_TESTS=1 git push'."

# ─── Desktop app (Tauri) ───
# The desktop app embeds the same jcode binary as a sidecar: Tauri renders the
# UI and provides native system integration, while the Go server (with the web
# UI baked in) runs on a loopback port. See site/docs/desktop.md.
DESKTOP_DIR  := desktop
SIDECAR_DIR  := $(DESKTOP_DIR)/src-tauri/binaries
RUST_TARGET  := $(shell rustc -vV 2>/dev/null | sed -n 's/^host: //p')
# Tauri's externalBin resolver requires the OS executable suffix, so Windows
# sidecars must be jcode-<triple>.exe.
SIDECAR_EXE  := $(if $(findstring windows,$(RUST_TARGET)),.exe,)
SWIFT_DESKTOP_ARCH = $(if $(filter aarch64-apple-darwin,$(RUST_TARGET)),arm64,$(if $(filter x86_64-apple-darwin,$(RUST_TARGET)),x86_64,))
SWIFT_DESKTOP_TARGET = $(SWIFT_DESKTOP_ARCH)-apple-macos$(SWIFT_MACOS_MIN)

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
	go build -tags "jcode_headless desktop" -ldflags "$(LDFLAGS)" -o $(SIDECAR_DIR)/jcode-$(RUST_TARGET)$(SIDECAR_EXE) $(PKG)
	@echo "Building jcode-ble helper for $(RUST_TARGET)..."
	CGO_ENABLED=$(BLE_CGO) go build -tags ble -ldflags "$(LDFLAGS)" -o $(SIDECAR_DIR)/jcode-ble-$(RUST_TARGET)$(SIDECAR_EXE) ./cmd/jcode-ble
ifeq ($(TARGET_GOOS),darwin)
	@echo "Building jcode-computerd.app helper bundle for $(RUST_TARGET)..."
	@test -n "$(SWIFT_DESKTOP_ARCH)" || { echo "Unsupported Rust target for Swift helper: $(RUST_TARGET)"; exit 1; }
	script/build_computerd_bundle.sh $(SWIFT_DESKTOP_TARGET) $(DESKTOP_DIR)/src-tauri/bundles $(RUST_TARGET)
endif

# Run the desktop app in development (hot window; rebuilds the sidecar first).
desktop-dev: desktop-sidecar
	cd $(DESKTOP_DIR) && (pnpm install 2>/dev/null || npm install) && pnpm tauri dev

# Produce a distributable bundle (.app/.dmg on macOS, .msi on Windows, etc.).
desktop-build: desktop-sidecar
	cd $(DESKTOP_DIR) && (pnpm install 2>/dev/null || npm install) && pnpm tauri build

desktop-clean:
	rm -rf $(SIDECAR_DIR) $(DESKTOP_DIR)/src-tauri/bundles $(DESKTOP_DIR)/src-tauri/target
