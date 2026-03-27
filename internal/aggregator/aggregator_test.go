package aggregator

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nelfander/losu/internal/model"
)

func TestIncidentTrigger(t *testing.T) {
	agg := NewAggregator()
	weights := map[string]int{"INFO": 1, "WARN": 2, "ERROR": 3}
	minWeight := 1

	//  Warm up the IncidentTrendHistory (35 seconds of "Normal" 10 EPS)
	// This ensures getAverageEPS() > 0 and satisfies len > 30 check.
	for i := 0; i < 35; i++ {
		for j := 0; j < 10; j++ {
			agg.Update(model.LogEvent{Level: "INFO", Message: "Normal traffic"}, minWeight, weights)
		}
		agg.PushTrend()
	}

	t.Logf("Warmed up. Avg EPS: %.2f", agg.getAverageEPS())

	//  Simulate the Anomaly (Spike to 200 EPS)
	// Our trigger is current > (avg * 3) and current > 20.
	// 200 > (10 * 3) is true.
	for i := 0; i < 200; i++ {
		agg.Update(model.LogEvent{
			Level:     "ERROR",
			Message:   "DATABASE CONNECTION FAILURE",
			Timestamp: time.Now(),
		}, minWeight, weights)
	}

	//  Wait a moment for the background goroutine to write the file
	time.Sleep(500 * time.Millisecond)

	//  Verification: Look for any file starting with "incident_"
	files, _ := os.ReadDir(".")
	found := false
	for _, f := range files {
		if strings.HasPrefix(f.Name(), "incident_") && strings.HasSuffix(f.Name(), ".json") {
			found = true
			t.Logf("Success! Found incident report: %s", f.Name())
			// Clean up after test
			//	os.Remove(f.Name())
			break
		}
	}

	if !found {
		t.Errorf("Failed to trigger incident report. CurrentSec: %d, Avg: %.2f",
			agg.IncidentSecCount, agg.getAverageEPS())
	}
}
