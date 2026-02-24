package conf

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

// Config is the top-level configuration structure.
type Config struct {
	Log            LogConfig    `json:"log" mapstructure:"log"`
	BlockSourceIPs []string     `json:"block_source_ips" mapstructure:"block_source_ips"`
	Nodes          []NodeConfig `json:"nodes" mapstructure:"nodes"`
}

// LogConfig controls logging behavior.
type LogConfig struct {
	Level  string `json:"level" mapstructure:"level"`
	Output string `json:"output" mapstructure:"output"`
}

// NodeConfig describes a single node registration with an XBoard panel.
type NodeConfig struct {
	ApiHost  string `json:"api_host" mapstructure:"api_host"`
	ApiKey   string `json:"api_key" mapstructure:"api_key"`
	NodeID   int    `json:"node_id" mapstructure:"node_id"`
	NodeType string `json:"node_type" mapstructure:"node_type"`
	Timeout  int    `json:"timeout" mapstructure:"timeout"`
}

const defaultConfigPath = "/etc/sbxb/config.json"

// Load reads the configuration file at path and returns a parsed Config.
// If path is empty, the default path /etc/sbxb/config.json is used.
func Load(path string) (*Config, error) {
	if path == "" {
		path = defaultConfigPath
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve config path: %w", err)
	}

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("config file not found: %s", absPath)
	}

	v := viper.New()
	v.SetConfigFile(absPath)
	v.SetConfigType("json")

	// Defaults
	v.SetDefault("log.level", "warning")
	v.SetDefault("log.output", "")

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if len(cfg.Nodes) == 0 {
		return nil, fmt.Errorf("no nodes configured in %s", absPath)
	}

	// Apply per-node defaults and validate.
	for i := range cfg.Nodes {
		node := &cfg.Nodes[i]
		if node.Timeout <= 0 {
			node.Timeout = 30
		}
		if node.ApiHost == "" {
			return nil, fmt.Errorf("node[%d]: api_host is required", i)
		}
		if node.ApiKey == "" {
			return nil, fmt.Errorf("node[%d]: api_key is required", i)
		}
		if node.NodeID <= 0 {
			return nil, fmt.Errorf("node[%d]: node_id must be positive", i)
		}
		if node.NodeType == "" {
			return nil, fmt.Errorf("node[%d]: node_type is required", i)
		}
	}

	slog.Info("configuration loaded", "path", absPath, "nodes", len(cfg.Nodes))
	return &cfg, nil
}

// Watch monitors the configuration file for changes and calls callback when
// a modification is detected. A 5-second debounce prevents rapid repeated
// invocations. Watch blocks; call it in a goroutine.
func Watch(path string, callback func()) {
	if path == "" {
		path = defaultConfigPath
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		slog.Error("failed to resolve config path for watching", "error", err)
		return
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		slog.Error("failed to create file watcher", "error", err)
		return
	}
	defer watcher.Close()

	// Watch the directory so we catch renames/recreations.
	dir := filepath.Dir(absPath)
	if err := watcher.Add(dir); err != nil {
		slog.Error("failed to watch config directory", "dir", dir, "error", err)
		return
	}

	slog.Info("watching config file for changes", "path", absPath)

	var (
		mu        sync.Mutex
		timer     *time.Timer
		debounce  = 5 * time.Second
	)

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			// Only react to our specific config file.
			if filepath.Clean(event.Name) != filepath.Clean(absPath) {
				continue
			}

			if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
				continue
			}

			mu.Lock()
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(debounce, func() {
				slog.Info("config file changed, triggering reload", "path", absPath)
				callback()
			})
			mu.Unlock()

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			slog.Error("file watcher error", "error", err)
		}
	}
}
