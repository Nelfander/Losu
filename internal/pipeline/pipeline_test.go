package pipeline

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/nelfander/losu/internal/model"
)

// MockParser is a fake parser used only for testing the pipeline's
// ability to move data, without needing real regex logic.
type MockParser struct{}

func (m *MockParser) Parse(raw model.RawLog) model.LogEvent {
	return model.LogEvent{
		Message: raw.Line,
		Level:   "INFO",
		Source:  raw.Source, // preserve source for multi-file support
	}
}

/*
	TestProcess_MultiWorkerFlow verifies that:

1. Multiple workers can pull from the same input channel.
2. All data sent into the pipeline is successfully transformed and sent to output.
3. The WaitGroup correctly tracks the lifecycle of all workers.
*/
func TestProcess_MultiWorkerFlow(t *testing.T) {
	numLogs := 100
	numWorkers := 5

	input := make(chan model.RawLog, numLogs)
	output := make(chan model.LogEvent, numLogs)
	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the pipeline
	Process(ctx, &wg, numWorkers, &MockParser{}, input, output)

	//  Feed the pipeline
	for i := 0; i < numLogs; i++ {
		input <- model.RawLog{Line: "test log"}
	}
	close(input) // Closing input should trigger workers to finish

	//  Wait for workers to finish
	// We use a goroutine to wait so the test doesn't hang if there's a bug
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success: Workers exited after input closed
	case <-time.After(1 * time.Second):
		t.Fatal("Pipeline workers failed to exit after channel close")
	}

	//  Verify output count
	if len(output) != numLogs {
		t.Errorf("Data loss! Expected %d events, got %d", numLogs, len(output))
	}
}

/*
	TestProcess_ContextCancel verifies that:

Workers stop immediately when the context is canceled, even if
the input channel still has data or is still open.
*/
func TestProcess_ContextCancel(t *testing.T) {
	input := make(chan model.RawLog, 10)
	output := make(chan model.LogEvent, 10)
	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())

	Process(ctx, &wg, 1, &MockParser{}, input, output)

	// Cancel before sending data
	cancel()

	// Wait for worker to stop
	wg.Wait()

	// Try to send data - if worker is still running, it might pull this.
	// But since wg.Wait() passed, we know it's dead.
	select {
	case input <- model.RawLog{Line: "wont be processed"}:
		// Channel accepted it, but no one is watching
	default:
	}

	if len(output) > 0 {
		t.Error("Worker processed data after context was canceled")
	}
}

func TestProcess_OutputContent(t *testing.T) {
	input := make(chan model.RawLog, 1)
	output := make(chan model.LogEvent, 1)
	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	Process(ctx, &wg, 1, &MockParser{}, input, output)

	input <- model.RawLog{Line: "hello world", Source: "/var/log/app.log"}
	close(input)
	wg.Wait()

	select {
	case event := <-output:
		if event.Message != "hello world" {
			t.Errorf("message: got %q want %q", event.Message, "hello world")
		}
		if event.Level != "INFO" {
			t.Errorf("level: got %q want INFO", event.Level)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("no output received")
	}
}

func TestProcess_SourcePreserved(t *testing.T) {
	input := make(chan model.RawLog, 1)
	output := make(chan model.LogEvent, 1)
	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	Process(ctx, &wg, 1, &MockParser{}, input, output)

	input <- model.RawLog{Line: "test", Source: "/logs/auth.log"}
	close(input)
	wg.Wait()

	select {
	case event := <-output:
		if event.Source != "/logs/auth.log" {
			t.Errorf("source: got %q want /logs/auth.log", event.Source)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("no output received")
	}
}
