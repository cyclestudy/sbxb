package sbxb

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/cyclestudy/sbxb/conf"
	"github.com/cyclestudy/sbxb/node"
)

var configPath string

func init() {
	serverCmd.Flags().StringVarP(&configPath, "config", "c", "/etc/sbxb/config.json", "config file path")
	rootCmd.AddCommand(serverCmd)
}

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Start the sbxb node backend",
	RunE:  runServer,
}

func runServer(cmd *cobra.Command, args []string) error {
	// Load configuration.
	cfg, err := conf.Load(configPath)
	if err != nil {
		return err
	}

	// Set log level.
	setupLogger(cfg.Log.Level, cfg.Log.Output)

	slog.Info("sbxb starting",
		"version", version,
		"nodes", len(cfg.Nodes),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create and start the node manager.
	mgr := node.NewManager()
	if err := mgr.Start(ctx, cfg.Nodes, cfg.BlockSourceIPs); err != nil {
		return err
	}

	// Watch config file for hot-reload.
	go conf.Watch(configPath, func() {
		slog.Info("config file changed, reloading...")
		newCfg, err := conf.Load(configPath)
		if err != nil {
			slog.Error("failed to reload config", "error", err)
			return
		}
		if err := mgr.Reload(ctx, newCfg.Nodes, newCfg.BlockSourceIPs); err != nil {
			slog.Error("failed to apply reloaded config", "error", err)
		}
	})

	// Wait for shutdown signal.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigCh
	slog.Info("received signal, shutting down", "signal", sig)

	cancel()
	mgr.Close()

	slog.Info("sbxb stopped")
	return nil
}

func setupLogger(level, output string) {
	var logLevel slog.Level
	switch level {
	case "debug":
		logLevel = slog.LevelDebug
	case "info":
		logLevel = slog.LevelInfo
	case "warning", "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelWarn
	}

	opts := &slog.HandlerOptions{Level: logLevel}

	var handler slog.Handler
	if output != "" {
		f, err := os.OpenFile(output, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			slog.Error("failed to open log file, using stderr", "error", err)
			handler = slog.NewTextHandler(os.Stderr, opts)
		} else {
			handler = slog.NewTextHandler(f, opts)
		}
	} else {
		handler = slog.NewTextHandler(os.Stderr, opts)
	}

	slog.SetDefault(slog.New(handler))
}
