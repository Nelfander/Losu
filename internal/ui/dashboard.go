// Package ui provides the terminal user interface for LOSU.
// This file defines the Dashboard struct and NewDashboard() constructor.
// It wires together the layout (tview primitives, pages, flex containers)
// and delegates all input handling to input.go.
//
// File overview:
//
//	dashboard.go  — struct, NewDashboard(), layout wiring
//	input.go      — all keyboard and mouse capture handlers
//	update.go     — Update() method, real-time rendering logic
//	inspector.go  — ShowVariantInspector(), ShowInspector(), GetSummaryForAI()
//	sparkline.go  — truncate, getSparkline, getSparklineLog, getStatusLabel, getColor
package ui

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/nelfander/losu/internal/aggregator"
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
	// Multi-file support — one aggregator per file.
	// ActiveAgg is the one currently displayed. Tab cycles through AggKeys.
	ActiveAgg *aggregator.Aggregator
	AggMap    map[string]*aggregator.Aggregator
	AggKeys   []string // insertion-ordered list of paths for Tab cycling
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
		SetTitle(" [red]Top Errors / Warns [gray](↑↓: navigate  Enter: inspect  f: fullscreen) ")

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
		SetTitle(" [white][ Filter Panel ] [gray](/ to focus) ").
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

	// Wire up all keyboard and mouse handlers (defined in input.go)
	dashboard.setupInput(app, stats, topErrors, logs, searchInput, ai, graph)

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

	app.SetFocus(stats)
	app.ForceDraw()

	return dashboard
}
