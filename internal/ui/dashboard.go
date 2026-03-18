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
	App           *tview.Application
	StatsView     *tview.TextView
	TopErrorsView *tview.TextView
	LogView       *tview.TextView
	GraphView     *tview.TextView
	AIView        *tview.TextView
	SearchInput   *tview.InputField
	SearchFilter  string
	Pages         *tview.Flex
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
		SetWordWrap(true).
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
		SetTitle(" [cyan]Error Graph (60s) ")

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
		App:           app,
		StatsView:     stats,
		TopErrorsView: topErrors,
		LogView:       logs,
		GraphView:     graph,
		AIView:        ai,
		SearchInput:   searchInput,
		SearchFilter:  "", // Start with an empty filter string
		Pages:         nil,
	}

	searchInput.SetChangedFunc(func(text string) {
		dashboard.SearchFilter = text
	})

	dashboard.SearchInput = searchInput

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
	//  Build the Stats String
	var statsStr strings.Builder
	statsStr.WriteString(fmt.Sprintf("\n[white]Total Logs Processed: [blue]%d\n\n", snap.TotalLines))

	// Row 1: ERROR and WARN
	statsStr.WriteString(fmt.Sprintf(" [red]ERROR : [white]%-6d    [yellow]WARN  : [white]%-6d\n",
		snap.ErrorCounts["ERROR"],
		snap.ErrorCounts["WARN"]))

	// Row 2: INFO and DEBUG
	statsStr.WriteString(fmt.Sprintf(" [green]INFO  : [white]%-6d    [cyan]DEBUG : [white]%-6d\n",
		snap.ErrorCounts["INFO"],
		snap.ErrorCounts["DEBUG"]))

	d.StatsView.SetText(statsStr.String())

	//  Build Graph
	maxVal := 0
	for _, v := range snap.Trend {
		if v > maxVal {
			maxVal = v
		}
	}

	// Generate a 10-line high graph now
	spark := getSparkline(snap.Trend, 10)

	var graphBody strings.Builder
	graphBody.WriteString(fmt.Sprintf("\n [white]Current Status: %s\n", getStatusLabel(maxVal)))
	graphBody.WriteString(fmt.Sprintf(" [white]Peak: [red]%d Errors per Second\n", maxVal))
	graphBody.WriteString("\n" + spark + "\n")
	graphBody.WriteString(" [white]" + strings.Repeat("▔", 25)) // Bottom axis line

	d.GraphView.SetText(graphBody.String())
	// --- Top 10 Errors/Warns (Two-Column Layout) ---
	var topStr strings.Builder

	type kv struct {
		Key  string
		Stat model.MessageStat
	}
	var sortedTop []kv
	for k, v := range snap.TopMessages {
		sortedTop = append(sortedTop, kv{k, v})
	}
	// Sort by Count, then by Message String
	sort.Slice(sortedTop, func(i, j int) bool {
		if sortedTop[i].Stat.Count == sortedTop[j].Stat.Count {
			return sortedTop[i].Key < sortedTop[j].Key
		}
		return sortedTop[i].Stat.Count > sortedTop[j].Stat.Count
	})

	topStr.WriteString("\n")

	for i := 0; i < 5; i++ {
		getRow := func(idx int) string {
			if idx >= len(sortedTop) {
				return strings.Repeat(" ", 45) // Empty space for alignment
			}
			item := sortedTop[idx]

			// Most frequent version of this pattern
			bestMsg := ""
			maxSubCount := -1

			//
			for msg, count := range item.Stat.VariantCounts {
				if count > maxSubCount {
					maxSubCount = count
					bestMsg = msg
				}
			}

			color := "red"
			if item.Stat.Level == "WARN" {
				color = "yellow"
			}

			// Truncate message to 35 chars to ensure it fits in the column
			msg := truncate(bestMsg, 35)
			return fmt.Sprintf("[%s]%5d [white]| %-35s", color, item.Stat.Count, msg)
		}

		left := getRow(i)
		right := getRow(i + 5)
		topStr.WriteString(fmt.Sprintf(" %s   %s\n", left, right))
	}

	d.TopErrorsView.SetText(topStr.String())
	d.TopErrorsView.SetTitle(fmt.Sprintf(" [red]Top %d Error/Warn Patterns ", len(snap.TopMessages)))

	// Build the Logs String
	var logStr strings.Builder
	for _, event := range snap.History {

		searchTarget := strings.ToLower(event.Level + " " + event.Message)
		filterText := strings.ToLower(d.SearchFilter)

		// Check if the filter matches either the Level or the Message
		if d.SearchFilter != "" && !strings.Contains(searchTarget, filterText) {
			continue
		}

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

		// tview tag format here too
		logStr.WriteString(fmt.Sprintf("[%s][%s] %-5s -> %-s\n",
			color,
			event.Timestamp.Format("15:04:05"),
			event.Level,
			event.Message,
		))

	}
	d.LogView.SetText(logStr.String())

	// --- Update AI View Header with Timestamps ---
	lastError := "None"
	if !snap.LastErrorTime.IsZero() {
		lastError = snap.LastErrorTime.Format("15:04:05")
	}

	lastWarn := "None"
	if !snap.LastWarnTime.IsZero() {
		lastWarn = snap.LastWarnTime.Format("15:04:05")
	}

	// Update the border title to show the "Heartbeat"
	d.AIView.SetTitle(fmt.Sprintf(" [purple]AI Insights | [red]Last Err: %s [yellow]Last Warn: %s ",
		lastError,
		lastWarn,
	))
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

// Small helper for the "Status" text
func getStatusLabel(eps int) string {
	if eps == 0 {
		return "[white]IDLE"
	}
	if eps > 300 {
		return "[blink][red]CRITICAL ERROR SPIKE"
	}
	if eps > 100 {
		return "[yellow]HIGH LOAD"
	}
	return "[green]NORMAL"
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
