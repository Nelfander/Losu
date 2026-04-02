package parser

import (
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
		checkTime bool // New check for real timestamp parsing
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
				// Verify it's NOT time.Now() (which would be 2026-04-02 based on current context)
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

	// Test a valid level with a leading timestamp
	input := "2026-03-08 15:00:00 [ERROR] Connection lost"
	res := p.Parse(model.RawLog{Line: input})

	if res.Level != "ERROR" || res.Timestamp.Year() != 2026 {
		t.Errorf("Brackets fast-path failed. Level: %s, Time: %v", res.Level, res.Timestamp)
	}

	// Test a false positive (should NOT be INFO/WARN/ERROR)
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
