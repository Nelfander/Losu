package main

import (
	"bufio"
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/joho/godotenv"
)

const (
	jsonLogsPerSecond = 20000
	jsonBatchSize     = 250
	jsonChannelSize   = 50
)

func main() {
	_ = godotenv.Load()

	logFile := os.Getenv("LOSU_LOG_PATH")
	if logFile == "" {
		logFile = "test.log"
	}

	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	writer := bufio.NewWriterSize(f, 1024*1024)
	defer writer.Flush()

	logChan := make(chan []byte, jsonChannelSize)

	// --- WRITER GOROUTINE ---
	go func() {
		flushTicker := time.NewTicker(50 * time.Millisecond)
		defer flushTicker.Stop()

		for {
			select {
			case batch := <-logChan:
				_, _ = writer.Write(batch)
			case <-flushTicker.C:
				_ = writer.Flush()
				time.Sleep(1 * time.Millisecond)
			}
		}
	}()

	// --- JSON TEMPLATES ---
	// Each template is a format string — we fill in the dynamic parts at runtime.
	// Covers the same scenarios as the logfmt generator so results are comparable.
	healthy := []string{
		`{"time":"%s","level":"info","msg":"User logged in","user_id":%d}`,
		`{"time":"%s","level":"debug","msg":"Cache hit","key":"user_profile_%d"}`,
		`{"time":"%s","level":"info","msg":"HTTP request finished","status":200,"duration":%d}`,
		`{"time":"%s","level":"info","msg":"Order processed","amount":%d}`,
	}
	warn := []string{
		`{"time":"%s","level":"warn","msg":"Failed login attempt","ip":"192.168.1.%d"}`,
		`{"time":"%s","level":"warn","msg":"High memory usage","threshold":%d}`,
	}
	errs := []string{
		`{"time":"%s","level":"error","msg":"Query timeout","duration":%d}`,
		`{"time":"%s","level":"error","msg":"S3 upload failed","key":"img_%d"}`,
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	interval := time.Second / (jsonLogsPerSecond / jsonBatchSize)

	fmt.Println("🚀 JSON Generator active. Rate:", jsonLogsPerSecond, "logs/sec")

	for {
		start := time.Now()
		// RFC3339 timestamp — matches what real apps emit and what JSONParser expects
		ts := start.Format(time.RFC3339)

		buf := make([]byte, 0, 128*jsonBatchSize)

		for i := 0; i < jsonBatchSize; i++ {
			val := rng.Intn(1000)
			chance := rng.Intn(1000)

			var line string
			if chance < 995 {
				tmpl := healthy[rng.Intn(len(healthy))]
				line = fmt.Sprintf(tmpl, ts, val)
			} else if chance < 999 {
				tmpl := warn[rng.Intn(len(warn))]
				line = fmt.Sprintf(tmpl, ts, val)
			} else {
				tmpl := errs[rng.Intn(len(errs))]
				line = fmt.Sprintf(tmpl, ts, val)
			}

			buf = append(buf, line...)
			buf = append(buf, '\n')
		}

		select {
		case logChan <- buf:
		default:
			time.Sleep(10 * time.Millisecond)
		}

		elapsed := time.Since(start)
		if elapsed < interval {
			time.Sleep(interval - elapsed)
		}
	}
}

// --- SAMPLE OUTPUT ---
// {"time":"2026-04-04T16:31:28+02:00","level":"info","msg":"User logged in","user_id":742}
// {"time":"2026-04-04T16:31:28+02:00","level":"warn","msg":"High memory usage","threshold":891}
// {"time":"2026-04-04T16:31:28+02:00","level":"error","msg":"Query timeout","duration":305}
//
// Field names match JSONParser's extraction keys:
//   "time"  → timestamp
//   "level" → log level
//   "msg"   → message
// Extra fields (user_id, status, duration etc) are preserved in the message
// via buildFallbackMessage if no "msg" field is present, or appended as context.

// Note on rate: defaulting to 10k/sec instead of 50k/sec because fmt.Sprintf
// is slower than the raw byte-append approach in the logfmt generator.
// For higher rates, pre-build the JSON templates as []byte and use append
// directly — same pattern as stress_gen.go.
