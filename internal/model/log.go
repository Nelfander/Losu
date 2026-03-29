package model

import "time"

type MessageStat struct {
	Count         int
	Level         string
	VariantCounts map[string]int
	Timestamps    [100]time.Time // Store the last 50-100 timestamps for the detail view
	Cursor        int
}

// RawLog is what the Tailer sends to the Parser
type RawLog struct {
	Source string
	Line   string
}

// LogEvent is the "Structured" version of the log
type LogEvent struct {
	Timestamp time.Time
	Level     string
	Message   string
	Source    string
}

// Snapshot is what the UI asks for to display stats
type Snapshot struct {
	TotalLines     int
	ErrorCounts    map[string]int
	BacklogSize    int
	History        []LogEvent
	TopMessages    map[string]MessageStat
	RecentMessages map[string]MessageStat
	Trend          []int
	LastErrorTime  time.Time
	LastWarnTime   time.Time
	AverageEPS     float64
	PeakEPS        float64
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
