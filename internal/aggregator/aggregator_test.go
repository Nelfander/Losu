package aggregator

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nelfander/losu/internal/model"
)

func TestAggregatorConcurrency(t *testing.T) {
	agg := NewAggregator()
	weights := map[string]int{"INFO": 1, "ERROR": 3}

	var wg sync.WaitGroup

	// Start 50 Writer goroutines
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				agg.Update(model.LogEvent{
					Level:   "INFO",
					Message: fmt.Sprintf("Log from writer %d", id),
				}, 1, weights)
			}
		}(i)
	}

	// Start 50 Reader goroutines
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = agg.Snapshot()
				// Serve cachedTopMsgs from Snapshot or manual call
				_ = agg.getTopMessages(5)
			}
		}()
	}

	// Manually trigger trend pushes
	for i := 0; i < 5; i++ {
		agg.PushTrend()
	}

	wg.Wait()

	if agg.TotalLines != 50000 {
		t.Errorf("Data lost! Expected 50,000 lines, got %d", agg.TotalLines)
	}
}

func TestIncidentTrigger(t *testing.T) {
	agg := NewAggregator()
	weights := map[string]int{"INFO": 1, "WARN": 2, "ERROR": 3}
	minWeight := 1

	// Warm up the trend history so AverageEPS isn't 0
	// We manually push values into the ring via PushTrend
	for i := 0; i < 10; i++ {
		agg.IncidentSecCount = 5
		agg.PushTrend()
	}

	// Simulate Anomaly Spike
	// Spike must be > 10 AND > AverageEPS * 3
	agg.IncidentSecCount = 100
	agg.PushTrend()

	// The trigger event
	agg.Update(model.LogEvent{Level: "ERROR", Message: "CRITICAL FAILURE"}, minWeight, weights)

	var foundFile string
	deadline := time.Now().Add(2 * time.Second)

	for time.Now().Before(deadline) {
		files, _ := os.ReadDir(".")
		for _, f := range files {
			if strings.HasPrefix(f.Name(), "incident_") {
				foundFile = f.Name()
				break
			}
		}
		if foundFile != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if foundFile == "" {
		t.Fatal("Failed to trigger incident report file within 2 seconds")
	}

	agg.Wait() // Ensure goroutine finished writing
	os.Remove(foundFile)
}

func TestFingerprint(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Static message",
			input:    "User logged in",
			expected: "User logged in",
		},
		{
			name:     "ID stripping",
			input:    "Order 12345 processed",
			expected: "Order * processed",
		},
		{
			name:     "Hex address stripping",
			input:    "Panic at 0x7ffd123abc",
			expected: "Panic at 0x*",
		},
		{
			name:     "IP-like stripping",
			input:    "Conn from 192.168.1.1",
			expected: "Conn from *.*.*.*",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := fingerprint(tt.input)
			if actual != tt.expected {
				t.Errorf("fingerprint() %s\n got:      %q\n expected: %q",
					tt.name, actual, tt.expected)
			}
		})
	}
}

func TestGroupingAndDetailPreservation(t *testing.T) {
	agg := NewAggregator()
	weights := map[string]int{"ERROR": 3}
	minWeight := 1

	log1 := model.LogEvent{Level: "ERROR", Message: "S3 upload failed | id=101"}
	log2 := model.LogEvent{Level: "ERROR", Message: "S3 upload failed | id=102"}

	agg.Update(log1, minWeight, weights)
	agg.Update(log2, minWeight, weights)

	pattern := fingerprint(log1.Message)
	stat, exists := agg.MessageCounts[pattern]

	if !exists {
		t.Fatalf("Pattern %q not found", pattern)
	}
	if stat.Count != 2 {
		t.Errorf("Expected 2 hits, got %d", stat.Count)
	}
	if len(stat.VariantCounts) != 2 {
		t.Errorf("Expected 2 unique variations, got %d", len(stat.VariantCounts))
	}
}

func TestCircularBufferStability(t *testing.T) {
	agg := NewAggregator()
	weights := map[string]int{"INFO": 1}

	// Push more than capacity
	totalToPush := maxHistory + 10

	for i := 0; i < totalToPush; i++ {
		msg := fmt.Sprintf("Log %d", i)
		agg.Update(model.LogEvent{Level: "INFO", Message: msg}, 1, weights)
	}

	history := agg.GetHistory()

	if len(history) != maxHistory {
		t.Errorf("Buffer size mismatch. Got %d, want %d", len(history), maxHistory)
	}

	// In a ring buffer of cap 50k, if we push 50,010 logs:
	// The first log should be "Log 10"
	expectedFirst := "Log 10"
	if history[0].Message != expectedFirst {
		t.Errorf("Circular shift failed. First log is %q, want %q", history[0].Message, expectedFirst)
	}

	expectedLast := fmt.Sprintf("Log %d", totalToPush-1)
	if history[maxHistory-1].Message != expectedLast {
		t.Errorf("Last log is %q, want %q", history[maxHistory-1].Message, expectedLast)
	}
}

func BenchmarkAggregatorUpdate(b *testing.B) {
	agg := NewAggregator()
	weights := map[string]int{"INFO": 1, "ERROR": 3}
	event := model.LogEvent{
		Level:   "ERROR",
		Message: "S3 upload failed | bucket=\"assets\" key=\"img_123.png\"",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		agg.Update(event, 1, weights)
	}
}
