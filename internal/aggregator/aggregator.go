package aggregator

import (
	"encoding/json"
	"os"
	"sync"

	"github.com/nelfander/losu/internal/model"
)

const maxHistory = 25 // keep 25 last logs for now its enough

type Aggregator struct {
	mu          sync.RWMutex
	TotalLines  int            `json:"total_lines"`
	ErrorCounts map[string]int `json:"error_counts"`
	history     []model.LogEvent
}

func NewAggregator() *Aggregator {
	return &Aggregator{
		ErrorCounts: make(map[string]int),
		history:     make([]model.LogEvent, 0, maxHistory),
	}
}

// Update takes an event and a filter weight to decide what to count vs what to record
func (a *Aggregator) Update(event model.LogEvent, minWeight int, weights map[string]int) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if event.Level == "IGNORE" {
		return
	}

	// ALWAYS update counts - we want the numbers to be accurate
	a.TotalLines++
	a.ErrorCounts[event.Level]++

	// ONLY add to history if it passes the weight check
	if weights[event.Level] >= minWeight {
		if len(a.history) >= maxHistory {
			a.history = a.history[1:]
		}
		a.history = append(a.history, event)
	}
}

// Snapshot returns a read-only copy of the current state
func (a *Aggregator) Snapshot() model.Snapshot {
	a.mu.RLock()
	defer a.mu.RUnlock()

	// Copy the map so the UI doesn't race with the Workers
	counts := make(map[string]int)
	for k, v := range a.ErrorCounts {
		counts[k] = v
	}

	// Again copy cause if a.history Ui can read the same memory that
	// a worker might be writing on
	historyCopy := make([]model.LogEvent, len(a.history))
	copy(historyCopy, a.history)

	return model.Snapshot{
		TotalLines:  a.TotalLines,
		ErrorCounts: counts,
		History:     historyCopy,
	}
}

func (a *Aggregator) GetHistory() []model.LogEvent {
	a.mu.RLock()
	defer a.mu.RUnlock()

	// Return a copy of the slice so the UI can use it safely
	snapshot := make([]model.LogEvent, len(a.history))
	copy(snapshot, a.history)
	return snapshot
}

// Saves
func (a *Aggregator) Save(filepath string) error {
	a.mu.RLock()
	defer a.mu.RUnlock()

	data, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath, data, 0644)
}

func (a *Aggregator) Load(filepath string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	file, err := os.ReadFile(filepath)
	if err != nil {
		return err // It's okay if the file doesn't exist yet
	}

	return json.Unmarshal(file, a)
}
