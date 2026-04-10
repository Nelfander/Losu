// sparkline.go — pure helper functions for the LOSU TUI.
//
// All functions here are stateless and have no side effects.
// They are used by update.go (sparklines, status label) and
// input.go / inspector.go (truncate, getColor).
//
//	truncate       — shorten a string to N chars with "..." suffix
//	getSparkline   — linear-scale bar chart (kept for reference)
//	getSparklineLog — log-scale bar chart (used in production)
//	getStatusLabel — health status string based on EPS and WPS
//	getColor       — map log level to tview color string
package ui

import (
	"math"
	"os"
	"strconv"
	"strings"
)

// truncate shortens a string to l characters, adding "..." if trimmed.
func truncate(s string, l int) string {
	if len(s) > l {
		return s[:l-3] + "..."
	}
	return s
}

// getSparkline renders a fixed-height linear-scale bar chart from a slice of ints.
// Uses cyan blocks by default — caller recolors via strings.ReplaceAll.
// Kept for reference; getSparklineLog is preferred for production use.
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

// getStatusLabel returns a health status string based on EPS and WPS independently.
// Error labels always take priority over warn labels.
// Warn labels only show when errors are below the minor threshold —
// giving early warning of degradation before errors start firing.
//
// Thresholds are read from env so each app can tune to its own baseline.
// These match the .env variable names exactly:
//
//	LOSU_EPS_MINOR       default 0.1   above this → Minor Issues
//	LOSU_EPS_WARN        default 1.0   above this → Unstable
//	LOSU_EPS_CRITICAL    default 5.0   above this → CRITICAL SPIKE
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

	epsCritical := thresh("LOSU_EPS_CRITICAL", 5.0)
	epsWarn := thresh("LOSU_EPS_WARN", 1.0)
	epsMinor := thresh("LOSU_EPS_MINOR", 0.1)
	wpsPreIncident := thresh("LOSU_WPS_PREINCIDENT", 200.0)
	wpsSuspicious := thresh("LOSU_WPS_SUSPICIOUS", 100.0)
	wpsPressure := thresh("LOSU_WPS_PRESSURE", 50.0)

	switch {
	case eps >= epsCritical:
		return "[blink][red]CRITICAL SPIKE"
	case eps >= epsWarn:
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
