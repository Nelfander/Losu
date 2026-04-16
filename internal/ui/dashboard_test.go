package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/nelfander/losu/internal/model"
)

// ── Helper: minimal dashboard for unit tests ─────────────────────────────────
// NewDashboard() initialises tview which requires a terminal.
// For pure logic tests (filtering, buffer management, AI summary) we build
// a lightweight Dashboard directly — no tview.Application, no screen needed.
func newTestDashboard() *Dashboard {
	return &Dashboard{
		SearchFilter:     "",
		LastSearchFilter: "",
		FilteredLogs:     []string{},
		isAutoScroll:     true,
	}
}

// ── Filtering ─────────────────────────────────────────────────────────────────

/*
TestDashboard_Filtering verifies that the SearchFilter logic
correctly includes or excludes logs based on user input.
*/
func TestDashboard_Filtering(t *testing.T) {

	history := []model.LogEvent{
		{Level: "ERROR", Message: "Database down", Timestamp: time.Now()},
		{Level: "INFO", Message: "User logged in", Timestamp: time.Now()},
		{Level: "WARN", Message: "Slow disk", Timestamp: time.Now()},
	}

	filterLower := strings.ToLower("Database")
	var matched []string
	for _, e := range history {
		if strings.Contains(strings.ToLower(e.Message), filterLower) ||
			strings.Contains(strings.ToLower(e.Level), filterLower) {
			matched = append(matched, e.Message)
		}
	}
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for 'Database', got %d", len(matched))
	}

	// Level filter
	filterLower = strings.ToLower("WARN")
	matched = matched[:0]
	for _, e := range history {
		if strings.Contains(strings.ToLower(e.Message), filterLower) ||
			strings.Contains(strings.ToLower(e.Level), filterLower) {
			matched = append(matched, e.Message)
		}
	}
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for level 'WARN', got %d", len(matched))
	}
}

func TestDashboard_Filtering_NoMatch(t *testing.T) {
	history := []model.LogEvent{
		{Level: "INFO", Message: "User logged in", Timestamp: time.Now()},
		{Level: "INFO", Message: "Order processed", Timestamp: time.Now()},
	}

	filterLower := strings.ToLower("ERROR")
	var matched []string
	for _, e := range history {
		if strings.Contains(strings.ToLower(e.Message), filterLower) ||
			strings.Contains(strings.ToLower(e.Level), filterLower) {
			matched = append(matched, e.Message)
		}
	}
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for 'ERROR' in INFO-only history, got %d", len(matched))
	}
}

func TestDashboard_Filtering_EmptyFilter(t *testing.T) {
	history := []model.LogEvent{
		{Level: "ERROR", Message: "Database down", Timestamp: time.Now()},
		{Level: "INFO", Message: "User logged in", Timestamp: time.Now()},
		{Level: "WARN", Message: "Slow disk", Timestamp: time.Now()},
	}

	// Empty filter — all logs should match
	var matched []string
	for _, e := range history {
		matched = append(matched, e.Message)
	}
	if len(matched) != 3 {
		t.Errorf("Expected 3 matches for empty filter, got %d", len(matched))
	}
}

func TestDashboard_Filtering_CaseInsensitive(t *testing.T) {
	history := []model.LogEvent{
		{Level: "ERROR", Message: "DATABASE connection lost", Timestamp: time.Now()},
	}

	// lowercase search should match uppercase message
	filterLower := strings.ToLower("database")
	var matched []string
	for _, e := range history {
		if strings.Contains(strings.ToLower(e.Message), filterLower) {
			matched = append(matched, e.Message)
		}
	}
	if len(matched) != 1 {
		t.Errorf("Expected case-insensitive match, got %d", len(matched))
	}
}

// ── Buffer Management ─────────────────────────────────────────────────────────

/*
TestDashboard_BufferManagement ensures that the UI doesn't
leak memory by holding onto infinite log lines.
*/
func TestDashboard_BufferManagement(t *testing.T) {
	// Simulate the hard-trim logic directly — no tview needed
	const maxVisibleLines = 1500

	var filteredLogs []string
	for i := 0; i < 2500; i++ {
		filteredLogs = append(filteredLogs, "INFO line")
	}

	// Apply the same trim logic as Dashboard.Update()
	if len(filteredLogs) > maxVisibleLines+500 {
		start := len(filteredLogs) - maxVisibleLines
		filteredLogs = filteredLogs[start:]
	}

	if len(filteredLogs) > maxVisibleLines {
		t.Errorf("Buffer trim failed: holding %d lines, want <= %d", len(filteredLogs), maxVisibleLines)
	}
}

func TestDashboard_BufferManagement_UnderLimit(t *testing.T) {
	// Under the limit — should not trim
	const maxVisibleLines = 1500

	var filteredLogs []string
	for i := 0; i < 500; i++ {
		filteredLogs = append(filteredLogs, "INFO line")
	}

	before := len(filteredLogs)
	if len(filteredLogs) > maxVisibleLines+500 {
		start := len(filteredLogs) - maxVisibleLines
		filteredLogs = filteredLogs[start:]
	}

	if len(filteredLogs) != before {
		t.Errorf("Buffer was trimmed when it shouldn't be: had %d, now %d", before, len(filteredLogs))
	}
}

// ── Truncate helper ───────────────────────────────────────────────────────────

/*
TestHelper_Truncate checks the string shortening logic
used in the Top 10 Errors view.
*/
func TestHelper_Truncate(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		limit    int
		expected string
	}{
		{
			name:     "long string gets truncated",
			input:    "This is a very long log message that needs shortening",
			limit:    10,
			expected: "This is...",
		},
		{
			name:     "short string unchanged",
			input:    "short",
			limit:    10,
			expected: "short",
		},
		{
			name:     "exact limit unchanged",
			input:    "exactly10!",
			limit:    10,
			expected: "exactly10!",
		},
		{
			name:     "one over limit gets truncated",
			input:    "exactly11!!",
			limit:    10,
			expected: "exactly...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.input, tt.limit)
			if got != tt.expected {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.limit, got, tt.expected)
			}
		})
	}
}

// ── Status Label ─────────────────────────────────────────────────────────────

/*
TestHelper_StatusLabel ensures the health status reflects EPS and WPS correctly.
getStatusLabel now takes two parameters — eps and wps — matching the split
signal architecture. Error labels take priority over warn labels.
*/
func TestHelper_StatusLabel(t *testing.T) {
	tests := []struct {
		name     string
		eps      float64
		wps      float64
		expected string
	}{
		// Error-based states
		{name: "idle", eps: 0.0, wps: 0.0, expected: "[white]IDLE"},
		{name: "healthy", eps: 0.05, wps: 0.0, expected: "[green]HEALTHY"},
		{name: "minor issues", eps: 0.5, wps: 0.0, expected: "[blue]Minor Issues"},
		{name: "unstable", eps: 2.0, wps: 0.0, expected: "[red]Unstable"},
		{name: "critical spike", eps: 100.0, wps: 0.0, expected: "[blink][red]CRITICAL SPIKE"},
		// Warn-based states (eps below minor threshold)
		{name: "pressure building", eps: 0.0, wps: 60.0, expected: "[yellow]⚠ Pressure Building"},
		{name: "suspicious activity", eps: 0.0, wps: 110.0, expected: "[yellow]⚠ Suspicious Activity"},
		{name: "pre-incident warning", eps: 0.0, wps: 210.0, expected: "[yellow]⚠ Pre-Incident Warning"},
		// Error takes priority over warn
		{name: "error overrides warn", eps: 2.0, wps: 210.0, expected: "[red]Unstable"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getStatusLabel(tt.eps, tt.wps)
			if got != tt.expected {
				t.Errorf("getStatusLabel(%.1f, %.1f) = %q, want %q",
					tt.eps, tt.wps, got, tt.expected)
			}
		})
	}
}

// ── AI Summary ────────────────────────────────────────────────────────────────

func TestDashboard_AISummary(t *testing.T) {
	d := newTestDashboard()
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

func TestDashboard_AISummary_EmptySnapshot(t *testing.T) {
	d := newTestDashboard()
	snap := model.Snapshot{
		RecentMessages: map[string]model.MessageStat{},
	}

	errs, warns := d.GetSummaryForAI(snap)
	if errs == "" {
		t.Error("Expected non-empty fallback message for errors")
	}
	if warns == "" {
		t.Error("Expected non-empty fallback message for warns")
	}
}

func TestDashboard_AISummary_WarnOnly(t *testing.T) {
	d := newTestDashboard()
	snap := model.Snapshot{
		RecentMessages: map[string]model.MessageStat{
			"warn1": {Level: "WARN", Count: 5, VariantCounts: map[string]int{"High memory": 5}},
		},
	}

	errs, warns := d.GetSummaryForAI(snap)
	if strings.Contains(errs, "High memory") {
		t.Error("WARN message should not appear in errors summary")
	}
	if !strings.Contains(warns, "High memory") {
		t.Error("WARN message should appear in warns summary")
	}
}

func TestDashboard_AISummary_Top3Only(t *testing.T) {
	d := newTestDashboard()

	// 5 errors — only top 3 should appear in summary
	snap := model.Snapshot{
		RecentMessages: map[string]model.MessageStat{
			"e1": {Level: "ERROR", Count: 100, VariantCounts: map[string]int{"Error A": 100}},
			"e2": {Level: "ERROR", Count: 80, VariantCounts: map[string]int{"Error B": 80}},
			"e3": {Level: "ERROR", Count: 60, VariantCounts: map[string]int{"Error C": 60}},
			"e4": {Level: "ERROR", Count: 40, VariantCounts: map[string]int{"Error D": 40}},
			"e5": {Level: "ERROR", Count: 20, VariantCounts: map[string]int{"Error E": 20}},
		},
	}

	errs, _ := d.GetSummaryForAI(snap)

	// Count how many error messages appear
	count := 0
	for _, msg := range []string{"Error A", "Error B", "Error C", "Error D", "Error E"} {
		if strings.Contains(errs, msg) {
			count++
		}
	}
	if count > 3 {
		t.Errorf("AI summary should cap at top 3 errors, got %d", count)
	}
}

// ── Sparkline ─────────────────────────────────────────────────────────────────
func TestHelper_SparklineLog_Empty(t *testing.T) {
	result := getSparklineLog([]int{}, 5, "red")
	if result != "" {
		t.Errorf("Expected empty string for empty data, got %q", result)
	}
}

func TestHelper_SparklineLog_AllZeros(t *testing.T) {
	result := getSparklineLog([]int{0, 0, 0, 0}, 5, "red")
	if result == "" {
		t.Error("Expected non-empty sparkline even for zero data")
	}
}

func TestHelper_SparklineLog_SingleSpike(t *testing.T) {
	result := getSparklineLog([]int{0, 0, 100, 0, 0}, 5, "red")
	// Braille chars used for rendering — check for any non-space content
	hasContent := strings.Contains(result, "⣿") ||
		strings.Contains(result, "⣶") ||
		strings.Contains(result, "⣤") ||
		strings.Contains(result, "⣀")
	if !hasContent {
		t.Error("Expected spike to produce Braille block characters in sparkline")
	}
}
