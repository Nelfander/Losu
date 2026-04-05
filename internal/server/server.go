package server

import (
	"context"
	_ "embed"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/nelfander/losu/internal/aggregator"
	"github.com/nelfander/losu/internal/hub"
)

// indexHTML is the compiled-in web dashboard.
// Lives at internal/server/static/index.html — baked into the binary at
// build time so no separate files are needed at runtime.
//
//go:embed static/index.html
var indexHTML []byte

// upgrader converts a plain HTTP connection into a WebSocket connection.
// CheckOrigin returns true unconditionally because this is a localhost tool —
// we don't need to validate where the request is coming from.
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Server owns the Echo instance, the hub, and the aggregator reference.
// It is the only place that knows about HTTP — nothing else in the codebase
// needs to care that a web server exists.
type Server struct {
	echo *echo.Echo
	hub  *hub.Hub
	agg  *aggregator.Aggregator
	addr string
}

// New creates a Server but does not start it.
// addr is the listen address e.g. ":8080"
func New(addr string, h *hub.Hub, agg *aggregator.Aggregator) *Server {
	e := echo.New()
	e.HideBanner = true // suppress Echo's startup ASCII art

	// Panic recovery and custom request logger
	e.Use(middleware.Recover())
	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogMethod:   true,
		LogURI:      true,
		LogStatus:   true,
		LogLatency:  true,
		LogRemoteIP: true,
		LogError:    true,
		LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
			level := "INFO"
			if v.Status >= 500 {
				level = "ERROR"
			} else if v.Status >= 400 {
				level = "WARN"
			}
			// log.Printf omitted intentionally — losu parses its own logs,
			// so we use a format the parser already understands.
			_ = level
			return nil
		},
	}))

	s := &Server{
		echo: e,
		hub:  h,
		agg:  agg,
		addr: addr,
	}

	// Routes
	e.GET("/", s.handleIndex)                       // serves the embedded dashboard
	e.GET("/ws/stream", s.handleWebSocket)          // WebSocket stream
	e.GET("/api/snapshot", s.handleSnapshot)        // initial REST snapshot
	e.GET("/api/inspect", s.handleInspect)          // on-demand detail for a pattern (?pattern=...)
	e.GET("/api/logs", s.handleLogs)                // full-history search (?q=...&level=...&limit=...)
	e.GET("/api/incidents", s.handleIncidentList)   // list all incident files
	e.GET("/api/incidents/:file", s.handleIncident) // get one incident file content

	return s
}

// Start begins three things:
//  1. The hub's event loop (owns the client map)
//  2. The broadcaster goroutine (ticks every 500ms, pushes to hub)
//  3. The Echo HTTP server (accepts connections)
//
// It blocks until ctx is cancelled, then shuts down cleanly.
func (s *Server) Start(ctx context.Context) error {
	// 1. Start the hub event loop
	go s.hub.Run()

	// 2. Start the broadcaster — this is the heartbeat of the web UI.
	//    Every 500ms it takes a WebSnapshot, serializes it to JSON,
	//    and hands it to the hub which fans it out to all connected clients.
	go s.broadcaster(ctx)

	// 3. Start Echo in its own goroutine so we can listen for ctx cancellation
	//    on the main goroutine below.
	errCh := make(chan error, 1)
	go func() {
		if err := s.echo.Start(s.addr); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	// Block until context is cancelled (Ctrl+C) or Echo errors out.
	select {
	case <-ctx.Done():
		// Graceful shutdown — give in-flight requests 5 seconds to finish.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.echo.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

// broadcaster is the heartbeat goroutine.
// It runs for the lifetime of the app and is the ONLY place that calls
// hub.Broadcast() — keeping fan-out logic in one place.
func (s *Server) broadcaster(ctx context.Context) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			snap := s.agg.WebSnapshot()
			payload, err := json.Marshal(snap)
			if err != nil {
				continue
			}
			s.hub.Broadcast(payload)
		}
	}
}

// handleIndex serves the embedded index.html dashboard.
func (s *Server) handleIndex(c echo.Context) error {
	return c.HTMLBlob(http.StatusOK, indexHTML)
}

// handleWebSocket upgrades the connection and hands it to the hub.
// From this point the hub and the client's writePump own the connection —
// this handler returns immediately.
func (s *Server) handleWebSocket(c echo.Context) error {
	conn, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	s.hub.ServeClient(conn)
	return nil
}

// handleInspect returns full detail for a single error/warn pattern.
// Called on demand when the user clicks a row in the top errors panel.
// Uses a query parameter (?pattern=...) instead of a path parameter because
// fingerprint patterns contain characters like * and . that Echo's router
// would interpret as wildcards in a path segment.
func (s *Server) handleInspect(c echo.Context) error {
	pattern := c.QueryParam("pattern")
	if pattern == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "pattern required"})
	}

	result := s.agg.GetInspect(pattern)
	if result == nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "pattern not found"})
	}
	return c.JSON(http.StatusOK, result)
}

// handleLogs searches the full 50k history ring and returns matching events.
// Query params:
//
//	q     — case-insensitive substring match on message (optional)
//	level — exact level filter: ERROR, WARN, INFO, DEBUG (optional)
//	limit — max results to return, default 500 (optional)
//
// Results are returned newest-first so the browser can render immediately
// without reversing. This endpoint is called on debounced input, not on
// every keystroke, so the 50k scan cost is acceptable.
func (s *Server) handleLogs(c echo.Context) error {
	q := c.QueryParam("q")
	level := c.QueryParam("level")
	limitStr := c.QueryParam("limit")

	limit := 500
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
			limit = n
		}
	}

	results := s.agg.SearchHistory(q, level, limit)
	return c.JSON(http.StatusOK, map[string]interface{}{
		"results": results,
		"count":   len(results),
		"q":       q,
		"level":   level,
		"limit":   limit,
	})
}

// handleIncidentList scans the incidents/ directory and returns a list of
// incident files with their metadata (filename, timestamp, reason, peaks).
// Only reads the first few fields of each file to keep the list response light.
func (s *Server) handleIncidentList(c echo.Context) error {
	entries, err := os.ReadDir("incidents")
	if err != nil {
		if os.IsNotExist(err) {
			return c.JSON(http.StatusOK, map[string]interface{}{
				"incidents": []interface{}{},
			})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	type IncidentMeta struct {
		Filename   string  `json:"filename"`
		Timestamp  string  `json:"timestamp"`
		Reason     string  `json:"reason"`
		PeakEPS    float64 `json:"peak_eps"`
		PeakWPS    float64 `json:"peak_wps"`
		TotalLines int     `json:"total_lines"`
		StartedAt  string  `json:"started_at"`
		EndedAt    string  `json:"ended_at"`
	}

	var incidents []IncidentMeta

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		path := filepath.Join("incidents", entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		// Partially unmarshal — only read the top-level fields we need for the list.
		// The full signal_history and full_context arrays can be huge (30k lines)
		// so we never load them just for the list view.
		var partial struct {
			Reason     string  `json:"reason"`
			PeakEPS    float64 `json:"peak_eps"`
			PeakWPS    float64 `json:"peak_wps"`
			TotalLines int     `json:"total_lines"`
			StartedAt  string  `json:"started_at"`
			EndedAt    string  `json:"ended_at"`
		}
		if err := json.Unmarshal(data, &partial); err != nil {
			continue
		}

		// Extract timestamp from filename: incident_2026-04-04_16-31-28.json
		ts := strings.TrimPrefix(entry.Name(), "incident_")
		ts = strings.TrimSuffix(ts, ".json")
		ts = strings.ReplaceAll(ts, "_", " ")

		incidents = append(incidents, IncidentMeta{
			Filename:   entry.Name(),
			Timestamp:  ts,
			Reason:     partial.Reason,
			PeakEPS:    partial.PeakEPS,
			PeakWPS:    partial.PeakWPS,
			TotalLines: partial.TotalLines,
			StartedAt:  partial.StartedAt,
			EndedAt:    partial.EndedAt,
		})
	}

	// Newest first
	for i, j := 0, len(incidents)-1; i < j; i, j = i+1, j-1 {
		incidents[i], incidents[j] = incidents[j], incidents[i]
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"incidents": incidents,
	})
}

// handleIncident returns the full content of a single incident file.
// The filename comes from the URL parameter — we sanitize it to prevent
// path traversal attacks (e.g. ../../etc/passwd).
func (s *Server) handleIncident(c echo.Context) error {
	filename := filepath.Base(c.Param("file")) // Base() strips any directory components

	// Only allow incident JSON files
	if !strings.HasPrefix(filename, "incident_") || !strings.HasSuffix(filename, ".json") {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid filename"})
	}

	path := filepath.Join("incidents", filename)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "incident not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	// Return raw JSON — browser renders it directly
	return c.JSONBlob(http.StatusOK, data)
}

// handleSnapshot returns a single JSON snapshot for the initial page load —
// so the browser has data to render before the first WS message arrives
// (which could be up to 500ms after connect).
func (s *Server) handleSnapshot(c echo.Context) error {
	snap := s.agg.WebSnapshot()
	return c.JSON(http.StatusOK, snap)
}
