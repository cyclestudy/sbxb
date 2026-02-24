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

	// The response wraps the node info in a "data" envelope.
	var envelope struct {
		Data *NodeInfo `json:"data"`
	}

	if err := json.Unmarshal(body, &envelope); err != nil {
		// Try parsing directly as NodeInfo (some panels return flat response).
		var info NodeInfo
		if err2 := json.Unmarshal(body, &info); err2 != nil {
			return nil, fmt.Errorf("failed to parse node info response: %w (also tried flat: %v)", err, err2)
		}
		slog.Debug("parsed node info (flat response)")
		return &info, nil
	}

	if envelope.Data == nil {
		return nil, fmt.Errorf("node info response contained no data")
	}

	slog.Debug("fetched node info",
		"protocol", envelope.Data.Protocol,
		"port", envelope.Data.ServerPort,
	)
	return envelope.Data, nil
}
