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
	{name: "logfmt-flexible", regex: regexp.MustCompile(`level=([a-zA-Z]+).*?msg="?([^"\n]*)"?`)},
	//{name: "logfmt", regex: regexp.MustCompile(`time=([^\s]+)\s+level=([a-zA-Z]+)\s+msg="?([^"|^\n]+)"?(.*)`)},

	//  Brackets (Test logs: 2026-03-08 [INFO] Message)
	{name: "brackets", regex: regexp.MustCompile(`.*?(\d{4}-\d{2}-\d{2}[\sT]\d{2}:\d{2}:\d{2}).*?\[([a-zA-Z]+)\]\s+(.*)`)},

	//  Simple Space (Legacy: 2026-03-08 15:00:00 ERROR Message)
	{name: "simple", regex: regexp.MustCompile(`(\d{4}-\d{2}-\d{2}.*?)\s+([a-zA-Z]+)\s+(.*)`)},

	// If nothing else matches, grab the whole line and
	// treat the whole line as the message and label it "UNKNOWN".
	{name: "catch-all", regex: regexp.MustCompile(`^(.*)$`)},
}

type RegexParser struct{}

func NewRegexParser() *RegexParser {
	return &RegexParser{}
}

func (p *RegexParser) Parse(rawLine model.RawLog) model.LogEvent {
	// Heavy-duty cleaning: Remove Nulls, Tabs, and Newlines
	cleanLine := strings.Map(func(r rune) rune {
		if r == '\x00' || r == '\r' || r == '\n' || r == '\t' {
			return -1 // Drop these characters
		}
		return r
	}, rawLine.Line)

	line := strings.TrimSpace(cleanLine)

	// Immediate Exit for empty lines
	if line == "" {
		return model.LogEvent{Level: "IGNORE", Message: ""}
	}
	// Loop through every pattern
	for _, probe := range patterns {
		matches := probe.regex.FindStringSubmatch(line)

		if len(matches) >= 3 {
			// Assume the last two are always Level and Message
			level := strings.ToUpper(matches[len(matches)-2])
			message := matches[len(matches)-1]

			// If the pattern has a timestamp (group 1), parse it, else use Now()
			timestamp := time.Now()
			if len(matches) >= 4 {
				timestamp = parseFlexibleTime(matches[1])
			}

			return model.LogEvent{
				Timestamp: timestamp,
				Level:     level,
				Message:   strings.TrimSpace(message),
				Source:    rawLine.Source,
			}
		}
	}

	// If we are here, it means no pattern matched, but it's NOT a ghost line.
	// Return it as UNKNOWN so we see EVERYTHING.
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
