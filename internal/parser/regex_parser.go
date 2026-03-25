package parser

import (
	"regexp"
	"strings"
	"time"

	"github.com/nelfander/losu/internal/model"
)

var patterns = []struct {
	name  string
	regex *regexp.Regexp
}{
	//  Flexible Logfmt: Looks for level= and msg= ANYWHERE in the line.
	// This catches "playingfield-app | time=... level=DEBUG msg=..."
	{
		name:  "logfmt-flexible",
		regex: regexp.MustCompile(`level=([a-zA-Z]+).*?msg="?([^"\n]*)"?(.*)`),
	},
	//{name: "logfmt", regex: regexp.MustCompile(`time=([^\s]+)\s+level=([a-zA-Z]+)\s+msg="?([^"|^\n]+)"?(.*)`)},

	//  Brackets (Test logs: 2026-03-08 [INFO] Message)
	{
		name:  "brackets",
		regex: regexp.MustCompile(`.*?(\d{4}-\d{2}-\d{2}[\sT]\d{2}:\d{2}:\d{2}).*?\[([a-zA-Z]+)\]\s+(.*)`),
	},

	//  Simple Space (Legacy: 2026-03-08 15:00:00 ERROR Message)
	{
		name:  "simple",
		regex: regexp.MustCompile(`(\d{4}-\d{2}-\d{2}.*?)\s+([a-zA-Z]+)\s+(.*)`),
	},

	// If nothing else matches, grab the whole line and
	// treat the whole line as the message and label it "UNKNOWN".
	{name: "catch-all", regex: regexp.MustCompile(`^(.*)$`)},
}

type RegexParser struct{}

func NewRegexParser() *RegexParser {
	return &RegexParser{}
}
func (p *RegexParser) Parse(rawLine model.RawLog) model.LogEvent {
	// strings.ReplaceAll is optimized in assembly and much faster for specific removals.
	line := rawLine.Line
	line = strings.ReplaceAll(line, "\x00", "") // Remove null bytes
	line = strings.ReplaceAll(line, "\r", "")   // Remove carriage returns
	line = strings.ReplaceAll(line, "\t", " ")  // Convert tabs to spaces for easier indexing
	line = strings.TrimSpace(line)

	// Immediate Exit for empty lines
	if line == "" {
		return model.LogEvent{Level: "IGNORE", Message: ""}
	}

	// If the line looks like logfmt (contains level= and msg=), we extract them manually.
	// This avoids the overhead of the Regex engine entirely for standard logs.
	lvlIdx := strings.Index(line, "level=")
	msgIdx := strings.Index(line, "msg=")

	if lvlIdx != -1 && msgIdx != -1 {
		//  Extract Level
		lvlPart := line[lvlIdx+6:]
		lvlEnd := strings.IndexAny(lvlPart, " \t,")
		if lvlEnd == -1 {
			lvlEnd = len(lvlPart)
		}
		level := strings.ToUpper(lvlPart[:lvlEnd])

		// Extract Message
		message := line[msgIdx+4:]
		var msgFull string
		var remaining string

		if strings.HasPrefix(message, "\"") {
			endQuote := strings.Index(message[1:], "\"")
			if endQuote != -1 {
				msgFull = message[1 : endQuote+1]
				remaining = message[endQuote+2:] // Grab everything AFTER the closing quote
			} else {
				msgFull = message // Fallback
			}
		} else {
			spaceIdx := strings.IndexAny(message, " \t")
			if spaceIdx != -1 {
				msgFull = message[:spaceIdx]
				remaining = message[spaceIdx+1:] // Grab everything AFTER the first word
			} else {
				msgFull = message
			}
		}

		// 3. RECONSTRUCT THE FULL ANALYTIC MESSAGE
		// Take the primary message and append any "remaining" key-value pairs
		finalMessage := msgFull
		if strings.TrimSpace(remaining) != "" {
			// Clean up the remaining string to remove leftover level= parts
			// This keeps log lines "analytic"
			finalMessage = msgFull + " | " + strings.TrimSpace(remaining)
		}

		return model.LogEvent{
			Timestamp: time.Now(),
			Level:     level,
			Message:   finalMessage,
			Source:    rawLine.Source,
		}
	}

	// If it's not logfmt, we loop through our specific patterns (Brackets, Simple, etc.)
	for _, probe := range patterns {
		//  Already handled logfmt-flexible above, so skip it here
		if probe.name == "logfmt-flexible" {
			continue
		}

		matches := probe.regex.FindStringSubmatch(line)
		if len(matches) >= 3 {
			var level, message string
			timestamp := time.Now()

			switch probe.name {
			case "brackets":
				// matches[1] = Timestamp, [2] = Level, [3] = Message
				timestamp = parseFlexibleTime(matches[1])
				level = strings.ToUpper(matches[2])
				message = matches[3]
			case "simple":
				// matches[1] = Timestamp, [2] = Level, [3] = Message
				timestamp = parseFlexibleTime(matches[1])
				level = strings.ToUpper(matches[2])
				message = matches[3]
			case "catch-all":
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
	}

	// Fallback
	return model.LogEvent{
		Timestamp: time.Now(),
		Level:     "UNKNOWN",
		Message:   line,
		Source:    rawLine.Source,
	}
}

// Helper to handle multiple time formats
func parseFlexibleTime(raw string) time.Time {
	// Try ISO8601 (2026-03-08T15:20:45Z)
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t
	}
	// Try RFC3339 without Nano
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t
	}
	// Try Standard Space (2026-03-08 15:04:05)
	if t, err := time.Parse("2006-01-02 15:04:05", raw); err == nil {
		return t
	}
	return time.Now()
}
