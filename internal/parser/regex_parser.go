package parser

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/nelfander/losu/internal/model"
)

// Default format: 2026-03-05 15:04:05 [LEVEL] Message
var logPattern = regexp.MustCompile(`.*?(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2})\s+\[(\w+)\]\s+(.*)$`)

type RegexParser struct{}

func NewRegexParser() *RegexParser {
	return &RegexParser{}
}

func (p *RegexParser) Parse(rawLine model.RawLog) model.LogEvent {
	// This replaces all NULL bytes with nothing, cleaning UTF-16 "noise"
	cleanLine := strings.ReplaceAll(rawLine.Line, "\x00", "")
	line := strings.Trim(cleanLine, " \t\n\r\ufeff")
	fmt.Printf("DEBUG STRING: %q\n", line)
	matches := logPattern.FindStringSubmatch(line)

	// If it doesn't match the format, return a "Malformed" event
	if len(matches) < 4 {
		return model.LogEvent{
			Timestamp: time.Now(),
			Level:     "UNKNOWN",
			Message:   line,
			Source:    rawLine.Source,
		}
	}

	// matches[1] is the date, [2] is level, [3] is message
	ts, err := time.Parse("2006-01-02 15:04:05", matches[1])
	if err != nil {
		ts = time.Now()
	}

	return model.LogEvent{
		Timestamp: ts,
		Level:     strings.ToUpper(matches[2]),
		Message:   matches[3],
		Source:    rawLine.Source,
	}
}
