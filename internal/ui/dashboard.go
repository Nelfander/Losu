package ui

import (
	"fmt"
	"sort"
	"strings"

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
	Pages            *tview.Flex
	LastHistoryLen   int
	LastSearchFilter string
	FilteredLogs     []string // Cache of logs that matched the current filter
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
		SetBorder(true).
		SetTitle(" [red]Top 10 Error/Warn Messages ")

	// Configure Latest Logs
	logs.SetDynamicColors(true).
		SetScrollable(true).
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
	var isDragging bool

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
			if x >= scrollbarX-1 || isDragging {
				isDragging = true

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
				totalLines := strings.Count(logs.GetText(false), "\n")
				targetLine := int(percentage * float64(totalLines))

				logs.ScrollTo(targetLine, 0)

				// Return nil for the event so tview doesn't try to
				// highlight text while dragging the bar
				return action, nil
			}
		} else {
			// Button is released
			isDragging = false
		}

		return action, event
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

	dashboard.Pages = flex
	// Enable Mouse support so clicking works
	app.EnableMouse(true)

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
	/*
		// Keys for scrolling latest logs
		logs.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
			row, col := logs.GetScrollOffset()
			switch event.Key() {
			case tcell.KeyPgUp:
				logs.ScrollTo(row-10, col) // Scrolls 10 up
				return nil
			case tcell.KeyPgDn:
				logs.ScrollTo(row+10, col) // Scrolls 10 down
				return nil
			case tcell.KeyHome:
				logs.ScrollToBeginning() // Scrolls to beginning of latest logs
				return nil
			case tcell.KeyEnd:
				logs.ScrollToEnd() // Scrolls to the end of latest logs
				return nil
			}
			return event
		})
	*/
	return dashboard

}

// Helper func
func truncate(s string, l int) string {
	if len(s) > l {
		return s[:l-3] + "..."
	}
	return s
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

			color := "red"
			if item.Stat.Level == "WARN" {
				color = "yellow"
			}
			return fmt.Sprintf("[%s]%5d [white]| %-35s", color, item.Stat.Count, truncate(bestMsg, 35))
		}
		topStr.WriteString(fmt.Sprintf(" %s   %s\n", getRow(i), getRow(i+5)))
	}
	d.TopErrorsView.SetText(topStr.String())

	//  --- HIGH PERFORMANCE LOG CACHING ---
	// If filter changed OR the history was truncated (reached max capacity), rebuild
	filterChanged := d.SearchFilter != d.LastSearchFilter
	historyFull := len(snap.History) >= 50000 // Assuming maxHistory is 50k

	if filterChanged || historyFull {
		// At 50k, we rebuild to ensure the "sliding window" stays accurate
		d.FilteredLogs = []string{}
		d.LastHistoryLen = 0
		d.LastSearchFilter = d.SearchFilter
	}

	// Only process logs that have arrived since last update
	if len(snap.History) > d.LastHistoryLen {
		filterLower := strings.ToLower(d.SearchFilter)

		for i := d.LastHistoryLen; i < len(snap.History); i++ {
			event := snap.History[i]

			if filterLower == "" ||
				strings.Contains(strings.ToLower(event.Level), filterLower) ||
				strings.Contains(strings.ToLower(event.Message), filterLower) {

				color := "white"
				switch event.Level {
				case "ERROR":
					color = "red"
				case "WARN":
					color = "yellow"
				case "INFO":
					color = "green"
				case "DEBUG":
					color = "cyan"
				}

				line := fmt.Sprintf("[%s][%s] %-5s -> %s",
					color, event.Timestamp.Format("15:04:05"), event.Level, event.Message)

				d.FilteredLogs = append(d.FilteredLogs, line)
			}
		}
		d.LastHistoryLen = len(snap.History)

		// Capping the cache so it doesn't grow to infinity
		if len(d.FilteredLogs) > 50000 {
			// Keep the most recent 50k matching logs
			d.FilteredLogs = d.FilteredLogs[len(d.FilteredLogs)-50000:]
		}

		d.LogView.SetText(strings.Join(d.FilteredLogs, "\n"))
	}

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
