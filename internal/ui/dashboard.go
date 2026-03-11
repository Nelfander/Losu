package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/nelfander/losu/internal/model"
	"github.com/rivo/tview"
)

type Dashboard struct {
	App           *tview.Application
	StatsView     *tview.TextView
	TopErrorsView *tview.TextView
	LogView       *tview.TextView
	GraphView     *tview.TextView
	Pages         *tview.Flex
}

func NewDashboard() *Dashboard {
	app := tview.NewApplication()

	// Create the views FIRST so they keep their type
	stats := tview.NewTextView()
	topErrors := tview.NewTextView()
	logs := tview.NewTextView()
	graph := tview.NewTextView()

	// Configure them separately
	stats.SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter).
		SetBorder(true).
		SetTitle(" [yellow]Stats Breakdown ")

	// Configure Top Errors
	topErrors.SetDynamicColors(true).
		SetBorder(true).
		SetTitle(" [red]Top 10 Error/Warn Messages ")

	// Configure Logs
	logs.SetDynamicColors(true).
		SetRegions(true).
		SetWordWrap(true).
		SetBorder(true).
		SetTitle(" [green]Latest Logs (Real-time) ")

	// Configure Graph
	graph.SetDynamicColors(true).
		SetBorder(true).
		SetTitle(" [cyan]Activity (60s) ")

	// Layout:
	// A horizontal flex for the top row
	header := tview.NewFlex().
		AddItem(stats, 0, 1, false).    // Stats takes 1/3
		AddItem(topErrors, 0, 2, false) // TopErrors takes 2/3 space for longer text

	// Split the bottom area into Logs (left) and Graph (right)
	body := tview.NewFlex().
		AddItem(logs, 0, 2, false).  // Logs takes 2/3
		AddItem(graph, 55, 1, false) // Graph takes a fixed width of 25

	flex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(header, 8, 1, false).
		AddItem(body, 0, 1, false) // Stack the split body below the header

	return &Dashboard{
		App:           app,
		StatsView:     stats,
		TopErrorsView: topErrors,
		LogView:       logs,
		GraphView:     graph, // Assigned here
		Pages:         flex,
	}
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

	maxVal := 0
	for _, v := range snap.Trend {
		if v > maxVal {
			maxVal = v
		}
	}

	// We generate a 10-line high graph now
	spark := getSparkline(snap.Trend, 10)

	var graphBody strings.Builder
	graphBody.WriteString(fmt.Sprintf("\n [white]Current Status: %s\n", getStatusLabel(maxVal)))
	graphBody.WriteString(fmt.Sprintf(" [white]Peak: [red]%d eps\n", maxVal))
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
				return strings.Repeat(" ", 50) // Empty space for alignment
			}
			item := sortedTop[idx]
			color := "red"
			if item.Stat.Level == "WARN" {
				color = "yellow"
			}

			// Truncate message to 35 chars to ensure it fits in the column
			msg := truncate(item.Key, 35)
			return fmt.Sprintf("[%s]%5d [white]| %-35s", color, item.Stat.Count, msg)
		}

		left := getRow(i)
		right := getRow(i + 5)
		topStr.WriteString(fmt.Sprintf(" %s   %s\n", left, right))
	}

	d.TopErrorsView.SetText(topStr.String())

	// Build the Logs String
	var logStr strings.Builder
	for _, event := range snap.History {
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
