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

func TestTailer_EmptyLinesFiltered(t *testing.T) {
	tmpFile, _ := os.CreateTemp("", "tailer_empty_*.log")
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	results := make(chan model.RawLog, 10)
	changes := make(chan struct{}, 1)
	tail := NewTailer(tmpFile.Name(), results)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() { _ = tail.Run(ctx, changes) }()
	time.Sleep(50 * time.Millisecond)

	// Write 3 empty lines and 1 real line
	_, _ = tmpFile.WriteString("\n\n\nHello LOSU\n")
	changes <- struct{}{}

	// Should only get 1 result — the empty lines must be filtered
	select {
	case res := <-results:
		if res.Line != "Hello LOSU" {
			t.Errorf("Expected 'Hello LOSU', got %q", res.Line)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("Tailer failed to pick up line")
	}

	// Drain channel — should be empty now
	time.Sleep(150 * time.Millisecond)
	if len(results) != 0 {
		t.Errorf("Expected 0 remaining results, got %d — empty lines were not filtered", len(results))
	}
}

func TestTailer_WhitespaceTrimming(t *testing.T) {
	tmpFile, _ := os.CreateTemp("", "tailer_trim_*.log")
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	results := make(chan model.RawLog, 10)
	changes := make(chan struct{}, 1)
	tail := NewTailer(tmpFile.Name(), results)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() { _ = tail.Run(ctx, changes) }()
	time.Sleep(50 * time.Millisecond)

	_, _ = tmpFile.WriteString("  Hello LOSU  \n")
	changes <- struct{}{}

	select {
	case res := <-results:
		if res.Line != "Hello LOSU" {
			t.Errorf("Expected trimmed 'Hello LOSU', got %q", res.Line)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("Tailer failed to pick up line")
	}
}

func TestTailer_SourceField(t *testing.T) {
	tmpFile, _ := os.CreateTemp("", "tailer_source_*.log")
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	results := make(chan model.RawLog, 10)
	changes := make(chan struct{}, 1)
	tail := NewTailer(tmpFile.Name(), results)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() { _ = tail.Run(ctx, changes) }()
	time.Sleep(50 * time.Millisecond)

	_, _ = tmpFile.WriteString("test line\n")
	changes <- struct{}{}

	select {
	case res := <-results:
		if res.Source != tmpFile.Name() {
			t.Errorf("Expected source %q, got %q", tmpFile.Name(), res.Source)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("Tailer failed to pick up line")
	}
}

func TestTailer_MultipleLines(t *testing.T) {
	tmpFile, _ := os.CreateTemp("", "tailer_multi_*.log")
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	results := make(chan model.RawLog, 10)
	changes := make(chan struct{}, 1)
	tail := NewTailer(tmpFile.Name(), results)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() { _ = tail.Run(ctx, changes) }()
	time.Sleep(50 * time.Millisecond)

	_, _ = tmpFile.WriteString("Line1\nLine2\nLine3\n")
	changes <- struct{}{}

	received := make([]string, 0, 3)
	timeout := time.After(500 * time.Millisecond)
	for len(received) < 3 {
		select {
		case res := <-results:
			received = append(received, res.Line)
		case <-timeout:
			t.Errorf("Only received %d/3 lines before timeout: %v", len(received), received)
			return
		}
	}

	expected := []string{"Line1", "Line2", "Line3"}
	for i, exp := range expected {
		if received[i] != exp {
			t.Errorf("Line %d: expected %q, got %q", i+1, exp, received[i])
		}
	}
}

func TestTailer_NonExistentFile(t *testing.T) {
	results := make(chan model.RawLog, 10)
	changes := make(chan struct{}, 1)
	tail := NewTailer("/tmp/this-file-does-not-exist-losu-test.log", results)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := tail.Run(ctx, changes)
	if err == nil {
		t.Error("Expected error for non-existent file, got nil")
	}
}
