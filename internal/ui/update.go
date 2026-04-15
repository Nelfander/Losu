// update.go — real-time rendering for the LOSU TUI.
//
// Update() is called on every aggregator snapshot tick (every 500ms)
// and refreshes all panels: stats, graph, top errors, log stream, and
// AI insights title. It uses incremental appending for the log view to
// avoid rewriting the full buffer on every tick — this is what allows
// LOSU to handle 50M+ logs without symbol walls or memory spikes.
package ui

import (
	"fmt"
	"strings"

	"github.com/nelfander/losu/internal/model"
	"github.com/rivo/tview"
)

func (d *Dashboard) Update(snap model.Snapshot) {
	// --- Stats ---
	var statsStr strings.Builder
	statsStr.WriteString(fmt.Sprintf("[white]Total Logs Processed: [blue]%d\n\n", snap.TotalLines))
	statsStr.WriteString(fmt.Sprintf(" [red]ERROR : [white]%-6d    [yellow]WARN  : [white]%-6d\n",
		snap.ErrorCounts["ERROR"], snap.ErrorCounts["WARN"]))
	statsStr.WriteString(fmt.Sprintf(" [green]INFO  : [white]%-6d    [cyan]DEBUG : [white]%-6d\n",
		snap.ErrorCounts["INFO"], snap.ErrorCounts["DEBUG"]))

	// Source indicator — only shown when multiple files are being watched
	if len(d.AggKeys) > 1 && d.ActiveAgg != nil {
		activeName := ""
		for _, key := range d.AggKeys {
			if d.AggMap[key] == d.ActiveAgg {
				parts := strings.Split(strings.ReplaceAll(key, "\\", "/"), "/")
				activeName = parts[len(parts)-1]
				break
			}
		}
		statsStr.WriteString(fmt.Sprintf("\n [gray]Source: [cyan]%s", activeName))
		statsStr.WriteString(" [gray](←/→: switch file)")
	}
	d.StatsView.SetText(statsStr.String())

	// --- Graph ---
	sparkErrors := getSparklineLog(snap.TrendError, 5, "red")
	sparkWarns := getSparklineLog(snap.TrendWarn, 5, "yellow")

	var graphBody strings.Builder
	graphBody.WriteString(fmt.Sprintf("\n [white]Status: %s\n\n", getStatusLabel(snap.AverageEPS, snap.AverageWPS)))
	graphBody.WriteString(fmt.Sprintf(" [red]EPS [white]| Peak: [red]%.1f [white]Avg: [red]%.2f\n", snap.PeakEPS, snap.AverageEPS))
	graphBody.WriteString("\n" + sparkErrors + "\n")
	graphBody.WriteString(" [white]" + strings.Repeat("▔", 25) + "\n\n")
	graphBody.WriteString(fmt.Sprintf(" [yellow]WPS [white]| Peak: [yellow]%.1f [white]Avg: [yellow]%.2f\n", snap.PeakWPS, snap.AverageWPS))
	graphBody.WriteString("\n" + sparkWarns + "\n")
	graphBody.WriteString(" [white]" + strings.Repeat("▔", 25))
	d.GraphView.SetText(graphBody.String())

	// --- Top Errors/Warns ---
	var topStr strings.Builder
	topStr.WriteString("\n")
	d.StatLookup = nil

	for i, item := range snap.TopMessages {
		bestMsg := ""
		maxSubCount := -1
		for msg, count := range item.VariantCounts {
			if count > maxSubCount {
				maxSubCount = count
				bestMsg = msg
			} else if count == maxSubCount && msg < bestMsg {
				bestMsg = msg
			}
		}
		d.StatLookup = append(d.StatLookup, item)
		color := "red"
		if item.Level == "WARN" {
			color = "yellow"
		}
		topStr.WriteString(fmt.Sprintf(" [\"top_%d\"][%s]%5d [white]| %-90s[\"\"]\n",
			i, color, item.Count, truncate(bestMsg, 90)))
	}
	d.TopErrorsView.SetText(topStr.String())

	// --- Log updates ---
	// Incremental append — never rewrite the full buffer on every tick.
	filterChanged := d.SearchFilter != d.LastSearchFilter
	historyFull := len(snap.History) >= 50000

	if filterChanged || historyFull {
		d.FilteredLogs = d.FilteredLogs[:0]
		d.LastHistoryLen = 0
		d.LastSearchFilter = d.SearchFilter
		d.LogView.Clear()
		d.isAutoScroll = true
	}

	if len(snap.History) > d.LastHistoryLen {
		var uiBatch strings.Builder
		filterLower := strings.ToLower(d.SearchFilter)

		for i := d.LastHistoryLen; i < len(snap.History); i++ {
			event := snap.History[i]
			match := filterLower == "" ||
				strings.Contains(strings.ToLower(event.Message), filterLower) ||
				strings.Contains(strings.ToLower(event.Level), filterLower)

			if match {
				line := fmt.Sprintf("[%s][%s] %-5s -> [-]%s\n",
					d.getColor(event.Level),
					event.Timestamp.Format("15:04:05"),
					event.Level,
					tview.Escape(event.Message))
				uiBatch.WriteString(line)
				d.FilteredLogs = append(d.FilteredLogs, line)
			}
		}

		if uiBatch.Len() > 0 {
			fmt.Fprint(d.LogView, uiBatch.String())
			if d.isAutoScroll && !d.isDragging {
				d.LogView.ScrollToEnd()
			}
		}
		d.LastHistoryLen = len(snap.History)
	}

	// Hard trim to prevent buffer explosion
	const maxVisibleLines = 1500
	if len(d.FilteredLogs) > maxVisibleLines+500 {
		start := len(d.FilteredLogs) - maxVisibleLines
		if start < 0 {
			start = 0
		}
		d.LogView.Clear()
		for _, line := range d.FilteredLogs[start:] {
			fmt.Fprint(d.LogView, line)
		}
		d.FilteredLogs = d.FilteredLogs[start:]
	}

	// Scroll feedback in log title
	matchCount := len(d.FilteredLogs)
	offset, _ := d.LogView.GetScrollOffset()
	_, _, _, rectHeight := d.LogView.GetInnerRect()

	if matchCount > rectHeight {
		maxScroll := matchCount - rectHeight
		if maxScroll <= 0 {
			maxScroll = 1
		}
		percent := (float64(offset) / float64(maxScroll)) * 100
		if percent > 100 {
			percent = 100
		}
		d.LogView.SetTitle(fmt.Sprintf(" [green]Latest Logs (%d total) [white]| Click and drag right side or use mouse wheel to scroll: %d%% ", matchCount, int(percent)))
	} else {
		d.LogView.SetTitle(fmt.Sprintf(" [green]Latest Logs (%d total) [white]| TOP ", matchCount))
	}

	// AI view title
	lastError := "None"
	if !snap.LastErrorTime.IsZero() {
		lastError = snap.LastErrorTime.Format("15:04:05")
	}
	lastWarn := "None"
	if !snap.LastWarnTime.IsZero() {
		lastWarn = snap.LastWarnTime.Format("15:04:05")
	}
	d.AIView.SetTitle(fmt.Sprintf(" [purple]AI Insights | [red]Last Err: %s [yellow]Last Warn: %s ", lastError, lastWarn))

	// Final render — rebuild full log buffer from FilteredLogs cache
	d.LogView.Clear()
	d.renderBuf.Reset()
	if d.renderBuf.Cap() < 150000 {
		d.renderBuf.Grow(150000)
	}
	for _, line := range d.FilteredLogs {
		d.renderBuf.WriteString(line)
	}
	d.LogView.SetText(d.renderBuf.String())
	if d.isAutoScroll && !d.isDragging {
		d.LogView.ScrollToEnd()
	}
}
