package watcher

import (
	"context"
)

// Watcher defines the behavior for monitoring file system events.
type Watcher interface {
	// Watch starts monitoring a file path and sends a signal
	// on the returned channel whenever the file is updated.
	Watch(ctx context.Context, path string) (<-chan struct{}, error)
}
