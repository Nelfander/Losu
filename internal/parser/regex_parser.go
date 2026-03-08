package parser

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/nelfander/losu/internal/model"
)

var patterns = []struct {
	name  string
	regex *regexp.Regexp
}{
	//  Logfmt (Real app: time=... level=... msg=...)
	{name: "logfmt", regex: regexp.MustCompile(`time=([^\s]+)\s+level=([a-zA-Z]+)\s+msg="?([^"|^\n]+)"?(.*)`)},

	//  Brackets (Test logs: 2026-03-08 [INFO] Message)
	{name: "brackets", regex: regexp.MustCompile(`.*?(\d{4}-\d{2}-\d{2}[\sT]\d{2}:\d{2}:\d{2}).*?\[([a-zA-Z]+)\]\s+(.*)`)},

	//  Simple Space (Legacy: 2026-03-08 15:00:00 ERROR Message)
	{name: "brackets", regex: regexp.MustCompile(`(\d{4}-\d{2}-\d{2}[\sT]\d{2}:\d{2}:\d{2}(?:\.\d+)?Z?)\s+\[([a-zA-Z]+)\]\s+(.*)`)},
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

	// 2. Immediate Exit for empty "Ghost" lines
	if line == "" {
		return model.LogEvent{Level: "IGNORE", Message: ""}
	}
	// Loop through every pattern
	for _, probe := range patterns {
		matches := probe.regex.FindStringSubmatch(line)

		if len(matches) >= 4 {
			// matches[1] = time
			// matches[2] = level
			// matches[3] = message
			// matches[4] = extra metadata (for logfmt)
			// SUCCESS! Found a match
			fmt.Printf("DEBUG: Matched using %s pattern\n", probe.name)

			// Logfmt specific: grab the extra key-values after the message
			msg := matches[3]
			if len(matches) > 4 {
				msg += " " + matches[4]
			}

			return model.LogEvent{
				Timestamp: parseFlexibleTime(matches[1]),
				Level:     strings.ToUpper(matches[2]),
				Message:   strings.TrimSpace(msg),
				Source:    rawLine.Source,
			}
		}
	}

	if !strings.Contains(line, "level=") && !strings.Contains(line, "[") {
		return model.LogEvent{Level: "IGNORE", Message: ""}
	}

	// No patterns matched
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
