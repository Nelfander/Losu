package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/nelfander/losu/internal/aggregator"
	"github.com/nelfander/losu/internal/ai"
	"github.com/nelfander/losu/internal/alerts"
	"github.com/nelfander/losu/internal/hub"
	"github.com/nelfander/losu/internal/model"
	"github.com/nelfander/losu/internal/parser"
	"github.com/nelfander/losu/internal/pipeline"
	"github.com/nelfander/losu/internal/server"
	"github.com/nelfander/losu/internal/tailer"
	"github.com/nelfander/losu/internal/ui"
	"github.com/nelfander/losu/internal/watcher"
)

var lastAlertTime time.Time
var alertMu sync.Mutex

func main() {
	_ = godotenv.Load()
	logPath := os.Getenv("LOSU_LOG_PATH")
	ntfyTopic := os.Getenv("LOSU_NTFY_TOPIC")

	// Define the flag (default to INFO, so DEBUG is hidden by default)
	minLevel := flag.String("level", os.Getenv("LOSU_MIN_LEVEL"), "minimum log level to display (DEBUG, INFO, WARN, ERROR)")

	// Resetmode : go run main.go -reset
	resetStats := flag.Bool("reset", false, "wipe existing stats and start fresh")

	// UI mode: tui (default), web, or both
	// tui  → terminal dashboard only (original behaviour)
	// web  → HTTP + WebSocket server only, no terminal UI
	// both → run both side by side
	uiMode := flag.String("ui", "tui", "ui mode: tui | web | both")

	// Web server listen address — configurable via flag or env
	// Defaults to :8080 if neither is set
	webAddr := flag.String("addr", os.Getenv("LOSU_WEB_ADDR"), "web server listen address (e.g. :8080)")

	flag.Parse()

	// Default address if not set via flag or env
	if *webAddr == "" {
		*webAddr = ":8080"
	}

	// Validate ui mode early so we fail fast before starting any goroutines
	switch *uiMode {
	case "tui", "web", "both":
		// valid
	default:
		log.Fatalf("invalid --ui value %q: must be tui, web, or both", *uiMode)
	}

	// Start pprof server on port 6060 in the background
	go func() {
		http.ListenAndServe("localhost:6060", nil)
	}()

	// Initialize the alert service and create alerts log
	notifier := alerts.NewAlerter("alerts.log")
	notifier.NtfyTopic = ntfyTopic

	// Hourly Report (time can be changed through env)
	windowStr := os.Getenv("LOSU_REPORT_WINDOW")
	windowMinutes, err := strconv.Atoi(windowStr)
	if err != nil || windowMinutes <= 0 {
		windowMinutes = 60 // Default to 1 hour if not set in env or invalid
	}
	reportTicker := time.NewTicker(time.Duration(windowMinutes) * time.Minute)
	defer reportTicker.Stop()

	// Setup Shutdown Handling
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Map levels to "Weights" so they can be compared
	levels := map[string]int{"DEBUG": 0, "INFO": 1, "WARN": 2, "ERROR": 3}
	minWeight := levels[strings.ToUpper(*minLevel)]

	// WaitGroup (Tracks background workers)
	var wg sync.WaitGroup

	// --- Multi-file support (Option B: one aggregator per file) ---
	// Each file gets its own Aggregator, Tailer, FSWatcher, Parser, and GOROUTINE A.
	// This means stats, EPS, history etc are completely isolated per file.
	// The UI switches between aggregators — no filtering needed.
	logPaths := strings.Split(logPath, ",")
	for i, p := range logPaths {
		logPaths[i] = strings.TrimSpace(p)
	}

	// aggMap holds one aggregator per log file path — the server and TUI
	// read from the currently active one.
	aggMap := make(map[string]*aggregator.Aggregator)
	// aggKeys preserves insertion order for TUI Tab cycling
	var aggKeys []string

	for _, path := range logPaths {
		if path == "" {
			continue
		}

		// One aggregator per file — fully isolated stats, tagged with source path
		agg := aggregator.NewAggregatorForSource(path)
		statsFile := path + ".stats.json"
		if *resetStats {
			os.Remove(statsFile)
		} else {
			agg.Load(statsFile)
		}
		aggMap[path] = agg
		aggKeys = append(aggKeys, path)

		// Each file gets its own watcher
		ws, err := watcher.NewFSWatcher()
		if err != nil {
			log.Fatalf("failed to start watcher for %s: %v", path, err)
		}

		// Auto-detect log format by peeking at the first few lines of the file.
		// Returns JSONParser for JSON logs, RegexParser for everything else.
		// Detection happens once at startup — format switching mid-run is not supported.
		// Each file gets its own parser — JSON and logfmt files can coexist.
		p := parser.DetectParser(path)

		// Start the Watcher
		changes, err := ws.Watch(ctx, path)
		if err != nil {
			log.Fatalf("failed to watch file %s: %v", path, err)
		}

		// Each file gets its own channels — completely isolated pipeline
		rawLineChan := make(chan model.RawLog, 10000)
		eventChan := make(chan model.LogEvent, 10000)

		// Start Tailer (Producer)
		filePath := path
		fileAgg := agg
		t := tailer.NewTailer(filePath, rawLineChan)
		go func() {
			if err := t.Run(ctx, changes); err != nil {
				// Errors are handled internally now to prevent UI corruption
			}
		}()

		// Start Worker Pool (Processors)
		pipeline.Process(ctx, &wg, 1, p, rawLineChan, eventChan)

		// GOROUTINE A (per file): Updates this file's aggregator as fast as possible
		go func() {
			count := 0
			for {
				select {
				case <-ctx.Done():
					return
				case event, ok := <-eventChan:
					if !ok {
						return
					}

					fileAgg.Update(event, minWeight, levels)
					count++

					// Every 100 logs, yield to let other goroutines (UI, WS) breathe
					if count >= 100 {
						time.Sleep(1 * time.Millisecond)
						count = 0
					}

					if event.Level == "ERROR" || event.Level == "WARN" {
						alertMu.Lock()
						if time.Since(lastAlertTime) > 10*time.Second {
							lastAlertTime = time.Now()
							alertMu.Unlock()
							// Pass current EPS so phone alert only fires above threshold
							notifier.Trigger(event, fileAgg.AverageEPS)
						} else {
							alertMu.Unlock()
						}
					}
				}
			}
		}()
	}

	// Fallback: if no valid paths were found, create a dummy aggregator
	// so the rest of main doesn't panic on nil map access
	if len(aggMap) == 0 {
		log.Fatal("no valid log paths found in LOSU_LOG_PATH")
	}

	// Default active aggregator — first file in the list
	activeAgg := aggMap[aggKeys[0]]

	// Initialize AI Explainer — used by the AI Observer goroutine.
	// On web-only mode this is still initialized but never called (Ollama not needed).
	explainer := ai.NewExplainer()

	// GOROUTINE C: The Heartbeat Reporter — uses the first/active aggregator
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-reportTicker.C:
				startTime, counts, topMsg := activeAgg.FlushHourlyStats()
				duration := time.Since(startTime).Round(time.Minute)

				rawSummary := fmt.Sprintf("📊 LOSU HEARTBEAT [%s]\n", startTime.Format("15:04"))
				rawSummary += fmt.Sprintf("Duration: %v\n", duration)
				rawSummary += fmt.Sprintf("Errors: %d | Warns: %d | Info: %d\n", counts["ERROR"], counts["WARN"], counts["INFO"])
				rawSummary += fmt.Sprintf("Top Issue: %s\n", topMsg)

				aiTake := ""
				if explainer != nil {
					aiTake, err = explainer.AnalyzeHeartbeat(counts, topMsg)
					if err == nil && aiTake != "" {
						aiTake = "\n🤖 SRE Take: " + aiTake
					}
				}

				notifier.PushNotification("System Heartbeat", rawSummary+aiTake)
			}
		}
	}()

	// --- Web layer setup (only if ui mode requires it) ---
	// We create the hub and server here but start them below after the
	// pipeline is running, so the aggregator already has data on first connect.
	var wsHub *hub.Hub
	var webServer *server.Server

	if *uiMode == "web" || *uiMode == "both" {
		wsHub = hub.NewHub()
		webServer = server.New(*webAddr, wsHub, aggMap)
	}

	// --- Start web server if needed ---
	if *uiMode == "web" || *uiMode == "both" {
		go func() {
			// Note: avoid log.Printf here when --ui=tui or --ui=both
			// because tview owns the terminal and raw writes corrupt the UI.
			// The address is visible in the .env or via the --addr flag.
			if err := webServer.Start(ctx); err != nil && err.Error() != "http: Server closed" {
				// Only print on real errors, not normal shutdown
				fmt.Fprintf(os.Stderr, "web server error: %v", err)
			}
		}()
	}

	// GOROUTINE E: AI Observer — runs regardless of UI mode.
	// Analyzes recent error/warn patterns every 30 seconds.
	// Results are stored in the aggregator and served to both TUI and web UI.
	// Requires Ollama running locally — gracefully handles offline state.
	go func() {
		// Wait for data to populate before first analysis
		time.Sleep(5 * time.Second)

		aiInterval := 60
		if v := os.Getenv("LOSU_AI_INTERVAL_SECONDS"); v != "" {
			if i, err := strconv.Atoi(v); err == nil && i > 0 {
				aiInterval = i
			}
		}
		ticker := time.NewTicker(time.Duration(aiInterval) * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				recentMessages := activeAgg.GetRecentSnapshot()
				fullSnap := activeAgg.Snapshot()
				fullSnap.RecentMessages = recentMessages

				// Build pattern summaries using aggregator data directly
				var errPatterns, warnPatterns string
				errCount, warnCount := 0, 0
				for _, stat := range fullSnap.RecentMessages {
					bestMsg := ""
					maxC := -1
					for msg, c := range stat.VariantCounts {
						if c > maxC {
							maxC = c
							bestMsg = msg
						}
					}
					if stat.Level == "ERROR" && errCount < 3 {
						errPatterns += fmt.Sprintf("- %s (%d hits)\n", bestMsg, stat.Count)
						errCount++
					} else if stat.Level == "WARN" && warnCount < 3 {
						warnPatterns += fmt.Sprintf("- %s (%d hits)\n", bestMsg, stat.Count)
						warnCount++
					}
				}
				if errPatterns == "" {
					errPatterns = "No new errors."
				}
				if warnPatterns == "" {
					warnPatterns = "No new warnings."
				}

				// Pass avgWps and peakWps separately — AI now sees independent
				// error and warn rates for more accurate root cause analysis.
				analysis, err := explainer.AnalyzeSystem(
					errPatterns,
					warnPatterns,
					fullSnap.AverageEPS,
					fullSnap.PeakEPS,
					fullSnap.AverageWPS,
					fullSnap.PeakWPS,
				)
				if err != nil {
					analysis = "AI Observer is currently offline (Check Ollama status)."
				}

				timestamp := time.Now().Format("15:04:05")

				// Strip markdown formatting before storing — tview's tag parser
				// chokes on ** and ### which corrupts rendering of other panels.
				analysis = strings.ReplaceAll(analysis, "**", "")
				analysis = strings.ReplaceAll(analysis, "### ", "")
				analysis = strings.ReplaceAll(analysis, "## ", "")
				analysis = strings.ReplaceAll(analysis, "# ", "")

				// Store plain text for web UI
				webText := fmt.Sprintf("Last Analysis @ %s | Avg EPS: %.2f | Avg WPS: %.2f\n\n%s",
					timestamp, fullSnap.AverageEPS, fullSnap.AverageWPS, analysis)
				activeAgg.SetAIAnalysis(webText)
			}
		}
	}()

	// --- Start TUI if needed ---
	if *uiMode == "tui" || *uiMode == "both" {
		dash := ui.NewDashboard()

		// Give dashboard the full aggMap and keys for Tab cycling
		dash.AggMap = aggMap
		dash.AggKeys = aggKeys
		dash.ActiveAgg = activeAgg

		// GOROUTINE D: TUI updater — only runs when TUI is active
		go func() {
			uiTicker := time.NewTicker(500 * time.Millisecond)
			defer uiTicker.Stop()

			for {
				select {
				case <-ctx.Done():
					dash.App.Stop()
					return
				case <-uiTicker.C:
					// Always read from dash.ActiveAgg — Tab cycling updates this pointer
					snap := dash.ActiveAgg.Snapshot()
					dash.App.QueueUpdateDraw(func() {
						dash.Update(snap)
					})
				}
			}
		}()

		// GOROUTINE F: TUI AI panel updater.
		// Watches the aggregator for new AI analysis text and pushes it to the TUI panel.
		// Separate from the AI goroutine so the TUI doesn't need to know about Ollama.
		go func() {
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			lastText := ""
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					text := dash.ActiveAgg.GetAIAnalysis()
					if text == "" || text == lastText {
						continue
					}
					lastText = text
					dash.App.QueueUpdateDraw(func() {
						// tview.Escape prevents any remaining brackets from
						// being interpreted as color tags and corrupting render
						safe := strings.ReplaceAll(text, "[", "(")
						safe = strings.ReplaceAll(safe, "]", ")")
						dash.AIView.SetText("[white]" + safe)
					})
				}
			}
		}()

		// Blocks here until TUI exits
		if err := dash.App.SetRoot(dash.Pages, true).Run(); err != nil {
			fmt.Printf("Error running dashboard: %v\n", err)
		}
	} else {
		// web only — block until Ctrl+C
		<-ctx.Done()
	}

	// --- Graceful shutdown ---
	stop()
	wg.Wait()

	fmt.Println("Finishing background incident reports...")
	for _, agg := range aggMap {
		agg.Wait()
	}

	fmt.Println("Saving session state...")
	for path, agg := range aggMap {
		statsFile := path + ".stats.json"
		if err := agg.Save(statsFile); err != nil {
			log.Printf("Failed to save stats for %s: %v", path, err)
		}
	}

	finalSnap := activeAgg.Snapshot()
	fmt.Printf("\n--- Final Report ---\n")
	fmt.Printf("Total Lines Processed: %d\n", finalSnap.TotalLines)
	for level, count := range finalSnap.ErrorCounts {
		fmt.Printf("%s: %d\n", level, count)
	}
	fmt.Println("--------------------")
	fmt.Println("All workers stopped. Goodbye!")
}
