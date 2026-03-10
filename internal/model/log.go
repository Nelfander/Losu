package model

import "time"

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
	TotalLines  int
	ErrorCounts map[string]int
	BacklogSize int
	History     []LogEvent
}
