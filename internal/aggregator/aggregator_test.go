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

	// We use a WaitGroup to make sure the test doesn't finish
	// until ALL goroutines are done.
	var wg sync.WaitGroup

	//  Start 50 Writer goroutines (Simulating high-speed logs)
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

	//  Start 50 Reader goroutines (Simulating UI refreshes/Snapshots)
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = agg.Snapshot()
				_ = agg.getTopMessages(5)
			}
		}()
	}

	//  Manually trigger the background processes
	for i := 0; i < 10; i++ {
		agg.PushTrend()
	}

	wg.Wait()

	// Final Sanity Check
	if agg.TotalLines != 50000 {
		t.Errorf("Data lost! Expected 50,000 lines, got %d", agg.TotalLines)
	}
}

func TestIncidentTrigger(t *testing.T) {
	agg := NewAggregator()
	weights := map[string]int{"INFO": 1, "WARN": 2, "ERROR": 3}
	minWeight := 1

	//  Warm up
	for i := 0; i < 35; i++ {
		agg.IncidentSecCount = 5
		agg.PushTrend()
	}

	//  Simulate Anomaly
	for i := 0; i < 100; i++ {
		agg.Update(model.LogEvent{Level: "ERROR", Message: "DB FAIL"}, minWeight, weights)
	}
	agg.PushTrend()

	//  The trigger Log
	agg.Update(model.LogEvent{Level: "ERROR", Message: "Trigger"}, minWeight, weights)

	// Instead of time.Sleep(500ms), we use a "Retry Loop"
	// This makes the test as fast as possible on fast PCs
	// but waits longer on slow PCs. ( thanks 100 go mistakes)
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
		time.Sleep(10 * time.Millisecond) // Small tick
	}

	//  Verification & Automatic Cleanup
	if foundFile == "" {
		t.Fatal("Failed to trigger incident report file within 2 seconds")
	}
	agg.Wait()

	t.Logf("Success! Found and removing: %s", foundFile)

	// Clean up immediately so the next test run starts fresh
	err := os.Remove(foundFile)
	if err != nil {
		t.Errorf("Could not clean up test file: %v", err)
	}
}

func TestFingerprint(t *testing.T) {
	// This is the "Table"
	tests := []struct {
		name     string // Clear name of what we are testing
		input    string // The raw log message
		expected string // What the "fingerprint" should look like
	}{
		{
			name:     "Static message",
			input:    "User logged in",
			expected: "User logged in",
		},
		{
			name:     "ID stripping",
			input:    "Order 12345 processed",
			expected: "Order  processed",
		},
		{
			name:     "Hex address stripping",
			input:    "Panic at 0x7ffd123abc",
			expected: "Panic at 0x*",
		},
		{
			name:     "Mixed data",
			input:    "Connection from 192.168.1.1 on port 80",
			expected: "Connection from ... on port ",
		},
	}

	for _, tt := range tests {
		// t.Run creates a 'sub-test' for each row in our table
		t.Run(tt.name, func(t *testing.T) {
			actual := fingerprint(tt.input)
			if actual != tt.expected {
				t.Errorf("fingerprint() for %s\n got:      %q\n expected: %q",
					tt.name, actual, tt.expected)
			}
		})
	}
}

// This is actually testing the top 10 error/warn logic
func TestGroupingAndDetailPreservation(t *testing.T) {
	agg := NewAggregator()
	weights := map[string]int{"ERROR": 3}
	minWeight := 1

	// Two logs that FINGERPRINT the same, but have UNIQUE data
	log1 := model.LogEvent{Level: "ERROR", Message: "S3 upload failed | bucket=\"assets\" key=\"img_1.png\""}
	log2 := model.LogEvent{Level: "ERROR", Message: "S3 upload failed | bucket=\"assets\" key=\"img_2.png\""}

	agg.Update(log1, minWeight, weights)
	agg.Update(log2, minWeight, weights)

	// ---  Test the "Summary" (The shelf) ---
	pattern := fingerprint(log1.Message)
	stat, exists := agg.MessageCounts[pattern]

	if !exists {
		t.Errorf("FAIL: Pattern %q not found in MessageCounts", pattern)
	}
	if stat.Count != 2 {
		t.Errorf("FAIL: Expected 2 hits for pattern, got %d", stat.Count)
	}

	// ---  Test the "Detail" (The unique variations) ---
	if len(stat.VariantCounts) != 2 {
		t.Errorf("FAIL: Expected 2 unique variations, got %d", len(stat.VariantCounts))
	}

	// Check specifically for one of the keys
	if _, ok := stat.VariantCounts[log1.Message]; !ok {
		t.Error("FAIL: Specific details for img_1.png were lost during grouping")
	}
}

// Test that Aggregator never holds more than maxHistory
func TestCircularBufferStability(t *testing.T) {
	agg := NewAggregator()
	weights := map[string]int{"INFO": 1}

	// Push 5 more logs than the max
	totalToPush := maxHistory + 5

	for i := 0; i < totalToPush; i++ {
		msg := fmt.Sprintf("Log number %d", i)
		agg.Update(model.LogEvent{Level: "INFO", Message: msg}, 1, weights)
	}

	//  Check the Length
	if len(agg.history) != maxHistory {
		t.Errorf("FAIL: Buffer grew beyond limit. Got %d, want %d",
			len(agg.history), maxHistory)
	}

	//  Check the First log in history
	// Since we pushed 5 extra logs, the original "Log number 0"
	// should be gone. The new first log should be "Log number 5".
	expectedFirst := "Log number 5"
	actualFirst := agg.history[0].Message

	if actualFirst != expectedFirst {
		t.Errorf("FAIL: Circular shift failed. First log is %q, want %q",
			actualFirst, expectedFirst)
	}

	//  Check the Last log
	expectedLast := fmt.Sprintf("Log number %d", totalToPush-1)
	actualLast := agg.history[maxHistory-1].Message

	if actualLast != expectedLast {
		t.Errorf("FAIL: Last log is %q, want %q", actualLast, expectedLast)
	}
}

func BenchmarkAggregatorUpdate(b *testing.B) {
	agg := NewAggregator()
	weights := map[string]int{"INFO": 1, "ERROR": 3}
	event := model.LogEvent{
		Level:   "ERROR",
		Message: "S3 upload failed | bucket=\"assets\" key=\"img_123.png\"",
	}

	// b.N is a number managed by Go. It will run the loop
	// until it has a statistically significant result.
	b.ResetTimer() // Don't count the setup time above
	for i := 0; i < b.N; i++ {
		agg.Update(event, 1, weights)
	}
}
