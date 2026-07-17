#!/bin/bash
# Assembles jcode-computerd.app — one .app bundle holding ALL computer-use
# helper executables: the long-lived AX daemon, the short-lived
# ScreenCaptureKit worker, and the permission-onboarding UI.
#
# Why a bundle, and why one bundle: macOS attributes TCC consent to a code
# identity. Accessibility keys on the calling binary, but Screen Recording
# keys on the *responsible process* — a bare-binary helper spawned by the
# desktop app inherits jcode-desktop's identity, and one spawned from a
# terminal inherits the terminal's. Packaging the executables in a single
# signed .app pins all grants to one branded identity ("jcode Computer
# Use", with its own icon) regardless of who launched it — the same shape
# Codex uses for "Codex Computer Use". One identity deliberately: every
# additional bundle would be another row the user has to authorize.
#
# The icon is drawn in code — see script/render_computerd_icon.sh; the
# committed .icns is copied here so ordinary builds don't rasterize.
#
# Usage: build_computerd_bundle.sh <swift-target-triple> <output-dir> [rust-target-triple]
# Produces <output-dir>/jcode-computerd.app.
set -euo pipefail

SWIFT_TARGET="${1:?usage: build_computerd_bundle.sh <swift-target> <output-dir> [rust-target]}"
OUT_DIR="${2:?usage: build_computerd_bundle.sh <swift-target> <output-dir> [rust-target]}"
RUST_TARGET="${3:-}"

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BUNDLE="${OUT_DIR}/jcode-computerd.app"
MACOS_DIR="${BUNDLE}/Contents/MacOS"
RESOURCES_DIR="${BUNDLE}/Contents/Resources"

rm -rf "$BUNDLE"
mkdir -p "$MACOS_DIR" "$RESOURCES_DIR"

swiftc -O -target "$SWIFT_TARGET" \
    -o "$MACOS_DIR/jcode-computerd" \
    "$ROOT/cmd/jcode-computerd/main.swift"
swiftc -O -target "$SWIFT_TARGET" \
    -o "$MACOS_DIR/jcode-computerd-capture" \
    "$ROOT/cmd/jcode-computerd/WindowCaptureHelper.swift"

# Onboarding UI (Rust). Skipped with a warning when cargo is unavailable —
# the daemon detects the missing binary and falls back to bare TCC prompts,
# so a Swift-only toolchain still yields a working bundle.
ONBOARDING_CRATE="$ROOT/cmd/jcode-computerd/onboarding"
if command -v cargo >/dev/null 2>&1; then
    if [ -z "$RUST_TARGET" ]; then
        # arm64-apple-macos14.0 -> aarch64-apple-darwin
        case "$SWIFT_TARGET" in
            arm64-*) RUST_TARGET="aarch64-apple-darwin" ;;
            x86_64-*) RUST_TARGET="x86_64-apple-darwin" ;;
        esac
    fi
    HOST_RUST_TARGET="$(rustc -vV | sed -n 's/^host: //p')"
    if [ -n "$RUST_TARGET" ] && [ "$RUST_TARGET" != "$HOST_RUST_TARGET" ]; then
        cargo build --quiet --release --manifest-path "$ONBOARDING_CRATE/Cargo.toml" \
            --target "$RUST_TARGET"
        ONBOARDING_BIN="$ONBOARDING_CRATE/target/$RUST_TARGET/release/jcode-computerd-onboarding"
    else
        cargo build --quiet --release --manifest-path "$ONBOARDING_CRATE/Cargo.toml"
        ONBOARDING_BIN="$ONBOARDING_CRATE/target/release/jcode-computerd-onboarding"
    fi
    cp "$ONBOARDING_BIN" "$MACOS_DIR/jcode-computerd-onboarding"
else
    echo "warning: cargo not found — bundling without the onboarding UI" >&2
fi

cp "$ROOT/cmd/jcode-computerd/Info.plist" "$BUNDLE/Contents/Info.plist"
cp "$ROOT/cmd/jcode-computerd/icons/jcode-computer-use.icns" \
    "$RESOURCES_DIR/jcode-computer-use.icns"

# Ad-hoc sign so the bundle is self-consistently sealed in local/dev builds.
# Every executable is signed with the BUNDLE's identifier: a TCC grant stores
# the granting process's designated requirement, and with Developer ID
# signing a shared identifier is what lets a grant obtained by the onboarding
# UI validate for the daemon and capture worker too (one identity, one row).
# Ad-hoc signatures are still pinned per-binary by cdhash — a known dev-mode
# limitation: local builds may re-prompt per binary and per rebuild. Release
# wiring (Developer ID re-sign of this bundle in release.yml) is NOT built
# yet — see computer-helper-design.md §11; until then only local/dev installs
# use the bundle.
codesign --force --sign - --identifier com.cnjack.jcode.computerd \
    --timestamp=none "$MACOS_DIR/jcode-computerd-capture"
if [ -x "$MACOS_DIR/jcode-computerd-onboarding" ]; then
    codesign --force --sign - --identifier com.cnjack.jcode.computerd \
        --timestamp=none "$MACOS_DIR/jcode-computerd-onboarding"
fi
codesign --force --sign - --identifier com.cnjack.jcode.computerd \
    --timestamp=none "$MACOS_DIR/jcode-computerd"
codesign --force --sign - --timestamp=none "$BUNDLE"

echo "Built ${BUNDLE}"
