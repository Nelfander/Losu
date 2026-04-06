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

func TestFSWatcher_CancelStopsNotifications(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "losu_cancel_*.log")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	fsw, err := NewFSWatcher()
	if err != nil {
		t.Fatalf("NewFSWatcher failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	notify, err := fsw.Watch(ctx, tmpFile.Name())
	if err != nil {
		t.Fatalf("Watch failed: %v", err)
	}

	// Cancel the context — goroutine should stop
	cancel()
	time.Sleep(50 * time.Millisecond)

	// Drain any buffered signal that arrived before cancel
	select {
	case <-notify:
	default:
	}

	// Write to the file AFTER cancel
	_ = os.WriteFile(tmpFile.Name(), []byte("post-cancel write\n"), 0644)

	// No new notification should arrive
	select {
	case <-notify:
		t.Error("Received notification after context was cancelled — goroutine leak suspected")
	case <-time.After(200 * time.Millisecond):
		// Correct — nothing arrived
	}
}

func TestFSWatcher_FloodCoalescing(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "losu_flood_*.log")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	fsw, err := NewFSWatcher()
	if err != nil {
		t.Fatalf("NewFSWatcher failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	notify, err := fsw.Watch(ctx, tmpFile.Name())
	if err != nil {
		t.Fatalf("Watch failed: %v", err)
	}

	// Flood with 10 rapid writes
	for i := 0; i < 10; i++ {
		_ = os.WriteFile(tmpFile.Name(), []byte("spam\n"), 0644)
	}

	// Wait for signals to settle
	time.Sleep(200 * time.Millisecond)

	// Channel buffer is 1 — at most 1 pending signal should be queued
	if len(notify) > 1 {
		t.Errorf("Expected at most 1 coalesced signal, got %d — non-blocking send not working", len(notify))
	}
}
