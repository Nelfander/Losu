package aggregator

import (
	"sync"

	"github.com/nelfander/losu/internal/model"
)

type Aggregator struct {
	mu          sync.RWMutex
	totalLines  int
	errorCounts map[string]int
}

func NewAggregator() *Aggregator {
	return &Aggregator{
		errorCounts: make(map[string]int),
	}
}

// Update takes an event and safely updates the stats
func (a *Aggregator) Update(event model.LogEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Skip the ghost lines
	if event.Level == "IGNORE" {
		return
	}

	a.totalLines++
	a.errorCounts[event.Level]++
}

// Snapshot returns a read-only copy of the current state
func (a *Aggregator) Snapshot() model.Snapshot {
	a.mu.RLock()
	defer a.mu.RUnlock()

	// Copy the map so the UI doesn't race with the Workers
	counts := make(map[string]int)
	for k, v := range a.errorCounts {
		counts[k] = v
	}

	return model.Snapshot{
		TotalLines:  a.totalLines,
		ErrorCounts: counts,
	}
}
