#!/bin/bash
# Regenerates the committed "jcode Computer Use" icon assets from source.
#
# The icon is *drawn in code* (cmd/jcode-computerd/onboarding/src/icon.rs, the
# same crate that draws the onboarding UI) so the design's source of truth is
# reviewable Rust, not an opaque binary. The rendered .icns is committed so
# ordinary bundle builds need neither cargo-at-build-time nor a rasterizer —
# re-run this script only when icon.rs changes.
#
# Usage: script/render_computerd_icon.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CRATE="$ROOT/cmd/jcode-computerd/onboarding"
OUT_DIR="$ROOT/cmd/jcode-computerd/icons"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

ICONSET="$TMP/jcode-computer-use.iconset"
cargo run --quiet --release --manifest-path "$CRATE/Cargo.toml" -- --render-icon "$ICONSET"

mkdir -p "$OUT_DIR"
iconutil -c icns "$ICONSET" -o "$OUT_DIR/jcode-computer-use.icns"
cp "$ICONSET/icon_512x512.png" "$OUT_DIR/jcode-computer-use-512.png"

echo "Rendered $OUT_DIR/jcode-computer-use.icns (+ 512px preview)"
