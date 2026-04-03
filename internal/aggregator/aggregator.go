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

	// Trend rings — split by signal type so EPS and WPS are tracked independently.
	// errorTrendRing feeds AverageEPS/PeakEPS (red line on graph).
	// warnTrendRing  feeds AverageWPS/PeakWPS (yellow line on graph).
	errorTrendRing    ringInt // 60s error trend for display
	warnTrendRing     ringInt // 60s warn trend for display
	errorIncidentRing ringInt // 3600s error forensic — full hour for incident reports
	warnIncidentRing  ringInt // 3600s warn forensic — full hour to catch slow buildups

	// Pre-computed values updated in PushTrend() (under lock, once/sec).
	// Snapshot() and shouldTriggerReport() read these cheaply without sorting.
	AverageEPS    float64
	PeakEPS       float64
	AverageWPS    float64
	PeakWPS       float64
	cachedTopMsgs []model.MessageStat // recomputed in PushTrend, served from Snapshot

	// Incremental running sums for O(1) average calculation.
	// Updated in PushTrend() when a value is evicted from the ring.
	errorTrendSum float64
	warnTrendSum  float64

	// Per-second counters — reset in PushTrend() after being pushed to the rings.
	ErrorSecCount    int // errors seen since last PushTrend tick
	WarnSecCount     int // warns seen since last PushTrend tick
	IncidentSecCount int // combined, used only for incident trigger check
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

	// Latest AI analysis text — set by the AI observer goroutine in main.go,
	// read by WebSnapshot() to send to the browser.
	aiAnalysis string
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
		HourlyStartTime:   time.Now(),
		history:           newRingEvent(maxHistory),
		signalHistory:     newRingEvent(maxSignalHistory),
		errorTrendRing:    newRingInt(maxTrend),
		warnTrendRing:     newRingInt(maxTrend),
		errorIncidentRing: newRingInt(maxIncidentTrend),
		warnIncidentRing:  newRingInt(maxIncidentTrend),
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
		// Increment the error-specific counter for PushTrend
		a.ErrorSecCount++
		a.IncidentSecCount++
	} else if event.Level == "WARN" {
		a.LastWarnTime = event.Timestamp
		// Increment the warn-specific counter for PushTrend
		a.WarnSecCount++
		a.IncidentSecCount++
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
					Level:             event.Level,
					VariantCounts:     make(map[string]int),
					VariantTimestamps: make(map[string]*model.VariantTimestamps),
				}
			}
			// Ensure VariantTimestamps map exists on older loaded stats
			if stat.VariantTimestamps == nil {
				stat.VariantTimestamps = make(map[string]*model.VariantTimestamps)
			}
			stat.Count++
			stat.VariantCounts[event.Message]++

			// Update per-variant timestamp ring
			vt, ok := stat.VariantTimestamps[event.Message]
			if !ok {
				vt = &model.VariantTimestamps{}
				stat.VariantTimestamps[event.Message] = vt
			}
			vt.Push(event.Timestamp)

			// Update cluster-level timestamp ring
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

// Snapshot returns a point-in-time copy of all data for the TUI.
// No sort, no heavy work — all values are pre-computed by PushTrend().
func (a *Aggregator) Snapshot() model.Snapshot {
	a.mu.RLock()
	defer a.mu.RUnlock()

	counts := make(map[string]int, len(a.ErrorCounts))
	for k, v := range a.ErrorCounts {
		counts[k] = v
	}

	topMsgsCopy := make([]model.MessageStat, len(a.cachedTopMsgs))
	copy(topMsgsCopy, a.cachedTopMsgs)

	return model.Snapshot{
		TotalLines:    a.TotalLines,
		ErrorCounts:   counts,
		History:       a.history.slice(),
		TopMessages:   topMsgsCopy,
		TrendError:    a.errorTrendRing.slice(),
		TrendWarn:     a.warnTrendRing.slice(),
		LastErrorTime: a.LastErrorTime,
		LastWarnTime:  a.LastWarnTime,
		AverageEPS:    a.AverageEPS,
		PeakEPS:       a.PeakEPS,
		AverageWPS:    a.AverageWPS,
		PeakWPS:       a.PeakWPS,
	}
}

// WebSnapshot returns a bandwidth-safe snapshot for WebSocket clients.
// Mirrors Snapshot() in cost — all values are pre-computed by PushTrend().
// SampleLogs is capped at 50 events to avoid overwhelming the browser.
func (a *Aggregator) WebSnapshot() model.WebSnapshot {
	a.mu.RLock()
	defer a.mu.RUnlock()

	counts := make(map[string]int, len(a.ErrorCounts))
	for k, v := range a.ErrorCounts {
		counts[k] = v
	}

	// Convert cachedTopMsgs to WebMessageStat — strip timestamp arrays,
	// keep only what the browser needs to render the top errors panel.
	// PatternKey is the fingerprint map key used by /api/inspect for lookup.
	topErrors := make([]model.WebMessageStat, len(a.cachedTopMsgs))
	for i, m := range a.cachedTopMsgs {
		topErrors[i] = model.WebMessageStat{
			Pattern:    a.getTopVariant(m),
			PatternKey: a.getPatternKey(m),
			Level:      m.Level,
			Count:      m.Count,
		}
	}

	// Cap history to last 50 events for the sample log stream.
	// history.slice() returns oldest→newest so we take the tail.
	history := a.history.slice()
	const maxSampleLogs = 50
	if len(history) > maxSampleLogs {
		history = history[len(history)-maxSampleLogs:]
	}

	return model.WebSnapshot{
		TotalLines:    a.TotalLines,
		ErrorCounts:   counts,
		AverageEPS:    a.AverageEPS,
		PeakEPS:       a.PeakEPS,
		AverageWPS:    a.AverageWPS,
		PeakWPS:       a.PeakWPS,
		TrendError:    a.errorTrendRing.slice(),
		TrendWarn:     a.warnTrendRing.slice(),
		TopErrors:     topErrors,
		SampleLogs:    history,
		LastErrorTime: a.LastErrorTime,
		LastWarnTime:  a.LastWarnTime,
		AIAnalysis:    a.aiAnalysis,
	}
}

// getTopVariant returns the most frequently seen raw message for a given
// MessageStat — used as the human-readable label in the web UI.
// Called inside RLock, so no additional locking needed.
func (a *Aggregator) getTopVariant(m model.MessageStat) string {
	best := ""
	bestCount := 0
	for msg, count := range m.VariantCounts {
		if count > bestCount {
			bestCount = count
			best = msg
		}
	}
	return best
}

// getPatternKey finds the fingerprint map key for a given MessageStat.
// We match by checking if any of the stat's variant messages fingerprint
// to the same key — this is the most reliable way to reverse-lookup the key.
// Called inside RLock, so no additional locking needed.
func (a *Aggregator) getPatternKey(m model.MessageStat) string {
	// Take any variant message from this stat and fingerprint it —
	// the result should be the map key for this cluster
	for msg := range m.VariantCounts {
		fp := fingerprint(msg)
		if _, exists := a.MessageCounts[fp]; exists {
			return fp
		}
	}
	// Fallback: match by count + level (less reliable but better than nothing)
	for k, v := range a.MessageCounts {
		if v.Count == m.Count && v.Level == m.Level {
			return k
		}
	}
	return ""
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

// PushTrend runs every second. It is the only place that sorts and computes
// averages, so Snapshot() and shouldTriggerReport() read pre-baked values cheaply.
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

	// --- Error trend ---
	currentEPS := float64(a.ErrorSecCount) / elapsed
	if currentEPS > a.PeakEPS {
		a.PeakEPS = currentEPS
	}
	evicted, hadEviction := a.errorTrendRing.push(int(currentEPS))
	if hadEviction {
		a.errorTrendSum -= float64(evicted)
	}
	a.errorTrendSum += currentEPS
	if a.errorTrendRing.count > 0 {
		a.AverageEPS = a.errorTrendSum / float64(a.errorTrendRing.count)
	}

	// --- Warn trend ---
	currentWPS := float64(a.WarnSecCount) / elapsed
	if currentWPS > a.PeakWPS {
		a.PeakWPS = currentWPS
	}
	evictedW, hadEvictionW := a.warnTrendRing.push(int(currentWPS))
	if hadEvictionW {
		a.warnTrendSum -= float64(evictedW)
	}
	a.warnTrendSum += currentWPS
	if a.warnTrendRing.count > 0 {
		a.AverageWPS = a.warnTrendSum / float64(a.warnTrendRing.count)
	}

	// --- Incident rings (full hour, split — used only for forensic dump) ---
	// Keeping them separate means incident reports can show whether it was
	// an error spike, a warn buildup, or both — critical for root cause analysis.
	a.errorIncidentRing.push(int(currentEPS))
	a.warnIncidentRing.push(int(currentWPS))

	// Cache top messages once per second — Snapshot() never sorts again
	a.cachedTopMsgs = a.getTopMessages(10)

	a.lastPush = now
	a.ErrorSecCount = 0
	a.WarnSecCount = 0
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

	// Copy both incident rings separately so the report accurately reflects
	// whether the incident was driven by errors, warns, or a combination.
	errorTrendCopy := a.errorIncidentRing.slice()
	warnTrendCopy := a.warnIncidentRing.slice()

	peakEPS := a.PeakEPS
	peakWPS := a.PeakWPS
	total := a.TotalLines

	a.mu.RUnlock()

	a.wg.Add(1)
	go func(full, signals []model.LogEvent, errTrend, warnTrend []int, r string, pEPS, pWPS float64, t int) {
		defer a.wg.Done()

		filename := fmt.Sprintf("incident_%s.json", time.Now().Format("2006-01-02_15-04-05"))
		f, err := os.Create(filename)
		if err != nil {
			return
		}
		defer f.Close()

		w := bufio.NewWriter(f)

		// Header — split peak values so it's immediately clear what spiked
		fmt.Fprintf(w, "{\n  \"reason\": %q,\n  \"peak_eps\": %.2f,\n  \"peak_wps\": %.2f,\n  \"total_lines\": %d,\n",
			r, pEPS, pWPS, t)

		// Separate trend arrays — lets you see the warn buildup that preceded
		// the error spike, which is often the most valuable forensic signal.
		errTrendData, _ := json.Marshal(errTrend)
		warnTrendData, _ := json.Marshal(warnTrend)
		fmt.Fprintf(w, "  \"hourly_trend_errors\": %s,\n", string(errTrendData))
		fmt.Fprintf(w, "  \"hourly_trend_warns\": %s,\n", string(warnTrendData))

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
	}(fullContextCopy, signalHistoryCopy, errorTrendCopy, warnTrendCopy, reason, peakEPS, peakWPS, total)
}

// shouldTriggerReport uses pre-computed AverageEPS — no inner loops, safe inside lock.
func (a *Aggregator) shouldTriggerReport(level string) bool {
	if time.Since(a.LastReportTime) < 5*time.Minute {
		return false
	}
	if level == "FATAL" || level == "CRITICAL" {
		return true
	}
	last, ok := a.errorIncidentRing.last()
	if !ok {
		return false
	}
	lastF := float64(last)
	return lastF > 10 && lastF > a.AverageEPS*3
}

// GetInspect returns the full detail for a single pattern key.
// Called on demand when a user clicks a top error/warn row in the web UI.
// Returns nil if the pattern is not found.
// Each variant includes its own timestamp ring so users can drill down
// into exactly when each specific IP/user/key was hitting.
func (a *Aggregator) GetInspect(pattern string) *model.InspectResult {
	a.mu.RLock()
	defer a.mu.RUnlock()

	stat, ok := a.MessageCounts[pattern]
	if !ok {
		return nil
	}

	// Build per-variant detail — count + newest-first timestamps
	variants := make(map[string]*model.VariantDetail, len(stat.VariantCounts))
	for msg, count := range stat.VariantCounts {
		detail := &model.VariantDetail{
			Count: count,
		}

		// Get per-variant timestamps if available
		if vt, ok := stat.VariantTimestamps[msg]; ok {
			ordered := vt.Slice() // oldest→newest
			ts := make([]string, 0, len(ordered))
			for i := len(ordered) - 1; i >= 0; i-- {
				if !ordered[i].IsZero() {
					ts = append(ts, ordered[i].Format(time.RFC3339Nano))
				}
			}
			detail.Timestamps = ts
		}

		variants[msg] = detail
	}

	return &model.InspectResult{
		Pattern:  pattern,
		Level:    stat.Level,
		Count:    stat.Count,
		Variants: variants,
	}
}

// SetAIAnalysis stores the latest AI analysis for the web UI.
// Called by the AI observer goroutine every 30 seconds.
func (a *Aggregator) SetAIAnalysis(text string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.aiAnalysis = text
}

// GetAIAnalysis returns the latest AI analysis text.
// Used by WebSnapshot() — called inside RLock so no extra locking needed.
func (a *Aggregator) GetAIAnalysis() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.aiAnalysis
}

func (a *Aggregator) Wait() {
	a.wg.Wait()
}
