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
	// Optimization: strings.NewReplacer is pre-compiled and does all replacements in one scan.
	// This is much faster than 3 separate ReplaceAll calls which create 3 temporary strings.
	cleaner := strings.NewReplacer("\x00", "", "\r", "", "\t", " ")
	line := strings.TrimSpace(cleaner.Replace(rawLine.Line))

	// Immediate Exit for empty lines
	if line == "" {
		return model.LogEvent{Level: "IGNORE", Message: ""}
	}

	// --- Optimization: logfmt Fast-Path ---
	// If the line looks like logfmt (contains level= and msg=), we extract them manually.
	// This avoids the overhead of the Regex engine entirely for standard logs.
	lvlIdx := strings.Index(line, "level=")
	msgIdx := strings.Index(line, "msg=")

	if lvlIdx != -1 && msgIdx != -1 {
		// Extract Level
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

		// RECONSTRUCT THE FULL ANALYTIC MESSAGE
		finalMessage := msgFull
		if strings.TrimSpace(remaining) != "" {
			finalMessage = msgFull + " | " + strings.TrimSpace(remaining)
		}

		return model.LogEvent{
			Timestamp: time.Now(),
			Level:     level,
			Message:   finalMessage,
			Source:    rawLine.Source,
		}
	}

	// --- Optimization: Brackets Fast-Path ---
	// Manually handle [INFO] style logs to avoid falling through to Regex
	openBracket := strings.Index(line, "[")
	closeBracket := strings.Index(line, "]")
	if openBracket != -1 && closeBracket > openBracket {
		level := strings.ToUpper(line[openBracket+1 : closeBracket])
		// Check if it's a valid known level to avoid false positives
		if level == "INFO" || level == "WARN" || level == "ERROR" || level == "DEBUG" {
			return model.LogEvent{
				Timestamp: time.Now(),
				Level:     level,
				Message:   strings.TrimSpace(line[closeBracket+1:]),
				Source:    rawLine.Source,
			}
		}
	}

	// If it's not logfmt or clear brackets, we loop through our specific patterns
	for _, probe := range patterns {
		if probe.name == "logfmt-flexible" {
			continue
		}

		matches := probe.regex.FindStringSubmatch(line)
		if len(matches) >= 3 {
			var level, message string
			timestamp := time.Now()

			switch probe.name {
			case "brackets":
				timestamp = parseFlexibleTime(matches[1])
				level = strings.ToUpper(matches[2])
				message = matches[3]
			case "simple":
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
	// ISO8601 (2026-03-08T15:20:45Z)
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t
	}
	//  RFC3339 without Nano
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t
	}
	// Standard Space (2026-03-08 15:04:05)
	if t, err := time.Parse("2006-01-02 15:04:05", raw); err == nil {
		return t
	}
	return time.Now()
}
