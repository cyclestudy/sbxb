package xboard

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
)

const (
	userListPath  = "/api/v1/server/UniProxy/user"
	aliveListPath = "/api/v1/server/UniProxy/alivelist"
	pushPath      = "/api/v1/server/UniProxy/push"
	alivePath     = "/api/v1/server/UniProxy/alive"
	statusPath    = "/api/v1/server/UniProxy/status"
)

// GetUserList fetches the current list of users from the panel.
// Returns nil without error if the server responds with 304 Not Modified.
func (c *Client) GetUserList() ([]UserInfo, error) {
	body, statusCode, err := c.get(userListPath)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch user list: %w", err)
	}

	if statusCode == http.StatusNotModified {
		slog.Debug("user list not modified")
		return nil, nil
	}

	// The panel wraps users in a "users" field.
	var envelope struct {
		Users []UserInfo `json:"users"`
	}

	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("failed to parse user list response: %w", err)
	}

	slog.Debug("fetched user list", "count", len(envelope.Users))
	return envelope.Users, nil
}

// GetAliveList fetches the map of online users from the panel.
// Returns nil without error if the server responds with 304 Not Modified.
func (c *Client) GetAliveList() (AliveMap, error) {
	body, statusCode, err := c.get(aliveListPath)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch alive list: %w", err)
	}

	if statusCode == http.StatusNotModified {
		slog.Debug("alive list not modified")
		return nil, nil
	}

	var envelope struct {
		Alive AliveMap `json:"alive"`
	}

	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("failed to parse alive list response: %w", err)
	}

	slog.Debug("fetched alive list", "online_users", len(envelope.Alive))
	return envelope.Alive, nil
}

// ReportTraffic reports per-user upload/download traffic to the panel.
// data maps user ID to [upload, download] byte counts.
func (c *Client) ReportTraffic(data map[int][2]int64) error {
	if len(data) == 0 {
		slog.Debug("no traffic data to report")
		return nil
	}

	// The panel expects string keys in the JSON body.
	payload := make(map[string][2]int64, len(data))
	for uid, traffic := range data {
		payload[strconv.Itoa(uid)] = traffic
	}

	_, err := c.post(pushPath, payload)
	if err != nil {
		return fmt.Errorf("failed to report traffic: %w", err)
	}

	slog.Debug("reported traffic", "users", len(data))
	return nil
}

// ReportAlive reports per-user IP connection info to the panel.
// data maps user ID to a list of "ip_nodeId" strings.
func (c *Client) ReportAlive(data map[int][]string) error {
	if len(data) == 0 {
		slog.Debug("no alive data to report")
		return nil
	}

	// The panel expects string keys in the JSON body.
	payload := make(map[string][]string, len(data))
	for uid, ips := range data {
		payload[strconv.Itoa(uid)] = ips
	}

	_, err := c.post(alivePath, payload)
	if err != nil {
		return fmt.Errorf("failed to report alive data: %w", err)
	}

	slog.Debug("reported alive data", "users", len(data))
	return nil
}

// ReportStatus reports the server's resource usage to the panel.
func (c *Client) ReportStatus(status *StatusReport) error {
	if status == nil {
		return fmt.Errorf("status report is nil")
	}

	_, err := c.post(statusPath, status)
	if err != nil {
		return fmt.Errorf("failed to report status: %w", err)
	}

	slog.Debug("reported server status",
		"cpu", status.CPU,
		"mem_used", status.Mem.Used,
		"mem_total", status.Mem.Total,
	)
	return nil
}
