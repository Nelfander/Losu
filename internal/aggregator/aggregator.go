package aggregator

import (
	"encoding/json"
	"os"
	"sort"
	"sync"

	"github.com/nelfander/losu/internal/model"
)

const maxHistory = 25 // keep 25 last logs for now its enough

type Aggregator struct {
	mu              sync.RWMutex
	TotalLines      int                          `json:"total_lines"`
	ErrorCounts     map[string]int               `json:"error_counts"`
	MessageCounts   map[string]model.MessageStat `json:"message_counts"` // Message frequency
	history         []model.LogEvent
	CurrentSecCount int   // Tracks logs in the CURRENT 1-second window
	TrendHistory    []int // Stores the last 50 snapshots of CurrentSecCount
}

func NewAggregator() *Aggregator {
	return &Aggregator{
		ErrorCounts:   make(map[string]int),
		MessageCounts: make(map[string]model.MessageStat),
		history:       make([]model.LogEvent, 0, maxHistory),
		TrendHistory:  make([]int, 0, 50), // Initialize with space for 50 second
	}
}

// Update takes an event and a filter weight to decide what to count vs what to record
func (a *Aggregator) Update(event model.LogEvent, minWeight int, weights map[string]int) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if event.Level == "IGNORE" {
		return
	}

	// ALWAYS update counts -  numbers must be accurate
	a.TotalLines++
	a.ErrorCounts[event.Level]++

	// Cluster unique messages (focusing on ERROR/WARN)
	if event.Level == "ERROR" || event.Level == "WARN" {
		stat := a.MessageCounts[event.Message]
		stat.Count++
		stat.Level = event.Level // Store the actual level from the log
		a.MessageCounts[event.Message] = stat
		a.CurrentSecCount++
	}

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

	trendCopy := make([]int, len(a.TrendHistory))
	copy(trendCopy, a.TrendHistory)

	// Again copy cause if a.history Ui can read the same memory that
	// a worker might be writing on
	historyCopy := make([]model.LogEvent, len(a.history))
	copy(historyCopy, a.history)

	return model.Snapshot{
		TotalLines:  a.TotalLines,
		ErrorCounts: counts,
		History:     historyCopy,
		TopMessages: a.getTopMessages(10),
		Trend:       trendCopy,
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

// Gets the top N most frequent messages
func (a *Aggregator) getTopMessages(n int) map[string]model.MessageStat {
	type kv struct {
		Key  string
		Stat model.MessageStat
	}
	var ss []kv
	for k, v := range a.MessageCounts {
		ss = append(ss, kv{k, v})
	}

	sort.Slice(ss, func(i, j int) bool {
		return ss[i].Stat.Count > ss[j].Stat.Count
	})

	top := make(map[string]model.MessageStat)
	for i := 0; i < n && i < len(ss); i++ {
		top[ss[i].Key] = ss[i].Stat
	}
	return top
}

func (a *Aggregator) PushTrend() {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Move the current second's count into history
	a.TrendHistory = append(a.TrendHistory, a.CurrentSecCount)

	// Keep only the last 50 seconds so the graph doesn't grow forever
	if len(a.TrendHistory) > 50 {
		a.TrendHistory = a.TrendHistory[1:]
	}

	// Reset the counter for the next second
	a.CurrentSecCount = 0
}
