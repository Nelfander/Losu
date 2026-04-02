package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/nelfander/losu/internal/model"
)

/*
	TestDashboard_Filtering verifies that the SearchFilter logic

correctly includes or excludes logs based on user input.
*/
func TestDashboard_Filtering(t *testing.T) {
	d := NewDashboard()

	// Create dummy history
	history := []model.LogEvent{
		{Level: "ERROR", Message: "Database down", Timestamp: time.Now()},
		{Level: "INFO", Message: "User logged in", Timestamp: time.Now()},
		{Level: "WARN", Message: "Slow disk", Timestamp: time.Now()},
	}

	snap := model.Snapshot{History: history}

	// Case 1: Search for "Database"
	d.SearchFilter = "Database"
	d.Update(snap)

	if len(d.FilteredLogs) != 1 {
		t.Errorf("Expected 1 filtered log for 'Database', got %d", len(d.FilteredLogs))
	}

	// Case 2: Search for "WARN" (checking level search)
	d.SearchFilter = "WARN"
	d.Update(snap) // This will trigger the filter change reset logic

	if len(d.FilteredLogs) != 1 {
		t.Errorf("Expected 1 filtered log for level 'WARN', got %d", len(d.FilteredLogs))
	}
}

/*
	TestDashboard_BufferManagement ensures that the UI doesn't

leak memory by holding onto infinite log lines.
*/
func TestDashboard_BufferManagement(t *testing.T) {
	d := NewDashboard()

	// Create a snapshot with 2500 logs (past the 1500 limit)
	var massiveHistory []model.LogEvent
	for i := 0; i < 2500; i++ {
		massiveHistory = append(massiveHistory, model.LogEvent{
			Level: "INFO", Message: "Line", Timestamp: time.Now(),
		})
	}

	d.Update(model.Snapshot{History: massiveHistory})

	// The logic should have trimmed FilteredLogs to maxVisibleLines (1500)
	if len(d.FilteredLogs) > 1600 { // Allowing a small buffer room
		t.Errorf("Buffer management failed: holding %d lines", len(d.FilteredLogs))
	}
}

/*
	TestHelper_Truncate checks the string shortening logic

used in the Top 10 Errors view.
*/
func TestHelper_Truncate(t *testing.T) {
	input := "This is a very long log message that needs shortening"
	output := truncate(input, 10)

	expected := "This is..."
	if output != expected {
		t.Errorf("Truncate failed. Expected %q, got %q", expected, output)
	}
}

/* TestHelper_StatusLabel ensures the health colors reflect the EPS.
 */
func TestHelper_StatusLabel(t *testing.T) {
	tests := []struct {
		eps      float64
		expected string
	}{
		{0.0, "[white]IDLE"},
		{0.5, "[blue]Minor Issues"},
		{100.0, "[blink][red]CRITICAL SPIKE"},
	}

	for _, tt := range tests {
		res := getStatusLabel(tt.eps)
		if res != tt.expected {
			t.Errorf("StatusLabel for %f: expected %s, got %s", tt.eps, tt.expected, res)
		}
	}
}

func TestDashboard_AISummary(t *testing.T) {
	d := NewDashboard()
	snap := model.Snapshot{
		RecentMessages: map[string]model.MessageStat{
			"err1": {Level: "ERROR", Count: 10, VariantCounts: map[string]int{"DB Fail": 10}},
		},
	}

	errs, _ := d.GetSummaryForAI(snap)
	if !strings.Contains(errs, "DB Fail") {
		t.Error("AI Summary failed to include the most frequent error message")
	}
}
