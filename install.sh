#!/bin/sh
set -e

# sbxb install / update / uninstall script
# Install:   sh -c "$(wget -qO- https://raw.githubusercontent.com/cyclestudy/sbxb/main/install.sh)"
# Update:    sh -c "$(wget -qO- https://raw.githubusercontent.com/cyclestudy/sbxb/main/install.sh)" -- update
# Uninstall: sh -c "$(wget -qO- https://raw.githubusercontent.com/cyclestudy/sbxb/main/install.sh)" -- uninstall

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

INSTALL_DIR="/usr/local/sbxb"
CONFIG_DIR="/etc/sbxb"
SERVICE_NAME="sbxb"
REPO="cyclestudy/sbxb"

info()  { echo -e "${GREEN}[INFO]${NC} $1"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $1"; }
error() { echo -e "${RED}[ERROR]${NC} $1"; exit 1; }

# Portable HTTP fetch: curl or wget
fetch() {
    local url="$1" dst="$2"
    if command -v curl >/dev/null 2>&1; then
        curl -sL "$url" -o "$dst" || return 1
    elif command -v wget >/dev/null 2>&1; then
        wget -qO "$dst" "$url" || return 1
    else
        error "Neither curl nor wget found. Install one first."
    fi
}

fetch_stdout() {
    local url="$1"
    if command -v curl >/dev/null 2>&1; then
        curl -sL "$url"
    elif command -v wget >/dev/null 2>&1; then
        wget -qO- "$url"
    else
        error "Neither curl nor wget found. Install one first."
    fi
}

detect_platform() {
    local os arch
    case "$(uname -s)" in
        Linux)   os="linux" ;;
        Darwin)  os="macos" ;;
        FreeBSD) os="freebsd" ;;
        *) error "Unsupported OS: $(uname -s)" ;;
    esac

    case "$(uname -m)" in
        x86_64|amd64)     arch="64" ;;
        i386|i686)        arch="32" ;;
        aarch64|arm64)    arch="arm64-v8a" ;;
        armv7*)           arch="arm32-v7a" ;;
        armv6*)           arch="arm32-v6" ;;
        armv5*|arm*)      arch="arm32-v5" ;;
        mips64el|mips64le) arch="mips64le" ;;
        mips64)           arch="mips64" ;;
        mipsel|mipsle)    arch="mips32le" ;;
        mips)             arch="mips32" ;;
        ppc64le)          arch="ppc64le" ;;
        riscv64)          arch="riscv64" ;;
        s390x)            arch="s390x" ;;
        *) error "Unsupported architecture: $(uname -m)" ;;
    esac

    echo "${os}-${arch}"
}

get_latest_version() {
    fetch_stdout "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed 's/.*"tag_name": "\(.*\)".*/\1/'
}

get_current_version() {
    if [ -x "${INSTALL_DIR}/sbxb" ]; then
        "${INSTALL_DIR}/sbxb" version 2>/dev/null | head -1 | awk '{print $2}'
    fi
}

download_and_extract() {
    local version="$1"
    local platform
    platform=$(detect_platform)
    local name="sbxb-${platform}"
    local url="https://github.com/${REPO}/releases/download/${version}/${name}"

    info "Downloading ${version} for ${platform}..."
    mkdir -p "$INSTALL_DIR"
    fetch "$url" "${INSTALL_DIR}/sbxb" || error "Download failed"

    # Verify it's not a 404 HTML page (Go binary is always > 1MB)
    local size
    size=$(wc -c < "${INSTALL_DIR}/sbxb")
    if [ "$size" -lt 1000000 ]; then
        rm -f "${INSTALL_DIR}/sbxb"
        error "Downloaded file is too small (${size} bytes). Check if this platform is supported."
    fi

    chmod +x "${INSTALL_DIR}/sbxb"
    mkdir -p /usr/local/bin
    ln -sf "${INSTALL_DIR}/sbxb" /usr/local/bin/sbxb
}

install() {
    local version
    version=$(get_latest_version)
    [ -z "$version" ] && error "Failed to get latest version"
    info "Latest version: ${version}"

    download_and_extract "$version"
    info "Binary installed to ${INSTALL_DIR}/sbxb"

    # Config
    mkdir -p "$CONFIG_DIR"
    if [ ! -f "${CONFIG_DIR}/config.json" ]; then
        cat > "${CONFIG_DIR}/config.json" <<'EOF'
{
    "log": {
        "level": "info"
    },
    "block_source_ips": ["geoip:cn"],
    "nodes": [
        {
            "api_host": "https://your-xboard-panel.com",
            "api_key": "your-api-token",
            "node_id": 1,
            "node_type": "vless",
            "timeout": 30
        }
    ]
}
EOF
        warn "Config created at ${CONFIG_DIR}/config.json — EDIT BEFORE STARTING"
    else
        info "Config already exists, skipping"
    fi

    # Install service (auto-detect init system)
    "${INSTALL_DIR}/sbxb" install
    info "Service installed"

    echo ""
    info "Installation complete! (${version})"
    echo ""
    echo "  Next steps:"
    echo "    1. Edit config:    nano ${CONFIG_DIR}/config.json"
    echo "    2. Start service:  sbxb start"
    echo "    3. Auto-start:     sbxb enable"
    echo "    4. View logs:      sbxb log"
    echo ""
}

update() {
    local current
    current=$(get_current_version)
    if [ -z "$current" ]; then
        warn "sbxb not installed, running full install..."
        install
        return
    fi

    local latest
    latest=$(get_latest_version)
    [ -z "$latest" ] && error "Failed to get latest version"

    if [ "$current" = "$latest" ]; then
        info "Already up to date: ${current}"
        return
    fi

    info "Updating: ${current} -> ${latest}"

    # Stop service if running
    local was_running=false
    if command -v systemctl >/dev/null 2>&1 && systemctl is-active --quiet ${SERVICE_NAME}.service 2>/dev/null; then
        was_running=true
        info "Stopping service..."
        systemctl stop ${SERVICE_NAME}.service
    elif [ -f "/etc/init.d/${SERVICE_NAME}" ] && /etc/init.d/${SERVICE_NAME} status >/dev/null 2>&1; then
        was_running=true
        info "Stopping service..."
        /etc/init.d/${SERVICE_NAME} stop
    fi

    download_and_extract "$latest"

    # Restart if was running
    if [ "$was_running" = true ]; then
        info "Starting service..."
        if command -v systemctl >/dev/null 2>&1; then
            systemctl start ${SERVICE_NAME}.service
        elif [ -f "/etc/init.d/${SERVICE_NAME}" ]; then
            /etc/init.d/${SERVICE_NAME} start
        fi
    fi

    info "Updated to ${latest}"
}

uninstall() {
    echo -n "This will stop and remove sbxb. Config will be preserved. Continue? [y/N] "
    read -r answer
    case "$answer" in
        [yY]|[yY][eE][sS]) ;;
        *) echo "Cancelled."; return ;;
    esac

    if command -v systemctl >/dev/null 2>&1; then
        systemctl stop ${SERVICE_NAME}.service 2>/dev/null || true
        systemctl disable ${SERVICE_NAME}.service 2>/dev/null || true
        rm -f /etc/systemd/system/${SERVICE_NAME}.service
        systemctl daemon-reload
    elif [ -f "/etc/init.d/${SERVICE_NAME}" ]; then
        /etc/init.d/${SERVICE_NAME} stop 2>/dev/null || true
        if command -v update-rc.d >/dev/null 2>&1; then
            update-rc.d -f ${SERVICE_NAME} remove 2>/dev/null || true
        elif command -v chkconfig >/dev/null 2>&1; then
            chkconfig ${SERVICE_NAME} off 2>/dev/null || true
        fi
        rm -f /etc/init.d/${SERVICE_NAME}
    fi

    rm -f /usr/local/bin/sbxb
    rm -rf "$INSTALL_DIR"

    info "sbxb removed. Config preserved at ${CONFIG_DIR}/"
    echo "  To remove config too: rm -rf ${CONFIG_DIR}"
}

# Main
case "${1:-install}" in
    install)   install ;;
    update)    update ;;
    uninstall) uninstall ;;
    *)
        echo "Usage: $0 {install|update|uninstall}"
        echo ""
        echo "  install    Install sbxb (default)"
        echo "  update     Update to latest version"
        echo "  uninstall  Remove sbxb (preserves config)"
        ;;
esac
