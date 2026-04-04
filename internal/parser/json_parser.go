package parser

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/nelfander/losu/internal/model"
)

// JSONParser handles structured JSON log lines.
// Supports common field name conventions across major logging libraries:
//
//	level/severity  → log level (zerolog, zap, slog, winston, pino)
//	msg/message     → log message
//	time/timestamp/ts/@timestamp → event timestamp
//
// Any unrecognised JSON is passed through as the raw message with level UNKNOWN.
type JSONParser struct{}

func NewJSONParser() *JSONParser {
	return &JSONParser{}
}

func (p *JSONParser) Parse(raw model.RawLog) model.LogEvent {
	line := strings.TrimSpace(raw.Line)

	if line == "" {
		return model.LogEvent{Level: "IGNORE", Message: ""}
	}

	// Fast exit for non-JSON lines — if it doesn't start with '{' don't
	// waste time trying to unmarshal it.
	if len(line) == 0 || line[0] != '{' {
		return model.LogEvent{
			Timestamp: time.Now(),
			Level:     "UNKNOWN",
			Message:   line,
			Source:    raw.Source,
		}
	}

	// Unmarshal into a generic map so we can probe arbitrary field names.
	// Using map[string]json.RawMessage defers parsing of values — we only
	// fully parse the fields we actually need (level, msg, time).
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &fields); err != nil {
		return model.LogEvent{
			Timestamp: time.Now(),
			Level:     "UNKNOWN",
			Message:   line,
			Source:    raw.Source,
		}
	}

	level := extractString(fields, "level", "severity", "lvl")
	message := extractString(fields, "msg", "message", "error", "err")
	timeStr := extractString(fields, "time", "timestamp", "ts", "@timestamp")

	// Normalise level to uppercase
	level = strings.ToUpper(strings.TrimSpace(level))
	if level == "" {
		level = "INFO"
	}

	// Parse timestamp — try common formats
	ts := time.Now()
	if timeStr != "" {
		ts = parseFlexibleTime(timeStr)
	}

	// Always append extra fields as context so nothing is silently dropped.
	// e.g. {"msg":"HTTP request finished","status":200,"duration":663}
	// becomes: "HTTP request finished | status=200 | duration=663"
	// This matches the logfmt generator output and keeps the fingerprinter happy.
	extras := buildFallbackMessage(fields)
	if message == "" {
		// No msg field at all — use the extras as the full message
		message = extras
	} else if extras != "" {
		message = message + " | " + extras
	}

	return model.LogEvent{
		Timestamp: ts,
		Level:     level,
		Message:   strings.TrimSpace(message),
		Source:    raw.Source,
	}
}

// extractString looks for the first matching key in the field map and
// returns its string value. Keys are checked in order so priority is
// left-to-right (e.g. "msg" preferred over "message").
func extractString(fields map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		if raw, ok := fields[key]; ok {
			// Try unquoting as a JSON string first
			var s string
			if err := json.Unmarshal(raw, &s); err == nil {
				return s
			}
			// Fallback: return the raw JSON value as a string (handles numbers, bools)
			return strings.Trim(string(raw), `"`)
		}
	}
	return ""
}

// buildFallbackMessage serializes all non-standard fields into a readable
// key=value string so no data is silently dropped.
func buildFallbackMessage(fields map[string]json.RawMessage) string {
	// Skip the fields we already consumed
	skip := map[string]bool{
		"level": true, "severity": true, "lvl": true,
		"msg": true, "message": true,
		"time": true, "timestamp": true, "ts": true, "@timestamp": true,
	}

	var parts []string
	for k, v := range fields {
		if skip[k] {
			continue
		}
		parts = append(parts, k+"="+strings.Trim(string(v), `"`))
	}

	return strings.Join(parts, " | ")
}
