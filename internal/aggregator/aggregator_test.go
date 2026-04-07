package aggregator

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nelfander/losu/internal/model"
)

func TestAggregatorConcurrency(t *testing.T) {
	agg := NewAggregator()
	weights := map[string]int{"INFO": 1, "ERROR": 3}

	var wg sync.WaitGroup

	// Start 50 Writer goroutines
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				agg.Update(model.LogEvent{
					Level:   "INFO",
					Message: fmt.Sprintf("Log from writer %d", id),
				}, 1, weights)
			}
		}(i)
	}

	// Start 50 Reader goroutines
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = agg.Snapshot()
				_ = agg.getTopMessages()
			}
		}()
	}

	// Manually trigger trend pushes
	for i := 0; i < 5; i++ {
		agg.PushTrend()
	}

	wg.Wait()

	if agg.TotalLines != 50000 {
		t.Errorf("Data lost! Expected 50,000 lines, got %d", agg.TotalLines)
	}
}

func TestIncidentTrigger(t *testing.T) {
	// Clean up any leftover files first
	os.MkdirAll("incidents", 0755)
	entries, _ := os.ReadDir("incidents")
	for _, e := range entries {
		os.Remove("incidents/" + e.Name())
	}

	agg := NewAggregator()
	weights := map[string]int{"INFO": 1, "WARN": 2, "ERROR": 3}
	minWeight := 1

	// Build baseline AverageEPS ~5 over 10 ticks using ErrorSecCount.
	// shouldTriggerReport needs lastF > AverageEPS*3, so spike must be > 15.
	for i := 0; i < 10; i++ {
		agg.ErrorSecCount = 5
		agg.PushTrend()
	}

	// Spike to 50 EPS — well above AverageEPS*3 (~15) and above epsMinimum (1.0)
	agg.ErrorSecCount = 50
	agg.PushTrend()

	// Fire the trigger event
	agg.Update(model.LogEvent{
		Level:     "ERROR",
		Message:   "CRITICAL FAILURE",
		Timestamp: time.Now(),
	}, minWeight, weights)

	var foundFile string
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		files, _ := os.ReadDir("incidents")
		for _, f := range files {
			if strings.HasPrefix(f.Name(), "incident_") {
				foundFile = f.Name()
				break
			}
		}
		if foundFile != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if foundFile == "" {
		t.Fatal("Failed to trigger incident report file within 3 seconds")
	}

	agg.Wait()
	os.Remove("incidents/" + foundFile)
}

func TestFingerprint(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Static message",
			input:    "User logged in",
			expected: "User logged in",
		},
		{
			name:     "ID stripping",
			input:    "Order 12345 processed",
			expected: "Order * processed",
		},
		{
			name:     "Hex address stripping",
			input:    "Panic at 0x7ffd123abc",
			expected: "Panic at 0x*",
		},
		{
			name:     "IP-like stripping",
			input:    "Conn from 192.168.1.1",
			expected: "Conn from *.*.*.*",
		},
		{
			name:     "S3 product name preserved",
			input:    "S3 upload failed | key=img_164",
			expected: "S3 upload failed | key=img_*",
		},
		{
			name:     "underscore separator",
			input:    "user_id=503",
			expected: "user_id=*",
		},
		{
			name:     "key value pair",
			input:    "duration=451",
			expected: "duration=*",
		},
		// --- Numeric values after separators ---
		{
			name:     "duration with ms suffix",
			input:    "duration=451ms",
			expected: "duration=*ms",
		},
		{
			name:     "status code",
			input:    "status=200",
			expected: "status=*",
		},
		{
			name:     "404 status",
			input:    "status=404",
			expected: "status=*",
		},
		{
			name:     "port number",
			input:    "port=8080",
			expected: "port=*",
		},
		{
			name:     "size in bytes",
			input:    "size=1024",
			expected: "size=*",
		},
		{
			name:     "retry count",
			input:    "retry=3",
			expected: "retry=*",
		},
		{
			name:     "attempt of total",
			input:    "attempt=1 of 3",
			expected: "attempt=* of *",
		},
		{
			name:     "pid",
			input:    "pid=12345",
			expected: "pid=*",
		},
		{
			name:     "exit code",
			input:    "exitcode=1",
			expected: "exitcode=*",
		},
		// --- IPs and addresses ---
		{
			name:     "internal IP",
			input:    "ip=10.0.0.1",
			expected: "ip=*.*.*.*",
		},
		{
			name:     "zero IP",
			input:    "ip=0.0.0.0",
			expected: "ip=*.*.*.*",
		},
		{
			name:     "addr with port",
			input:    "addr=127.0.0.1:8080",
			expected: "addr=*.*.*.*:*",
		},
		{
			name:     "host IP",
			input:    "host=192.168.0.255",
			expected: "host=*.*.*.*",
		},
		// --- File paths ---
		{
			name:     "rotated log file",
			input:    "file=/var/log/app.log.1",
			expected: "file=/var/log/app.log.*",
		},
		{
			name:     "thread id",
			input:    "thread_id=140234567890",
			expected: "thread_id=*",
		},
		{
			name:     "memory mb",
			input:    "memory_mb=4096",
			expected: "memory_mb=*",
		},
		{
			name:     "db pool size",
			input:    "db_pool_size=10",
			expected: "db_pool_size=*",
		},
		{
			name:     "unix timestamp",
			input:    "timestamp=1712345678",
			expected: "timestamp=*",
		},
		{
			name:     "errno",
			input:    "errno=111",
			expected: "errno=*",
		},
		// --- Product/version names preserved ---
		{
			name:     "HTTP2 preserved",
			input:    "HTTP2 connection established",
			expected: "HTTP2 connection established",
		},
		{
			name:     "S3 bucket",
			input:    "AWS S3 bucket created",
			expected: "AWS S3 bucket created",
		},
		{
			name:     "OAuth2 preserved",
			input:    "OAuth2 token expired",
			expected: "OAuth2 token expired",
		},
		{
			name:     "IPv4 preserved",
			input:    "IPv4 address assigned",
			expected: "IPv4 address assigned",
		},
		{
			name:     "IPv6 preserved",
			input:    "IPv6 not supported",
			expected: "IPv6 not supported",
		},
		{
			name:     "md5sum preserved",
			input:    "md5sum mismatch",
			expected: "md5sum mismatch",
		},
		{
			name:     "sha256 preserved",
			input:    "sha256 verification failed",
			expected: "sha256 verification failed",
		},
		// --- Tricky mixed cases ---
		{
			name:     "worker with number",
			input:    "error in worker_3",
			expected: "error in worker_*",
		},
		{
			name:     "shard with zero prefix",
			input:    "shard_01 unavailable",
			expected: "shard_* unavailable",
		},
		{
			name:     "cpu usage float",
			input:    "cpu_usage=98.5",
			expected: "cpu_usage=*.*",
		},
		// --- digit follows letter — preserved ---
		{
			name:     "username with number",
			input:    "user123 logged in",
			expected: "user123 logged in",
		},
		{
			name:     "TLSv1 preserved",
			input:    "TLSv1 deprecated",
			expected: "TLSv1 deprecated",
		},
		{
			// v is a letter so 2 follows a letter — preserved by design
			// gRPC v2 and gRPC v3 are the same product, different versions
			// but we accept this limitation — version numbers after v are kept
			name:     "gRPC v2 — v is letter so digit preserved",
			input:    "gRPC v2 error",
			expected: "gRPC v2 error",
		},
		{
			name:     "E11000 error code preserved",
			input:    "E11000 duplicate key",
			expected: "E11000 duplicate key",
		},
		// --- Brackets ---
		{
			name:     "worker in brackets",
			input:    "[worker-3] job failed",
			expected: "[worker-*] job failed",
		},
		{
			name:     "date in brackets",
			input:    "[2026-04-05] event fired",
			expected: "[*-*-*] event fired",
		},
		{
			// v is a letter so v2 is preserved — /api/v2/ stays as-is
			name:     "error code with path — v2 preserved",
			input:    "[ERROR] code=500 path=/api/v2/users",
			expected: "[ERROR] code=* path=/api/v2/users",
		},
		// --- Real app patterns ---
		{
			name:     "dial tcp refused",
			input:    "dial tcp 10.0.0.1:5432: connection refused",
			expected: "dial tcp *.*.*.*:*: connection refused",
		},
		{
			name:     "goroutine number",
			input:    "goroutine 18 [running]",
			expected: "goroutine * [running]",
		},
		// --- Hex IDs — letters reset afterSeparator so digits after letters are kept ---
		{
			// deadbeef starts with letters — resets afterSeparator=false
			// so 1234 after f (a letter) is preserved
			name:     "session hex — leading letters preserved by design",
			input:    "session=deadbeef1234",
			expected: "session=deadbeef1234",
		},
		{
			// abc starts with letters, resets afterSeparator
			// 123 after c (letter) kept, def after 3 (digit in token)... complex
			name:     "trace id mixed — leading letters preserved",
			input:    "trace_id=abc123def456",
			expected: "trace_id=abc123def456",
		},
		// --- UUID ---
		{
			name:     "request id uuid — mixed segment replacement",
			input:    "request_id=550e8400-e29b-41d4-a716-446655440000",
			expected: "request_id=*e8400-e29b-*d4-a716-*",
		},
		// --- Timestamps ---
		{
			name:     "unix float timestamp",
			input:    "unix_time=1712345678.123",
			expected: "unix_time=*.*",
		},
		// --- SQLSTATE ---
		{
			name:     "sqlstate code",
			input:    "SQLSTATE=42000",
			expected: "SQLSTATE=*",
		},
		// --- Size with unit — now fixed with inHexToken ---
		{
			// 'b' is hex but inHexToken=false so "bytes" is not consumed
			name:     "size with bytes label",
			input:    "size=2048bytes",
			expected: "size=*bytes",
		},
		// --- OOM ---
		{
			name:     "oom allocation",
			input:    "cannot allocate 1073741824-byte block",
			expected: "cannot allocate *-byte block",
		},
		// --- Code path with version ---
		{
			// v is a letter so v2 preserved, 503 after / separator is replaced
			name:     "api v2 path — v2 preserved, user id replaced",
			input:    "path=/api/v2/users/503",
			expected: "path=/api/v2/users/*",
		},
		// --- Quoted strings ---
		{
			name:     "quoted value replaced",
			input:    "timeout after 30s duration=\"30s\"",
			expected: "timeout after *s duration=\"*s\"",
		},
		{
			name:     "quoted number replaced",
			input:    "error=\"failed after 3 retries\"",
			expected: "error=\"failed after * retries\"",
		},
		{
			name:     "quoted ip replaced",
			input:    "blocked ip=\"192.168.1.54\"",
			expected: "blocked ip=\"*.*.*.*\"",
		},
		{
			name:     "quoted string with no numbers preserved",
			input:    "msg=\"connection refused\"",
			expected: "msg=\"connection refused\"",
		},
		{
			name:     "key with quoted numeric value",
			input:    "code=\"404\" msg=\"not found\"",
			expected: "code=\"*\" msg=\"not found\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := fingerprint(tt.input)
			if actual != tt.expected {
				t.Errorf("fingerprint() %s\n got:      %q\n expected: %q",
					tt.name, actual, tt.expected)
			}
		})
	}
}

func TestGroupingAndDetailPreservation(t *testing.T) {
	agg := NewAggregator()
	weights := map[string]int{"ERROR": 3}
	minWeight := 1

	log1 := model.LogEvent{Level: "ERROR", Message: "S3 upload failed | id=101"}
	log2 := model.LogEvent{Level: "ERROR", Message: "S3 upload failed | id=102"}

	agg.Update(log1, minWeight, weights)
	agg.Update(log2, minWeight, weights)

	pattern := fingerprint(log1.Message)
	stat, exists := agg.MessageCounts[pattern]

	if !exists {
		t.Fatalf("Pattern %q not found", pattern)
	}
	if stat.Count != 2 {
		t.Errorf("Expected 2 hits, got %d", stat.Count)
	}
	if len(stat.VariantCounts) != 2 {
		t.Errorf("Expected 2 unique variations, got %d", len(stat.VariantCounts))
	}
}

func TestCircularBufferStability(t *testing.T) {
	agg := NewAggregator()
	weights := map[string]int{"INFO": 1}

	totalToPush := maxHistory + 10

	for i := 0; i < totalToPush; i++ {
		msg := fmt.Sprintf("Log %d", i)
		agg.Update(model.LogEvent{Level: "INFO", Message: msg}, 1, weights)
	}

	history := agg.GetHistory()

	if len(history) != maxHistory {
		t.Errorf("Buffer size mismatch. Got %d, want %d", len(history), maxHistory)
	}

	expectedFirst := "Log 10"
	if history[0].Message != expectedFirst {
		t.Errorf("Circular shift failed. First log is %q, want %q", history[0].Message, expectedFirst)
	}

	expectedLast := fmt.Sprintf("Log %d", totalToPush-1)
	if history[maxHistory-1].Message != expectedLast {
		t.Errorf("Last log is %q, want %q", history[maxHistory-1].Message, expectedLast)
	}
}

func BenchmarkAggregatorUpdate(b *testing.B) {
	agg := NewAggregator()
	weights := map[string]int{"INFO": 1, "ERROR": 3}
	event := model.LogEvent{
		Level:   "ERROR",
		Message: "S3 upload failed | bucket=\"assets\" key=\"img_123.png\"",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		agg.Update(event, 1, weights)
	}
}
