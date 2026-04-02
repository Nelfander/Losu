package parser

import (
	"regexp"
	"strings"
	"time"

	"github.com/nelfander/losu/internal/model"
)

// cleaner is pre-compiled ONCE at package level so it is truly shared across
// all Parse calls
var cleaner = strings.NewReplacer("\x00", "", "\r", "", "\t", " ")

var patterns = []struct {
	name  string
	regex *regexp.Regexp
}{
	// Flexible Logfmt: Looks for level= and msg= ANYWHERE in the line.
	// This catches "playingfield-app | time=... level=DEBUG msg=..."
	// NOTE: In practice this pattern is skipped in the hot-path loop because
	// logfmt lines are intercepted by the fast-path below. It remains here
	// as a documented fallback only.
	{
		name:  "logfmt-flexible",
		regex: regexp.MustCompile(`level=([a-zA-Z]+).*?msg="?([^"\n]*)"?(.*)`),
	},

	// Brackets (Test logs: 2026-03-08 [INFO] Message)
	{
		name:  "brackets",
		regex: regexp.MustCompile(`.*?(\d{4}-\d{2}-\d{2}[\sT]\d{2}:\d{2}:\d{2}).*?\[([a-zA-Z]+)\]\s+(.*)`),
	},

	// Simple Space (Legacy: 2026-03-08 15:00:00 ERROR Message)
	{
		name:  "simple",
		regex: regexp.MustCompile(`(\d{4}-\d{2}-\d{2}.*?)\s+([a-zA-Z]+)\s+(.*)`),
	},

	// If nothing else matches, grab the whole line and
	// treat the whole line as the message and label it "UNKNOWN".
	{name: "catch-all", regex: regexp.MustCompile(`^(.*)$`)},
}

// knownLevels is a set used by the brackets fast-path to reject false positives
// (e.g. a URL fragment like [section] being mistaken for a log level).
var knownLevels = map[string]struct{}{
	"INFO":  {},
	"WARN":  {},
	"ERROR": {},
	"DEBUG": {},
}

type RegexParser struct{}

func NewRegexParser() *RegexParser {
	return &RegexParser{}
}

func (p *RegexParser) Parse(rawLine model.RawLog) model.LogEvent {
	line := strings.TrimSpace(cleaner.Replace(rawLine.Line))

	// Immediate exit for empty lines
	if line == "" {
		return model.LogEvent{Level: "IGNORE", Message: ""}
	}

	// --- Fast-Path: logfmt ---
	// If the line contains both level= and msg= we extract them manually,
	// avoiding the Regex engine entirely for the common structured-log case.
	// We also look for time= here so the timestamp is NOT silently discarded
	// (the original fast-path always fell back to time.Now(), losing real timestamps).
	lvlIdx := strings.Index(line, "level=")
	msgIdx := strings.Index(line, "msg=")

	if lvlIdx != -1 && msgIdx != -1 {
		// --- Extract Timestamp  ---
		timestamp := time.Now()
		if timeIdx := strings.Index(line, "time="); timeIdx != -1 {
			timePart := line[timeIdx+5:]
			if strings.HasPrefix(timePart, "\"") {
				if endQ := strings.Index(timePart[1:], "\""); endQ != -1 {
					timestamp = parseFlexibleTime(timePart[1 : endQ+1])
				}
			} else {
				// Bare value ends at the next whitespace.
				if spIdx := strings.IndexByte(timePart, ' '); spIdx != -1 {
					timestamp = parseFlexibleTime(timePart[:spIdx])
				} else {
					timestamp = parseFlexibleTime(timePart)
				}
			}
		}

		// --- Extract Level ---
		lvlPart := line[lvlIdx+6:]
		lvlEnd := strings.IndexAny(lvlPart, " \t,")
		if lvlEnd == -1 {
			lvlEnd = len(lvlPart)
		}
		level := strings.ToUpper(lvlPart[:lvlEnd])

		// --- Extract Message ---
		message := line[msgIdx+4:]
		var msgFull string
		var remaining string

		if strings.HasPrefix(message, "\"") {
			endQuote := strings.Index(message[1:], "\"")
			if endQuote != -1 {
				msgFull = message[1 : endQuote+1]
				remaining = message[endQuote+2:] // Everything AFTER the closing quote.
			} else {
				msgFull = message // Fallback: malformed quote, take the rest.
			}
		} else {
			// IndexByte for a single-byte delimiter = slightly faster than IndexAny.
			spaceIdx := strings.IndexByte(message, ' ')
			if spaceIdx != -1 {
				msgFull = message[:spaceIdx]
				remaining = message[spaceIdx+1:] // Everything AFTER the first word.
			} else {
				msgFull = message
			}
		}

		// Reconstruct the full analytic message, appending any trailing
		// key=value fields after the message so they aren't silently dropped.
		finalMessage := msgFull
		if strings.TrimSpace(remaining) != "" {
			finalMessage = msgFull + " | " + strings.TrimSpace(remaining)
		}

		return model.LogEvent{
			Timestamp: timestamp,
			Level:     level,
			Message:   finalMessage,
			Source:    rawLine.Source,
		}
	}

	// --- Fast-Path: Brackets ---
	// Manually handle [INFO] style logs to avoid falling through to Regex.
	// Uses the knownLevels map to guard against false positives.
	openBracket := strings.Index(line, "[")
	closeBracket := strings.Index(line, "]")
	if openBracket != -1 && closeBracket > openBracket {
		level := strings.ToUpper(line[openBracket+1 : closeBracket])
		if _, ok := knownLevels[level]; ok {
			// Attempt to parse a leading timestamp (original fast-path always
			// used time.Now() here, discarding the real timestamp in the line).
			timestamp := time.Now()
			if openBracket > 0 {
				timestamp = parseFlexibleTime(strings.TrimSpace(line[:openBracket]))
			}
			return model.LogEvent{
				Timestamp: timestamp,
				Level:     level,
				Message:   strings.TrimSpace(line[closeBracket+1:]),
				Source:    rawLine.Source,
			}
		}
	}

	// --- Regex fallback ---
	// Reached only when neither fast-path matched. Iterates the compiled
	// pattern list; logfmt-flexible is skipped because that case is already
	// handled above, and re-running it here would double-process some lines.
	for _, probe := range patterns {
		if probe.name == "logfmt-flexible" {
			continue
		}

		matches := probe.regex.FindStringSubmatch(line)
		if len(matches) == 0 {
			continue
		}

		var level, message string
		timestamp := time.Now()

		switch probe.name {
		case "brackets":
			// Regex variant: needs at least 4 subgroups (full + 3 captures).
			if len(matches) < 4 {
				continue
			}
			timestamp = parseFlexibleTime(matches[1])
			level = strings.ToUpper(matches[2])
			message = matches[3]
		case "simple":
			// Regex variant: needs at least 4 subgroups (full + 3 captures).
			if len(matches) < 4 {
				continue
			}
			timestamp = parseFlexibleTime(matches[1])
			level = strings.ToUpper(matches[2])
			message = matches[3]
		case "catch-all":
			// catch-all only has 1 capture group, so matches[1] is the whole line.
			// The original code checked len(matches) >= 3 which is always false here,
			// meaning catch-all silently fell through to the bottom fallback every time.
			level = "UNKNOWN"
			message = matches[1]
		}

		return model.LogEvent{
			Timestamp: timestamp,
			Level:     level,
			Message:   strings.TrimSpace(message),
			Source:    rawLine.Source,
		}
	}

	// Fallback: should rarely be reached now that catch-all is handled correctly above.
	return model.LogEvent{
		Timestamp: time.Now(),
		Level:     "UNKNOWN",
		Message:   line,
		Source:    rawLine.Source,
	}
}

// parseFlexibleTime tries a sequence of common timestamp formats, returning
// time.Now() if none match. Formats are ordered by expected frequency so the
// common case (RFC3339Nano) exits early without trying the others.
func parseFlexibleTime(raw string) time.Time {
	// ISO 8601 with sub-second precision (2026-03-08T15:20:45.000Z)
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t
	}
	// RFC3339 without nanoseconds (2026-03-08T15:20:45Z)
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t
	}
	// Standard space-separated (2026-03-08 15:04:05)
	if t, err := time.Parse("2006-01-02 15:04:05", raw); err == nil {
		return t
	}
	return time.Now()
}
