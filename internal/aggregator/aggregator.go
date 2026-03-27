package aggregator

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/nelfander/losu/internal/model"
)

const maxHistory = 50000 // keep 50000 last logs for now its enough
var (
	reDigits = regexp.MustCompile(`\d+`)
	reHex    = regexp.MustCompile(`0x[0-9a-fA-F]+`)
)

func fingerprint(msg string) string {
	msg = reHex.ReplaceAllString(msg, "0x*")
	return reDigits.ReplaceAllString(msg, "")
}

type Aggregator struct {
	mu            sync.RWMutex
	wg            sync.WaitGroup
	TotalLines    int                          `json:"total_lines"`
	ErrorCounts   map[string]int               `json:"error_counts"`
	MessageCounts map[string]model.MessageStat `json:"message_counts"` // Message frequency

	history       []model.LogEvent // Used for "Latest Logs"
	signalHistory []model.LogEvent // This bucket only moves for WARN/ERROR logs

	CurrentSecCount      int       // Tracks logs in the CURRENT 1-second window
	IncidentSecCount     int       // For the Total Traffic/Incident Report
	TrendHistory         []int     // Stores the last 50 snapshots of CurrentSecCount for UI Graph
	IncidentTrendHistory []int     // Exactly 3,600 points (1hr) for Incident Reports
	lastPush             time.Time // Tracks time from last push

	// --- State Management ---
	RecentMessages map[string]*model.MessageStat // clears everytime AI succesfully reads it
	LastErrorTime  time.Time
	LastWarnTime   time.Time
	PeakEPS        float64
	LastReportTime time.Time // Cooldown timer for snapshots

	// --- Incident Report ---

	// --- Hourly Heartbeat/Report--- (Not necesserily hourly, time can be adjusted through env)
	HourlyStartTime time.Time
	HourlyCounts    map[string]int
	TopMessages     map[string]struct {
		Count int
		Level string
	} // To track top most appeared this hour

}

func NewAggregator() *Aggregator {
	a := &Aggregator{
		ErrorCounts:          make(map[string]int),
		MessageCounts:        make(map[string]model.MessageStat),
		history:              make([]model.LogEvent, 0, maxHistory),
		signalHistory:        make([]model.LogEvent, 0, 10000),
		TrendHistory:         make([]int, 0, 50),   // Initialize with space for 50 second
		IncidentTrendHistory: make([]int, 0, 3600), // Initialize for forensics for  1 hour
		RecentMessages:       make(map[string]*model.MessageStat),
		HourlyCounts:         make(map[string]int),
		TopMessages: make(map[string]struct {
			Count int
			Level string
		}),
		HourlyStartTime: time.Now(),
	}

	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

		for range ticker.C {
			a.PushTrend()
		}
	}()

	return a

}

// Update processes a single log event into the global state
func (a *Aggregator) Update(event model.LogEvent, minWeight int, weights map[string]int) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if event.Level == "IGNORE" {
		return
	}

	// ALWAYS update counts - numbers must be accurate

	a.TotalLines++
	a.ErrorCounts[event.Level]++

	// Hourly Stats
	a.HourlyCounts[event.Level]++
	if event.Level == "WARN" || event.Level == "ERROR" {
		// Check if we already have this message
		entry := a.TopMessages[event.Message]
		entry.Count++
		entry.Level = event.Level // Store the level (ERROR or WARN)
		a.TopMessages[event.Message] = entry
	}

	// Track the last time we saw a specific severity
	if event.Level == "ERROR" {
		a.LastErrorTime = event.Timestamp
	} else if event.Level == "WARN" {
		a.LastWarnTime = event.Timestamp
	}

	// Signal History (10m of Errors/Warns)
	if event.Level == "ERROR" || event.Level == "WARN" {
		if len(a.signalHistory) < 10000 {
			a.signalHistory = append(a.signalHistory, event)
		} else {
			copy(a.signalHistory, a.signalHistory[1:])
			a.signalHistory[9999] = event
		}

		// --- THE TRIGGER CHECK ---
		// Pass the Level (string) to the checker
		if a.shouldTriggerReport(event.Level) {
			a.LastReportTime = time.Now()
			// Fire the background worker
			go a.TriggerIncidentReport(fmt.Sprintf("Anomaly: %s Spike Detected", event.Level))
		}
	}

	// Cluster unique messages (focusing on ERROR/WARN)
	if event.Level == "ERROR" || event.Level == "WARN" {
		pattern := fingerprint(event.Message)

		// Update graph counter
		a.CurrentSecCount++
		a.IncidentSecCount++

		//  Safety Check: Don't grow the map forever
		_, exists := a.MessageCounts[pattern]
		if !exists && len(a.MessageCounts) > 10000 {
			// Stop tracking new unique patterns but still make the graph to move

		} else {

			// Update Global MessageCounts (For the UI Top 10)
			stat, exists := a.MessageCounts[pattern]
			if !exists {
				stat = model.MessageStat{
					Level:         event.Level,
					VariantCounts: make(map[string]int),
					Timestamps:    make([]time.Time, 0, 100),
				}
			}
			stat.Count++
			stat.VariantCounts[event.Message]++

			// Add the timestamp to the slice, keeping only the last 100
			stat.Timestamps = append(stat.Timestamps, event.Timestamp)
			if len(stat.Timestamps) > 100 {
				stat.Timestamps = stat.Timestamps[1:]
			}
			a.MessageCounts[pattern] = stat

			// Update RecentMessages (AI short memory)
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

		}
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

	now := time.Now()
	if a.lastPush.IsZero() {
		a.lastPush = now
		return
	}

	elapsed := now.Sub(a.lastPush).Seconds()
	if elapsed <= 0 {
		elapsed = 1.0
	}

	// This is now "Error/Warn Per Second" because IncidentSecCount
	// is only incremented for Errors/Warns in Update()
	currentErrorEPS := float64(a.IncidentSecCount) / elapsed

	// Update Peak
	if currentErrorEPS > a.PeakEPS {
		a.PeakEPS = currentErrorEPS
	}

	// Update UI Graph (Normalized for time)
	a.TrendHistory = append(a.TrendHistory, int(currentErrorEPS))
	if len(a.TrendHistory) > 60 { // Adjusted to 60 for your 1m window
		a.TrendHistory = a.TrendHistory[1:]
	}

	// Forensic History (Also Error/Warn EPS)
	a.IncidentTrendHistory = append(a.IncidentTrendHistory, int(currentErrorEPS))
	if len(a.IncidentTrendHistory) > 3600 {
		a.IncidentTrendHistory = a.IncidentTrendHistory[1:]
	}

	a.lastPush = now
	a.CurrentSecCount = 0
	a.IncidentSecCount = 0
}

/*
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
*/

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

// Function for Hourly Report
func (a *Aggregator) FlushHourlyStats() (time.Time, map[string]int, string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	startTime := a.HourlyStartTime
	counts := a.HourlyCounts

	// Find the single most frequent error/warn
	topMsg := "None"
	maxCount := 0

	// Search for the most frequent ERROR
	for msg, entry := range a.TopMessages {
		if entry.Level == "ERROR" {
			if entry.Count > maxCount {
				maxCount = entry.Count
				topMsg = "[ERROR] " + msg
			}
		}
	}

	// If no error found, search for the most frequent WARN
	if topMsg == "None" {
		for msg, entry := range a.TopMessages {
			if entry.Level == "WARN" {
				if entry.Count > maxCount {
					maxCount = entry.Count
					topMsg = "[WARN] " + msg
				}
			}
		}
	}

	// RESET for the next hour
	a.HourlyStartTime = time.Now()
	a.HourlyCounts = make(map[string]int)
	a.TopMessages = make(map[string]struct {
		Count int
		Level string
	})

	return startTime, counts, fmt.Sprintf("%s (%d times)", topMsg, maxCount)
}

func (a *Aggregator) TriggerIncidentReport(reason string) {
	a.mu.RLock()

	// --- Full Context (Slicing 30k from 50k history) ---
	contextSize := 30000
	if len(a.history) < contextSize {
		contextSize = len(a.history)
	}
	fullContextCopy := make([]model.LogEvent, contextSize)
	copy(fullContextCopy, a.history[len(a.history)-contextSize:])

	// --- Signal History (10m of Errors/Warns) ---
	// Grab the WHOLE signalHistory bucket as it represents the longer window
	signalHistoryCopy := make([]model.LogEvent, len(a.signalHistory))
	copy(signalHistoryCopy, a.signalHistory)

	// --- Metadata (The 1-Hour Trend) ---
	trendCopy := make([]int, len(a.IncidentTrendHistory))
	copy(trendCopy, a.IncidentTrendHistory)

	// Capture other metadata
	peak := a.PeakEPS
	total := a.TotalLines

	a.mu.RUnlock() // Release lock, rest is background work

	// Tell the WaitGroup we are starting a background task
	a.wg.Add(1)

	// Run the I/O and JSON marshaling in a separate goroutine
	go func(full []model.LogEvent, signals []model.LogEvent, trends []int, r string, p float64, t int) {
		defer a.wg.Done()
		timestamp := time.Now().Format("2006-01-02_15-04-05")
		filename := fmt.Sprintf("incident_%s.json", timestamp)

		f, err := os.Create(filename)
		if err != nil {
			return
		}
		defer f.Close()

		writer := bufio.NewWriter(f)

		// Start JSON Structure
		fmt.Fprintf(writer, "{\n  \"reason\": %q,\n  \"peak_eps\": %.2f,\n  \"total_lines\": %d,\n", r, p, t)

		// Write Trend Data
		trendData, _ := json.Marshal(trends)
		fmt.Fprintf(writer, "  \"hourly_trend\": %s,\n", string(trendData))

		// Write  Signal Logs - (Error/Warn only)
		writer.WriteString("  \"signal_history\": [\n")
		for i, event := range signals {
			line, _ := json.Marshal(event)
			writer.Write(line)
			if i < len(signals)-1 {
				writer.WriteString(",\n")
			}
		}
		writer.WriteString("\n  ],\n")

		// Write Full Context Logs - (Everything)
		writer.WriteString("  \"full_context\": [\n")
		for i, event := range full {
			line, _ := json.Marshal(event)
			writer.Write(line)
			if i < len(full)-1 {
				writer.WriteString(",\n")
			}
		}

		writer.WriteString("\n  ]\n}")
		writer.Flush()
	}(fullContextCopy, signalHistoryCopy, trendCopy, reason, peak, total)
}

// When a report should be triggered.
// Times must be changed depending on the app
// Losu runs
func (a *Aggregator) shouldTriggerReport(level string) bool {
	if time.Since(a.LastReportTime) < 5*time.Minute {
		return false
	}
	if level == "FATAL" || level == "CRITICAL" {
		return true
	}

	if len(a.TrendHistory) > 0 {
		avg := a.getAverageEPS()
		// Check the most recent completed second in the trend
		lastFullSec := float64(a.TrendHistory[len(a.TrendHistory)-1])

		if lastFullSec > 10 && lastFullSec > (avg*3) {
			return true
		}
	}
	return false
}

// Helper func to get the average EPS over the last hour
func (a *Aggregator) getAverageEPS() float64 {
	if len(a.IncidentTrendHistory) == 0 {
		return 0
	}
	total := 0
	for _, v := range a.IncidentTrendHistory {
		total += v
	}
	return float64(total) / float64(len(a.IncidentTrendHistory))
}

// Wait waits for all background incident reports to finish writing to disk
func (a *Aggregator) Wait() {
	a.wg.Wait()
}
