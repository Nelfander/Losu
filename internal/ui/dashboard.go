package ui

import (
	"fmt"
	"sort"
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

	// Create the views FIRST so they keep their type
	stats := tview.NewTextView()
	topErrors := tview.NewTextView()
	logs := tview.NewTextView()
	graph := tview.NewTextView()
	ai := tview.NewTextView()

	// Configure Stats
	stats.SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter).
		SetBorder(true).
		SetTitle(" [yellow]Stats Breakdown ")

	// Configure Top Errors/Warns
	topErrors.SetDynamicColors(true).
		SetRegions(true).
		SetBorder(true).
		SetTitle(" [red]Top 10 Error/Warn Messages ")

	// Prevent losing focus when clicking or hitting Enter
	topErrors.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyTab {
			app.SetFocus(logs) // Tab moves to the logs
		}
	})

	// Configure Latest Logs
	logs.SetDynamicColors(true).
		SetScrollable(true).
		SetMaxLines(5000). // Keep the UI buffer light and fast
		SetRegions(true).
		SetWordWrap(false).
		SetBorder(true).
		SetTitle(" [green]Latest Logs (Real-time) ")

	// Configure AI View
	ai.SetDynamicColors(true).
		SetWordWrap(true).
		SetBorder(true).
		SetTitle(" [purple]AI Observer Insights ")
	ai.SetText("[gray]Gathering data for initial analysis...")

	// Configure Graph
	graph.SetDynamicColors(true).
		SetBorder(true).
		SetTitle(" [cyan]Error/Warn Graph (60s) ")

	// Configure Search Box
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
		SearchFilter:     "", // Start with an empty filter string
		Pages:            nil,
		LastHistoryLen:   0,          // Start at 0
		LastSearchFilter: "",         // Start empty
		FilteredLogs:     []string{}, // Initialize the slice
	}

	// Fix the SetChangedFunc to update the dashboard's filter
	searchInput.SetChangedFunc(func(text string) {
		dashboard.SearchFilter = text
	})

	dashboard.SearchInput = searchInput

	// --- THE UNIVERSAL DRAGGABLE LOGIC ---
	logs.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		x, y := event.Position()
		rectX, rectY, rectWidth, rectHeight := logs.GetInnerRect()

		// The scrollbar hit area (far right edge)
		scrollbarX := rectX + rectWidth - 1

		// Check if the LEFT MOUSE BUTTON is currently pressed
		// tcell.Button1 is the standard constant for Left Click
		leftPressed := event.Buttons()&tcell.Button1 != 0

		if leftPressed {
			// If they just clicked the scrollbar OR they are already dragging
			if x >= scrollbarX-1 || dashboard.isDragging {
				dashboard.isDragging = true

				// Calculate percentage (0.0 at top, 1.0 at bottom)
				relativeY := float64(y - rectY)
				percentage := relativeY / float64(rectHeight)

				if percentage < 0 {
					percentage = 0
				}
				if percentage > 1 {
					percentage = 1
				}

				// Get total lines and jump
				totalLines := len(dashboard.FilteredLogs)
				targetLine := int(percentage * float64(totalLines))

				logs.ScrollTo(targetLine, 0)

				// If dragged to the very bottom, re-enable auto-scroll
				dashboard.isAutoScroll = (percentage >= 0.95)

				// Return nil for the event so tview doesn't try to
				// highlight text while dragging the bar
				return action, nil
			}
		} else {
			// Button is released
			dashboard.isDragging = false
		}

		return action, event
	})

	// --- TOP 10 ERROR/WARN DRAGGABLE LOGIC --- (For later when we populate with more than 10 err/warn)
	var isDraggingTop bool

	topErrors.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		x, y := event.Position()
		rectX, rectY, rectWidth, rectHeight := topErrors.GetInnerRect()
		scrollbarX := rectX + rectWidth - 1
		leftPressed := event.Buttons()&tcell.Button1 != 0

		if leftPressed {
			// Check if they clicked the right edge OR are already dragging
			if x >= scrollbarX-1 || isDraggingTop {
				isDraggingTop = true

				relativeY := float64(y - rectY)
				percentage := relativeY / float64(rectHeight)

				// Clamp 0.0 to 1.0
				if percentage < 0 {
					percentage = 0
				}
				if percentage > 1 {
					percentage = 1
				}

				// Get total lines in the TopErrors text
				// We split by newline to see how many lines are actually there
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

	logs.SetMaxLines(2000) // hard limit on visible + internal buffer
	logs.SetChangedFunc(func() {
		dashboard.App.Draw()
	})

	// Layout:
	// A horizontal flex for the top row
	header := tview.NewFlex().
		AddItem(stats, 0, 1, false).    // Stats takes 1/3
		AddItem(topErrors, 0, 2, false) // TopErrors takes 2/3 space for longer text

	// Split the bottom area into Logs/AI (left) and Graph (right)

	// ---Split the bottom left into Logs (Top) and AI (Bottom) ---
	logContainer := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(logs, 15, 1, false). // Latest Logs
		AddItem(ai, 0, 1, false)     // AI Box

	rightSide := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(graph, 0, 1, false).      // Graph takes all available top space
		AddItem(searchInput, 5, 1, false) // Search bar takes 3 lines at the bottom

	body := tview.NewFlex().
		AddItem(logContainer, 0, 2, false). // Latest Logs takes 2/3
		AddItem(rightSide, 55, 1, false)    // Graph takes a fixed width of 25

	flex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(header, 8, 1, false).
		AddItem(body, 0, 1, false) // Stack the split body below the header

	// Enable Mouse support so clicking works
	app.EnableMouse(true)

	// Create the Pages container
	pages := tview.NewPages()

	//  Add  main dashboard as the bottom layer
	pages.AddPage("dashboard", flex, true, true)

	dashboard.MainLayout = flex
	dashboard.Pages = pages

	//  Set initial focus to the app itself, not a specific box
	// This keeps Ctrl+C working and prevents the search box from
	// "stealing" all the keys until you actually click it.
	app.SetFocus(flex)

	//  Simple Input Capture ONLY for the Emergency Exit
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyCtrlC {
			app.Stop()
			return nil
		}
		return event // This is crucial: it sends all other keys back to the app!
	})

	searchInput.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEscape {
			searchInput.SetText("")     // Clears the UI box
			dashboard.SearchFilter = "" // Clears the logic filter
		}
	})

	// Top 10 error/warn clickable popup logic
	topErrors.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEnter {
			highlights := topErrors.GetHighlights()
			if len(highlights) > 0 {
				var index int
				// Parse index back out of top_0 , top_1 etc..
				fmt.Sscanf(highlights[0], "top_%d", &index)

				if index >= 0 && index < len(dashboard.StatLookup) {
					stat := dashboard.StatLookup[index]
					levelColor := dashboard.getColor(stat.Level)

					// We grab the 'bestMsg' again so the Title of the popup
					// matches the text of the row you just pressed Enter on.
					bestMsg := ""
					max := -1
					for msg, count := range stat.VariantCounts {
						if count > max {
							max = count
							bestMsg = msg
						}
					}

					//Build the details string
					var sb strings.Builder
					// Use the levelColor for the Level label and the Total count
					sb.WriteString(fmt.Sprintf("[%s]Log Level: [%s]%s\n", levelColor, levelColor, stat.Level))
					sb.WriteString(fmt.Sprintf("Message : [%s]%s\n", levelColor, tview.Escape(bestMsg)))
					sb.WriteString(fmt.Sprintf("[%s]Total Occurrences: [%s]%d\n", levelColor, levelColor, stat.Count))
					sb.WriteString("[gray]" + strings.Repeat("━", 64) + "\n") // Visual separator

					// 100-timestamp timeline!
					sb.WriteString("\n[cyan]🕒 Recent Timeline (Last 100):\n")
					orderedTimestamps := stat.GetSortedTimestamps()

					// Loop through the ORDERED timestamps backwards (newest at the top)
					for i := len(orderedTimestamps) - 1; i >= 0; i-- {
						ts := orderedTimestamps[i]
						timeDiff := time.Since(ts).Truncate(time.Second)
						diffStr := timeDiff.String()
						if timeDiff < time.Second {
							diffStr = "< 1s"
						}

						sb.WriteString(fmt.Sprintf(" [white]%s [gray](%s ago)\n",
							ts.Format("15:04:05.000"),
							diffStr))
					}

					sb.WriteString("\n[yellow]📝 Unique Variations in this Cluster:\n")
					for msg, count := range stat.VariantCounts {
						sb.WriteString(fmt.Sprintf(" [white](%d hits) %s\n", count, tview.Escape(msg)))
					}

					// Show popup
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
	//  --- Build the Stats String ---
	var statsStr strings.Builder
	statsStr.WriteString(fmt.Sprintf("\n[white]Total Logs Processed: [blue]%d\n\n", snap.TotalLines))

	statsStr.WriteString(fmt.Sprintf(" [red]ERROR : [white]%-6d    [yellow]WARN  : [white]%-6d\n",
		snap.ErrorCounts["ERROR"],
		snap.ErrorCounts["WARN"]))

	statsStr.WriteString(fmt.Sprintf(" [green]INFO  : [white]%-6d    [cyan]DEBUG : [white]%-6d\n",
		snap.ErrorCounts["INFO"],
		snap.ErrorCounts["DEBUG"]))

	d.StatsView.SetText(statsStr.String())

	//  --- GRAPH VIEW ---
	avgEPS := snap.AverageEPS
	spark := getSparkline(snap.Trend, 10)

	var graphBody strings.Builder
	graphBody.WriteString(fmt.Sprintf("\n [white]Current Status: %s\n", getStatusLabel(avgEPS)))
	graphBody.WriteString(fmt.Sprintf(" [white]Peak: [red]%.1f [white]Err+Warn/s | Avg: [cyan]%.2f [white]Err+Warn/s\n", snap.PeakEPS, avgEPS))
	graphBody.WriteString("\n" + spark + "\n")
	graphBody.WriteString(" [white]" + strings.Repeat("▔", 25))

	d.GraphView.SetText(graphBody.String())

	//  --- Top 10 Errors/Warns ---
	type kv struct {
		Key  string
		Stat model.MessageStat
	}
	var sortedTop []kv
	for k, v := range snap.TopMessages {
		sortedTop = append(sortedTop, kv{k, v})
	}

	sort.Slice(sortedTop, func(i, j int) bool {
		if sortedTop[i].Stat.Count != sortedTop[j].Stat.Count {
			return sortedTop[i].Stat.Count > sortedTop[j].Stat.Count
		}
		return sortedTop[i].Key < sortedTop[j].Key
	})

	var topStr strings.Builder
	topStr.WriteString("\n")

	//Clear the lookup slice every update
	d.StatLookup = nil

	for i := 0; i < 5; i++ {
		getRow := func(idx int) string {
			if idx >= len(sortedTop) {
				return strings.Repeat(" ", 45)
			}
			item := sortedTop[idx]

			bestMsg := ""
			maxSubCount := -1
			for msg, count := range item.Stat.VariantCounts {
				if count > maxSubCount {
					maxSubCount = count
					bestMsg = msg
				} else if count == maxSubCount && msg < bestMsg {
					bestMsg = msg
				}
			}

			d.StatLookup = append(d.StatLookup, item.Stat)
			lookupIdx := len(d.StatLookup) - 1

			color := "red"
			if item.Stat.Level == "WARN" {
				color = "yellow"
			}

			// wrap it in a ["top_X"] region so its clickable!
			return fmt.Sprintf(`["top_%d"][%s]%5d [white]| %-35s[""]`,
				lookupIdx,
				color,
				item.Stat.Count,
				truncate(bestMsg, 35))
		}
		topStr.WriteString(fmt.Sprintf(" %s   %s\n", getRow(i), getRow(i+5)))
	}
	d.TopErrorsView.SetText(topStr.String())

	//  --- HIGH PERFORMANCE + STABLE LOG UPDATES ---
	filterChanged := d.SearchFilter != d.LastSearchFilter
	historyFull := len(snap.History) >= 50000

	if filterChanged || historyFull {
		d.FilteredLogs = d.FilteredLogs[:0]
		d.LastHistoryLen = 0
		d.LastSearchFilter = d.SearchFilter
		d.LogView.Clear()
		d.isAutoScroll = true // Reset auto-scroll when starting a new search
	}

	if len(snap.History) > d.LastHistoryLen {
		var uiBatch strings.Builder
		filterLower := strings.ToLower(d.SearchFilter)

		for i := d.LastHistoryLen; i < len(snap.History); i++ {
			event := snap.History[i]

			// We search BOTH the level and the message now for better effectiveness
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

			// Only scroll if we are in "AutoScroll" mode and NOT dragging
			if d.isAutoScroll && !d.isDragging {
				d.LogView.ScrollToEnd()
			}
		}

		d.LastHistoryLen = len(snap.History)
	}

	// === CRITICAL: Force hard trim to prevent buffer explosion and garbage ===
	const maxVisibleLines = 1500

	if len(d.FilteredLogs) > maxVisibleLines+500 {
		// Keep only the newest lines
		start := len(d.FilteredLogs) - maxVisibleLines
		if start < 0 {
			start = 0
		}

		d.LogView.Clear() // fastest way to reset internal buffer

		for _, line := range d.FilteredLogs[start:] {
			fmt.Fprint(d.LogView, line)
		}

		d.FilteredLogs = d.FilteredLogs[start:] // trim cache too
	}
	//d.LogView.SetText(d.LogView.GetText(false)) // forces full re-render (slow but cleans artifacts)
	//d.LogView.ScrollToEnd()

	//  --- SCROLL FEEDBACK ---
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

	//  --- AI View ---
	lastError := "None"
	if !snap.LastErrorTime.IsZero() {
		lastError = snap.LastErrorTime.Format("15:04:05")
	}
	lastWarn := "None"
	if !snap.LastWarnTime.IsZero() {
		lastWarn = snap.LastWarnTime.Format("15:04:05")
	}
	d.AIView.SetTitle(fmt.Sprintf(" [purple]AI Insights | [red]Last Err: %s [yellow]Last Warn: %s ", lastError, lastWarn))

	// === FINAL RENDERING STEP (Optimized) ===
	d.LogView.Clear()

	d.renderBuf.Reset() // Clear the persistent buffer without deallocating memory

	// Pre-allocate know the size (approx 100 chars per line * 1500 lines)
	if d.renderBuf.Cap() < 150000 {
		d.renderBuf.Grow(150000)
	}

	for _, line := range d.FilteredLogs {
		d.renderBuf.WriteString(line)
	}

	// SetText from the persistent buffer
	d.LogView.SetText(d.renderBuf.String())

	if d.isAutoScroll && !d.isDragging {
		d.LogView.ScrollToEnd()
	}
}

// Helper func
func truncate(s string, l int) string {
	if len(s) > l {
		return s[:l-3] + "..."
	}
	return s
}

// Helper visual func!
func getSparkline(data []int, height int) string {
	if len(data) == 0 {
		return ""
	}

	// Find max for scaling
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
	// Create the graph from top to bottom
	for h := height; h > 0; h-- {
		var line strings.Builder
		threshold := (float64(h) / float64(height)) * float64(max)

		for _, v := range data {
			if float64(v) >= threshold {
				line.WriteString("[cyan]█") // Solid block for peaks
			} else if float64(v) >= threshold-(float64(max)/float64(height*2)) {
				line.WriteString("[cyan]▄") // Half block for mid-range
			} else {
				line.WriteString(" ")
			}
		}
		lines = append(lines, line.String())
	}

	return strings.Join(lines, "\n")
}

// getStatusLabel provides a readable health status based on combined ERROR/WARN throughput
// Should be changed accoring the app that implements Losu
func getStatusLabel(eps float64) string {
	switch {
	case eps < 0.01:
		return "[white]IDLE"
	case eps > 20.0:
		return "[blink][red]CRITICAL SPIKE"
	case eps > 5.0:
		return "[red]Sustained Errors"
	case eps > 1.0:
		return "[yellow]Unstable"
	case eps > 0.1:
		return "[blue]Minor Issues"
	default:
		return "[green]HEALTHY"
	}
}

// Gathers the top 3 Errors and top 3 Warns into a single string for the AI to analyze
func (d *Dashboard) GetSummaryForAI(snap model.Snapshot) (errors string, warns string) {
	type kv struct {
		Key  string
		Stat model.MessageStat
	}

	// This ensures the AI only sees what happened since the last check.
	var sorted []kv
	for k, v := range snap.RecentMessages {
		sorted = append(sorted, kv{k, v})
	}

	// If there's literally no new data, give the AI a clear signal
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

// Helper func to get color
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

// Helper to make top10 err/warn clickable with popup detailed box!
func (d *Dashboard) ShowInspector(title, content string) {
	textView := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetText("\n " + content)

	// Helper to update the title based on scroll position
	updatePopupTitle := func() {
		offset, _ := textView.GetScrollOffset()
		_, _, _, rectHeight := textView.GetInnerRect()

		// Count total lines in content
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

	// Initial title set
	updatePopupTitle()
	textView.SetBorder(true)

	// --- DRAGGABLE LOGIC ---
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

				// Update title while dragging
				updatePopupTitle()
				return action, nil
			}
		} else {
			isDraggingInspector = false
		}

		// Also update title with mouse wheel
		if action == tview.MouseScrollUp || action == tview.MouseScrollDown {
			// Let the default scroll happen first, then update title
			defer updatePopupTitle()
		}

		return action, event
	})

	// Layout and Page logic
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
