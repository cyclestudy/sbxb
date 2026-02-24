package node

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/cyclestudy/sbxb/api/xboard"
	"github.com/cyclestudy/sbxb/conf"
	"github.com/cyclestudy/sbxb/core"
	"github.com/cyclestudy/sbxb/limiter"
)

// Controller manages the lifecycle of a single node: fetching
// configuration from the panel, building sing-box inbounds, tracking
// traffic, and reporting it back.
type Controller struct {
	nodeConfig conf.NodeConfig
	client     *xboard.Client
	core       *core.Core
	tracker    *core.TrafficTracker
	limiter    *limiter.Limiter

	nodeInfo *xboard.NodeInfo
	users    []xboard.UserInfo
	tag      string // inbound tag: "{protocol}-{nodeID}"

	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
}

// NewController creates a Controller for the given node configuration.
func NewController(nodeConfig conf.NodeConfig, c *core.Core, tracker *core.TrafficTracker) *Controller {
	client := xboard.NewClient(
		nodeConfig.ApiHost,
		nodeConfig.ApiKey,
		nodeConfig.NodeID,
		nodeConfig.NodeType,
		nodeConfig.Timeout,
	)
	return &Controller{
		nodeConfig: nodeConfig,
		client:     client,
		core:       c,
		tracker:    tracker,
	}
}

// Start initializes the node by fetching its configuration and user
// list from the panel, building the sing-box inbound, and launching
// periodic pull/push tasks.
func (ctrl *Controller) Start(ctx context.Context) error {
	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()

	ctrl.ctx, ctrl.cancel = context.WithCancel(ctx)

	// 1. Fetch initial node info.
	nodeInfo, err := ctrl.client.GetNodeInfo()
	if err != nil {
		return fmt.Errorf("node %d: failed to fetch node info: %w", ctrl.nodeConfig.NodeID, err)
	}
	ctrl.nodeInfo = nodeInfo
	ctrl.tag = fmt.Sprintf("%s-%d", nodeInfo.Protocol, ctrl.nodeConfig.NodeID)

	slog.Info("node info fetched",
		"nodeID", ctrl.nodeConfig.NodeID,
		"protocol", nodeInfo.Protocol,
		"tag", ctrl.tag,
	)

	// 2. Fetch initial user list.
	users, err := ctrl.client.GetUserList()
	if err != nil {
		return fmt.Errorf("node %d: failed to fetch users: %w", ctrl.nodeConfig.NodeID, err)
	}
	ctrl.users = users

	slog.Info("user list fetched",
		"nodeID", ctrl.nodeConfig.NodeID,
		"userCount", len(users),
	)

	// 3. Build inbound and add to core.
	if err := ctrl.buildAndAddInbound(); err != nil {
		return fmt.Errorf("node %d: failed to build inbound: %w", ctrl.nodeConfig.NodeID, err)
	}

	// 4. Create limiter with user info.
	ctrl.limiter = limiter.New(ctrl.nodeConfig.NodeID)
	userInfos := make([]limiter.UserInfo, len(users))
	for i, u := range users {
		userInfos[i] = limiter.UserInfo{
			ID:          u.ID,
			SpeedLimit:  u.SpeedLimit,
			DeviceLimit: u.DeviceLimit,
		}
	}
	ctrl.limiter.UpdateUsers(userInfos)

	// 5. Start periodic tasks.
	ctrl.startTasks()

	slog.Info("node controller started",
		"nodeID", ctrl.nodeConfig.NodeID,
		"tag", ctrl.tag,
	)

	return nil
}

// Close shuts down the controller: cancels periodic tasks and removes
// the inbound from the core.
func (ctrl *Controller) Close() {
	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()

	if ctrl.cancel != nil {
		ctrl.cancel()
		ctrl.cancel = nil
	}

	if ctrl.tag != "" {
		if err := ctrl.core.RemoveInbound(ctrl.tag); err != nil {
			slog.Warn("node controller: failed to remove inbound",
				"tag", ctrl.tag,
				"error", err,
			)
		}
	}

	slog.Info("node controller closed",
		"nodeID", ctrl.nodeConfig.NodeID,
		"tag", ctrl.tag,
	)
}

// reload tears down and rebuilds the sing-box inbound with the current
// node info and user list. Must be called while ctrl.mu is NOT held.
func (ctrl *Controller) reload() error {
	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()

	slog.Info("reloading inbound",
		"nodeID", ctrl.nodeConfig.NodeID,
		"tag", ctrl.tag,
	)

	// Remove old inbound (ignore error if it wasn't present).
	if ctrl.tag != "" {
		_ = ctrl.core.RemoveInbound(ctrl.tag)
	}

	// Recalculate tag in case protocol changed.
	ctrl.tag = fmt.Sprintf("%s-%d", ctrl.nodeInfo.Protocol, ctrl.nodeConfig.NodeID)

	// Build and add new inbound.
	if err := ctrl.buildAndAddInbound(); err != nil {
		return fmt.Errorf("reload: failed to build inbound: %w", err)
	}

	slog.Info("inbound reloaded",
		"nodeID", ctrl.nodeConfig.NodeID,
		"tag", ctrl.tag,
	)

	return nil
}

// buildAndAddInbound constructs the sing-box inbound option from the
// current nodeInfo and users, then adds it to the core.
func (ctrl *Controller) buildAndAddInbound() error {
	inbound, err := core.BuildInbound(ctrl.nodeInfo, ctrl.users, ctrl.nodeConfig.NodeID)
	if err != nil {
		return err
	}
	return ctrl.core.AddInbound(inbound)
}
