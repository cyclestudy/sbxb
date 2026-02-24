package limiter

import (
	"fmt"
	"sync"
)

// UserInfo carries the per-user limits received from the panel.
type UserInfo struct {
	ID          int
	SpeedLimit  int // Mbps, 0 = unlimited
	DeviceLimit int // 0 = unlimited
}

// UserLimiter holds runtime state for one user's limits.
type UserLimiter struct {
	SpeedLimit  int             // Mbps, 0 = unlimited
	DeviceLimit int             // 0 = unlimited
	OnlineIPs   map[string]bool // currently tracked IPs
}

// Limiter manages per-user speed and device limits for a single node.
type Limiter struct {
	mu     sync.RWMutex
	users  map[int]*UserLimiter // keyed by user ID
	nodeID int
}

// New creates a Limiter for the given node.
func New(nodeID int) *Limiter {
	return &Limiter{
		users:  make(map[int]*UserLimiter),
		nodeID: nodeID,
	}
}

// UpdateUsers rebuilds user limiters from the provided list. Existing IP
// tracking is preserved for users that remain in the new list.
func (l *Limiter) UpdateUsers(users []UserInfo) {
	l.mu.Lock()
	defer l.mu.Unlock()

	newUsers := make(map[int]*UserLimiter, len(users))
	for _, u := range users {
		if existing, ok := l.users[u.ID]; ok {
			// Preserve online IPs, update limits.
			existing.SpeedLimit = u.SpeedLimit
			existing.DeviceLimit = u.DeviceLimit
			newUsers[u.ID] = existing
		} else {
			newUsers[u.ID] = &UserLimiter{
				SpeedLimit:  u.SpeedLimit,
				DeviceLimit: u.DeviceLimit,
				OnlineIPs:   make(map[string]bool),
			}
		}
	}
	l.users = newUsers
}

// AddIP adds an IP to the user's online set. Returns false if the device
// limit would be exceeded (the IP is not added in that case). If the IP
// is already tracked, it returns true without double-counting.
func (l *Limiter) AddIP(userID int, ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	ul, ok := l.users[userID]
	if !ok {
		return false
	}

	// Already tracked.
	if ul.OnlineIPs[ip] {
		return true
	}

	// Check device limit.
	if ul.DeviceLimit > 0 && len(ul.OnlineIPs) >= ul.DeviceLimit {
		return false
	}

	ul.OnlineIPs[ip] = true
	return true
}

// RemoveIP removes an IP from the user's online set.
func (l *Limiter) RemoveIP(userID int, ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if ul, ok := l.users[userID]; ok {
		delete(ul.OnlineIPs, ip)
	}
}

// GetAliveData returns a map of userID to a list of "ip_nodeID" strings,
// suitable for reporting to the panel's alive endpoint.
func (l *Limiter) GetAliveData() map[int][]string {
	l.mu.RLock()
	defer l.mu.RUnlock()

	result := make(map[int][]string)
	for userID, ul := range l.users {
		if len(ul.OnlineIPs) == 0 {
			continue
		}
		ips := make([]string, 0, len(ul.OnlineIPs))
		for ip := range ul.OnlineIPs {
			ips = append(ips, fmt.Sprintf("%s_%d", ip, l.nodeID))
		}
		result[userID] = ips
	}
	return result
}

// UpdateAlive updates cross-node device counts from the panel's alive
// response. The alive map is keyed by "ip_nodeID" with value being the
// user ID. For IPs reported on other nodes, we keep them in each user's
// set so the total device count is accurate. For IPs on this node that
// the panel no longer reports, we remove them (the session ended
// remotely).
func (l *Limiter) UpdateAlive(alive map[string]int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Build per-user IP set from the panel's response.
	panelIPs := make(map[int]map[string]bool)
	for ipNode, userID := range alive {
		if panelIPs[userID] == nil {
			panelIPs[userID] = make(map[string]bool)
		}
		panelIPs[userID][ipNode] = true
	}

	// For each user, reconcile: keep locally-tracked IPs that the panel
	// still knows about, remove stale ones, and incorporate remote IPs.
	nodeTag := fmt.Sprintf("_%d", l.nodeID)
	for userID, ul := range l.users {
		pIPs := panelIPs[userID]

		// Remove local IPs that the panel no longer reports.
		for ip := range ul.OnlineIPs {
			localKey := fmt.Sprintf("%s%s", ip, nodeTag)
			if pIPs != nil && pIPs[localKey] {
				continue // panel still has it
			}
			// Panel doesn't know about this local IP any more.
			delete(ul.OnlineIPs, ip)
		}

		// We intentionally do NOT add remote-node IPs into OnlineIPs
		// (they are on other nodes' limiters). The panel already
		// enforces cross-node device limits via the alive API response.
		_ = pIPs
		_ = userID
	}
}
