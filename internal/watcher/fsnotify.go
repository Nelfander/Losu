package watcher

import (
	"context"
	"fmt"
	"log"

	"github.com/fsnotify/fsnotify"
)

// FSWatcher is our concrete implementation using the fsnotify library
type FSWatcher struct {
	watcher *fsnotify.Watcher
}

// NewFSWatcher initializes the underlying library
func NewFSWatcher() (*FSWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create watcher: %w", err)
	}
	return &FSWatcher{watcher: w}, nil
}

// Watch starts the OS event loop
func (f *FSWatcher) Watch(ctx context.Context, path string) (<-chan struct{}, error) {
	// Create the "Signal" channel
	// Struct{} because it takes 0 bytes of memory
	notify := make(chan struct{}, 1)

	//  Tell the OS to watch this specific file
	err := f.watcher.Add(path)
	if err != nil {
		return nil, fmt.Errorf("failed to add path: %w", err)
	}

	// Start Goroutine
	go func() {
		defer f.watcher.Close() // Cleanup when done

		for {
			select {
			case <-ctx.Done():
				// The app is shutting down, stop the loop.
				return
			case event, ok := <-f.watcher.Events:
				if !ok {
					return
				}

				// Only care if the file was WRITTEN to.
				if event.Has(fsnotify.Write) {
					// Non-blocking send: if the channel is full,
					// it means the tailer is already busy, so no need to nag it
					select {
					case notify <- struct{}{}:
					default:
						// Tailer is already busy, which is fine
					}
				}
			case err, ok := <-f.watcher.Errors:
				if !ok {
					return
				}
				log.Printf("watcher error: %v", err)
			}
		}
	}()

	return notify, nil
}
