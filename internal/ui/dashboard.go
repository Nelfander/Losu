package ui

import (
	"fmt"
	"strings"

	"github.com/nelfander/losu/internal/model"
	"github.com/rivo/tview"
)

type Dashboard struct {
	App       *tview.Application
	StatsView *tview.TextView
	LogView   *tview.TextView
	Pages     *tview.Flex
}

func NewDashboard() *Dashboard {
	app := tview.NewApplication()

	// Create the views FIRST so they keep their type
	stats := tview.NewTextView()
	logs := tview.NewTextView()

	// Configure them separately
	stats.SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter).
		SetBorder(true).
		SetTitle(" [yellow]Stats Breakdown ")

	logs.SetDynamicColors(true).
		SetRegions(true).
		SetWordWrap(true).
		SetBorder(true).
		SetTitle(" [green]Latest Logs (Real-time) ")

	// Layout: Stack them vertically
	flex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(stats, 8, 1, false).
		AddItem(logs, 0, 1, false)

	return &Dashboard{
		App:       app,
		StatsView: stats,
		LogView:   logs,
		Pages:     flex,
	}
}

func (d *Dashboard) Update(snap model.Snapshot) {
	//  Build the Stats String
	var statsStr strings.Builder
	statsStr.WriteString(fmt.Sprintf("\n[white]Total Logs Processed: [blue]%d\n\n", snap.TotalLines))

	levels := []string{"ERROR", "WARN", "INFO", "DEBUG"}
	for _, l := range levels {
		count := snap.ErrorCounts[l]
		color := "white"
		switch l {
		case "ERROR":
			color = "red"
		case "WARN":
			color = "yellow"
		case "INFO":
			color = "green"
		case "DEBUG":
			color = "cyan"
		}
		// tview tags look like [color]
		statsStr.WriteString(fmt.Sprintf("[%s]%-6s[white]: %-6d  ", color, l, count))
	}

	d.StatsView.SetText(statsStr.String())

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
