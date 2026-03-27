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

	//  Warm up: 35 seconds of 5 EPS (Normal baseline)
	// Note: Use a lower baseline so the 3x spike is easier to hit
	for i := 0; i < 35; i++ {
		agg.IncidentSecCount = 5
		agg.PushTrend()
	}

	avg := agg.getAverageEPS()
	t.Logf("Warmed up. Avg EPS: %.2f", avg)

	//  Simulate the Anomaly: 100 Errors
	// We update the aggregator so the NEXT PushTrend captures them
	for i := 0; i < 100; i++ {
		agg.Update(model.LogEvent{
			Level:   "ERROR",
			Message: "DATABASE FAILURE",
		}, minWeight, weights)
	}

	//  CRITICAL: Push the 100 errors into TrendHistory
	agg.PushTrend()

	//  Trigger the check: Send ONE more log.
	// shouldTriggerReport looks at the LAST entry in TrendHistory (which is now 100)
	agg.Update(model.LogEvent{Level: "ERROR", Message: "Trigger Log"}, minWeight, weights)

	// Wait for the background file writer
	time.Sleep(500 * time.Millisecond)

	//  Verification
	files, _ := os.ReadDir(".")
	found := false
	for _, f := range files {
		if strings.HasPrefix(f.Name(), "incident_") {
			found = true
			t.Logf("Success! Found: %s", f.Name())
			//	os.Remove(f.Name()) // Clean up
			break
		}
	}

	if !found {
		t.Errorf("Failed. TrendHistory Last: %v, Avg: %.2f",
			agg.TrendHistory[len(agg.TrendHistory)-1], avg)
	}
}
