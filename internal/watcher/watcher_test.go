package watcher

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestFSWatcher_Lifecycle(t *testing.T) {
	//  Create a temporary file to watch
	tmpFile, err := os.CreateTemp("", "losu_test_*.log")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name()) // Cleanup file after test
	tmpFile.Close()

	//  Initialize the watcher
	fsw, err := NewFSWatcher()
	if err != nil {
		t.Fatalf("NewFSWatcher failed: %v", err)
	}

	//  Start watching with a cancelable context
	ctx, cancel := context.WithCancel(context.Background())
	notify, err := fsw.Watch(ctx, tmpFile.Name())
	if err != nil {
		t.Fatalf("Watch failed: %v", err)
	}

	//  Write to the file and expect a notification
	err = os.WriteFile(tmpFile.Name(), []byte("new log line\n"), 0644)
	if err != nil {
		t.Fatalf("failed to write to temp file: %v", err)
	}

	select {
	case <-notify:
		// Success! Got the signal
	case <-time.After(1 * time.Second):
		t.Error("Timed out waiting for file write notification")
	}

	// Non-blocking behavior (Flood the watcher)
	// We send multiple writes, the channel buffer is 1.
	// It shouldn't block or panic.
	for i := 0; i < 5; i++ {
		_ = os.WriteFile(tmpFile.Name(), []byte("spam\n"), 0644)
	}

	//  Test: Shutdown
	cancel()

	// Small sleep to allow the goroutine to hit the ctx.Done() and f.watcher.Close()
	time.Sleep(50 * time.Millisecond)
}

func TestFSWatcher_InvalidPath(t *testing.T) {
	fsw, err := NewFSWatcher()
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	_, err = fsw.Watch(ctx, "/path/to/nowhere/that/does/not/exist")
	if err == nil {
		t.Error("Expected error when watching non-existent path, got nil")
	}
}
