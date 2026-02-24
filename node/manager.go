package node

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"

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

	// 1. Create the sing-box core.
	m.core = core.New()

	// 2. Build base options with direct + block outbounds.
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

	// 3. Start the core.
	if err := m.core.Start(baseOpts); err != nil {
		return fmt.Errorf("manager: failed to start core: %w", err)
	}

	// 4. Create traffic tracker and register with the router.
	m.tracker = core.NewTrafficTracker()
	m.core.SetTracker(m.tracker)

	// 5. Create and start a controller for each node.
	m.controllers = make([]*Controller, 0, len(configs))
	for _, cfg := range configs {
		ctrl := NewController(cfg, m.core, m.tracker)
		if err := ctrl.Start(ctx); err != nil {
			slog.Error("manager: failed to start controller, rolling back",
				"nodeID", cfg.NodeID,
				"error", err,
			)
			// Roll back: close already-started controllers and core.
			for _, started := range m.controllers {
				started.Close()
			}
			m.controllers = nil
			_ = m.core.Close()
			return fmt.Errorf("manager: node %d failed to start: %w", cfg.NodeID, err)
		}
		m.controllers = append(m.controllers, ctrl)
		slog.Info("manager: controller started", "nodeID", cfg.NodeID)
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
