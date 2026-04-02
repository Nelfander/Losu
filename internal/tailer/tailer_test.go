package tailer

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/nelfander/losu/internal/model"
)

// Veriies the happy path
func TestTailer_BasicIngestion(t *testing.T) {
	//  Setup temp file
	tmpFile, _ := os.CreateTemp("", "tailer_test_*.log")
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	//  Setup Tailer and Channel
	results := make(chan model.RawLog, 10)
	changes := make(chan struct{}, 1)
	tail := NewTailer(tmpFile.Name(), results)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	//  Start Tailer in background
	go func() {
		_ = tail.Run(ctx, changes)
	}()

	// Small sleep to let Tailer reach the end of the empty file
	time.Sleep(50 * time.Millisecond)

	//  Write data
	testLine := "Hello LOSU\n"
	_, _ = tmpFile.WriteString(testLine)

	// Signal a change manually
	changes <- struct{}{}

	//  Verify
	select {
	case res := <-results:
		if res.Line != "Hello LOSU" {
			t.Errorf("Expected 'Hello LOSU', got %q", res.Line)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("Tailer failed to pick up new line via signal")
	}
}

/*
	TestTailer_PollingFallback verifies the "Windows Kick" safety net:

Even if the 'changes' channel is never signaled (simulating an OS that
fails to report file events), the 100ms internal ticker should
eventually discover the new data and ingest it.
*/
func TestTailer_PollingFallback(t *testing.T) {
	// Tests the Windows Kick (ticker-based ingestion)
	tmpFile, _ := os.CreateTemp("", "tailer_poll_*.log")
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	results := make(chan model.RawLog, 10)
	changes := make(chan struct{}, 1) // We wont send to this channel
	tail := NewTailer(tmpFile.Name(), results)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	go func() {
		_ = tail.Run(ctx, changes)
	}()

	time.Sleep(50 * time.Millisecond)

	// Write without signaling the channel
	_, _ = tmpFile.WriteString("Polling Worktest\n")

	// Wait longer than the 100ms ticker
	select {
	case res := <-results:
		if res.Line != "Polling Worktest" {
			t.Errorf("Got wrong line: %q", res.Line)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("Ticker fallback failed to pick up line")
	}
}

/*
	TestTailer_Shutdown verifies the lifecycle management:

Ensures that when the parent Context is canceled, the Tailer stops
its infinite loop and returns the correct error, preventing goroutine leaks.
*/
func TestTailer_Shutdown(t *testing.T) {
	tmpFile, _ := os.CreateTemp("", "tailer_stop_*.log")
	defer os.Remove(tmpFile.Name())

	results := make(chan model.RawLog)
	changes := make(chan struct{})
	tail := NewTailer(tmpFile.Name(), results)

	ctx, cancel := context.WithCancel(context.Background())

	errChan := make(chan error, 1)
	go func() {
		errChan <- tail.Run(ctx, changes)
	}()

	// Kill it immediately
	cancel()

	select {
	case err := <-errChan:
		if err != context.Canceled {
			t.Errorf("Expected context.Canceled error, got %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Tailer did not shutdown gracefully")
	}
}
