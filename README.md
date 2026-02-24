# sbxb

sing-box node backend for [XBoard](https://github.com/cedar2025/Xboard) panel.

Built on [sing-box](https://github.com/SagerNet/sing-box) core as a Go library, providing multi-protocol proxy node management with automatic user synchronization, traffic reporting, and device limiting.

## Features

- **9 protocols**: VMess, VLESS, Trojan, Shadowsocks, Hysteria2, TUIC, SOCKS, NaiveProxy, HTTP
- **6 transports**: TCP, WebSocket, gRPC, HTTP/2, HTTPUpgrade, QUIC
- **TLS**: Standard TLS + Reality
- **Multi-node**: Single instance manages multiple nodes simultaneously
- **Hot reload**: Config file changes auto-detected via fsnotify
- **Traffic tracking**: Per-user upload/download statistics with atomic counters
- **Device limiting**: Cross-node online device enforcement via panel alive API
- **Static binary**: `CGO_ENABLED=0`, zero runtime dependencies

## Quick Start

### One-click Install (Linux)

```bash
bash <(curl -sL https://raw.githubusercontent.com/cyclestudy/sbxb/main/install.sh)
```

This will download the latest release, install to `/usr/local/sbxb/`, create a systemd service, and generate a config template at `/etc/sbxb/config.json`.

### Manual Install

Download from [Releases](https://github.com/cyclestudy/sbxb/releases):

```bash
tar -xzf sbxb-linux-amd64.tar.gz
./sbxb-linux-amd64 server -c config.json
```

### Docker

```bash
docker run -d \
  --name sbxb \
  --restart unless-stopped \
  -v /etc/sbxb/config.json:/etc/sbxb/config.json \
  ghcr.io/cyclestudy/sbxb:latest
```

## Configuration

```json
{
    "log": {
        "level": "info",
        "output": ""
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
```

| Field | Description |
|---|---|
| `log.level` | Log level: `debug`, `info`, `warn`, `error` |
| `log.output` | Log file path (empty = stderr) |
| `nodes[].api_host` | XBoard panel URL |
| `nodes[].api_key` | Node communication token from panel |
| `nodes[].node_id` | Node ID assigned by panel |
| `nodes[].node_type` | Protocol: `vmess`, `vless`, `trojan`, `shadowsocks`, `hysteria`, `tuic`, `socks`, `naive`, `http` |
| `nodes[].timeout` | API request timeout in seconds (default: 30) |

Multiple nodes can be configured in the `nodes` array to run on a single instance.

## Usage

Run `sbxb` without arguments to enter the interactive management menu:

```
  sbxb Management
  Version: v0.2.0 (efc0df1)  Go go1.24.12 linux/amd64
  Status: running  Auto-start: enabled
—————————————————————————————
  0. Edit config
  1. Start sbxb
  2. Stop sbxb
  3. Restart sbxb
  4. View status
  5. View logs
  ...
```

### CLI Commands

| Command | Description |
|---|---|
| `sbxb` | Interactive management menu |
| `sbxb server -c config.json` | Start the node backend (foreground) |
| `sbxb start` | Start systemd service |
| `sbxb stop` | Stop systemd service |
| `sbxb restart` | Restart systemd service |
| `sbxb status` | Show service status |
| `sbxb log` | View service logs (realtime) |
| `sbxb enable` | Enable auto-start on boot |
| `sbxb disable` | Disable auto-start on boot |
| `sbxb config` | Edit config file, optionally restart |
| `sbxb update` | Self-update from GitHub releases |
| `sbxb install` | Install systemd service |
| `sbxb uninstall` | Uninstall service and binary |
| `sbxb version` | Print version information |

## Supported Platforms

| OS | Architecture |
|---|---|
| Linux | amd64, arm64, armv7 |
| macOS | amd64 (Intel), arm64 (Apple Silicon) |
| Windows | amd64 |
| FreeBSD | amd64 |

## Building from Source

```bash
# Requires Go 1.24+
go build -tags "with_quic" -o sbxb .
```

The `with_quic` build tag enables Hysteria2 and TUIC protocol support.

## Architecture

```
main.go → cmd/sbxb (CLI)
              ↓
          node/Manager (orchestrates multiple nodes)
              ↓
          node/Controller (per-node lifecycle)
           ↙     ↘
    api/xboard     core/Core (sing-box instance)
    (panel API)    core/inbound (protocol builders)
                   core/traffic (per-user counters)
                   limiter (speed/device limits)
```

## License

MIT
