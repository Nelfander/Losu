package aggregator

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
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

// fingerprint replaces dynamic numeric values with * for clustering.
// Only digits that follow a separator (=, :, ", space, [, ], /, -).
// are replaced — digits embedded in names like S3, md5, HTTP2, v2 are preserved.
// This prevents product names and version strings from being mangled.
//
// Examples:
//
//	"S3 upload failed | key=img_164"  → "S3 upload failed | key=img_*"
//	"duration=451"                     → "duration=*"
//	"ip=192.168.1.54"                  → "ip=*.*.*.*"
//	"HTTP2 request"                    → "HTTP2 request"  (digit after letter → kept)
//	"user_id=503"                      → "user_id=*"      (_ is separator)
func fingerprint(msg string) string {
	if len(msg) == 0 {
		return ""
	}
	b := builderPool.Get().(*strings.Builder)
	b.Reset()
	b.Grow(len(msg))

	inToken := false
	inHexToken := false    // true only after 0x prefix — prevents "bytes" → "*ytes"
	afterSeparator := true // treat start of string as after separator

	for i := 0; i < len(msg); i++ {
		c := msg[i]

		isDigit := c >= '0' && c <= '9'
		isLetter := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		isHex := (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
		// _ is a separator: img_340 → img_* not img_340
		isSeparator := c == ' ' || c == '=' || c == '[' || c == ']' ||
			c == ':' || c == '"' || c == '/' || c == '-' || c == '.' || c == '_'

		if isDigit {
			if inToken {
				// Continue existing token — skip the digit
				continue
			}
			if afterSeparator {
				// Special case: 0x... hex prefix — write "0x" then start token
				if i+1 < len(msg) && (msg[i+1] == 'x' || msg[i+1] == 'X') {
					b.WriteByte(c)        // write '0'
					b.WriteByte(msg[i+1]) // write 'x'
					b.WriteByte('*')      // write placeholder for the hex value
					i++                   // skip 'x' in next iteration
					inToken = true
					inHexToken = true // only consume hex chars when explicitly 0x-prefixed
					continue
				}
				b.WriteByte('*')
				inToken = true
				inHexToken = false
				continue
			}
			// Digit after a letter → part of a name (S3, md5, v2) → keep it
			b.WriteByte(c)
			continue
		}

		if inToken && inHexToken && isHex {
			// Only consume hex chars when we explicitly started with 0x prefix
			// — prevents "bytes", "deadbeef" etc from being consumed as hex
			continue
		}

		// Not a digit — reset token state
		inToken = false
		inHexToken = false
		afterSeparator = isSeparator || isLetter == false && !isDigit && !isLetter

		if isSeparator {
			afterSeparator = true
		} else if isLetter {
			afterSeparator = false
		}

		b.WriteByte(c)
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
	errorTrendRing    ringInt
	warnTrendRing     ringInt
	errorIncidentRing ringInt
	warnIncidentRing  ringInt

	// Pre-computed values updated in PushTrend()
	AverageEPS    float64
	PeakEPS       float64
	AverageWPS    float64
	PeakWPS       float64
	cachedTopMsgs []model.MessageStat

	// Incremental running sums for O(1) average calculation.
	errorTrendSum float64
	warnTrendSum  float64

	// Per-second counters — reset in PushTrend()
	ErrorSecCount    int
	WarnSecCount     int
	IncidentSecCount int
	lastPush         time.Time

	// Timestamps
	StartTime      time.Time
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

	aiAnalysis string
	Source     string // log file path this aggregator watches
}

// NewAggregatorForSource creates an aggregator tagged with the log file path it watches.
// The source path is written into incident reports so they can be filtered per file.
func NewAggregatorForSource(path string) *Aggregator {
	a := NewAggregator()
	a.Source = path
	return a
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
		StartTime:         time.Now(),
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

func (a *Aggregator) Update(event model.LogEvent, minWeight int, weights map[string]int) {
	if event.Level == "IGNORE" {
		return
	}

	isSignal := event.Level == "ERROR" || event.Level == "WARN"

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
		a.ErrorSecCount++
		a.IncidentSecCount++
	} else if event.Level == "WARN" {
		a.LastWarnTime = event.Timestamp
		a.WarnSecCount++
		a.IncidentSecCount++
	}

	if isSignal {
		entry := a.TopMessages[event.Message]
		entry.Count++
		entry.Level = event.Level
		a.TopMessages[event.Message] = entry

		a.signalHistory.push(event)

		if a.shouldTriggerReport(event.Level) {
			windowStart := a.LastReportTime
			triggerTime := time.Now()
			a.LastReportTime = triggerTime
			go a.TriggerIncidentReport(
				fmt.Sprintf("Anomaly: %s Spike Detected", event.Level),
				windowStart,
				triggerTime,
			)
		}

		if event.Message != a.lastMsg {
			a.lastMsg = event.Message
			a.lastPattern = pattern
		} else {
			pattern = a.lastPattern
		}

		if _, exists := a.MessageCounts[pattern]; !exists && len(a.MessageCounts) > 10000 {
		} else {
			stat := a.MessageCounts[pattern]
			if stat.VariantCounts == nil {
				stat = model.MessageStat{
					Level:             event.Level,
					VariantCounts:     make(map[string]int),
					VariantTimestamps: make(map[string]*model.VariantTimestamps),
				}
			}
			if stat.VariantTimestamps == nil {
				stat.VariantTimestamps = make(map[string]*model.VariantTimestamps)
			}
			stat.Count++
			stat.VariantCounts[event.Message]++

			// Cap timestamp tracking at 50 variants per pattern.
			// Counts still accumulate for all variants — full observability.
			// Only the timestamp ring (used for hit timeline in inspector) is capped
			// to prevent unbounded heap growth at high throughput.
			vt, ok := stat.VariantTimestamps[event.Message]
			if !ok && len(stat.VariantTimestamps) < 50 {
				vt = &model.VariantTimestamps{}
				stat.VariantTimestamps[event.Message] = vt
			}
			if vt != nil {
				vt.Push(event.Timestamp)
			}

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

	if weights[event.Level] >= minWeight {
		a.history.push(event)
	}
}

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

func (a *Aggregator) WebSnapshot() model.WebSnapshot {
	a.mu.RLock()
	defer a.mu.RUnlock()

	counts := make(map[string]int, len(a.ErrorCounts))
	for k, v := range a.ErrorCounts {
		counts[k] = v
	}

	topErrors := make([]model.WebMessageStat, len(a.cachedTopMsgs))
	for i, m := range a.cachedTopMsgs {
		topErrors[i] = model.WebMessageStat{
			Pattern:    a.getPatternKey(m),
			PatternKey: a.getPatternKey(m),
			Level:      m.Level,
			Count:      m.Count,
		}
	}

	history := a.history.slice()
	const maxSampleLogs = 250
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

func (a *Aggregator) getPatternKey(m model.MessageStat) string {
	for msg := range m.VariantCounts {
		fp := fingerprint(msg)
		if _, exists := a.MessageCounts[fp]; exists {
			return fp
		}
	}
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

func (a *Aggregator) SearchHistory(q, level string, limit int) []model.LogEvent {
	a.mu.RLock()
	defer a.mu.RUnlock()

	all := a.history.slice()
	qLower := strings.ToLower(q)
	levelUpper := strings.ToUpper(level)
	results := make([]model.LogEvent, 0, 256)

	for i := len(all) - 1; i >= 0; i-- {
		e := all[i]
		if levelUpper != "" && strings.ToUpper(e.Level) != levelUpper {
			continue
		}
		if qLower != "" && !strings.Contains(strings.ToLower(e.Message), qLower) {
			continue
		}
		results = append(results, e)
		if limit > 0 && len(results) >= limit {
			break
		}
	}

	return results
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

// getTopMessages returns all clustered error/warn patterns sorted by:
//  1. Level priority — ERROR always before WARN regardless of count
//  2. Count descending within each level — most frequent first
//
// No cap — all patterns are returned. The 10,000-pattern guard in Update()
// is the only memory limit. Both TUI and web scroll to show everything.
func (a *Aggregator) getTopMessages() []model.MessageStat {
	var ss []model.MessageStat
	// Collect pattern keys so we have a stable tiebreaker
	type statWithKey struct {
		stat model.MessageStat
		key  string
	}
	keyed := make([]statWithKey, 0, len(a.MessageCounts))
	for k, v := range a.MessageCounts {
		keyed = append(keyed, statWithKey{stat: v, key: k})
	}
	sort.Slice(keyed, func(i, j int) bool {
		si, sj := keyed[i].stat, keyed[j].stat
		// 1. ERROR always before WARN regardless of count
		if si.Level != sj.Level {
			return si.Level == "ERROR"
		}
		// 2. Higher count first
		if si.Count != sj.Count {
			return si.Count > sj.Count
		}
		// 3. Stable tiebreaker — alphabetical by pattern key
		// Prevents flickering when two patterns have identical level + count
		return keyed[i].key < keyed[j].key
	})
	ss = make([]model.MessageStat, len(keyed))
	for i, k := range keyed {
		ss[i] = k.stat
	}
	return ss
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

	a.errorIncidentRing.push(int(currentEPS))
	a.warnIncidentRing.push(int(currentWPS))

	epsMin := 1.0
	if v := os.Getenv("LOSU_EPS_WARN"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			epsMin = f
		}
	}
	if !a.LastReportTime.IsZero() && a.AverageEPS < epsMin && a.AverageWPS < epsMin {
		a.LastReportTime = time.Time{}
	}

	a.cachedTopMsgs = a.getTopMessages()

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

func (a *Aggregator) TriggerIncidentReport(reason string, windowStart, triggerTime time.Time) {
	a.mu.RLock()

	contextSize := 30000
	if a.history.count < contextSize {
		contextSize = a.history.count
	}
	fullContextCopy := a.history.slice()
	if len(fullContextCopy) > contextSize {
		fullContextCopy = fullContextCopy[len(fullContextCopy)-contextSize:]
	}

	allSignals := a.signalHistory.slice()
	var signalHistoryCopy []model.LogEvent
	for _, e := range allSignals {
		if windowStart.IsZero() || e.Timestamp.After(windowStart) {
			signalHistoryCopy = append(signalHistoryCopy, e)
		}
	}

	errorTrendCopy := a.errorIncidentRing.slice()
	warnTrendCopy := a.warnIncidentRing.slice()

	peakEPS := a.PeakEPS
	peakWPS := a.PeakWPS
	total := a.TotalLines

	a.mu.RUnlock()

	a.wg.Add(1)
	go func(full, signals []model.LogEvent, errTrend, warnTrend []int, r string, pEPS, pWPS float64, t int) {
		defer a.wg.Done()

		if err := os.MkdirAll("incidents", 0755); err != nil {
			return
		}
		filename := fmt.Sprintf("incidents/incident_%s.json", time.Now().Format("2006-01-02_15-04-05"))
		f, err := os.Create(filename)
		if err != nil {
			return
		}
		defer f.Close()

		w := bufio.NewWriter(f)

		effectiveStart := windowStart
		if effectiveStart.IsZero() {
			effectiveStart = a.StartTime
		}
		startedAt := effectiveStart.Format(time.RFC3339)
		endedAt := triggerTime.Format(time.RFC3339)

		fmt.Fprintf(w, "{\n  \"reason\": %q,\n  \"source\": %q,\n  \"peak_eps\": %.2f,\n  \"peak_wps\": %.2f,\n  \"total_lines\": %d,\n  \"started_at\": %q,\n  \"ended_at\": %q,\n",
			r, a.Source, pEPS, pWPS, t, startedAt, endedAt)

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

func (a *Aggregator) shouldTriggerReport(level string) bool {
	epsCritical := 5.0
	if v := os.Getenv("LOSU_EPS_CRITICAL"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			epsCritical = f
		}
	}
	if a.AverageEPS >= epsCritical || level == "FATAL" {
		return a.LastReportTime.IsZero() || time.Since(a.LastReportTime) >= 1*time.Minute
	}

	if !a.LastReportTime.IsZero() && time.Since(a.LastReportTime) < 5*time.Minute {
		return false
	}

	last, ok := a.errorIncidentRing.last()
	if !ok {
		return false
	}

	epsMinimum := 1.0
	if v := os.Getenv("LOSU_EPS_WARN"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			epsMinimum = f
		}
	}

	lastF := float64(last)
	return lastF >= epsMinimum && lastF > a.AverageEPS*3
}

func (a *Aggregator) GetInspect(pattern string) *model.InspectResult {
	a.mu.RLock()
	defer a.mu.RUnlock()

	stat, ok := a.MessageCounts[pattern]
	if !ok {
		return nil
	}

	variants := make(map[string]*model.VariantDetail, len(stat.VariantCounts))
	for msg, count := range stat.VariantCounts {
		detail := &model.VariantDetail{
			Count: count,
		}

		if vt, ok := stat.VariantTimestamps[msg]; ok {
			ordered := vt.Slice()
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

func (a *Aggregator) SetAIAnalysis(text string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.aiAnalysis = text
}

func (a *Aggregator) GetAIAnalysis() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.aiAnalysis
}

func (a *Aggregator) Wait() {
	a.wg.Wait()
}
