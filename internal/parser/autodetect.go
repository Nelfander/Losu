package parser

import (
	"bufio"
	"os"
	"strings"
	"time"
)

// DetectParser reads the first few non-empty lines of the log file and
// returns the most appropriate Parser implementation.
//
// Detection order (first match wins):
//  1. JSON   — line starts with '{'
//  2. logfmt — line contains both 'level=' and 'msg='
//  3. default → RegexParser (handles bracketed, simple space, catch-all)
//
// Detection happens once at startup. Real applications use a single
// consistent log format — mixing formats in one file is not supported.
// If the file doesn't exist yet (will be created by the app), defaults
// to RegexParser and lets the fast-path handle whatever arrives.
func DetectParser(logPath string) Parser {
	format := detectFormat(logPath)
	switch format {
	case "json":
		return NewJSONParser()
	default:
		// RegexParser handles logfmt, bracketed, simple, and catch-all
		return NewRegexParser()
	}
}

// detectFormat peeks at the first 10 non-empty lines of the file and
// returns a format string. Uses majority vote across the sample lines
// so a stray malformed line at the top doesn't pick the wrong parser.
//
// If the file doesn't exist or is empty at startup (log generator hasn't
// written yet), retries for up to 5 seconds before defaulting to regex.
// This handles the common case where LOSU starts before the app does.
func detectFormat(logPath string) string {
	const (
		sampleSize = 10
		maxWait    = 5 * time.Second
		retryEvery = 200 * time.Millisecond
	)

	deadline := time.Now().Add(maxWait)

	for time.Now().Before(deadline) {
		f, err := os.Open(logPath)
		if err != nil {
			// File doesn't exist yet — wait and retry
			time.Sleep(retryEvery)
			continue
		}

		scanner := bufio.NewScanner(f)
		jsonVotes := 0
		totalLines := 0

		for scanner.Scan() && totalLines < sampleSize {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			totalLines++
			if line[0] == '{' {
				jsonVotes++
			}
		}
		f.Close()

		if totalLines == 0 {
			// File exists but empty — wait for generator to write something
			time.Sleep(retryEvery)
			continue
		}

		// Got enough lines — majority vote
		if jsonVotes > totalLines/2 {
			return "json"
		}
		return "regex"
	}

	// Timeout — default to regex
	return "regex"
}
