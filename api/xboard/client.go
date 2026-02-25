package xboard

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	maxRetries     = 3
	initialBackoff = 1 * time.Second
)

// Client communicates with an XBoard panel's server API.
type Client struct {
	httpClient *http.Client
	apiHost    string
	apiKey     string
	nodeID     int

	mu    sync.RWMutex
	etags map[string]string // per-endpoint ETag cache
}

// NewClient creates a new XBoard API client.
// timeout is in seconds; if <= 0 it defaults to 30.
func NewClient(apiHost, apiKey string, nodeID, timeout int) *Client {
	if timeout <= 0 {
		timeout = 30
	}

	// Normalize the host: strip trailing slash.
	apiHost = strings.TrimRight(apiHost, "/")

	return &Client{
		httpClient: &http.Client{
			Timeout: time.Duration(timeout) * time.Second,
		},
		apiHost: apiHost,
		apiKey:  apiKey,
		nodeID:  nodeID,
		etags:   make(map[string]string),
	}
}

// buildURL constructs the full request URL with authentication query parameters.
func (c *Client) buildURL(path string) (string, error) {
	base := c.apiHost + path

	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("failed to parse URL %q: %w", base, err)
	}

	q := u.Query()
	q.Set("token", c.apiKey)
	q.Set("node_id", strconv.Itoa(c.nodeID))
	u.RawQuery = q.Encode()

	return u.String(), nil
}

// getETag retrieves the cached ETag for a given path.
func (c *Client) getETag(path string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.etags[path]
}

// setETag stores the ETag for a given path.
func (c *Client) setETag(path, etag string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if etag != "" {
		c.etags[path] = etag
	}
}

// get performs a GET request with ETag caching and retry logic.
// Returns (body, statusCode, error).
// On 304 Not Modified: returns (nil, 304, nil).
func (c *Client) get(ctx context.Context, path string) ([]byte, int, error) {
	reqURL, err := c.buildURL(path)
	if err != nil {
		return nil, 0, err
	}

	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			backoff := initialBackoff * time.Duration(1<<(attempt-1))
			slog.Debug("retrying GET request",
				"path", path,
				"attempt", attempt,
				"backoff", backoff,
			)
			select {
			case <-ctx.Done():
				return nil, 0, ctx.Err()
			case <-time.After(backoff):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Accept", "application/json")

		if etag := c.getETag(path); etag != "" {
			req.Header.Set("If-None-Match", etag)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("GET %s failed: %w", path, err)
			slog.Warn("HTTP request failed",
				"path", path,
				"attempt", attempt,
				"error", err,
			)
			continue
		}

		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()

		if readErr != nil {
			lastErr = fmt.Errorf("failed to read response body: %w", readErr)
			slog.Warn("failed to read response body",
				"path", path,
				"attempt", attempt,
				"error", readErr,
			)
			continue
		}

		// Cache the ETag from the response.
		if etag := resp.Header.Get("ETag"); etag != "" {
			c.setETag(path, etag)
		}

		if resp.StatusCode == http.StatusNotModified {
			slog.Debug("resource not modified", "path", path)
			return nil, http.StatusNotModified, nil
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return body, resp.StatusCode, nil
		}

		lastErr = fmt.Errorf("GET %s returned status %d: %s", path, resp.StatusCode, string(body))
		slog.Warn("unexpected HTTP status",
			"path", path,
			"status", resp.StatusCode,
			"attempt", attempt,
		)

		// Don't retry client errors (4xx) except 429.
		if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
			return nil, resp.StatusCode, lastErr
		}
	}

	return nil, 0, fmt.Errorf("GET %s failed after %d retries: %w", path, maxRetries, lastErr)
}

// post performs a POST request with JSON body and retry logic.
func (c *Client) post(ctx context.Context, path string, data interface{}) ([]byte, error) {
	reqURL, err := c.buildURL(path)
	if err != nil {
		return nil, err
	}

	payload, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			backoff := initialBackoff * time.Duration(1<<(attempt-1))
			slog.Debug("retrying POST request",
				"path", path,
				"attempt", attempt,
				"backoff", backoff,
			)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(payload))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("POST %s failed: %w", path, err)
			slog.Warn("HTTP request failed",
				"path", path,
				"attempt", attempt,
				"error", err,
			)
			continue
		}

		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()

		if readErr != nil {
			lastErr = fmt.Errorf("failed to read response body: %w", readErr)
			slog.Warn("failed to read response body",
				"path", path,
				"attempt", attempt,
				"error", readErr,
			)
			continue
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return body, nil
		}

		lastErr = fmt.Errorf("POST %s returned status %d: %s", path, resp.StatusCode, string(body))
		slog.Warn("unexpected HTTP status",
			"path", path,
			"status", resp.StatusCode,
			"attempt", attempt,
		)

		// Don't retry client errors (4xx) except 429.
		if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
			return nil, lastErr
		}
	}

	return nil, fmt.Errorf("POST %s failed after %d retries: %w", path, maxRetries, lastErr)
}
