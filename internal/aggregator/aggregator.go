package aggregator

import (
	"encoding/json"
	"os"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/nelfander/losu/internal/model"
)

const maxHistory = 50000 // keep 50000 last logs for now its enough

type Aggregator struct {
	mu              sync.RWMutex
	TotalLines      int                          `json:"total_lines"`
	ErrorCounts     map[string]int               `json:"error_counts"`
	MessageCounts   map[string]model.MessageStat `json:"message_counts"` // Message frequency
	history         []model.LogEvent
	CurrentSecCount int                           // Tracks logs in the CURRENT 1-second window
	TrendHistory    []int                         // Stores the last 50 snapshots of CurrentSecCount
	RecentMessages  map[string]*model.MessageStat // clears everytime AI succesfully reads it
	LastErrorTime   time.Time
	LastWarnTime    time.Time
	PeakEPS         float64
}

func NewAggregator() *Aggregator {
	return &Aggregator{
		ErrorCounts:    make(map[string]int),
		MessageCounts:  make(map[string]model.MessageStat),
		history:        make([]model.LogEvent, 0, maxHistory),
		TrendHistory:   make([]int, 0, 50), // Initialize with space for 50 second
		RecentMessages: make(map[string]*model.MessageStat),
	}
}

// Update takes an event and a filter weight to decide what to count vs what to record
func (a *Aggregator) Update(event model.LogEvent, minWeight int, weights map[string]int) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if event.Level == "IGNORE" {
		return
	}

	// ALWAYS update counts - numbers must be accurate
	a.TotalLines++
	a.ErrorCounts[event.Level]++

	// Track the last time we saw a specific severity
	if event.Level == "ERROR" {
		a.LastErrorTime = event.Timestamp
	} else if event.Level == "WARN" {
		a.LastWarnTime = event.Timestamp
	}

	// Cluster unique messages (focusing on ERROR/WARN)
	if event.Level == "ERROR" || event.Level == "WARN" {
		pattern := fingerprint(event.Message)

		//  Update Global MessageCounts (For the UI Top 10)
		stat, exists := a.MessageCounts[pattern]
		if !exists {
			stat = model.MessageStat{
				Level:         event.Level,
				VariantCounts: make(map[string]int),
			}
		}
		stat.Count++
		stat.VariantCounts[event.Message]++
		a.MessageCounts[pattern] = stat

		//  Update RecentMessages (AI short memory)
		recentStat, recentExists := a.RecentMessages[pattern]
		if !recentExists {
			recentStat = &model.MessageStat{
				Level:         event.Level,
				VariantCounts: make(map[string]int),
			}
			a.RecentMessages[pattern] = recentStat
		}
		recentStat.Count++
		recentStat.VariantCounts[event.Message]++

		// Increment the counter for the Sparkline graph
		a.CurrentSecCount++
	}

	// ONLY add to history if it passes the weight check
	if weights[event.Level] >= minWeight {
		if len(a.history) < maxHistory {
			// Still filling up the initial buffer
			a.history = append(a.history, event)
		} else {
			// Buffer is full: Overwrite the oldest instead of shifting [1:]
			// This prevents massive memory re-allocations and copies.
			// We shift the data once or use an index, but for simplicity
			// and tview compatibility, copy() is actually faster than [1:]
			// because it's a primitive hardware-optimized operation.
			copy(a.history, a.history[1:])
			a.history[maxHistory-1] = event
		}
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

	// Average eps calculation
	avg := 0.0
	if len(a.TrendHistory) > 0 {
		total := 0
		for _, v := range a.TrendHistory {
			total += v
		}
		avg = float64(total) / float64(len(a.TrendHistory))
	}

	return model.Snapshot{
		TotalLines:    a.TotalLines,
		ErrorCounts:   counts,
		History:       historyCopy,
		TopMessages:   a.getTopMessages(10),
		Trend:         trendCopy,
		LastErrorTime: a.LastErrorTime,
		LastWarnTime:  a.LastWarnTime,
		AverageEPS:    avg,
		PeakEPS:       a.PeakEPS,
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
		// Hits (Primary)
		if ss[i].Stat.Count != ss[j].Stat.Count {
			return ss[i].Stat.Count > ss[j].Stat.Count
		}
		// Alphabetical (Tie-breaker)
		// This ensures the slice sent to the UI is ALWAYS in the same order
		return ss[i].Key < ss[j].Key
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

	//  Check for new High Water Mark
	currentEPS := float64(a.CurrentSecCount)
	if currentEPS > a.PeakEPS {
		a.PeakEPS = currentEPS
	}

	//  Move count into history
	a.TrendHistory = append(a.TrendHistory, a.CurrentSecCount)

	if len(a.TrendHistory) > 50 {
		a.TrendHistory = a.TrendHistory[1:]
	}

	//  Reset for next second
	a.CurrentSecCount = 0
}

// fingerprint simplifies a message by replacing variable data (numbers, hex) with '*'
func fingerprint(msg string) string {
	// Match digits (IDs, database numbers, etc.)
	reDigits := regexp.MustCompile(`\d+`)
	// Match hex sequences (Memory addresses like 0x7ffd...)
	reHex := regexp.MustCompile(`0x[0-9a-fA-F]+`)

	msg = reHex.ReplaceAllString(msg, "0x*")
	msg = reDigits.ReplaceAllString(msg, "")

	return msg
}

func (a *Aggregator) GetRecentSnapshot() map[string]model.MessageStat {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Clone the current recent stats to send to AI
	recent := make(map[string]model.MessageStat)
	for k, v := range a.RecentMessages {
		recent[k] = *v
	}

	// Clear the buffer so the next AI call starts fresh!
	a.RecentMessages = make(map[string]*model.MessageStat)

	return recent
}
