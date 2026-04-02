package aggregator

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nelfander/losu/internal/model"
)

const (
	maxHistory       = 50000
	maxSignalHistory = 10000
	maxTrend         = 60
	maxIncidentTrend = 3600
)

// --- Pool for fingerprint builder ---
var builderPool = sync.Pool{
	New: func() any { return &strings.Builder{} },
}

// fingerprint is pure CPU work with no shared state — safe to call outside any lock.
func fingerprint(msg string) string {
	if len(msg) == 0 {
		return ""
	}
	b := builderPool.Get().(*strings.Builder)
	b.Reset()
	b.Grow(len(msg))

	inToken := false
	for i := 0; i < len(msg); i++ {
		c := msg[i]

		isDigit := (c >= '0' && c <= '9')
		isHex := (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
		// Special case for hex prefix 0x
		isHexPrefix := (c == 'x' || c == 'X') && i > 0 && msg[i-1] == '0'

		if isDigit || (inToken && (isHex || isHexPrefix)) {
			if !inToken {
				// If it's 0x..., write the 0 and start the token at x
				if i+1 < len(msg) && (msg[i+1] == 'x' || msg[i+1] == 'X') {
					b.WriteByte(c) // Write the '0'
					continue       // Let the next iteration handle the 'x'
				}
				b.WriteByte('*')
				inToken = true
			}
			continue
		}

		// Standard separators
		if c == ' ' || c == '=' || c == '[' || c == ']' || c == ':' || c == '"' || c == '/' || c == '-' || c == '.' {
			inToken = false
		}

		if inToken && !isDigit && !isHex && !isHexPrefix {
			inToken = false
		}

		if !inToken {
			b.WriteByte(c)
		}
	}

	result := b.String()
	builderPool.Put(b)
	return result
}

// ringInt is a fixed-size circular buffer of ints — no allocations after init.
type ringInt struct {
	data  []int
	head  int
	count int
	cap   int
}

func newRingInt(capacity int) ringInt {
	return ringInt{data: make([]int, capacity), cap: capacity}
}

func (r *ringInt) push(v int) (evicted int, hadEviction bool) {
	if r.count == r.cap {
		evicted = r.data[r.head%r.cap]
		hadEviction = true
	}
	r.data[r.head%r.cap] = v
	r.head++
	if r.count < r.cap {
		r.count++
	}
	return
}

func (r *ringInt) last() (int, bool) {
	if r.count == 0 {
		return 0, false
	}
	idx := (r.head - 1 + r.cap) % r.cap
	return r.data[idx], true
}

func (r *ringInt) slice() []int {
	out := make([]int, r.count)
	start := (r.head - r.count + r.cap) % r.cap
	for i := 0; i < r.count; i++ {
		out[i] = r.data[(start+i)%r.cap]
	}
	return out
}

// ringEvent is a fixed-size circular buffer of LogEvents.
type ringEvent struct {
	data  []model.LogEvent
	head  int
	count int
	cap   int
}

func newRingEvent(capacity int) ringEvent {
	return ringEvent{data: make([]model.LogEvent, capacity), cap: capacity}
}

func (r *ringEvent) push(e model.LogEvent) {
	r.data[r.head%r.cap] = e
	r.head++
	if r.count < r.cap {
		r.count++
	}
}

func (r *ringEvent) slice() []model.LogEvent {
	out := make([]model.LogEvent, r.count)
	start := (r.head - r.count + r.cap) % r.cap
	for i := 0; i < r.count; i++ {
		out[i] = r.data[(start+i)%r.cap]
	}
	return out
}

// --- Aggregator ---

type Aggregator struct {
	mu sync.RWMutex
	wg sync.WaitGroup

	TotalLines  int            `json:"total_lines"`
	ErrorCounts map[string]int `json:"error_counts"`

	// Message clustering (ERROR/WARN only)
	MessageCounts  map[string]model.MessageStat  `json:"message_counts"`
	RecentMessages map[string]*model.MessageStat `json:"-"`
	lastMsg        string
	lastPattern    string

	// History buffers — ring-based, no shifting
	history       ringEvent
	signalHistory ringEvent

	// Trend — ring-based, no shifting
	trendRing    ringInt // 60s UI graph
	incidentRing ringInt // 3600s forensic

	// Pre-computed values, updated in PushTrend (under lock, once/sec)
	AverageEPS      float64
	PeakEPS         float64
	cachedTopMsgs   []model.MessageStat // recomputed in PushTrend, served from Snapshot
	trendRunningSum float64             // incremental sum for O(1) average

	CurrentSecCount  int
	IncidentSecCount int
	lastPush         time.Time

	// Timestamps
	LastErrorTime  time.Time
	LastWarnTime   time.Time
	LastReportTime time.Time

	// Hourly stats
	HourlyStartTime time.Time
	HourlyCounts    map[string]int
	TopMessages     map[string]struct {
		Count int
		Level string
	}
}

func NewAggregator() *Aggregator {
	a := &Aggregator{
		ErrorCounts:    make(map[string]int),
		MessageCounts:  make(map[string]model.MessageStat),
		RecentMessages: make(map[string]*model.MessageStat),
		HourlyCounts:   make(map[string]int),
		TopMessages: make(map[string]struct {
			Count int
			Level string
		}),
		HourlyStartTime: time.Now(),
		history:         newRingEvent(maxHistory),
		signalHistory:   newRingEvent(maxSignalHistory),
		trendRing:       newRingInt(maxTrend),
		incidentRing:    newRingInt(maxIncidentTrend),
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

// Update processes a single log event.
// Heavy CPU work (fingerprinting) is done BEFORE acquiring the lock.
func (a *Aggregator) Update(event model.LogEvent, minWeight int, weights map[string]int) {
	if event.Level == "IGNORE" {
		return
	}

	isSignal := event.Level == "ERROR" || event.Level == "WARN"

	// --- Compute fingerprint OUTSIDE the lock ---
	// fingerprint() uses only its argument and a sync.Pool — no shared state.
	var pattern string
	if isSignal {
		pattern = fingerprint(event.Message)
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	a.TotalLines++
	a.ErrorCounts[event.Level]++
	a.HourlyCounts[event.Level]++

	if event.Level == "ERROR" {
		a.LastErrorTime = event.Timestamp
	} else if event.Level == "WARN" {
		a.LastWarnTime = event.Timestamp
	}

	if isSignal {
		// Hourly top-messages tracking
		entry := a.TopMessages[event.Message]
		entry.Count++
		entry.Level = event.Level
		a.TopMessages[event.Message] = entry

		// Signal ring (no copy/shift)
		a.signalHistory.push(event)

		// Incident trigger — uses pre-computed AverageEPS, no inner loop
		if a.shouldTriggerReport(event.Level) {
			a.LastReportTime = time.Now()
			go a.TriggerIncidentReport(fmt.Sprintf("Anomaly: %s Spike Detected", event.Level))
		}

		// Message clustering
		a.CurrentSecCount++
		a.IncidentSecCount++

		// Cache last pattern to skip fingerprinting for repeated messages
		if event.Message != a.lastMsg {
			a.lastMsg = event.Message
			a.lastPattern = pattern
		} else {
			pattern = a.lastPattern
		}

		if _, exists := a.MessageCounts[pattern]; !exists && len(a.MessageCounts) > 10000 {
			// Cap reached — still count traffic but don't add new patterns
		} else {
			stat := a.MessageCounts[pattern]
			if stat.VariantCounts == nil {
				stat = model.MessageStat{
					Level:         event.Level,
					VariantCounts: make(map[string]int),
				}
			}
			stat.Count++
			stat.VariantCounts[event.Message]++
			stat.Timestamps[stat.Cursor%100] = event.Timestamp
			stat.Cursor++
			a.MessageCounts[pattern] = stat

			rStat, ok := a.RecentMessages[pattern]
			if !ok {
				rStat = &model.MessageStat{
					Level:         event.Level,
					VariantCounts: make(map[string]int),
				}
				a.RecentMessages[pattern] = rStat
			}
			rStat.Count++
			rStat.VariantCounts[event.Message]++
		}
	}

	// History ring (no copy/shift)
	if weights[event.Level] >= minWeight {
		a.history.push(event)
	}
}

// Snapshot returns a point-in-time copy. No sort, no heavy work — all pre-computed.
func (a *Aggregator) Snapshot() model.Snapshot {
	a.mu.RLock()
	defer a.mu.RUnlock()

	counts := make(map[string]int, len(a.ErrorCounts))
	for k, v := range a.ErrorCounts {
		counts[k] = v
	}

	// cachedTopMsgs and trendRing.slice() are cheap — ring copy only
	topMsgsCopy := make([]model.MessageStat, len(a.cachedTopMsgs))
	copy(topMsgsCopy, a.cachedTopMsgs)

	return model.Snapshot{
		TotalLines:    a.TotalLines,
		ErrorCounts:   counts,
		History:       a.history.slice(),
		TopMessages:   topMsgsCopy,
		Trend:         a.trendRing.slice(),
		LastErrorTime: a.LastErrorTime,
		LastWarnTime:  a.LastWarnTime,
		AverageEPS:    a.AverageEPS,
		PeakEPS:       a.PeakEPS,
	}
}

func (a *Aggregator) GetHistory() []model.LogEvent {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.history.slice()
}

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
		return err
	}
	return json.Unmarshal(file, a)
}

func (a *Aggregator) getTopMessages(n int) []model.MessageStat {
	ss := make([]model.MessageStat, 0, len(a.MessageCounts))
	for _, v := range a.MessageCounts {
		ss = append(ss, v)
	}
	sort.Slice(ss, func(i, j int) bool {
		if ss[i].Count != ss[j].Count {
			return ss[i].Count > ss[j].Count
		}
		return ss[i].Level < ss[j].Level
	})
	if len(ss) > n {
		return ss[:n]
	}
	return ss
}

// PushTrend runs every second. It's the only place that sorts and computes averages,
// so Snapshot() and shouldTriggerReport() can read pre-baked values cheaply.
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

	currentEPS := float64(a.IncidentSecCount) / elapsed
	if currentEPS > a.PeakEPS {
		a.PeakEPS = currentEPS
	}

	// Push to trend ring — get evicted value for O(1) running sum update
	evicted, hadEviction := a.trendRing.push(int(currentEPS))
	if hadEviction {
		a.trendRunningSum -= float64(evicted)
	}
	a.trendRunningSum += currentEPS

	if a.trendRing.count > 0 {
		a.AverageEPS = a.trendRunningSum / float64(a.trendRing.count)
	}

	// Incident ring — no sum needed (used only for forensic dump)
	a.incidentRing.push(int(currentEPS))

	// Cache top messages once per second — Snapshot never sorts again
	a.cachedTopMsgs = a.getTopMessages(10)

	a.lastPush = now
	a.CurrentSecCount = 0
	a.IncidentSecCount = 0
}

func (a *Aggregator) GetRecentSnapshot() map[string]model.MessageStat {
	a.mu.Lock()
	defer a.mu.Unlock()

	recent := make(map[string]model.MessageStat, len(a.RecentMessages))
	for k, v := range a.RecentMessages {
		recent[k] = *v
	}
	a.RecentMessages = make(map[string]*model.MessageStat)
	return recent
}

func (a *Aggregator) FlushHourlyStats() (time.Time, map[string]int, string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	startTime := a.HourlyStartTime
	counts := a.HourlyCounts

	topMsg := "None"
	maxCount := 0

	for msg, entry := range a.TopMessages {
		if entry.Level == "ERROR" && entry.Count > maxCount {
			maxCount = entry.Count
			topMsg = "[ERROR] " + msg
		}
	}
	if topMsg == "None" {
		for msg, entry := range a.TopMessages {
			if entry.Level == "WARN" && entry.Count > maxCount {
				maxCount = entry.Count
				topMsg = "[WARN] " + msg
			}
		}
	}

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

	contextSize := 30000
	if a.history.count < contextSize {
		contextSize = a.history.count
	}
	fullContextCopy := a.history.slice()
	if len(fullContextCopy) > contextSize {
		fullContextCopy = fullContextCopy[len(fullContextCopy)-contextSize:]
	}

	signalHistoryCopy := a.signalHistory.slice()
	trendCopy := a.incidentRing.slice()
	peak := a.PeakEPS
	total := a.TotalLines

	a.mu.RUnlock()

	a.wg.Add(1)
	go func(full, signals []model.LogEvent, trends []int, r string, p float64, t int) {
		defer a.wg.Done()

		filename := fmt.Sprintf("incident_%s.json", time.Now().Format("2006-01-02_15-04-05"))
		f, err := os.Create(filename)
		if err != nil {
			return
		}
		defer f.Close()

		w := bufio.NewWriter(f)
		fmt.Fprintf(w, "{\n  \"reason\": %q,\n  \"peak_eps\": %.2f,\n  \"total_lines\": %d,\n", r, p, t)

		trendData, _ := json.Marshal(trends)
		fmt.Fprintf(w, "  \"hourly_trend\": %s,\n", string(trendData))

		w.WriteString("  \"signal_history\": [\n")
		for i, event := range signals {
			line, _ := json.Marshal(event)
			w.Write(line)
			if i < len(signals)-1 {
				w.WriteString(",\n")
			}
		}
		w.WriteString("\n  ],\n")

		w.WriteString("  \"full_context\": [\n")
		for i, event := range full {
			line, _ := json.Marshal(event)
			w.Write(line)
			if i < len(full)-1 {
				w.WriteString(",\n")
			}
		}
		w.WriteString("\n  ]\n}")
		w.Flush()
	}(fullContextCopy, signalHistoryCopy, trendCopy, reason, peak, total)
}

// shouldTriggerReport uses pre-computed AverageEPS — no inner loops, safe inside lock.
func (a *Aggregator) shouldTriggerReport(level string) bool {
	if time.Since(a.LastReportTime) < 5*time.Minute {
		return false
	}
	if level == "FATAL" || level == "CRITICAL" {
		return true
	}
	last, ok := a.trendRing.last()
	if !ok {
		return false
	}
	lastF := float64(last)
	return lastF > 10 && lastF > a.AverageEPS*3
}

func (a *Aggregator) Wait() {
	a.wg.Wait()
}
