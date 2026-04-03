package model

import "time"

// VariantTimestamps stores the last 20 hit times for a single raw message variant.
// 20 is enough to distinguish burst vs sustained at 10-20k logs/sec —
// a hot variant hits 1-5/sec so 20 entries covers 4-20 seconds of history.
type VariantTimestamps struct {
	Times  [20]time.Time
	Cursor int
	Count  int // total hits ever, not just the 20 stored
}

// Push adds a new timestamp to the ring, overwriting the oldest.
func (v *VariantTimestamps) Push(t time.Time) {
	v.Times[v.Cursor%20] = t
	v.Cursor++
	v.Count++
}

// Slice returns timestamps oldest→newest, skipping zero values.
func (v *VariantTimestamps) Slice() []time.Time {
	out := make([]time.Time, 0, 20)
	size := v.Cursor
	if size > 20 {
		size = 20
	}
	start := (v.Cursor - size + 20) % 20
	for i := 0; i < size; i++ {
		t := v.Times[(start+i)%20]
		if !t.IsZero() {
			out = append(out, t)
		}
	}
	return out
}

type MessageStat struct {
	Count             int
	Level             string
	VariantCounts     map[string]int
	VariantTimestamps map[string]*VariantTimestamps // per-variant timestamp ring
	Timestamps        [100]time.Time                // cluster-level last 100 timestamps
	Cursor            int
}

// RawLog is what the Tailer sends to the Parser
type RawLog struct {
	Source string
	Line   string
}

// LogEvent is the "Structured" version of the log
type LogEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
	Source    string    `json:"source"`
}

// Snapshot is what the TUI asks for to display stats.
// This is the FULL snapshot — never sent over the network.
type Snapshot struct {
	TotalLines     int
	ErrorCounts    map[string]int
	BacklogSize    int
	History        []LogEvent
	TopMessages    []MessageStat
	RecentMessages map[string]MessageStat
	// TrendError and TrendWarn are separate 60s rings — one per signal type.
	// Keeping them split lets the graph show two independent lines (red/yellow)
	// instead of a combined value that hides whether it's errors or warns spiking.
	TrendError    []int
	TrendWarn     []int
	LastErrorTime time.Time
	LastWarnTime  time.Time
	AverageEPS    float64 // average errors per second (60s window)
	PeakEPS       float64 // highest errors per second seen
	AverageWPS    float64 // average warns per second (60s window)
	PeakWPS       float64 // highest warns per second seen
}

// WebMessageStat is a stripped-down version of MessageStat for the browser.
// No timestamp arrays, no variant maps — just what the UI needs to render.
type WebMessageStat struct {
	Pattern    string `json:"pattern"`     // human-readable: most common raw message
	PatternKey string `json:"pattern_key"` // fingerprint key used for /api/inspect lookup
	Level      string `json:"level"`
	Count      int    `json:"count"`
}

// VariantDetail carries count + recent timestamps for one specific raw message variant.
// Returned inside InspectResult — gives users per-variant forensic data.
type VariantDetail struct {
	Count      int      `json:"count"`
	Timestamps []string `json:"timestamps"` // last 20, ISO8601Nano, newest first
}

// InspectResult is the full detail for a single error/warn pattern.
// Returned by GET /api/inspect — only fetched on demand when
// the user clicks a row, never sent in the 500ms WebSocket stream.
type InspectResult struct {
	Pattern  string                    `json:"pattern"`
	Level    string                    `json:"level"`
	Count    int                       `json:"count"`
	Variants map[string]*VariantDetail `json:"variants"` // raw message → detail
}

// WebSnapshot is the bandwidth-safe snapshot sent over WebSocket every 500ms.
// Key differences from Snapshot:
//   - No full History (would be up to 50k events)
//   - No RecentMessages (AI observer's private feed)
//   - SampleLogs is capped at 50 events max
//   - TopErrors uses WebMessageStat (no timestamp arrays)
type WebSnapshot struct {
	TotalLines    int              `json:"total_lines"`
	ErrorCounts   map[string]int   `json:"error_counts"`
	AverageEPS    float64          `json:"average_eps"`
	PeakEPS       float64          `json:"peak_eps"`
	AverageWPS    float64          `json:"average_wps"`
	PeakWPS       float64          `json:"peak_wps"`
	TrendError    []int            `json:"trend_error"`
	TrendWarn     []int            `json:"trend_warn"`
	TopErrors     []WebMessageStat `json:"top_errors"`
	SampleLogs    []LogEvent       `json:"sample_logs"` // last 50 events max
	LastErrorTime time.Time        `json:"last_error_time"`
	LastWarnTime  time.Time        `json:"last_warn_time"`
	// AIAnalysis is the latest text from the AI observer goroutine.
	// Empty string means no analysis has run yet or AI is disabled.
	AIAnalysis string `json:"ai_analysis"`
}

// GetSortedTimestamps returns the timestamps from oldest to newest
func (m *MessageStat) GetSortedTimestamps() []time.Time {
	res := make([]time.Time, 0, 100)

	// If we haven't filled the buffer yet, just take what we have
	if m.Cursor < 100 {
		return m.Timestamps[:m.Cursor]
	}

	// If we HAVE wrapped around:
	// The oldest is at m.Cursor % 100
	// The newest is just before it
	for i := 0; i < 100; i++ {
		idx := (m.Cursor + i) % 100
		res = append(res, m.Timestamps[idx])
	}
	return res
}
