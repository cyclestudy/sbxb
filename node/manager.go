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

	// Stored for full reload on route changes.
	lastCtx  context.Context
	lastCfg  conf.Config
	routeChangeCh chan struct{}
	reloadCancel  context.CancelFunc

	reloadMu sync.Mutex // serializes Reload calls
	mu       sync.Mutex // protects fields above
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
func (m *Manager) Start(ctx context.Context, cfg conf.Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Store for route-change full reload.
	m.lastCtx = ctx
	m.lastCfg = cfg
	m.routeChangeCh = make(chan struct{}, 1)

	// 1. Pre-fetch node info for all nodes to collect route rules.
	type prefetch struct {
		config   conf.NodeConfig
		nodeInfo *xboard.NodeInfo
		client   *xboard.Client
	}

	fetched := make([]prefetch, 0, len(cfg.Nodes))
	routeSeen := make(map[int]bool)
	var allRoutes []xboard.Route

	for _, nodeCfg := range cfg.Nodes {
		client := xboard.NewClient(nodeCfg.ApiHost, nodeCfg.ApiKey, nodeCfg.NodeID, nodeCfg.NodeType, nodeCfg.Timeout)
		nodeInfo, err := client.GetNodeInfo()
		if err != nil {
			return fmt.Errorf("manager: failed to pre-fetch node %d info: %w", nodeCfg.NodeID, err)
		}
		fetched = append(fetched, prefetch{config: nodeCfg, nodeInfo: nodeInfo, client: client})
		for _, r := range nodeInfo.Routes {
			if !routeSeen[r.ID] {
				routeSeen[r.ID] = true
				allRoutes = append(allRoutes, r)
			}
		}
	}

	// 2. Build route rules from panel routes.
	routeResult := core.BuildRouteRules(allRoutes)

	// 3. Create the sing-box core.
	m.core = core.New()

	// 4. Build base options with direct + block outbounds and route rules.
	logLevel := cfg.Log.Level
	if logLevel == "" {
		logLevel = "warning"
	}
	baseOpts := option.Options{
		Log: &option.LogOptions{
			Level: logLevel,
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

	// Build source IP blocking rules from config.
	var srcRules []option.Rule
	var srcRuleSets []option.RuleSet
	if len(cfg.BlockSourceIPs) > 0 {
		srcRules, srcRuleSets = core.BuildSourceIPRules(cfg.BlockSourceIPs)
	}

	// Build route options (deduplicate rule sets by tag).
	allRuleSets := deduplicateRuleSets(append(routeResult.RuleSets, srcRuleSets...))

	if len(routeResult.Rules) > 0 || len(srcRules) > 0 || routeResult.NeedSniff {
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

		// Source IP blocking rules first (reject early).
		rules = append(rules, srcRules...)
		rules = append(rules, routeResult.Rules...)

		finalOut := "direct"
		if routeResult.Final != "" {
			finalOut = routeResult.Final
		}

		baseOpts.Route = &option.RouteOptions{
			Rules:   rules,
			RuleSet: allRuleSets,
			Final:   finalOut,
		}
		slog.Info("route configured",
			"rules", len(rules),
			"final", finalOut,
			"sniff", routeResult.NeedSniff,
			"sourceBlocked", len(srcRules),
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
		ctrl.routeChangeCh = m.routeChangeCh
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

	// Launch route-change watcher.
	reloadCtx, reloadCancel := context.WithCancel(ctx)
	m.reloadCancel = reloadCancel
	go m.watchRouteChanges(reloadCtx)

	return nil
}

// Close shuts down all controllers and the core.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.reloadCancel != nil {
		m.reloadCancel()
		m.reloadCancel = nil
	}

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

// watchRouteChanges waits for route-change signals from any controller
// and triggers a full Manager reload.
func (m *Manager) watchRouteChanges(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.routeChangeCh:
			slog.Info("manager: route change detected, performing full reload")

			m.mu.Lock()
			savedCtx := m.lastCtx
			savedCfg := m.lastCfg
			m.mu.Unlock()

			if err := m.Reload(savedCtx, savedCfg); err != nil {
				slog.Error("manager: route-change reload failed", "error", err)
			}
		}
	}
}

// Reload tears down all controllers and the core, then restarts with
// the new set of configurations. This is a full restart, not a hot
// reload. It is serialized via reloadMu to prevent concurrent reloads
// from orphaning resources.
func (m *Manager) Reload(ctx context.Context, cfg conf.Config) error {
	m.reloadMu.Lock()
	defer m.reloadMu.Unlock()

	slog.Info("manager: reloading", "newNodeCount", len(cfg.Nodes))

	m.Close()

	if err := m.Start(ctx, cfg); err != nil {
		return fmt.Errorf("manager: reload failed: %w", err)
	}

	slog.Info("manager: reload complete")
	return nil
}

// deduplicateRuleSets removes duplicate RuleSets by tag, keeping the first occurrence.
func deduplicateRuleSets(sets []option.RuleSet) []option.RuleSet {
	seen := make(map[string]bool, len(sets))
	result := make([]option.RuleSet, 0, len(sets))
	for _, s := range sets {
		if !seen[s.Tag] {
			seen[s.Tag] = true
			result = append(result, s)
		}
	}
	return result
}
