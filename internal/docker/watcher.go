package docker

import (
	"context"
	"log/slog"
	"time"

	"github.com/docker/docker/api/types/events"
)

// EventHandler is called when a container event occurs
type EventHandler func(ctx context.Context, event events.Message)

// Watcher monitors Docker container events
type Watcher struct {
	client       *Client
	handler      EventHandler
	pollInterval time.Duration
}

// NewWatcher creates a new container watcher
func NewWatcher(client *Client, handler EventHandler, pollInterval time.Duration) *Watcher {
	return &Watcher{
		client:       client,
		handler:      handler,
		pollInterval: pollInterval,
	}
}

// Start begins watching for container events
func (w *Watcher) Start(ctx context.Context) {
	// Start event stream
	go w.watchEvents(ctx)

	// Also do periodic polling as a fallback
	go w.pollContainers(ctx)
}

func (w *Watcher) watchEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		eventsChan, errChan := w.client.WatchEvents(ctx)

		for {
			select {
			case <-ctx.Done():
				return
			case event := <-eventsChan:
				w.handler(ctx, event)
			case err := <-errChan:
				if err != nil {
					slog.Warn("docker event stream error, reconnecting", "error", err)
					time.Sleep(5 * time.Second)
				}
				break
			}
		}
	}
}

func (w *Watcher) pollContainers(ctx context.Context) {
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	// Initial sync
	w.handler(ctx, events.Message{Action: "sync"})

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.handler(ctx, events.Message{Action: "sync"})
		}
	}
}
