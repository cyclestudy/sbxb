package node

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"

	"github.com/cyclestudy/sbxb/api/xboard"
	"github.com/cyclestudy/sbxb/conf"
	"github.com/cyclestudy/sbxb/core"
)

// Manager orchestrates multiple node Controllers, one per configured
// node. It owns the shared sing-box Core and TrafficTracker.
type Manager struct {
	controllers []*Controller
	core        *core.Core
	tracker     *core.TrafficTracker

	mu sync.Mutex
}

// NewManager creates a Manager but does not start anything. Call
// Start() to initialize the core and launch controllers.
func NewManager() *Manager {
	return &Manager{}
}

// Start initializes the sing-box core, creates a traffic tracker,
// then creates and starts a Controller for each node configuration.
// If any controller fails to start, previously started controllers
// are closed and an error is returned.
func (m *Manager) Start(ctx context.Context, configs []conf.NodeConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 1. Pre-fetch node info for all nodes to collect route rules.
	type prefetch struct {
		config   conf.NodeConfig
		nodeInfo *xboard.NodeInfo
		client   *xboard.Client
	}

	fetched := make([]prefetch, 0, len(configs))
	var allRoutes []xboard.Route

	for _, cfg := range configs {
		client := xboard.NewClient(cfg.ApiHost, cfg.ApiKey, cfg.NodeID, cfg.NodeType, cfg.Timeout)
		nodeInfo, err := client.GetNodeInfo()
		if err != nil {
			return fmt.Errorf("manager: failed to pre-fetch node %d info: %w", cfg.NodeID, err)
		}
		fetched = append(fetched, prefetch{config: cfg, nodeInfo: nodeInfo, client: client})
		allRoutes = append(allRoutes, nodeInfo.Routes...)
	}

	// 2. Build route rules from panel routes.
	routeResult := core.BuildRouteRules(allRoutes)

	// 3. Create the sing-box core.
	m.core = core.New()

	// 4. Build base options with direct + block outbounds and route rules.
	baseOpts := option.Options{
		Log: &option.LogOptions{
			Level: "warning",
		},
		Outbounds: []option.Outbound{
			{
				Type:    C.TypeDirect,
				Tag:     "direct",
				Options: &option.DirectOutboundOptions{},
			},
			{
				Type:    C.TypeBlock,
				Tag:     "block",
				Options: &option.StubOptions{},
			},
			{
				Type:    C.TypeDNS,
				Tag:     "dns-out",
				Options: &option.StubOptions{},
			},
		},
	}

	// Add extra outbounds (e.g. default_out SOCKS proxy).
	baseOpts.Outbounds = append(baseOpts.Outbounds, routeResult.Outbounds...)

	// Build route options.
	if len(routeResult.Rules) > 0 || routeResult.NeedSniff {
		var rules []option.Rule

		// Add sniff rule first if protocol detection is needed.
		if routeResult.NeedSniff {
			rules = append(rules, option.Rule{
				Type: C.RuleTypeDefault,
				DefaultOptions: option.DefaultRule{
					RuleAction: option.RuleAction{
						Action: C.RuleActionTypeSniff,
					},
				},
			})
		}

		rules = append(rules, routeResult.Rules...)

		finalOut := "direct"
		if routeResult.Final != "" {
			finalOut = routeResult.Final
		}

		baseOpts.Route = &option.RouteOptions{
			Rules:   rules,
			RuleSet: routeResult.RuleSets,
			Final:   finalOut,
		}
		slog.Info("route configured",
			"rules", len(rules),
			"final", finalOut,
			"sniff", routeResult.NeedSniff,
		)
	}

	// Build DNS options if custom DNS rules exist.
	if len(routeResult.DNSServers) > 0 {
		// Always include a local DNS server as fallback.
		servers := []option.DNSServerOptions{
			{
				Type:    C.DNSTypeLocal,
				Tag:     "local-dns",
				Options: &option.LocalDNSServerOptions{},
			},
		}
		servers = append(servers, routeResult.DNSServers...)

		baseOpts.DNS = &option.DNSOptions{
			RawDNSOptions: option.RawDNSOptions{
				Servers: servers,
				Rules:   routeResult.DNSRules,
				Final:   "local-dns",
			},
		}
		slog.Info("dns configured",
			"servers", len(servers),
			"rules", len(routeResult.DNSRules),
		)
	}

	// 5. Start the core.
	if err := m.core.Start(baseOpts); err != nil {
		return fmt.Errorf("manager: failed to start core: %w", err)
	}

	// 6. Create traffic tracker and register with the router.
	m.tracker = core.NewTrafficTracker()
	m.core.SetTracker(m.tracker)

	// 7. Create and start a controller for each node (with pre-fetched info).
	m.controllers = make([]*Controller, 0, len(fetched))
	for _, f := range fetched {
		ctrl := NewController(f.config, m.core, m.tracker)
		ctrl.SetNodeInfo(f.nodeInfo)
		if err := ctrl.Start(ctx); err != nil {
			slog.Error("manager: failed to start controller, rolling back",
				"nodeID", f.config.NodeID,
				"error", err,
			)
			// Roll back: close already-started controllers and core.
			for _, started := range m.controllers {
				started.Close()
			}
			m.controllers = nil
			_ = m.core.Close()
			return fmt.Errorf("manager: node %d failed to start: %w", f.config.NodeID, err)
		}
		m.controllers = append(m.controllers, ctrl)
		slog.Info("manager: controller started", "nodeID", f.config.NodeID)
	}

	slog.Info("manager: all controllers started", "count", len(m.controllers))
	return nil
}

// Close shuts down all controllers and the core.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, ctrl := range m.controllers {
		ctrl.Close()
	}
	m.controllers = nil

	if m.core != nil {
		if err := m.core.Close(); err != nil {
			slog.Error("manager: failed to close core", "error", err)
		}
		m.core = nil
	}

	slog.Info("manager: closed")
}

// Reload tears down all controllers and the core, then restarts with
// the new set of configurations. This is a full restart, not a hot
// reload.
func (m *Manager) Reload(ctx context.Context, configs []conf.NodeConfig) error {
	slog.Info("manager: reloading", "newNodeCount", len(configs))

	m.Close()

	if err := m.Start(ctx, configs); err != nil {
		return fmt.Errorf("manager: reload failed: %w", err)
	}

	slog.Info("manager: reload complete")
	return nil
}
