# sbxb

sing-box node backend for [XBoard](https://github.com/cedar2025/Xboard) panel.

Built on [sing-box](https://github.com/SagerNet/sing-box) core as a Go library, providing multi-protocol proxy node management with automatic user synchronization, traffic reporting, and device limiting.

## Features

- **9 protocols**: VMess, VLESS, Trojan, Shadowsocks, Hysteria2, TUIC, SOCKS, NaiveProxy, HTTP
- **6 transports**: TCP, WebSocket, gRPC, HTTP/2, HTTPUpgrade, QUIC
- **TLS**: Standard TLS + Reality (uTLS)
- **Multi-node**: Single instance manages multiple nodes simultaneously
- **Hot reload**: Config file changes auto-detected, panel route rule changes auto-synced (60s)
- **Traffic tracking**: Per-user upload/download statistics with atomic counters
- **Device limiting**: Cross-node online device enforcement via panel alive API
- **Route rules**: Full v2node-compatible route rule support (8 action types)
- **Source IP blocking**: Block connections from specified countries/CIDRs
- **Static binary**: `CGO_ENABLED=0`, zero runtime dependencies

## Quick Start

### One-click Install (Linux)

```bash
bash <(curl -sL https://raw.githubusercontent.com/cyclestudy/sbxb/main/install.sh)
```

### Update

```bash
bash <(curl -sL https://raw.githubusercontent.com/cyclestudy/sbxb/main/install.sh) update
```

Or if already installed: `sbxb update`

### Uninstall

```bash
bash <(curl -sL https://raw.githubusercontent.com/cyclestudy/sbxb/main/install.sh) uninstall
```

Or if already installed: `sbxb uninstall`

### Manual Install

Download from [Releases](https://github.com/cyclestudy/sbxb/releases):

```bash
tar -xzf sbxb-linux-amd64.tar.gz
./sbxb-linux-amd64 server -c config.json
```

## Configuration

```json
{
    "log": {
        "level": "info",
        "output": ""
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
```

| Field | Description |
|---|---|
| `log.level` | Log level: `debug`, `info`, `warn`, `error` |
| `log.output` | Log file path (empty = stderr) |
| `block_source_ips` | Block incoming connections by source IP. Supports `geoip:XX` and CIDR (e.g. `["geoip:cn", "10.0.0.0/8"]`) |
| `nodes[].api_host` | XBoard panel URL |
| `nodes[].api_key` | Node communication token from panel |
| `nodes[].node_id` | Node ID assigned by panel |
| `nodes[].node_type` | Protocol: `vmess`, `vless`, `trojan`, `shadowsocks`, `hysteria`, `tuic`, `socks`, `naive`, `http` |
| `nodes[].timeout` | API request timeout in seconds (default: 30) |

Multiple nodes can be configured in the `nodes` array to run on a single instance.

## Route Rules

Automatically syncs route rules from the XBoard panel (60s polling interval). Supports all v2node-compatible action types:

| Action | Description |
|---|---|
| `block` | Block matched domains |
| `block_ip` | Block destination IPs / GeoIP (e.g. `geoip:cn`) |
| `block_port` | Block destination ports / port ranges |
| `protocol` | Block protocols (e.g. `bittorrent`, `webrtc`) with auto sniff |
| `dns` | Custom DNS server for matched domains |
| `route` | Route matched domains to custom outbound (SOCKS/HTTP) |
| `route_ip` | Route matched IPs / GeoIP to custom outbound |
| `default_out` | Default outbound for all traffic (SOCKS/HTTP) |

When route rules change on the panel, sbxb automatically detects the change and performs a full reload within the next polling cycle.

## Usage

Run `sbxb` without arguments to enter the interactive management menu:

```
  sbxb Management
  Version: v0.3.4  Go go1.24.12 linux/amd64
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
go build -tags "with_quic,with_utls" -o sbxb .
```

Build tags:
- `with_quic` — Hysteria2 / TUIC protocol support
- `with_utls` — Reality (uTLS) support

## Architecture

```
main.go → cmd/sbxb (CLI + systemd management)
              ↓
          node/Manager (orchestrates nodes, route rules, source IP blocking)
              ↓
          node/Controller (per-node lifecycle, hot reload)
           ↙     ↘
    api/xboard     core/Core (sing-box instance)
    (panel API)    core/inbound (9 protocol builders)
                   core/route (8 route rule types + source IP blocking)
                   core/traffic (per-user counters)
                   limiter (speed/device limits)
```

## License

MIT
