#!/bin/bash
set -e

# sbxb install / update / uninstall script
# Install:   bash <(curl -sL https://raw.githubusercontent.com/cyclestudy/sbxb/main/install.sh)
# Update:    bash <(curl -sL https://raw.githubusercontent.com/cyclestudy/sbxb/main/install.sh) update
# Uninstall: bash <(curl -sL https://raw.githubusercontent.com/cyclestudy/sbxb/main/install.sh) uninstall

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

detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64) echo "amd64" ;;
        aarch64|arm64) echo "arm64" ;;
        armv7*) echo "armv7" ;;
        *) error "Unsupported architecture: $(uname -m)" ;;
    esac
}

detect_os() {
    case "$(uname -s)" in
        Linux)   echo "linux" ;;
        Darwin)  echo "darwin" ;;
        FreeBSD) echo "freebsd" ;;
        *) error "Unsupported OS: $(uname -s)" ;;
    esac
}

get_latest_version() {
    curl -sL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed 's/.*"tag_name": "\(.*\)".*/\1/'
}

get_current_version() {
    if [ -x "${INSTALL_DIR}/sbxb" ]; then
        "${INSTALL_DIR}/sbxb" version 2>/dev/null | head -1 | awk '{print $2}'
    fi
}

download_and_extract() {
    local version="$1"
    local os=$(detect_os)
    local arch=$(detect_arch)
    local binary="sbxb-${os}-${arch}"
    local url="https://github.com/${REPO}/releases/download/${version}/${binary}.tar.gz"

    info "Downloading ${version} for ${os}/${arch}..."
    local tmpdir=$(mktemp -d)
    curl -sL "$url" -o "${tmpdir}/${binary}.tar.gz" || error "Download failed"
    tar -xzf "${tmpdir}/${binary}.tar.gz" -C "${tmpdir}" || error "Extract failed"

    mkdir -p "$INSTALL_DIR"
    cp "${tmpdir}/${binary}" "${INSTALL_DIR}/sbxb"
    chmod +x "${INSTALL_DIR}/sbxb"
    ln -sf "${INSTALL_DIR}/sbxb" /usr/local/bin/sbxb

    rm -rf "$tmpdir"
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

    # Systemd
    if command -v systemctl &>/dev/null; then
        cat > /etc/systemd/system/${SERVICE_NAME}.service <<EOF
[Unit]
Description=sbxb - sing-box node backend for XBoard
After=network.target

[Service]
Type=simple
ExecStart=${INSTALL_DIR}/sbxb server -c ${CONFIG_DIR}/config.json
Restart=on-failure
RestartSec=5
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
EOF
        systemctl daemon-reload
        info "Systemd service installed"
    fi

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
    if command -v systemctl &>/dev/null && systemctl is-active --quiet ${SERVICE_NAME}.service 2>/dev/null; then
        was_running=true
        info "Stopping service..."
        systemctl stop ${SERVICE_NAME}.service
    fi

    download_and_extract "$latest"

    # Restart if was running
    if [ "$was_running" = true ]; then
        info "Starting service..."
        systemctl start ${SERVICE_NAME}.service
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

    if command -v systemctl &>/dev/null; then
        systemctl stop ${SERVICE_NAME}.service 2>/dev/null || true
        systemctl disable ${SERVICE_NAME}.service 2>/dev/null || true
        rm -f /etc/systemd/system/${SERVICE_NAME}.service
        systemctl daemon-reload
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
