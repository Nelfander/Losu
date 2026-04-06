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
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/nelfander/losu/internal/aggregator"
	"github.com/nelfander/losu/internal/hub"
)

//go:embed static/index.html
var indexHTML []byte

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Server owns the Echo instance, the hub, the aggregator map, and a pointer
// to the currently active aggregator for broadcasting.
type Server struct {
	echo      *echo.Echo
	hub       *hub.Hub
	aggMap    map[string]*aggregator.Aggregator // all aggregators, keyed by log path
	activeAgg unsafe.Pointer                    // *aggregator.Aggregator — swapped atomically on source change
	addr      string
}

// New creates a Server but does not start it.
// aggMap is the full map of path→aggregator. The first entry is the default active one.
func New(addr string, h *hub.Hub, aggMap map[string]*aggregator.Aggregator) *Server {
	e := echo.New()
	e.HideBanner = true

	e.Use(middleware.Recover())
	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogMethod:   true,
		LogURI:      true,
		LogStatus:   true,
		LogLatency:  true,
		LogRemoteIP: true,
		LogError:    true,
		LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
			return nil
		},
	}))

	s := &Server{
		echo:   e,
		hub:    h,
		aggMap: aggMap,
		addr:   addr,
	}

	// Pick the first aggregator as the default active one
	for _, agg := range aggMap {
		atomic.StorePointer(&s.activeAgg, unsafe.Pointer(agg))
		break
	}

	// Routes
	e.GET("/", s.handleIndex)
	e.GET("/ws/stream", s.handleWebSocket)
	e.GET("/api/snapshot", s.handleSnapshot)
	e.GET("/api/inspect", s.handleInspect)
	e.GET("/api/logs", s.handleLogs)
	e.GET("/api/incidents", s.handleIncidentList)
	e.GET("/api/incidents/:file", s.handleIncident)

	return s
}

// getActiveAgg returns the currently active aggregator safely.
func (s *Server) getActiveAgg() *aggregator.Aggregator {
	return (*aggregator.Aggregator)(atomic.LoadPointer(&s.activeAgg))
}

// setActiveAgg swaps the active aggregator atomically — instant, no lock needed.
func (s *Server) setActiveAgg(agg *aggregator.Aggregator) {
	atomic.StorePointer(&s.activeAgg, unsafe.Pointer(agg))
}

// Start begins the hub, broadcaster, and Echo HTTP server.
// Blocks until ctx is cancelled, then shuts down cleanly.
func (s *Server) Start(ctx context.Context) error {
	go s.hub.Run()
	go s.broadcaster(ctx)

	errCh := make(chan error, 1)
	go func() {
		if err := s.echo.Start(s.addr); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.echo.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

// broadcaster ticks every 500ms and broadcasts the active aggregator's snapshot.
// Source switching is instant — the next tick picks up the new aggregator.
func (s *Server) broadcaster(ctx context.Context) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	// Build the sources list once — it never changes after startup
	sources := make([]string, 0, len(s.aggMap))
	for path := range s.aggMap {
		sources = append(sources, path)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			snap := s.getActiveAgg().WebSnapshot()
			// Inject the full sources list so the browser knows all available files
			snap.Sources = sources
			payload, err := json.Marshal(snap)
			if err != nil {
				continue
			}
			s.hub.Broadcast(payload)
		}
	}
}

// handleIndex serves the embedded dashboard.
func (s *Server) handleIndex(c echo.Context) error {
	return c.HTMLBlob(http.StatusOK, indexHTML)
}

// handleWebSocket upgrades the connection, registers it with the hub,
// and starts a read loop to handle incoming messages from the browser.
// Currently handles one message type:
//
//	{"type":"set_source","source":"/logs/app.log"}
//
// On receiving set_source, the active aggregator is swapped atomically.
// The next broadcaster tick (≤500ms) will send the new source's data.
func (s *Server) handleWebSocket(c echo.Context) error {
	conn, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}

	// Register with hub for outgoing broadcasts
	s.hub.ServeClient(conn)

	// Read loop for incoming messages (source switching)
	// Runs in its own goroutine so it doesn't block the handler
	go func() {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return // client disconnected
			}

			var payload struct {
				Type   string `json:"type"`
				Source string `json:"source"`
			}
			if err := json.Unmarshal(msg, &payload); err != nil {
				continue
			}

			if payload.Type == "set_source" {
				if payload.Source == "" || payload.Source == "all" {
					// "all" — pick the first aggregator (combined view not supported in Option B)
					// For now just pick first; future: could add a combined aggregator
					for _, agg := range s.aggMap {
						s.setActiveAgg(agg)
						break
					}
				} else {
					if agg, ok := s.aggMap[payload.Source]; ok {
						s.setActiveAgg(agg)
					}
				}
			}
		}
	}()

	return nil
}

func (s *Server) handleInspect(c echo.Context) error {
	pattern := c.QueryParam("pattern")
	if pattern == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "pattern required"})
	}
	result := s.getActiveAgg().GetInspect(pattern)
	if result == nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "pattern not found"})
	}
	return c.JSON(http.StatusOK, result)
}

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

	results := s.getActiveAgg().SearchHistory(q, level, limit)
	return c.JSON(http.StatusOK, map[string]interface{}{
		"results": results,
		"count":   len(results),
		"q":       q,
		"level":   level,
		"limit":   limit,
	})
}

func (s *Server) handleIncidentList(c echo.Context) error {
	// Optional source filter — only return incidents from this log file
	sourceFilter := c.QueryParam("source")

	entries, err := os.ReadDir("incidents")
	if err != nil {
		if os.IsNotExist(err) {
			return c.JSON(http.StatusOK, map[string]interface{}{"incidents": []interface{}{}})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	type IncidentMeta struct {
		Filename   string  `json:"filename"`
		Timestamp  string  `json:"timestamp"`
		Reason     string  `json:"reason"`
		Source     string  `json:"source"`
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
		var partial struct {
			Reason     string  `json:"reason"`
			Source     string  `json:"source"`
			PeakEPS    float64 `json:"peak_eps"`
			PeakWPS    float64 `json:"peak_wps"`
			TotalLines int     `json:"total_lines"`
			StartedAt  string  `json:"started_at"`
			EndedAt    string  `json:"ended_at"`
		}
		if err := json.Unmarshal(data, &partial); err != nil {
			continue
		}

		// Filter by source if requested
		if sourceFilter != "" && partial.Source != sourceFilter {
			continue
		}
		ts := strings.TrimPrefix(entry.Name(), "incident_")
		ts = strings.TrimSuffix(ts, ".json")
		ts = strings.ReplaceAll(ts, "_", " ")

		incidents = append(incidents, IncidentMeta{
			Filename:   entry.Name(),
			Timestamp:  ts,
			Reason:     partial.Reason,
			Source:     partial.Source,
			PeakEPS:    partial.PeakEPS,
			PeakWPS:    partial.PeakWPS,
			TotalLines: partial.TotalLines,
			StartedAt:  partial.StartedAt,
			EndedAt:    partial.EndedAt,
		})
	}

	for i, j := 0, len(incidents)-1; i < j; i, j = i+1, j-1 {
		incidents[i], incidents[j] = incidents[j], incidents[i]
	}

	return c.JSON(http.StatusOK, map[string]interface{}{"incidents": incidents})
}

func (s *Server) handleIncident(c echo.Context) error {
	filename := filepath.Base(c.Param("file"))
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
	return c.JSONBlob(http.StatusOK, data)
}

func (s *Server) handleSnapshot(c echo.Context) error {
	snap := s.getActiveAgg().WebSnapshot()
	return c.JSON(http.StatusOK, snap)
}
