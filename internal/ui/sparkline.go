// sparkline.go — pure helper functions for the LOSU TUI.
//
// All functions here are stateless and have no side effects.
// They are used by update.go (sparklines, status label) and
// input.go / inspector.go (truncate, getColor).
//
//	truncate        — shorten a string to N chars with "..." suffix
//	getSparklineLog — log-scale bar chart with Braille chars + Y-axis labels
//	getStatusLabel  — health status string based on EPS and WPS
//	getColor        — map log level to tview color string
package ui

import (
	"fmt"
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

// getSparklineLog renders a log-scale area chart using block characters.
//
// Uses range normalization: values are spread relative to (min, max) in the
// window rather than (0, max). This means even at high sustained throughput
// the chart shows variation instead of a solid wall.
// Y-axis shows real peak and min values so nothing is misleading.
func getSparklineLog(data []int, height int, color string, maxWidth int) string {
	if len(data) == 0 {
		return ""
	}

	// Scale into fixed array — maxTrend is 60, no heap allocation needed
	var scaled [60]float64
	n := len(data)
	if n > 60 {
		n = 60
	}
	// Clamp to panel width to prevent right-border overflow corruption
	if maxWidth > 0 && n > maxWidth {
		n = maxWidth
		data = data[len(data)-n:]
	}

	// Log-transform first
	maxVal := 0.0
	minVal := math.MaxFloat64
	for i := 0; i < n; i++ {
		s := math.Log1p(float64(data[i]))
		scaled[i] = s
		if s > maxVal {
			maxVal = s
		}
		if s < minVal {
			minVal = s
		}
	}

	// Range-normalize: spread values relative to (min, max) in window.
	// Falls back to absolute if range is zero (perfectly flat signal).
	rangeVal := maxVal - minVal
	if rangeVal < 0.01 {
		// Flat signal — use absolute scale so we don't amplify noise
		minVal = 0
		rangeVal = maxVal
		if rangeVal == 0 {
			rangeVal = 1
		}
	}
	for i := 0; i < n; i++ {
		scaled[i] = (scaled[i] - minVal) / rangeVal
	}

	// Y-axis: show real (un-logged) peak and min values
	peakReal := int(math.Round(math.Expm1(maxVal)))
	minReal := int(math.Round(math.Expm1(minVal)))
	yLabel := fmt.Sprintf("%d", peakReal)
	yWidth := len(yLabel)

	var b strings.Builder
	b.Grow(height * (yWidth + 2 + n*10))

	for row := height; row >= 1; row-- {
		switch row {
		case height:
			b.WriteString(fmt.Sprintf("[white]%*d ", yWidth, peakReal))
		case 1:
			b.WriteString(fmt.Sprintf("[white]%*d ", yWidth, minReal))
		default:
			b.WriteString(strings.Repeat(" ", yWidth+1))
		}

		rowTop := float64(row) / float64(height)
		rowBot := float64(row-1) / float64(height)
		bandH := rowTop - rowBot

		b.WriteString("[" + color + "]")
		for i := 0; i < n; i++ {
			v := scaled[i]
			if v <= rowBot {
				b.WriteString(" ")
			} else if v >= rowTop {
				b.WriteString("█")
			} else {
				frac := (v - rowBot) / bandH
				if frac >= 0.5 {
					b.WriteString("▄")
				} else {
					b.WriteString("░")
				}
			}
		}
		if row > 1 {
			b.WriteByte('\n')
		}
	}

	return b.String()
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
