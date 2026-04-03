package ui

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/nelfander/losu/internal/model"
	"github.com/rivo/tview"
)

type Dashboard struct {
	App              *tview.Application
	StatsView        *tview.TextView
	TopErrorsView    *tview.TextView
	LogView          *tview.TextView
	GraphView        *tview.TextView
	AIView           *tview.TextView
	SearchInput      *tview.InputField
	SearchFilter     string
	MainLayout       *tview.Flex
	Pages            *tview.Pages
	StatLookup       []model.MessageStat
	LastHistoryLen   int
	LastSearchFilter string
	FilteredLogs     []string // Cache of logs that matched the current filter
	isDragging       bool
	isAutoScroll     bool
	renderBuf        strings.Builder
}

func NewDashboard() *Dashboard {
	app := tview.NewApplication()

	stats := tview.NewTextView()
	topErrors := tview.NewTextView()
	logs := tview.NewTextView()
	graph := tview.NewTextView()
	ai := tview.NewTextView()

	stats.SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter).
		SetBorder(true).
		SetTitle(" [yellow]Stats Breakdown ")

	topErrors.SetDynamicColors(true).
		SetRegions(true).
		SetBorder(true).
		SetTitle(" [red]Top 10 Error/Warn Messages ")

	topErrors.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyTab {
			app.SetFocus(logs)
		}
	})

	logs.SetDynamicColors(true).
		SetScrollable(true).
		SetMaxLines(5000).
		SetRegions(true).
		SetWordWrap(false).
		SetBorder(true).
		SetTitle(" [green]Latest Logs (Real-time) ")

	ai.SetDynamicColors(true).
		SetWordWrap(true).
		SetBorder(true).
		SetTitle(" [purple]AI Observer Insights ")
	ai.SetText("[gray]Gathering data for initial analysis...")

	graph.SetDynamicColors(true).
		SetBorder(true).
		SetTitle(" [cyan]Error/Warn Graph (60s) ")

	searchInput := tview.NewInputField().
		SetLabel(" 🔍 Search: ").
		SetPlaceholder(" Click to type filters for Latest Logs box...").
		SetFieldBackgroundColor(tcell.ColorBlack).
		SetFieldTextColor(tcell.ColorWhite)

	searchInput.SetBorder(true).
		SetTitle(" [white][ Filter Panel ] ").
		SetTitleAlign(tview.AlignLeft)

	dashboard := &Dashboard{
		App:              app,
		StatsView:        stats,
		TopErrorsView:    topErrors,
		LogView:          logs,
		GraphView:        graph,
		AIView:           ai,
		SearchInput:      searchInput,
		SearchFilter:     "",
		Pages:            nil,
		LastHistoryLen:   0,
		LastSearchFilter: "",
		FilteredLogs:     []string{},
	}

	searchInput.SetChangedFunc(func(text string) {
		dashboard.SearchFilter = text
	})

	dashboard.SearchInput = searchInput

	// --- DRAGGABLE LOGIC: Latest Logs ---
	logs.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		x, y := event.Position()
		rectX, rectY, rectWidth, rectHeight := logs.GetInnerRect()
		scrollbarX := rectX + rectWidth - 1
		leftPressed := event.Buttons()&tcell.Button1 != 0

		if leftPressed {
			if x >= scrollbarX-1 || dashboard.isDragging {
				dashboard.isDragging = true
				relativeY := float64(y - rectY)
				percentage := relativeY / float64(rectHeight)
				if percentage < 0 {
					percentage = 0
				}
				if percentage > 1 {
					percentage = 1
				}
				totalLines := len(dashboard.FilteredLogs)
				targetLine := int(percentage * float64(totalLines))
				logs.ScrollTo(targetLine, 0)
				dashboard.isAutoScroll = (percentage >= 0.95)
				return action, nil
			}
		} else {
			dashboard.isDragging = false
		}
		return action, event
	})

	// --- DRAGGABLE LOGIC: Top 10 Errors/Warns ---
	var isDraggingTop bool

	topErrors.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		x, y := event.Position()
		rectX, rectY, rectWidth, rectHeight := topErrors.GetInnerRect()
		scrollbarX := rectX + rectWidth - 1
		leftPressed := event.Buttons()&tcell.Button1 != 0

		if leftPressed {
			if x >= scrollbarX-1 || isDraggingTop {
				isDraggingTop = true
				relativeY := float64(y - rectY)
				percentage := relativeY / float64(rectHeight)
				if percentage < 0 {
					percentage = 0
				}
				if percentage > 1 {
					percentage = 1
				}
				totalLines := strings.Count(topErrors.GetText(false), "\n")
				targetLine := int(percentage * float64(totalLines))
				topErrors.ScrollTo(targetLine, 0)
				return action, nil
			}
		} else {
			isDraggingTop = false
		}
		return action, event
	})

	logs.SetMaxLines(2000)
	logs.SetChangedFunc(func() {
		dashboard.App.Draw()
	})

	header := tview.NewFlex().
		AddItem(stats, 0, 1, false).
		AddItem(topErrors, 0, 2, false)

	logContainer := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(logs, 15, 1, false).
		AddItem(ai, 0, 1, false)

	rightSide := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(graph, 0, 1, false).
		AddItem(searchInput, 5, 1, false)

	body := tview.NewFlex().
		AddItem(logContainer, 0, 2, false).
		AddItem(rightSide, 55, 1, false)

	flex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(header, 8, 1, false).
		AddItem(body, 0, 1, false)

	app.EnableMouse(true)

	pages := tview.NewPages()
	pages.AddPage("dashboard", flex, true, true)

	dashboard.MainLayout = flex
	dashboard.Pages = pages

	app.SetFocus(flex)

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyCtrlC {
			app.Stop()
			return nil
		}
		return event
	})

	searchInput.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEscape {
			searchInput.SetText("")
			dashboard.SearchFilter = ""
		}
	})

	// Top 10 error/warn clickable popup logic
	topErrors.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEnter {
			highlights := topErrors.GetHighlights()
			if len(highlights) > 0 {
				var index int
				fmt.Sscanf(highlights[0], "top_%d", &index)

				if index >= 0 && index < len(dashboard.StatLookup) {
					stat := dashboard.StatLookup[index]
					levelColor := dashboard.getColor(stat.Level)

					bestMsg := ""
					max := -1
					for msg, count := range stat.VariantCounts {
						if count > max {
							max = count
							bestMsg = msg
						}
					}

					var sb strings.Builder
					sb.WriteString(fmt.Sprintf("[%s]Log Level: [%s]%s\n", levelColor, levelColor, stat.Level))
					sb.WriteString(fmt.Sprintf("Message : [%s]%s\n", levelColor, tview.Escape(bestMsg)))
					sb.WriteString(fmt.Sprintf("[%s]Total Occurrences: [%s]%d\n", levelColor, levelColor, stat.Count))
					sb.WriteString("[gray]" + strings.Repeat("━", 64) + "\n")

					sb.WriteString("\n[cyan]🕒 Recent Timeline (Last 100):\n")
					orderedTimestamps := stat.GetSortedTimestamps()

					for i := len(orderedTimestamps) - 1; i >= 0; i-- {
						ts := orderedTimestamps[i]
						timeDiff := time.Since(ts).Truncate(time.Second)
						diffStr := timeDiff.String()
						if timeDiff < time.Second {
							diffStr = "< 1s"
						}
						sb.WriteString(fmt.Sprintf(" [white]%s [gray](%s ago)\n",
							ts.Format("15:04:05.000"), diffStr))
					}

					sb.WriteString("\n[yellow]📝 Unique Variations — click variant for its timeline:\n")
					// Sort variants by count descending for consistent display
					type varEntry struct {
						msg   string
						count int
					}
					varList := make([]varEntry, 0, len(stat.VariantCounts))
					for msg, count := range stat.VariantCounts {
						varList = append(varList, varEntry{msg, count})
					}
					sort.Slice(varList, func(i, j int) bool {
						return varList[i].count > varList[j].count
					})

					for _, ve := range varList {
						// Show most recent hit time per variant if available
						recentStr := ""
						if stat.VariantTimestamps != nil {
							if vt, ok := stat.VariantTimestamps[ve.msg]; ok {
								ordered := vt.Slice()
								if len(ordered) > 0 {
									latest := ordered[len(ordered)-1]
									recentStr = fmt.Sprintf(" [gray](last: %s)", latest.Format("15:04:05.000"))
								}
							}
						}
						sb.WriteString(fmt.Sprintf(" [white](%d hits)%s %s\n",
							ve.count, recentStr, tview.Escape(ve.msg)))
					}

					dashboard.ShowInspector("[#ff8c00]Error/Warn Detail Analysis[-]", sb.String())
				}
			}
			return nil
		}
		return event
	})

	return dashboard
}

func (d *Dashboard) Update(snap model.Snapshot) {
	// --- Stats ---
	var statsStr strings.Builder
	statsStr.WriteString(fmt.Sprintf("\n[white]Total Logs Processed: [blue]%d\n\n", snap.TotalLines))
	statsStr.WriteString(fmt.Sprintf(" [red]ERROR : [white]%-6d    [yellow]WARN  : [white]%-6d\n",
		snap.ErrorCounts["ERROR"], snap.ErrorCounts["WARN"]))
	statsStr.WriteString(fmt.Sprintf(" [green]INFO  : [white]%-6d    [cyan]DEBUG : [white]%-6d\n",
		snap.ErrorCounts["INFO"], snap.ErrorCounts["DEBUG"]))
	d.StatsView.SetText(statsStr.String())

	// --- Graph ---
	// Two log-scale sparklines — red for errors, yellow for warns.
	// Log scale (math.Log1p) compresses the dynamic range so spikes remain
	// visible at high throughput instead of becoming a solid wall of blocks.
	sparkErrors := getSparklineLog(snap.TrendError, 5)
	sparkWarns := getSparklineLog(snap.TrendWarn, 5)
	sparkErrors = strings.ReplaceAll(sparkErrors, "[cyan]", "[red]")
	sparkWarns = strings.ReplaceAll(sparkWarns, "[cyan]", "[yellow]")

	var graphBody strings.Builder
	graphBody.WriteString(fmt.Sprintf("\n [white]Status: %s\n\n", getStatusLabel(snap.AverageEPS, snap.AverageWPS)))
	graphBody.WriteString(fmt.Sprintf(" [red]EPS [white]| Peak: [red]%.1f [white]Avg: [red]%.2f\n", snap.PeakEPS, snap.AverageEPS))
	graphBody.WriteString("\n" + sparkErrors + "\n")
	graphBody.WriteString(" [white]" + strings.Repeat("▔", 25) + "\n\n")
	graphBody.WriteString(fmt.Sprintf(" [yellow]WPS [white]| Peak: [yellow]%.1f [white]Avg: [yellow]%.2f\n", snap.PeakWPS, snap.AverageWPS))
	graphBody.WriteString("\n" + sparkWarns + "\n")
	graphBody.WriteString(" [white]" + strings.Repeat("▔", 25))
	d.GraphView.SetText(graphBody.String())

	// --- Top 10 Errors/Warns ---
	var topStr strings.Builder
	topStr.WriteString("\n")
	d.StatLookup = nil
	sortedTop := snap.TopMessages

	for i := 0; i < 5; i++ {
		getRow := func(idx int) string {
			if idx >= len(sortedTop) {
				return strings.Repeat(" ", 45)
			}
			item := sortedTop[idx]
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
			lookupIdx := len(d.StatLookup) - 1
			color := "red"
			if item.Level == "WARN" {
				color = "yellow"
			}
			return fmt.Sprintf(`["top_%d"][%s]%5d [white]| %-35s[""]`,
				lookupIdx, color, item.Count, truncate(bestMsg, 35))
		}
		topStr.WriteString(fmt.Sprintf(" %s   %s\n", getRow(i), getRow(i+5)))
	}
	d.TopErrorsView.SetText(topStr.String())

	// --- Log updates (original high-performance implementation) ---
	// Only append new lines — never rewrite the whole buffer on every tick.
	// This is what made 50m logs work without the symbol wall.
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

	// Scroll feedback
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

	// Final rendering step — identical to original working implementation
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

// truncate shortens a string to l characters, adding "..." if trimmed.
func truncate(s string, l int) string {
	if len(s) > l {
		return s[:l-3] + "..."
	}
	return s
}

// getSparkline renders a fixed-height bar chart from a slice of ints.
// Uses cyan blocks by default — caller recolors via strings.ReplaceAll.
func getSparkline(data []int, height int) string {
	if len(data) == 0 {
		return ""
	}
	max := 0
	for _, v := range data {
		if v > max {
			max = v
		}
	}
	if max == 0 {
		max = 1
	}
	var lines []string
	for h := height; h > 0; h-- {
		var line strings.Builder
		threshold := (float64(h) / float64(height)) * float64(max)
		for _, v := range data {
			if float64(v) >= threshold {
				line.WriteString("[cyan]█")
			} else if float64(v) >= threshold-(float64(max)/float64(height*2)) {
				line.WriteString("[cyan]▄")
			} else {
				line.WriteString(" ")
			}
		}
		lines = append(lines, line.String())
	}
	return strings.Join(lines, "\n")
}

// getSparklineLog applies math.Log1p to compress the dynamic range before
// rendering. This prevents the graph becoming a solid wall at high throughput —
// a 10x spike still looks like a noticeable bump rather than maxing everything.
// math.Log1p(x) = log(1+x) maps 0→0 cleanly with no -Inf edge case.
func getSparklineLog(data []int, height int) string {
	if len(data) == 0 {
		return ""
	}
	scaled := make([]float64, len(data))
	maxVal := 0.0
	for i, v := range data {
		s := math.Log1p(float64(v))
		scaled[i] = s
		if s > maxVal {
			maxVal = s
		}
	}
	if maxVal == 0 {
		maxVal = 1
	}
	var lines []string
	for h := height; h > 0; h-- {
		var line strings.Builder
		threshold := (float64(h) / float64(height)) * maxVal
		for _, v := range scaled {
			if v >= threshold {
				line.WriteString("[cyan]█")
			} else if v >= threshold-(maxVal/float64(height*2)) {
				line.WriteString("[cyan]▄")
			} else {
				line.WriteString(" ")
			}
		}
		lines = append(lines, line.String())
	}
	return strings.Join(lines, "\n")
}

// getStatusLabel returns a health status based on EPS and WPS independently.
// Error labels always take priority over warn labels.
// Warn labels only show when errors are below the minor threshold —
// giving early warning of degradation before errors start firing.
//
// Thresholds are read from env so each app can tune to its own baseline:
//
//	LOSU_EPS_MINOR       default 0.1   above this → Minor Issues
//	LOSU_EPS_UNSTABLE    default 1.0   above this → Unstable
//	LOSU_EPS_SUSTAINED   default 5.0   above this → Sustained Errors
//	LOSU_EPS_CRITICAL    default 20.0  above this → CRITICAL SPIKE
//	LOSU_WPS_PRESSURE    default 50    above this → Pressure Building
//	LOSU_WPS_SUSPICIOUS  default 100   above this → Suspicious Activity
//	LOSU_WPS_PREINCIDENT default 200   above this → Pre-Incident Warning
func getStatusLabel(eps, wps float64) string {
	thresh := func(key string, def float64) float64 {
		if v := os.Getenv(key); v != "" {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				return f
			}
		}
		return def
	}

	epsCritical := thresh("LOSU_EPS_CRITICAL", 20.0)
	epsSustained := thresh("LOSU_EPS_SUSTAINED", 5.0)
	epsUnstable := thresh("LOSU_EPS_UNSTABLE", 1.0)
	epsMinor := thresh("LOSU_EPS_MINOR", 0.1)
	wpsPreIncident := thresh("LOSU_WPS_PREINCIDENT", 200.0)
	wpsSuspicious := thresh("LOSU_WPS_SUSPICIOUS", 100.0)
	wpsPressure := thresh("LOSU_WPS_PRESSURE", 50.0)

	switch {
	case eps >= epsCritical:
		return "[blink][red]CRITICAL SPIKE"
	case eps >= epsSustained:
		return "[red]Sustained Errors"
	case eps >= epsUnstable:
		return "[red]Unstable"
	case eps >= epsMinor:
		return "[blue]Minor Issues"
	}

	switch {
	case wps >= wpsPreIncident:
		return "[yellow]⚠ Pre-Incident Warning"
	case wps >= wpsSuspicious:
		return "[yellow]⚠ Suspicious Activity"
	case wps >= wpsPressure:
		return "[yellow]⚠ Pressure Building"
	}

	if eps < 0.01 && wps < 0.01 {
		return "[white]IDLE"
	}
	return "[green]HEALTHY"
}

// GetSummaryForAI gathers top 3 errors and top 3 warns for the AI observer.
func (d *Dashboard) GetSummaryForAI(snap model.Snapshot) (errors string, warns string) {
	type kv struct {
		Key  string
		Stat model.MessageStat
	}

	var sorted []kv
	for k, v := range snap.RecentMessages {
		sorted = append(sorted, kv{k, v})
	}

	if len(sorted) == 0 {
		return "No new errors reported in this window.", "No new warnings."
	}

	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Stat.Count > sorted[j].Stat.Count })

	var errB, warnB strings.Builder
	eCount, wCount := 0, 0

	for _, item := range sorted {
		bestMsg := ""
		max := -1
		for msg, count := range item.Stat.VariantCounts {
			if count > max {
				max = count
				bestMsg = msg
			}
		}
		if item.Stat.Level == "ERROR" && eCount < 3 {
			errB.WriteString(fmt.Sprintf("- %s (%d hits)\n", bestMsg, item.Stat.Count))
			eCount++
		} else if item.Stat.Level == "WARN" && wCount < 3 {
			warnB.WriteString(fmt.Sprintf("- %s (%d hits)\n", bestMsg, item.Stat.Count))
			wCount++
		}
	}
	return errB.String(), warnB.String()
}

// getColor maps a log level to a tview color string.
func (d *Dashboard) getColor(level string) string {
	switch level {
	case "ERROR":
		return "red"
	case "WARN":
		return "yellow"
	case "INFO":
		return "green"
	case "DEBUG":
		return "cyan"
	default:
		return "white"
	}
}

// ShowInspector opens a scrollable popup with detailed stats for a clicked error/warn entry.
func (d *Dashboard) ShowInspector(title, content string) {
	textView := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetText("\n " + content)

	updatePopupTitle := func() {
		offset, _ := textView.GetScrollOffset()
		_, _, _, rectHeight := textView.GetInnerRect()
		lines := strings.Split(content, "\n")
		totalLines := len(lines)
		if totalLines > rectHeight {
			maxScroll := totalLines - rectHeight
			percent := (float64(offset) / float64(maxScroll)) * 100
			if percent > 100 {
				percent = 100
			}
			textView.SetTitle(fmt.Sprintf(" %s [white]| %d%% ", title, int(percent)))
		} else {
			textView.SetTitle(fmt.Sprintf(" %s [white]| TOP ", title))
		}
	}

	updatePopupTitle()
	textView.SetBorder(true)

	var isDraggingInspector bool
	textView.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		x, y := event.Position()
		rectX, rectY, rectWidth, rectHeight := textView.GetInnerRect()
		scrollbarX := rectX + rectWidth - 1
		leftPressed := event.Buttons()&tcell.Button1 != 0

		if leftPressed {
			if x >= scrollbarX-1 || isDraggingInspector {
				isDraggingInspector = true
				relativeY := float64(y - rectY)
				percentage := relativeY / float64(rectHeight)
				if percentage < 0 {
					percentage = 0
				}
				if percentage > 1 {
					percentage = 1
				}
				totalLines := strings.Count(textView.GetText(false), "\n")
				targetLine := int(percentage * float64(totalLines))
				textView.ScrollTo(targetLine, 0)
				updatePopupTitle()
				return action, nil
			}
		} else {
			isDraggingInspector = false
		}
		if action == tview.MouseScrollUp || action == tview.MouseScrollDown {
			defer updatePopupTitle()
		}
		return action, event
	})

	modal := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(textView, 0, 4, true).
			AddItem(nil, 0, 1, false), 100, 1, true).
		AddItem(nil, 0, 1, false)

	d.Pages.AddPage("inspector", modal, true, true)
	d.App.SetFocus(textView)

	textView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEsc || event.Rune() == 'q' {
			d.Pages.RemovePage("inspector")
			d.App.SetFocus(d.TopErrorsView)
			return nil
		}
		return event
	})
}
