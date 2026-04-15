#!/bin/sh
set -e

REPO="cnjack/jcode"
INSTALL_DIR="/usr/local/bin"
BINARY="jcode"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

info()  { printf "${CYAN}%s${NC}\n" "$1"; }
ok()    { printf "${GREEN}%s${NC}\n" "$1"; }
warn()  { printf "${YELLOW}%s${NC}\n" "$1"; }
error() { printf "${RED}%s${NC}\n" "$1"; }

detect_os() {
    OS=$(uname -s | tr '[:upper:]' '[:lower:]')
    case "$OS" in
        linux)  echo "linux" ;;
        darwin) echo "darwin" ;;
        mingw*|msys*|cygwin*) echo "windows" ;;
        *) error "Unsupported OS: $OS"; exit 1 ;;
    esac
}

detect_arch() {
    ARCH=$(uname -m)
    case "$ARCH" in
        x86_64|amd64)  echo "amd64" ;;
        aarch64|arm64) echo "arm64" ;;
        *) error "Unsupported architecture: $ARCH"; exit 1 ;;
    esac
}

get_latest_version() {
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/'
    elif command -v wget >/dev/null 2>&1; then
        wget -qO- "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/'
    else
        error "Neither curl nor wget found. Please install one of them."
        exit 1
    fi
}

download() {
    URL="$1"
    OUTPUT="$2"
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL -o "$OUTPUT" "$URL"
    elif command -v wget >/dev/null 2>&1; then
        wget -qO "$OUTPUT" "$URL"
    fi
}

main() {
    printf "\n"
    info "  Little Jack — Coding Assistant Installer"
    printf "\n"

    OS=$(detect_os)
    ARCH=$(detect_arch)
    info "Detected: ${OS}/${ARCH}"

    # Allow version override via env or argument
    VERSION="${JCODE_VERSION:-$1}"
    if [ -z "$VERSION" ]; then
        info "Fetching latest version..."
        VERSION=$(get_latest_version)
    fi

    if [ -z "$VERSION" ]; then
        error "Failed to determine version. Set JCODE_VERSION or pass as argument."
        exit 1
    fi

    ok "Version: ${VERSION}"

    SUFFIX=""
    if [ "$OS" = "windows" ]; then
        SUFFIX=".exe"
    fi

    FILENAME="${BINARY}-${OS}-${ARCH}${SUFFIX}"
    DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${VERSION}/${FILENAME}"

    TMPDIR=$(mktemp -d)
    TMPFILE="${TMPDIR}/${BINARY}${SUFFIX}"
    trap 'rm -rf "$TMPDIR"' EXIT

    info "Downloading ${DOWNLOAD_URL}..."
    download "$DOWNLOAD_URL" "$TMPFILE"

    if [ ! -f "$TMPFILE" ]; then
        error "Download failed."
        exit 1
    fi

    chmod +x "$TMPFILE"

    # Install
    if [ -w "$INSTALL_DIR" ]; then
        mv "$TMPFILE" "${INSTALL_DIR}/${BINARY}${SUFFIX}"
    else
        warn "Need sudo to install to ${INSTALL_DIR}"
        sudo mv "$TMPFILE" "${INSTALL_DIR}/${BINARY}${SUFFIX}"
    fi

    ok "Installed ${BINARY} ${VERSION} to ${INSTALL_DIR}/${BINARY}${SUFFIX}"
    printf "\n"
    info "Run 'jcode --version' to verify."
    printf "\n"
}

main "$@"
