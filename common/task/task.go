package task

import (
	"context"
	"log/slog"
	"time"
)

// Task runs a function periodically until its context is cancelled.
type Task struct {
	name     string
	interval time.Duration
	fn       func(ctx context.Context) error
}

// New creates a new periodic Task.
func New(name string, interval time.Duration, fn func(ctx context.Context) error) *Task {
	return &Task{
		name:     name,
		interval: interval,
		fn:       fn,
	}
}

// Start runs the task function immediately, then on interval, until ctx is
// done. Errors from fn are logged but do not stop the loop.
func (t *Task) Start(ctx context.Context) {
	slog.Info("periodic task started", "task", t.name, "interval", t.interval)

	// Run immediately on start.
	if err := t.fn(ctx); err != nil {
		slog.Error("periodic task error", "task", t.name, "error", err)
	}

	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("periodic task stopped", "task", t.name)
			return
		case <-ticker.C:
			if err := t.fn(ctx); err != nil {
				slog.Error("periodic task error", "task", t.name, "error", err)
			}
		}
	}
}
