package core

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
)

// Core wraps a sing-box instance, providing thread-safe lifecycle management.
type Core struct {
	mu         sync.RWMutex
	instance   *box.Box
	ctx        context.Context
	cancel     context.CancelFunc
	started    bool
	logFactory log.Factory
}

// New creates a new Core instance. Call Start to begin processing traffic.
func New() *Core {
	return &Core{}
}

// Start creates and starts a sing-box instance with the given options.
// The context is enriched with include.Context to register all protocol
// implementations (inbound, outbound, DNS transports, etc.).
func (c *Core) Start(options option.Options) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return fmt.Errorf("core already started")
	}

	ctx, cancel := context.WithCancel(context.Background())
	ctx = include.Context(ctx)

	instance, err := box.New(box.Options{
		Context: ctx,
		Options: options,
	})
	if err != nil {
		cancel()
		return fmt.Errorf("failed to create sing-box instance: %w", err)
	}

	if err = instance.Start(); err != nil {
		instance.Close()
		cancel()
		return fmt.Errorf("failed to start sing-box instance: %w", err)
	}

	c.ctx = ctx
	c.cancel = cancel
	c.instance = instance
	c.started = true
	c.logFactory = log.NewNOPFactory()

	slog.Info("sing-box core started")
	return nil
}

// Close shuts down the running sing-box instance and releases resources.
// It is safe to call Close on an already-stopped Core.
func (c *Core) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.started {
		return nil
	}

	var closeErr error
	if c.instance != nil {
		closeErr = c.instance.Close()
	}
	if c.cancel != nil {
		c.cancel()
	}

	c.instance = nil
	c.ctx = nil
	c.cancel = nil
	c.started = false
	c.logFactory = nil

	slog.Info("sing-box core closed")
	return closeErr
}

// Restart performs an atomic stop-then-start cycle with new options.
func (c *Core) Restart(options option.Options) error {
	if err := c.Close(); err != nil {
		slog.Warn("error closing core during restart, proceeding anyway", "error", err)
	}
	return c.Start(options)
}

// InboundManager returns the inbound manager of the running instance.
// Returns nil if the core is not started.
func (c *Core) InboundManager() adapter.InboundManager {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.instance == nil {
		return nil
	}
	return c.instance.Inbound()
}

// Router returns the router of the running instance.
// Returns nil if the core is not started.
func (c *Core) Router() adapter.Router {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.instance == nil {
		return nil
	}
	return c.instance.Router()
}

// Started reports whether the core is currently running.
func (c *Core) Started() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.started
}

// AddInbound dynamically adds an inbound to the running instance.
// If an inbound with the same tag exists, it is replaced.
func (c *Core) AddInbound(inbound *option.Inbound) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.started {
		return fmt.Errorf("core not started")
	}

	logger := c.logFactory.NewLogger("inbound/" + inbound.Tag)
	return c.instance.Inbound().Create(
		c.ctx, c.instance.Router(), logger,
		inbound.Tag, inbound.Type, inbound.Options,
	)
}

// RemoveInbound removes an inbound by tag from the running instance.
func (c *Core) RemoveInbound(tag string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.started {
		return fmt.Errorf("core not started")
	}
	return c.instance.Inbound().Remove(tag)
}

// SetTracker registers a ConnectionTracker with the router for traffic
// counting. Must be called after Start.
func (c *Core) SetTracker(tracker adapter.ConnectionTracker) {
	r := c.Router()
	if r == nil {
		slog.Warn("cannot set tracker: core not started")
		return
	}
	r.AppendTracker(tracker)
	slog.Info("traffic tracker registered")
}
