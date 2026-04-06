package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nelfander/losu/internal/model"
)

/*
TestRegexParser_LogfmtFastPath verifies the manual string slicing logic.

This now tests that we correctly extract the 'time=' field and preserve
real timestamps instead of defaulting to time.Now().
*/
func TestRegexParser_LogfmtFastPath(t *testing.T) {
	p := NewRegexParser()

	tests := []struct {
		name      string
		input     string
		expLevel  string
		expMsg    string
		checkTime bool
	}{
		{
			name:      "Logfmt with Timestamp",
			input:     `time="2026-03-08T15:20:45Z" level=info msg=hello`,
			expLevel:  "INFO",
			expMsg:    "hello",
			checkTime: true,
		},
		{
			name:      "Quoted Logfmt with Extra",
			input:     `level=error msg="database failure" retry=true`,
			expLevel:  "ERROR",
			expMsg:    "database failure | retry=true",
			checkTime: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := p.Parse(model.RawLog{Line: tt.input})
			if res.Level != tt.expLevel || !strings.Contains(res.Message, tt.expMsg) {
				t.Errorf("Logfmt Fail [%s]\nGot: %s:%s\nExp: %s:%s",
					tt.name, res.Level, res.Message, tt.expLevel, tt.expMsg)
			}

			if tt.checkTime {
				if res.Timestamp.Year() != 2026 || res.Timestamp.Month() != 3 {
					t.Errorf("Timestamp was not parsed correctly, got: %v", res.Timestamp)
				}
			}
		})
	}
}

/*
TestRegexParser_BracketsFastPath verifies that the map-based level check
works and that it attempts to parse leading timestamps.
*/
func TestRegexParser_BracketsFastPath(t *testing.T) {
	p := NewRegexParser()

	input := "2026-03-08 15:00:00 [ERROR] Connection lost"
	res := p.Parse(model.RawLog{Line: input})

	if res.Level != "ERROR" || res.Timestamp.Year() != 2026 {
		t.Errorf("Brackets fast-path failed. Level: %s, Time: %v", res.Level, res.Timestamp)
	}

	falsePositive := "See documentation [section] for details"
	res2 := p.Parse(model.RawLog{Line: falsePositive})
	if res2.Level == "SECTION" {
		t.Error("False positive: brackets logic caught a non-level word")
	}
}

/*
TestRegexParser_CatchAll verifies the fix for the subgroup length bug.
The previous version required len(matches) >= 3, which broke the catch-all.
*/
func TestRegexParser_CatchAll(t *testing.T) {
	p := NewRegexParser()
	input := "Raw unformatted system message"

	res := p.Parse(model.RawLog{Line: input})

	if res.Level != "UNKNOWN" || res.Message != input {
		t.Errorf("Catch-all failed. Got Level: %s, Msg: %q", res.Level, res.Message)
	}
}

// ── JSON Parser Tests ─────────────────────────────────────────────────────────

func TestJSONParser_StandardFields(t *testing.T) {
	p := NewJSONParser()

	tests := []struct {
		name     string
		input    string
		expLevel string
		expMsg   string
	}{
		{
			name:     "zerolog style",
			input:    `{"level":"error","message":"db timeout","duration":42}`,
			expLevel: "ERROR",
			expMsg:   "db timeout",
		},
		{
			name:     "zap/slog style — msg field",
			input:    `{"level":"warn","msg":"high memory","threshold":89}`,
			expLevel: "WARN",
			expMsg:   "high memory",
		},
		{
			name:     "severity field instead of level",
			input:    `{"severity":"ERROR","msg":"connection refused"}`,
			expLevel: "ERROR",
			expMsg:   "connection refused",
		},
		{
			name:     "lvl field",
			input:    `{"lvl":"debug","msg":"cache hit","key":"user_123"}`,
			expLevel: "DEBUG",
			expMsg:   "cache hit",
		},
		{
			name:     "lowercase level normalised to uppercase",
			input:    `{"level":"info","msg":"request finished"}`,
			expLevel: "INFO",
			expMsg:   "request finished",
		},
		{
			name:     "missing level defaults to INFO",
			input:    `{"msg":"heartbeat"}`,
			expLevel: "INFO",
			expMsg:   "heartbeat",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := p.Parse(model.RawLog{Line: tt.input})
			if res.Level != tt.expLevel {
				t.Errorf("level: got %q want %q", res.Level, tt.expLevel)
			}
			if !strings.Contains(res.Message, tt.expMsg) {
				t.Errorf("message: got %q want it to contain %q", res.Message, tt.expMsg)
			}
		})
	}
}

func TestJSONParser_ExtraFieldsPreserved(t *testing.T) {
	p := NewJSONParser()

	// Extra fields should be appended as key=value context
	// so no data is silently dropped and the fingerprinter can cluster them
	input := `{"level":"info","msg":"HTTP request finished","status":200,"duration":663}`
	res := p.Parse(model.RawLog{Line: input})

	if res.Level != "INFO" {
		t.Errorf("level: got %q want INFO", res.Level)
	}
	if !strings.Contains(res.Message, "HTTP request finished") {
		t.Errorf("base message missing: %q", res.Message)
	}
	if !strings.Contains(res.Message, "status=200") {
		t.Errorf("status field missing from message: %q", res.Message)
	}
	if !strings.Contains(res.Message, "duration=663") {
		t.Errorf("duration field missing from message: %q", res.Message)
	}
}

func TestJSONParser_TimestampParsing(t *testing.T) {
	p := NewJSONParser()

	tests := []struct {
		name     string
		input    string
		expYear  int
		expMonth int
	}{
		{
			name:     "time field RFC3339",
			input:    `{"time":"2026-04-05T13:00:00Z","level":"info","msg":"ok"}`,
			expYear:  2026,
			expMonth: 4,
		},
		{
			name:     "timestamp field",
			input:    `{"timestamp":"2026-04-05T13:00:00Z","level":"info","msg":"ok"}`,
			expYear:  2026,
			expMonth: 4,
		},
		{
			name:     "ts field",
			input:    `{"ts":"2026-04-05T13:00:00Z","level":"info","msg":"ok"}`,
			expYear:  2026,
			expMonth: 4,
		},
		{
			name:     "@timestamp field (elasticsearch style)",
			input:    `{"@timestamp":"2026-04-05T13:00:00Z","level":"info","msg":"ok"}`,
			expYear:  2026,
			expMonth: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := p.Parse(model.RawLog{Line: tt.input})
			if res.Timestamp.Year() != tt.expYear || int(res.Timestamp.Month()) != tt.expMonth {
				t.Errorf("timestamp: got %v want year=%d month=%d",
					res.Timestamp, tt.expYear, tt.expMonth)
			}
		})
	}
}

func TestJSONParser_EdgeCases(t *testing.T) {
	p := NewJSONParser()

	tests := []struct {
		name     string
		input    string
		expLevel string
	}{
		{
			name:     "empty object",
			input:    `{}`,
			expLevel: "INFO", // defaults to INFO when no level field
		},
		{
			name:     "non-JSON line falls through as UNKNOWN",
			input:    "this is not json at all",
			expLevel: "UNKNOWN",
		},
		{
			name:     "empty line returns IGNORE",
			input:    "",
			expLevel: "IGNORE",
		},
		{
			name:     "malformed JSON returns UNKNOWN",
			input:    `{"level":"error","msg":`,
			expLevel: "UNKNOWN",
		},
		{
			name:     "numeric status field preserved",
			input:    `{"level":"error","msg":"request failed","status":500}`,
			expLevel: "ERROR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := p.Parse(model.RawLog{Line: tt.input})
			if res.Level != tt.expLevel {
				t.Errorf("level: got %q want %q", res.Level, tt.expLevel)
			}
		})
	}
}

func TestJSONParser_NoMsgFieldFallsBackToExtras(t *testing.T) {
	p := NewJSONParser()

	// When there is no msg/message field, extra fields become the message
	input := `{"level":"error","code":500,"path":"/api/users"}`
	res := p.Parse(model.RawLog{Line: input})

	if res.Level != "ERROR" {
		t.Errorf("level: got %q want ERROR", res.Level)
	}
	if res.Message == "" {
		t.Error("message should not be empty when extra fields exist")
	}
	// Should contain the extra fields since there's no msg
	if !strings.Contains(res.Message, "code=500") && !strings.Contains(res.Message, "path=") {
		t.Errorf("extra fields not in message: %q", res.Message)
	}
}

func TestJSONParser_SourceFieldPreserved(t *testing.T) {
	p := NewJSONParser()

	input := `{"level":"info","msg":"started"}`
	res := p.Parse(model.RawLog{Line: input, Source: "/var/log/app.log"})

	if res.Source != "/var/log/app.log" {
		t.Errorf("source: got %q want /var/log/app.log", res.Source)
	}
}

// ── Autodetect Tests ──────────────────────────────────────────────────────────

// writeLines is a helper that writes lines to a temp file and returns the path.
func writeLines(t *testing.T, lines []string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "losu-detect-*.log")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	for _, line := range lines {
		f.WriteString(line + "\n")
	}
	f.Close()
	return f.Name()
}

func TestDetectParser_AllJSON(t *testing.T) {
	path := writeLines(t, []string{
		`{"level":"info","msg":"started"}`,
		`{"level":"error","msg":"db timeout"}`,
		`{"level":"warn","msg":"high memory"}`,
	})

	p := DetectParser(path)
	if _, ok := p.(*JSONParser); !ok {
		t.Errorf("expected JSONParser, got %T", p)
	}
}

func TestDetectParser_AllLogfmt(t *testing.T) {
	path := writeLines(t, []string{
		`time="2026-04-05T13:00:00Z" level=info msg="started"`,
		`time="2026-04-05T13:00:01Z" level=error msg="db timeout"`,
		`time="2026-04-05T13:00:02Z" level=warn msg="high memory"`,
	})

	p := DetectParser(path)
	if _, ok := p.(*RegexParser); !ok {
		t.Errorf("expected RegexParser, got %T", p)
	}
}

func TestDetectParser_MajorityJSON(t *testing.T) {
	// 7 JSON lines, 3 logfmt — majority is JSON
	path := writeLines(t, []string{
		`{"level":"info","msg":"a"}`,
		`{"level":"info","msg":"b"}`,
		`{"level":"info","msg":"c"}`,
		`{"level":"info","msg":"d"}`,
		`{"level":"info","msg":"e"}`,
		`{"level":"info","msg":"f"}`,
		`{"level":"info","msg":"g"}`,
		`time="2026-04-05T13:00:00Z" level=info msg="h"`,
		`time="2026-04-05T13:00:00Z" level=info msg="i"`,
		`time="2026-04-05T13:00:00Z" level=info msg="j"`,
	})

	p := DetectParser(path)
	if _, ok := p.(*JSONParser); !ok {
		t.Errorf("expected JSONParser for 7/10 JSON majority, got %T", p)
	}
}

func TestDetectParser_MajorityLogfmt(t *testing.T) {
	// 3 JSON, 7 logfmt — majority is logfmt
	path := writeLines(t, []string{
		`{"level":"info","msg":"a"}`,
		`{"level":"info","msg":"b"}`,
		`{"level":"info","msg":"c"}`,
		`time="2026-04-05T13:00:00Z" level=info msg="d"`,
		`time="2026-04-05T13:00:00Z" level=info msg="e"`,
		`time="2026-04-05T13:00:00Z" level=info msg="f"`,
		`time="2026-04-05T13:00:00Z" level=info msg="g"`,
		`time="2026-04-05T13:00:00Z" level=info msg="h"`,
		`time="2026-04-05T13:00:00Z" level=info msg="i"`,
		`time="2026-04-05T13:00:00Z" level=info msg="j"`,
	})

	p := DetectParser(path)
	if _, ok := p.(*RegexParser); !ok {
		t.Errorf("expected RegexParser for 3/10 JSON minority, got %T", p)
	}
}

func TestDetectParser_EmptyFile(t *testing.T) {
	// Empty file — should default to regex without hanging
	path := writeLines(t, []string{})

	// detectFormat has a 5s retry loop — pass a non-existent path
	// so it times out immediately via the deadline
	// For empty file we need to pass a real path but with no content
	p := DetectParser(path)
	if _, ok := p.(*RegexParser); !ok {
		t.Errorf("expected RegexParser for empty file, got %T", p)
	}
}

func TestDetectParser_NonExistentFile(t *testing.T) {
	// Non-existent file — should default to regex after timeout
	// Use a very short path that definitely won't exist
	path := filepath.Join(t.TempDir(), "does-not-exist.log")

	// NOTE: detectFormat has a 5s retry loop so this test will take ~5s.
	// This is intentional — we're verifying the timeout behaviour.
	// If this is too slow for CI, mock the deadline or reduce maxWait.
	p := DetectParser(path)
	if _, ok := p.(*RegexParser); !ok {
		t.Errorf("expected RegexParser for non-existent file, got %T", p)
	}
}
