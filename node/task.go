package node

import (
	"context"
	"log/slog"
	"time"

	"github.com/cyclestudy/sbxb/common/task"
	"github.com/cyclestudy/sbxb/limiter"
)

// startTasks launches the periodic pull (nodeInfoMonitor) and push
// (trafficReporter) goroutines.
func (ctrl *Controller) startTasks() {
	pullInterval := time.Duration(ctrl.nodeInfo.BaseConfig.PullInterval) * time.Second
	if pullInterval <= 0 {
		pullInterval = 60 * time.Second
	}
	pushInterval := time.Duration(ctrl.nodeInfo.BaseConfig.PushInterval) * time.Second
	if pushInterval <= 0 {
		pushInterval = 60 * time.Second
	}

	pullTask := task.New(
		"nodeInfoMonitor",
		pullInterval,
		ctrl.nodeInfoMonitor,
	)

	pushTask := task.New(
		"trafficReporter",
		pushInterval,
		ctrl.trafficReporter,
	)

	ctrl.wg.Add(2)
	go func() {
		defer ctrl.wg.Done()
		pullTask.Start(ctrl.ctx)
	}()
	go func() {
		defer ctrl.wg.Done()
		pushTask.Start(ctrl.ctx)
	}()
}

// nodeInfoMonitor is called periodically to pull configuration changes
// from the panel and reconcile with the running state.
func (ctrl *Controller) nodeInfoMonitor(ctx context.Context) error {
	nodeID := ctrl.nodeConfig.NodeID

	// 1. Fetch node info (returns nil if unchanged via ETag/304).
	newNodeInfo, err := ctrl.client.GetNodeInfo(ctx)
	if err != nil {
		return err
	}

	needReload := false
	if newNodeInfo != nil {
		// Check if routes changed (requires full manager reload).
		newHash := hashRoutes(newNodeInfo.Routes)
		ctrl.mu.Lock()
		oldHash := ctrl.routesHash
		ctrl.nodeInfo = newNodeInfo
		ctrl.routesHash = newHash
		ctrl.mu.Unlock()

		if newHash != oldHash && ctrl.routeChangeCh != nil {
			slog.Info("route rules changed, requesting full reload", "nodeID", nodeID)
			select {
			case ctrl.routeChangeCh <- struct{}{}:
			default:
			}
			return nil // skip inbound reload, full reload will handle it
		}

		needReload = true
		slog.Info("node info updated", "nodeID", nodeID)
	}

	// 2. Fetch user list (returns nil if unchanged).
	newUsers, err := ctrl.client.GetUserList(ctx)
	if err != nil {
		return err
	}

	if newUsers != nil {
		if err := ctrl.compareAndUpdateUsers(newUsers); err != nil {
			return err
		}
		needReload = true
	}

	// 3. Reload the inbound if anything changed.
	if needReload {
		if err := ctrl.reload(); err != nil {
			slog.Error("failed to reload inbound",
				"nodeID", nodeID,
				"error", err,
			)
			return err
		}
	}

	// 4. Fetch alive list from panel and update limiter.
	alive, err := ctrl.client.GetAliveList(ctx)
	if err != nil {
		slog.Warn("failed to fetch alive data",
			"nodeID", nodeID,
			"error", err,
		)
		// Non-fatal: don't return error for alive fetch failure.
	} else if alive != nil {
		ctrl.limiter.UpdateAlive(alive)
	}

	return nil
}

// trafficReporter is called periodically to push traffic stats and
// online-IP data to the panel.
func (ctrl *Controller) trafficReporter(ctx context.Context) error {
	nodeID := ctrl.nodeConfig.NodeID

	// 1. Collect traffic from the tracker (only this node's inbound).
	ctrl.mu.Lock()
	tag := ctrl.tag
	ctrl.mu.Unlock()

	traffic := ctrl.tracker.GetTrafficByInbound(tag)

	// 2. Report traffic to panel (only if there is data).
	if len(traffic) > 0 {
		if err := ctrl.client.ReportTraffic(ctx, traffic); err != nil {
			// Restore unreported traffic back to the counters so it will
			// be included in the next reporting cycle.
			ctrl.tracker.RestoreTrafficByInbound(tag, traffic)
			slog.Error("failed to report traffic, data preserved for retry",
				"nodeID", nodeID,
				"error", err,
			)
			return err
		}
		slog.Info("traffic reported",
			"nodeID", nodeID,
			"userCount", len(traffic),
		)
	}

	// 3. Get alive data from limiter.
	aliveData := ctrl.limiter.GetAliveData()

	// 4. Report alive to panel (only if there is data).
	if len(aliveData) > 0 {
		if err := ctrl.client.ReportAlive(ctx, aliveData); err != nil {
			slog.Error("failed to report alive data",
				"nodeID", nodeID,
				"error", err,
			)
			return err
		}
		slog.Debug("alive data reported",
			"nodeID", nodeID,
			"userCount", len(aliveData),
		)
	}

	// 5. Report server status.
	status := collectStatus()
	if err := ctrl.client.ReportStatus(ctx, status); err != nil {
		slog.Debug("failed to report status",
			"nodeID", nodeID,
			"error", err,
		)
		// Non-fatal: don't return error for status report failure.
	}

	// 6. Update limiter user info from current user list.
	ctrl.mu.Lock()
	users := ctrl.users
	ctrl.mu.Unlock()

	userInfos := make([]limiter.UserInfo, len(users))
	for i, u := range users {
		userInfos[i] = limiter.UserInfo{
			ID:          u.ID,
			SpeedLimit:  u.SpeedLimit,
			DeviceLimit: u.DeviceLimit,
		}
	}
	ctrl.limiter.UpdateUsers(userInfos)

	return nil
}
