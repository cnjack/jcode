#!/bin/bash
# build_frontend.sh — Builds the Vue frontend and embeds it into the Go binary.
#
# Usage:
#   ./build_frontend.sh          Build frontend only
#   ./build_frontend.sh --go     Build frontend + Go binary
#
# The Vite build outputs to internal/web/dist/ which is picked up by
# Go's embed directive in internal/web/frontend.go.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$SCRIPT_DIR"
WEB_DIR="$PROJECT_ROOT/web"
DIST_DIR="$PROJECT_ROOT/internal/web/dist"

echo "📦 Building jcode frontend..."

# Step 1: Install dependencies if needed
if [ ! -d "$WEB_DIR/node_modules" ]; then
  echo "  → Installing npm dependencies..."
  cd "$WEB_DIR"
  pnpm install --frozen-lockfile 2>/dev/null || pnpm install
fi

# Step 2: Build the frontend
echo "  → Running Vite build..."
cd "$WEB_DIR"
npx vite build

# Step 3: Verify output
if [ ! -f "$DIST_DIR/index.html" ]; then
  echo "❌ Build failed: $DIST_DIR/index.html not found"
  exit 1
fi

FILE_COUNT=$(find "$DIST_DIR" -type f | wc -l)
TOTAL_SIZE=$(du -sh "$DIST_DIR" | cut -f1)
echo "  ✅ Frontend built: $FILE_COUNT files, $TOTAL_SIZE total → $DIST_DIR"

# Step 4: Optionally build Go binary
if [ "${1:-}" = "--go" ]; then
  echo ""
  echo "🔨 Building Go binary..."
  cd "$PROJECT_ROOT"
  go build -o jcode ./cmd/jcode/
  echo "  ✅ Binary built: ./jcode"
  echo ""
  echo "Run with: ./jcode web [--port 8080] [--host 127.0.0.1]"
fi
