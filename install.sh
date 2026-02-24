#!/bin/bash
set -e

# sbxb install script
# Usage: bash <(curl -sL https://raw.githubusercontent.com/YOUR_REPO/sbxb/main/install.sh)

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

install() {
    local os=$(detect_os)
    local arch=$(detect_arch)
    local binary="sbxb-${os}-${arch}"

    info "Detecting system: ${os}/${arch}"

    # Get latest version
    local version
    version=$(curl -sL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed 's/.*"tag_name": "\(.*\)".*/\1/')
    if [ -z "$version" ]; then
        error "Failed to get latest version"
    fi
    info "Latest version: ${version}"

    # Download
    local url="https://github.com/${REPO}/releases/download/${version}/${binary}.tar.gz"
    info "Downloading from: ${url}"

    local tmpdir=$(mktemp -d)
    curl -sL "$url" -o "${tmpdir}/${binary}.tar.gz" || error "Download failed"
    tar -xzf "${tmpdir}/${binary}.tar.gz" -C "${tmpdir}" || error "Extract failed"

    # Install binary
    mkdir -p "$INSTALL_DIR"
    cp "${tmpdir}/${binary}" "${INSTALL_DIR}/sbxb"
    chmod +x "${INSTALL_DIR}/sbxb"
    info "Binary installed to ${INSTALL_DIR}/sbxb"

    # Install config
    mkdir -p "$CONFIG_DIR"
    if [ ! -f "${CONFIG_DIR}/config.json" ]; then
        if [ -f "${tmpdir}/config.json.example" ]; then
            cp "${tmpdir}/config.json.example" "${CONFIG_DIR}/config.json"
        else
            cat > "${CONFIG_DIR}/config.json" <<'EOF'
{
    "log": {
        "level": "info"
    },
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
        fi
        warn "Config file created at ${CONFIG_DIR}/config.json - EDIT IT BEFORE STARTING"
    else
        info "Config file already exists, skipping"
    fi

    # Create symlink
    ln -sf "${INSTALL_DIR}/sbxb" /usr/local/bin/sbxb

    # Install systemd service
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

    # Cleanup
    rm -rf "$tmpdir"

    info "Installation complete!"
    echo ""
    echo "  1. Edit config:    nano ${CONFIG_DIR}/config.json"
    echo "  2. Start service:  systemctl start ${SERVICE_NAME}"
    echo "  3. Enable on boot: systemctl enable ${SERVICE_NAME}"
    echo "  4. View logs:      journalctl -u ${SERVICE_NAME} -f"
    echo ""
}

uninstall() {
    info "Uninstalling sbxb..."

    if command -v systemctl &>/dev/null; then
        systemctl stop ${SERVICE_NAME} 2>/dev/null || true
        systemctl disable ${SERVICE_NAME} 2>/dev/null || true
        rm -f /etc/systemd/system/${SERVICE_NAME}.service
        systemctl daemon-reload
    fi

    rm -f /usr/local/bin/sbxb
    rm -rf "$INSTALL_DIR"
    info "Binary removed. Config at ${CONFIG_DIR} preserved."
}

case "${1:-install}" in
    install)   install ;;
    uninstall) uninstall ;;
    *) echo "Usage: $0 {install|uninstall}" ;;
esac
