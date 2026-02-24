package xboard

// NodeInfo contains the full node configuration returned by the XBoard panel.
type NodeInfo struct {
	// Core settings
	Protocol   string `json:"protocol"`
	ListenIP   string `json:"listen_ip"`
	ServerPort int    `json:"server_port"`

	// Network transport
	Network         string                 `json:"network"`
	NetworkSettings map[string]interface{} `json:"networkSettings"`

	// TLS configuration
	TLS         int                    `json:"tls"`
	TLSSettings map[string]interface{} `json:"tls_settings"`

	// VLESS
	Flow string `json:"flow"`

	// Shadowsocks
	Cipher     string `json:"cipher"`
	ServerKey  string `json:"server_key"`
	Plugin     string `json:"plugin"`
	PluginOpts string `json:"plugin_opts"`

	// Hysteria / Hysteria2
	Version      int    `json:"version"`
	UpMbps       int    `json:"up_mbps"`
	DownMbps     int    `json:"down_mbps"`
	Obfs         string `json:"obfs"`
	ObfsPassword string `json:"obfs-password"`

	// TUIC
	CongestionControl string `json:"congestion_control"`
	AuthTimeout       string `json:"auth_timeout"`
	ZeroRTTHandshake  bool   `json:"zero_rtt_handshake"`
	Heartbeat         string `json:"heartbeat"`

	// Trojan
	Host       string `json:"host"`
	ServerName string `json:"server_name"`

	// Base config and routing
	BaseConfig BaseConfig `json:"base_config"`
	Routes     []Route    `json:"routes"`
}

// BaseConfig holds timing intervals for push/pull operations.
type BaseConfig struct {
	PushInterval int `json:"push_interval"`
	PullInterval int `json:"pull_interval"`
}

// Route defines a traffic routing rule from the panel.
type Route struct {
	ID          int      `json:"id"`
	Match       []string `json:"match"`
	Action      string   `json:"action"`
	ActionValue string   `json:"action_value"`
}

// UserInfo represents a user fetched from the panel.
type UserInfo struct {
	ID          int    `json:"id"`
	UUID        string `json:"uuid"`
	SpeedLimit  int    `json:"speed_limit"`
	DeviceLimit int    `json:"device_limit"`
}

// AliveMap maps a user ID (as string) to their online device count.
type AliveMap map[string]int

// StatusReport is sent to the panel to report server resource usage.
type StatusReport struct {
	CPU  float64 `json:"cpu"`
	Mem  MemInfo `json:"mem"`
	Swap MemInfo `json:"swap"`
	Disk MemInfo `json:"disk"`
}

// MemInfo describes memory/disk usage in bytes.
type MemInfo struct {
	Total int64 `json:"total"`
	Used  int64 `json:"used"`
}
