package node

import (
	"log/slog"
	"strconv"

	"github.com/cyclestudy/sbxb/api/xboard"
)

// compareAndUpdateUsers diffs the new user list against the current one
// and updates the controller state. The diff is keyed on
// uuid+speedLimit so that a user whose speed limit changed is treated
// as removed+added, triggering an inbound rebuild.
func (ctrl *Controller) compareAndUpdateUsers(newUsers []xboard.UserInfo) error {
	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()

	oldMap := buildUserKey(ctrl.users)
	newMap := buildUserKey(newUsers)

	var added, removed []int

	// Detect removed or changed users.
	for key, uid := range oldMap {
		if _, exists := newMap[key]; !exists {
			removed = append(removed, uid)
		}
	}

	// Detect added or changed users.
	for key, uid := range newMap {
		if _, exists := oldMap[key]; !exists {
			added = append(added, uid)
		}
	}

	if len(added) == 0 && len(removed) == 0 {
		slog.Debug("user list unchanged",
			"nodeID", ctrl.nodeConfig.NodeID,
			"totalUsers", len(newUsers),
		)
		return nil
	}

	slog.Info("user list changed",
		"nodeID", ctrl.nodeConfig.NodeID,
		"added", len(added),
		"removed", len(removed),
		"totalUsers", len(newUsers),
	)

	if len(added) > 0 {
		slog.Debug("users added",
			"nodeID", ctrl.nodeConfig.NodeID,
			"userIDs", added,
		)
	}
	if len(removed) > 0 {
		slog.Debug("users removed",
			"nodeID", ctrl.nodeConfig.NodeID,
			"userIDs", removed,
		)
	}

	// Replace the user list; the caller will trigger an inbound rebuild.
	ctrl.users = newUsers
	return nil
}

// buildUserKey creates a map from a composite key (uuid + speedLimit)
// to user ID. This composite key ensures that when a user's speed limit
// changes, they are treated as a different entry, triggering a rebuild.
func buildUserKey(users []xboard.UserInfo) map[string]int {
	m := make(map[string]int, len(users))
	for _, u := range users {
		key := u.UUID + "|" + strconv.Itoa(u.SpeedLimit)
		m[key] = u.ID
	}
	return m
}
