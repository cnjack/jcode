#!/bin/sh
set -e

REPO="cnjack/jcode"
INSTALL_DIR="${JCODE_INSTALL_DIR:-/usr/local/bin}"
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

verify_checksum() {
    FILE="$1"
    URL="$2"
    CHECKSUM_FILE="${FILE}.sha256"

    download "${URL}.sha256" "$CHECKSUM_FILE"
    EXPECTED=$(awk 'NR == 1 { print $1 }' "$CHECKSUM_FILE")
    if [ -z "$EXPECTED" ]; then
        error "Checksum file for $(basename "$FILE") is empty."
        exit 1
    fi

    if command -v sha256sum >/dev/null 2>&1; then
        ACTUAL=$(sha256sum "$FILE" | awk '{ print $1 }')
    elif command -v shasum >/dev/null 2>&1; then
        ACTUAL=$(shasum -a 256 "$FILE" | awk '{ print $1 }')
    else
        error "Neither sha256sum nor shasum is available for checksum verification."
        exit 1
    fi
    if [ "$ACTUAL" != "$EXPECTED" ]; then
        error "Checksum verification failed for $(basename "$FILE")."
        exit 1
    fi
    ok "Verified $(basename "$FILE")"
}

install_ripgrep() {
    if command -v rg >/dev/null 2>&1; then
        ok "ripgrep (rg) already installed: $(command -v rg)"
        return 0
    fi

    info "Installing ripgrep (rg)..."
    OS=$(detect_os)

    if [ "$OS" = "linux" ]; then
        if command -v apt-get >/dev/null 2>&1; then
            sudo apt-get update -qq && sudo apt-get install -y -qq ripgrep
        elif command -v dnf >/dev/null 2>&1; then
            sudo dnf install -y ripgrep
        elif command -v yum >/dev/null 2>&1; then
            sudo yum install -y ripgrep
        elif command -v pacman >/dev/null 2>&1; then
            sudo pacman -S --noconfirm ripgrep
        elif command -v apk >/dev/null 2>&1; then
            sudo apk add ripgrep
        else
            warn "No supported package manager found. Installing from GitHub release..."
            install_ripgrep_from_github
            return $?
        fi
    elif [ "$OS" = "darwin" ]; then
        if command -v brew >/dev/null 2>&1; then
            brew install ripgrep
        else
            warn "Homebrew not found. Installing from GitHub release..."
            install_ripgrep_from_github
            return $?
        fi
    else
        warn "Auto-install not supported on $OS. Please install manually:"
        warn "https://github.com/BurntSushi/ripgrep#installation"
        return 1
    fi

    if command -v rg >/dev/null 2>&1; then
        ok "ripgrep installed successfully: $(rg --version | head -1)"
    else
        error "ripgrep installation failed."
        return 1
    fi
}

install_ripgrep_from_github() {
    RG_VERSION=$(curl -fsSL "https://api.github.com/repos/BurntSushi/ripgrep/releases/latest" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')
    if [ -z "$RG_VERSION" ]; then
        error "Failed to fetch ripgrep version."
        return 1
    fi

    ARCH=$(detect_arch)
    case "$ARCH" in
        amd64)  RG_ARCH="x86_64" ;;
        arm64)  RG_ARCH="aarch64" ;;
        *)      error "Unsupported arch for ripgrep: $ARCH"; return 1 ;;
    esac

    RG_TARGET="${RG_ARCH}-unknown-linux-musl"
    RG_FILENAME="ripgrep-${RG_VERSION}-${RG_TARGET}.tar.gz"
    RG_URL="https://github.com/BurntSushi/ripgrep/releases/download/${RG_VERSION}/${RG_FILENAME}"

    RG_TMP=$(mktemp -d)
    trap 'rm -rf "$RG_TMP"' EXIT

    info "Downloading ${RG_URL}..."
    download "$RG_URL" "${RG_TMP}/${RG_FILENAME}"
    tar xzf "${RG_TMP}/${RG_FILENAME}" -C "$RG_TMP"

    RG_BIN=$(find "$RG_TMP" -name rg -type f | head -1)
    if [ -z "$RG_BIN" ]; then
        error "Failed to extract ripgrep binary."
        return 1
    fi

    chmod +x "$RG_BIN"
    if [ -w "$INSTALL_DIR" ]; then
        mv "$RG_BIN" "${INSTALL_DIR}/rg"
    else
        sudo mv "$RG_BIN" "${INSTALL_DIR}/rg"
    fi
}

main() {
    printf "\n"
    info "  JCODE — Coding Assistant Installer"
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
    TMPFILE="${TMPDIR}/${FILENAME}"
    trap 'rm -rf "$TMPDIR"' EXIT

    info "Downloading ${DOWNLOAD_URL}..."
    download "$DOWNLOAD_URL" "$TMPFILE"

    if [ ! -f "$TMPFILE" ]; then
        error "Download failed."
        exit 1
    fi
    verify_checksum "$TMPFILE" "$DOWNLOAD_URL"

    # The macOS accessibility daemon and its isolated capture worker are part
    # of the CLI runtime, not optional examples. Download and verify the whole
    # set before installing any file so a partial release cannot leave a mixed
    # version in the install directory.
    HELPER_FILE=""
    CAPTURE_FILE=""
    if [ "$OS" = "darwin" ]; then
        HELPER_NAME="jcode-computerd-${OS}-${ARCH}"
        CAPTURE_NAME="jcode-computerd-capture-${OS}-${ARCH}"
        HELPER_URL="https://github.com/${REPO}/releases/download/${VERSION}/${HELPER_NAME}"
        CAPTURE_URL="https://github.com/${REPO}/releases/download/${VERSION}/${CAPTURE_NAME}"
        HELPER_FILE="${TMPDIR}/${HELPER_NAME}"
        CAPTURE_FILE="${TMPDIR}/${CAPTURE_NAME}"

        info "Downloading ${HELPER_URL}..."
        download "$HELPER_URL" "$HELPER_FILE"
        info "Downloading ${CAPTURE_URL}..."
        download "$CAPTURE_URL" "$CAPTURE_FILE"
        verify_checksum "$HELPER_FILE" "$HELPER_URL"
        verify_checksum "$CAPTURE_FILE" "$CAPTURE_URL"
    fi

    chmod +x "$TMPFILE"
    if [ "$OS" = "darwin" ]; then
        chmod +x "$HELPER_FILE" "$CAPTURE_FILE"
    fi

    # Install
    if [ -w "$INSTALL_DIR" ]; then
        mv "$TMPFILE" "${INSTALL_DIR}/${BINARY}${SUFFIX}"
        if [ "$OS" = "darwin" ]; then
            mv "$HELPER_FILE" "${INSTALL_DIR}/jcode-computerd"
            mv "$CAPTURE_FILE" "${INSTALL_DIR}/jcode-computerd-capture"
        fi
    else
        warn "Need sudo to install to ${INSTALL_DIR}"
        sudo mv "$TMPFILE" "${INSTALL_DIR}/${BINARY}${SUFFIX}"
        if [ "$OS" = "darwin" ]; then
            sudo mv "$HELPER_FILE" "${INSTALL_DIR}/jcode-computerd"
            sudo mv "$CAPTURE_FILE" "${INSTALL_DIR}/jcode-computerd-capture"
        fi
    fi

    ok "Installed ${BINARY} ${VERSION} to ${INSTALL_DIR}/${BINARY}${SUFFIX}"
    if [ "$OS" = "darwin" ]; then
        ok "Installed jcode-computerd and jcode-computerd-capture to ${INSTALL_DIR}"
    fi
    printf "\n"

    # Install ripgrep dependency
    install_ripgrep
    printf "\n"

    info "Run 'jcode --version' to verify."
    printf "\n"
}

main "$@"
