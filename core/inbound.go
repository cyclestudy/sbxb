package core

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"strconv"
	"strings"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/auth"
	"github.com/sagernet/sing/common/json/badoption"

	"github.com/cyclestudy/sbxb/api/xboard"
)

// BuildInbound constructs a sing-box Inbound option from XBoard NodeInfo and
// user list. The tag is formatted as "{protocol}-{nodeID}" so that traffic
// statistics can be correlated back to the panel node.
func BuildInbound(nodeInfo *xboard.NodeInfo, users []xboard.UserInfo, nodeID int) (*option.Inbound, error) {
	tag := fmt.Sprintf("%s-%d", nodeInfo.Protocol, nodeID)

	switch nodeInfo.Protocol {
	case "vmess":
		return buildVMess(nodeInfo, users, tag)
	case "vless":
		return buildVLess(nodeInfo, users, tag)
	case "trojan":
		return buildTrojan(nodeInfo, users, tag)
	case "shadowsocks":
		return buildShadowsocks(nodeInfo, users, tag)
	case "hysteria":
		return buildHysteria2(nodeInfo, users, tag)
	case "tuic":
		return buildTUIC(nodeInfo, users, tag)
	case "socks":
		return buildSOCKS(nodeInfo, users, tag)
	case "naive":
		return buildNaive(nodeInfo, users, tag)
	case "http":
		return buildHTTP(nodeInfo, users, tag)
	default:
		return nil, fmt.Errorf("unsupported protocol: %s", nodeInfo.Protocol)
	}
}

// ---------------------------------------------------------------------------
// Helper: map value extraction (JSON maps decode numbers as float64)
// ---------------------------------------------------------------------------

func getMapString(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getMapInt(m map[string]interface{}, key string) int {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	case string:
		n, _ := strconv.Atoi(v)
		return n
	}
	return 0
}

func getMapUint16(m map[string]interface{}, key string) uint16 {
	return uint16(getMapInt(m, key))
}

func getMapStringMap(m map[string]interface{}, key string) map[string]interface{} {
	if m == nil {
		return nil
	}
	if v, ok := m[key].(map[string]interface{}); ok {
		return v
	}
	return nil
}

// getCertPath extracts the certificate file path from tls_settings,
// supporting both XBoard naming conventions:
//   - certificate_path (standard)
//   - cert_file (SubscriptionGuard plugin)
func getCertPath(m map[string]interface{}) string {
	if p := getMapString(m, "certificate_path"); p != "" {
		return p
	}
	return getMapString(m, "cert_file")
}

// getKeyPath extracts the private key file path from tls_settings,
// supporting both XBoard naming conventions:
//   - key_path (standard)
//   - key_file (SubscriptionGuard plugin)
func getKeyPath(m map[string]interface{}) string {
	if p := getMapString(m, "key_path"); p != "" {
		return p
	}
	return getMapString(m, "key_file")
}

// ---------------------------------------------------------------------------
// Listen options
// ---------------------------------------------------------------------------

func buildListenOptions(nodeInfo *xboard.NodeInfo) option.ListenOptions {
	listenIP := nodeInfo.ListenIP
	if listenIP == "" {
		listenIP = "0.0.0.0"
	}

	addr, err := netip.ParseAddr(listenIP)
	if err != nil {
		slog.Warn("invalid listen IP, falling back to 0.0.0.0",
			"listen_ip", listenIP, "error", err)
		addr = netip.MustParseAddr("0.0.0.0")
	}

	listenAddr := badoption.Addr(addr)
	return option.ListenOptions{
		Listen:     &listenAddr,
		ListenPort: uint16(nodeInfo.ServerPort),
	}
}

// ---------------------------------------------------------------------------
// TLS
// ---------------------------------------------------------------------------

func buildTLS(nodeInfo *xboard.NodeInfo) *option.InboundTLSOptions {
	if nodeInfo.TLS == 0 {
		return nil
	}

	// Resolve server_name: prefer tls_settings, fall back to top-level fields.
	serverName := getMapString(nodeInfo.TLSSettings, "server_name")
	if serverName == "" {
		serverName = nodeInfo.ServerName
	}
	if serverName == "" {
		serverName = nodeInfo.Host
	}

	// Reality (tls == 2)
	if nodeInfo.TLS == 2 {
		privateKey := getMapString(nodeInfo.TLSSettings, "private_key")
		shortID := getMapString(nodeInfo.TLSSettings, "short_id")

		// Handshake destination defaults to server_name:443.
		dest := getMapString(nodeInfo.TLSSettings, "dest")
		if dest == "" {
			dest = serverName
		}
		destPort := getMapUint16(nodeInfo.TLSSettings, "server_port")
		if destPort == 0 {
			destPort = 443
		}

		return &option.InboundTLSOptions{
			Enabled:    true,
			ServerName: serverName,
			Reality: &option.InboundRealityOptions{
				Enabled:    true,
				PrivateKey: privateKey,
				ShortID:    badoption.Listable[string]{shortID},
				Handshake: option.InboundRealityHandshakeOptions{
					ServerOptions: option.ServerOptions{
						Server:     dest,
						ServerPort: destPort,
					},
				},
			},
		}
	}

	// Standard TLS (tls == 1)
	certPath := getCertPath(nodeInfo.TLSSettings)
	keyPath := getKeyPath(nodeInfo.TLSSettings)

	return &option.InboundTLSOptions{
		Enabled:         true,
		ServerName:      serverName,
		CertificatePath: certPath,
		KeyPath:         keyPath,
	}
}

// ensureTLS returns the TLS config from buildTLS, or creates one for
// protocols that always require TLS (Hysteria2, TUIC). It checks
// tls_settings for cert paths (supporting both certificate_path/key_path
// and cert_file/key_file naming), then default paths, then self-signed.
func ensureTLS(nodeInfo *xboard.NodeInfo) (*option.InboundTLSOptions, error) {
	if tls := buildTLS(nodeInfo); tls != nil {
		return tls, nil
	}

	// Resolve server name from available fields.
	serverName := getMapString(nodeInfo.TLSSettings, "server_name")
	if serverName == "" {
		serverName = nodeInfo.ServerName
	}
	if serverName == "" {
		serverName = nodeInfo.Host
	}
	if serverName == "" {
		serverName = "localhost"
	}

	// Check tls_settings for cert paths (even when tls==0).
	certPath := getCertPath(nodeInfo.TLSSettings)
	keyPath := getKeyPath(nodeInfo.TLSSettings)
	if certPath != "" && keyPath != "" {
		if _, err := os.Stat(certPath); err == nil {
			if _, err := os.Stat(keyPath); err == nil {
				slog.Info("TLS: using cert from tls_settings",
					"cert", certPath, "key", keyPath)
				return &option.InboundTLSOptions{
					Enabled:         true,
					ServerName:      serverName,
					CertificatePath: certPath,
					KeyPath:         keyPath,
				}, nil
			}
		}
		slog.Warn("TLS: cert paths in tls_settings not found on disk",
			"cert", certPath, "key", keyPath)
	}

	// Try default cert paths.
	defaultCert := "/root/.cert/server.crt"
	defaultKey := "/root/.cert/server.key"
	if _, err := os.Stat(defaultCert); err == nil {
		if _, err := os.Stat(defaultKey); err == nil {
			slog.Info("TLS: using default cert files",
				"cert", defaultCert, "key", defaultKey)
			return &option.InboundTLSOptions{
				Enabled:         true,
				ServerName:      serverName,
				CertificatePath: defaultCert,
				KeyPath:         defaultKey,
			}, nil
		}
	}

	// No cert files found — generate self-signed.
	certPEM, keyPEM, err := generateSelfSignedCert(serverName)
	if err != nil {
		return nil, fmt.Errorf("failed to generate self-signed cert: %w", err)
	}

	slog.Info("TLS: generated self-signed certificate", "serverName", serverName)
	return &option.InboundTLSOptions{
		Enabled:    true,
		ServerName: serverName,
		Certificate: badoption.Listable[string]{certPEM},
		Key:         badoption.Listable[string]{keyPEM},
	}, nil
}

// ---------------------------------------------------------------------------
// V2Ray Transport
// ---------------------------------------------------------------------------

func buildTransport(nodeInfo *xboard.NodeInfo) *option.V2RayTransportOptions {
	network := nodeInfo.Network
	if network == "" || network == "tcp" {
		return nil
	}

	ns := nodeInfo.NetworkSettings // may be nil; helpers handle that

	switch network {
	case "ws":
		path := getMapString(ns, "path")
		if path == "" {
			path = "/"
		}

		host := getMapString(ns, "host")
		if host == "" {
			// Nested headers.Host pattern used by some panels.
			headers := getMapStringMap(ns, "headers")
			host = getMapString(headers, "Host")
		}

		wsOpts := option.V2RayWebsocketOptions{
			Path: path,
		}
		if host != "" {
			wsOpts.Headers = badoption.HTTPHeader(map[string]badoption.Listable[string]{
				"Host": {host},
			})
		}

		return &option.V2RayTransportOptions{
			Type:             C.V2RayTransportTypeWebsocket,
			WebsocketOptions: wsOpts,
		}

	case "grpc":
		serviceName := getMapString(ns, "serviceName")
		if serviceName == "" {
			serviceName = getMapString(ns, "service_name")
		}

		return &option.V2RayTransportOptions{
			Type: C.V2RayTransportTypeGRPC,
			GRPCOptions: option.V2RayGRPCOptions{
				ServiceName: serviceName,
			},
		}

	case "h2":
		path := getMapString(ns, "path")
		if path == "" {
			path = "/"
		}

		var hosts []string
		if ns != nil {
			switch h := ns["host"].(type) {
			case string:
				if h != "" {
					hosts = []string{h}
				}
			case []interface{}:
				for _, item := range h {
					if s, ok := item.(string); ok && s != "" {
						hosts = append(hosts, s)
					}
				}
			}
		}

		return &option.V2RayTransportOptions{
			Type: C.V2RayTransportTypeHTTP,
			HTTPOptions: option.V2RayHTTPOptions{
				Host: badoption.Listable[string](hosts),
				Path: path,
			},
		}

	case "httpupgrade":
		path := getMapString(ns, "path")
		if path == "" {
			path = "/"
		}
		host := getMapString(ns, "host")

		opts := option.V2RayHTTPUpgradeOptions{
			Path: path,
			Host: host,
		}

		// Optional custom headers.
		if headers := getMapStringMap(ns, "headers"); headers != nil {
			httpHeaders := make(map[string]badoption.Listable[string])
			for k, v := range headers {
				if s, ok := v.(string); ok {
					httpHeaders[k] = badoption.Listable[string]{s}
				}
			}
			if len(httpHeaders) > 0 {
				opts.Headers = badoption.HTTPHeader(httpHeaders)
			}
		}

		return &option.V2RayTransportOptions{
			Type:               C.V2RayTransportTypeHTTPUpgrade,
			HTTPUpgradeOptions: opts,
		}

	case "quic":
		return &option.V2RayTransportOptions{
			Type: C.V2RayTransportTypeQUIC,
		}

	default:
		slog.Warn("unsupported transport network, falling back to raw TCP",
			"network", network)
		return nil
	}
}

// ---------------------------------------------------------------------------
// VMess
// ---------------------------------------------------------------------------

func buildVMess(nodeInfo *xboard.NodeInfo, users []xboard.UserInfo, tag string) (*option.Inbound, error) {
	vmessUsers := make([]option.VMessUser, 0, len(users))
	for _, u := range users {
		vmessUsers = append(vmessUsers, option.VMessUser{
			Name: strconv.Itoa(u.ID),
			UUID: u.UUID,
		})
	}

	return &option.Inbound{
		Type: C.TypeVMess,
		Tag:  tag,
		Options: &option.VMessInboundOptions{
			ListenOptions: buildListenOptions(nodeInfo),
			Users:         vmessUsers,
			InboundTLSOptionsContainer: option.InboundTLSOptionsContainer{
				TLS: buildTLS(nodeInfo),
			},
			Transport: buildTransport(nodeInfo),
		},
	}, nil
}

// ---------------------------------------------------------------------------
// VLESS
// ---------------------------------------------------------------------------

func buildVLess(nodeInfo *xboard.NodeInfo, users []xboard.UserInfo, tag string) (*option.Inbound, error) {
	vlessUsers := make([]option.VLESSUser, 0, len(users))
	for _, u := range users {
		vlessUsers = append(vlessUsers, option.VLESSUser{
			Name: strconv.Itoa(u.ID),
			UUID: u.UUID,
			Flow: nodeInfo.Flow,
		})
	}

	return &option.Inbound{
		Type: C.TypeVLESS,
		Tag:  tag,
		Options: &option.VLESSInboundOptions{
			ListenOptions: buildListenOptions(nodeInfo),
			Users:         vlessUsers,
			InboundTLSOptionsContainer: option.InboundTLSOptionsContainer{
				TLS: buildTLS(nodeInfo),
			},
			Transport: buildTransport(nodeInfo),
		},
	}, nil
}

// ---------------------------------------------------------------------------
// Trojan
// ---------------------------------------------------------------------------

func buildTrojan(nodeInfo *xboard.NodeInfo, users []xboard.UserInfo, tag string) (*option.Inbound, error) {
	trojanUsers := make([]option.TrojanUser, 0, len(users))
	for _, u := range users {
		trojanUsers = append(trojanUsers, option.TrojanUser{
			Name:     strconv.Itoa(u.ID),
			Password: u.UUID,
		})
	}

	return &option.Inbound{
		Type: C.TypeTrojan,
		Tag:  tag,
		Options: &option.TrojanInboundOptions{
			ListenOptions: buildListenOptions(nodeInfo),
			Users:         trojanUsers,
			InboundTLSOptionsContainer: option.InboundTLSOptionsContainer{
				TLS: buildTLS(nodeInfo),
			},
			Transport: buildTransport(nodeInfo),
		},
	}, nil
}

// ---------------------------------------------------------------------------
// Shadowsocks
// ---------------------------------------------------------------------------

// isSIP022 reports whether the cipher is a SIP022 (2022-blake3-*) method that
// supports multi-user with per-user sub-keys.
func isSIP022(method string) bool {
	return strings.HasPrefix(method, "2022-blake3-")
}

// sip022KeySize returns the required key length in bytes for the given SIP022
// cipher method.
func sip022KeySize(method string) int {
	switch method {
	case "2022-blake3-aes-128-gcm":
		return 16
	case "2022-blake3-aes-256-gcm":
		return 32
	case "2022-blake3-chacha20-poly1305":
		return 32
	default:
		return 16
	}
}

// deriveSSUserKey converts a UUID string to a base64-encoded Shadowsocks
// sub-key for SIP022 multi-user mode.
//
// This follows the XBoard / V2bX convention: take the first keySize bytes
// of the UUID string as raw ASCII (including dashes) and base64-encode them.
//
// For 2022-blake3-aes-128-gcm (16 bytes): uuid[:16]
// For 2022-blake3-aes-256-gcm (32 bytes): uuid[:32]
func deriveSSUserKey(uuid string, keySize int) string {
	if len(uuid) < keySize {
		return base64.StdEncoding.EncodeToString([]byte(uuid))
	}
	return base64.StdEncoding.EncodeToString([]byte(uuid[:keySize]))
}

func buildShadowsocks(nodeInfo *xboard.NodeInfo, users []xboard.UserInfo, tag string) (*option.Inbound, error) {
	method := nodeInfo.Cipher
	if method == "" {
		return nil, fmt.Errorf("shadowsocks cipher method is required")
	}

	opts := option.ShadowsocksInboundOptions{
		ListenOptions: buildListenOptions(nodeInfo),
		Method:        method,
	}

	if len(users) == 0 {
		return nil, fmt.Errorf("shadowsocks requires at least one user")
	}

	ssUsers := make([]option.ShadowsocksUser, 0, len(users))

	if isSIP022(method) {
		// SIP022 multi-user mode: server PSK + per-user sub-keys.
		if nodeInfo.ServerKey == "" {
			return nil, fmt.Errorf("shadowsocks SIP022 requires a server_key")
		}
		opts.Password = nodeInfo.ServerKey

		keySize := sip022KeySize(method)
		for _, u := range users {
			ssUsers = append(ssUsers, option.ShadowsocksUser{
				Name:     strconv.Itoa(u.ID),
				Password: deriveSSUserKey(u.UUID, keySize),
			})
		}
	} else {
		// Legacy AEAD ciphers: multi-user mode via ShadowsocksMulti.
		for _, u := range users {
			ssUsers = append(ssUsers, option.ShadowsocksUser{
				Name:     strconv.Itoa(u.ID),
				Password: u.UUID,
			})
		}
	}
	opts.Users = ssUsers

	return &option.Inbound{
		Type:    C.TypeShadowsocks,
		Tag:     tag,
		Options: &opts,
	}, nil
}

// ---------------------------------------------------------------------------
// Hysteria2
// ---------------------------------------------------------------------------

func buildHysteria2(nodeInfo *xboard.NodeInfo, users []xboard.UserInfo, tag string) (*option.Inbound, error) {
	hy2Users := make([]option.Hysteria2User, 0, len(users))
	for _, u := range users {
		hy2Users = append(hy2Users, option.Hysteria2User{
			Name:     strconv.Itoa(u.ID),
			Password: u.UUID,
		})
	}

	tls, err := ensureTLS(nodeInfo)
	if err != nil {
		return nil, fmt.Errorf("hysteria2: %w", err)
	}

	opts := option.Hysteria2InboundOptions{
		ListenOptions:         buildListenOptions(nodeInfo),
		UpMbps:                nodeInfo.UpMbps,
		DownMbps:              nodeInfo.DownMbps,
		IgnoreClientBandwidth: nodeInfo.UpMbps == 0 && nodeInfo.DownMbps == 0,
		Users:                 hy2Users,
		InboundTLSOptionsContainer: option.InboundTLSOptionsContainer{
			TLS: tls,
		},
	}

	if nodeInfo.Obfs != "" {
		opts.Obfs = &option.Hysteria2Obfs{
			Type:     nodeInfo.Obfs,
			Password: nodeInfo.ObfsPassword,
		}
	}

	return &option.Inbound{
		Type:    C.TypeHysteria2,
		Tag:     tag,
		Options: &opts,
	}, nil
}

// ---------------------------------------------------------------------------
// TUIC
// ---------------------------------------------------------------------------

func buildTUIC(nodeInfo *xboard.NodeInfo, users []xboard.UserInfo, tag string) (*option.Inbound, error) {
	tuicUsers := make([]option.TUICUser, 0, len(users))
	for _, u := range users {
		tuicUsers = append(tuicUsers, option.TUICUser{
			Name:     strconv.Itoa(u.ID),
			UUID:     u.UUID,
			Password: u.UUID,
		})
	}

	congestion := nodeInfo.CongestionControl
	if congestion == "" {
		congestion = "bbr" // sensible default
	}

	tls, err := ensureTLS(nodeInfo)
	if err != nil {
		return nil, fmt.Errorf("tuic: %w", err)
	}

	return &option.Inbound{
		Type: C.TypeTUIC,
		Tag:  tag,
		Options: &option.TUICInboundOptions{
			ListenOptions:     buildListenOptions(nodeInfo),
			Users:             tuicUsers,
			CongestionControl: congestion,
			InboundTLSOptionsContainer: option.InboundTLSOptionsContainer{
				TLS: tls,
			},
		},
	}, nil
}

// ---------------------------------------------------------------------------
// SOCKS
// ---------------------------------------------------------------------------

func buildSOCKS(nodeInfo *xboard.NodeInfo, users []xboard.UserInfo, tag string) (*option.Inbound, error) {
	socksUsers := make([]auth.User, 0, len(users))
	for _, u := range users {
		socksUsers = append(socksUsers, auth.User{
			Username: u.UUID,
			Password: u.UUID,
		})
	}

	return &option.Inbound{
		Type: C.TypeSOCKS,
		Tag:  tag,
		Options: &option.SocksInboundOptions{
			ListenOptions: buildListenOptions(nodeInfo),
			Users:         socksUsers,
		},
	}, nil
}

// ---------------------------------------------------------------------------
// Naive
// ---------------------------------------------------------------------------

func buildNaive(nodeInfo *xboard.NodeInfo, users []xboard.UserInfo, tag string) (*option.Inbound, error) {
	naiveUsers := make([]auth.User, 0, len(users))
	for _, u := range users {
		naiveUsers = append(naiveUsers, auth.User{
			Username: strconv.Itoa(u.ID),
			Password: u.UUID,
		})
	}

	return &option.Inbound{
		Type: C.TypeNaive,
		Tag:  tag,
		Options: &option.NaiveInboundOptions{
			ListenOptions: buildListenOptions(nodeInfo),
			Users:         naiveUsers,
			InboundTLSOptionsContainer: option.InboundTLSOptionsContainer{
				TLS: buildTLS(nodeInfo),
			},
		},
	}, nil
}

// ---------------------------------------------------------------------------
// HTTP
// ---------------------------------------------------------------------------

func buildHTTP(nodeInfo *xboard.NodeInfo, users []xboard.UserInfo, tag string) (*option.Inbound, error) {
	httpUsers := make([]auth.User, 0, len(users))
	for _, u := range users {
		httpUsers = append(httpUsers, auth.User{
			Username: strconv.Itoa(u.ID),
			Password: u.UUID,
		})
	}

	return &option.Inbound{
		Type: C.TypeHTTP,
		Tag:  tag,
		Options: &option.HTTPMixedInboundOptions{
			ListenOptions: buildListenOptions(nodeInfo),
			Users:         httpUsers,
			InboundTLSOptionsContainer: option.InboundTLSOptionsContainer{
				TLS: buildTLS(nodeInfo),
			},
		},
	}, nil
}
