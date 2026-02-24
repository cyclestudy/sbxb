package xboard

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
)

const nodeInfoPath = "/api/v1/server/UniProxy/config"

// GetNodeInfo fetches the node configuration from the panel.
// Returns nil without error if the server responds with 304 Not Modified,
// indicating the configuration has not changed since the last fetch.
func (c *Client) GetNodeInfo() (*NodeInfo, error) {
	body, statusCode, err := c.get(nodeInfoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch node info: %w", err)
	}

	if statusCode == http.StatusNotModified {
		slog.Debug("node info not modified")
		return nil, nil
	}

	// Try envelope format first: {"data": {...}}
	var envelope struct {
		Data *NodeInfo `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil && envelope.Data != nil && envelope.Data.Protocol != "" {
		slog.Debug("fetched node info",
			"protocol", envelope.Data.Protocol,
			"port", envelope.Data.ServerPort,
		)
		return envelope.Data, nil
	}

	// Flat format: {"protocol": "vless", ...}
	var info NodeInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("failed to parse node info response: %w", err)
	}
	if info.Protocol == "" {
		return nil, fmt.Errorf("node info response contained no data")
	}

	slog.Debug("fetched node info (flat)",
		"protocol", info.Protocol,
		"port", info.ServerPort,
	)
	return &info, nil
}
