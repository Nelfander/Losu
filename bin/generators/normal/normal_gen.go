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
	normalLogsPerSecond = 2000
	normalBatchSize     = 250
	normalChannelSize   = 50
)

func main() {
	_ = godotenv.Load()

	logFile := os.Getenv("LOSU_TEST_LOG_PATH")
	if logFile == "" {
		logFile = "logs/test.log"
	}

	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	writer := bufio.NewWriterSize(f, 1024*1024)
	defer writer.Flush()

	logChan := make(chan []byte, normalChannelSize)

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

	// --- LOGFMT TEMPLATES ---
	// Wide variety of real-world log patterns covering auth, HTTP, DB, cache,
	// storage, payments, workers, and system events.
	// Each template uses logfmt key=value format — matches RegexParser fast-path.

	healthy := []string{
		// Auth & sessions
		`time="%s" level=info msg="User logged in" user_id=%d ip="10.0.1.%d"`,
		`time="%s" level=info msg="Session created" user_id=%d duration=%dms`,
		`time="%s" level=info msg="Password changed" user_id=%d`,
		`time="%s" level=debug msg="Token refreshed" user_id=%d ttl=%ds`,
		`time="%s" level=info msg="User logged out" user_id=%d session_duration=%ds`,

		// HTTP requests
		`time="%s" level=info msg="HTTP request finished" method=GET path="/api/users/%d" status=200 duration=%dms`,
		`time="%s" level=info msg="HTTP request finished" method=POST path="/api/orders" status=201 duration=%dms`,
		`time="%s" level=info msg="HTTP request finished" method=GET path="/api/products/%d" status=200 duration=%dms`,
		`time="%s" level=debug msg="HTTP request finished" method=PUT path="/api/profile/%d" status=200 duration=%dms`,
		`time="%s" level=info msg="Static asset served" path="/static/bundle.%d.js" size=%dkb`,

		// Database
		`time="%s" level=debug msg="Query executed" table=users rows=%d duration=%dms`,
		`time="%s" level=debug msg="Query executed" table=orders rows=%d duration=%dms`,
		`time="%s" level=info msg="Transaction committed" tx_id=%d duration=%dms`,
		`time="%s" level=debug msg="Index scan used" table=products index=idx_category rows=%d`,
		`time="%s" level=info msg="Connection pool healthy" active=%d idle=%d max=20`,

		// Cache
		`time="%s" level=debug msg="Cache hit" key="user_profile_%d" ttl=%ds`,
		`time="%s" level=debug msg="Cache hit" key="product_list_%d" ttl=%ds`,
		`time="%s" level=info msg="Cache set" key="session_%d" ttl=3600s`,
		`time="%s" level=debug msg="Cache hit" key="rate_limit_%d" hits=%d`,

		// Storage & S3
		`time="%s" level=info msg="File uploaded" bucket=assets key="img_%d.png" size=%dkb`,
		`time="%s" level=info msg="File downloaded" bucket=exports key="report_%d.csv" size=%dkb`,
		`time="%s" level=debug msg="Presigned URL generated" bucket=uploads key="tmp_%d" expires=300s`,

		// Workers & queues
		`time="%s" level=info msg="Job processed" queue=email job_id=%d duration=%dms`,
		`time="%s" level=info msg="Job processed" queue=notifications job_id=%d duration=%dms`,
		`time="%s" level=debug msg="Worker heartbeat" worker_id=%d queue=default jobs_processed=%d`,
		`time="%s" level=info msg="Batch completed" batch_id=%d records=%d duration=%dms`,

		// Payments
		`time="%s" level=info msg="Payment processed" order_id=%d amount=%d currency=EUR`,
		`time="%s" level=info msg="Invoice generated" customer_id=%d invoice_id=%d amount=%d`,
		`time="%s" level=debug msg="Stripe webhook received" event=payment.succeeded order_id=%d`,

		// System
		`time="%s" level=info msg="Health check passed" service=api latency=%dms`,
		`time="%s" level=debug msg="Metrics flushed" datapoints=%d duration=%dms`,
		`time="%s" level=info msg="Config reloaded" version=%d`,
		`time="%s" level=debug msg="GC completed" freed_mb=%d duration=%dms`,
	}

	warn := []string{
		// Auth warnings
		`time="%s" level=warn msg="Failed login attempt" user_id=%d ip="192.168.1.%d" attempts=%d`,
		`time="%s" level=warn msg="Session expired" user_id=%d idle_seconds=%d`,
		`time="%s" level=warn msg="Rate limit approaching" user_id=%d requests=%d limit=100`,
		`time="%s" level=warn msg="Suspicious login location" user_id=%d country=XX`,

		// HTTP warnings
		`time="%s" level=warn msg="Slow request detected" path="/api/reports/%d" duration=%dms threshold=500ms`,
		`time="%s" level=warn msg="HTTP request finished" method=GET path="/api/search" status=429 duration=%dms`,
		`time="%s" level=warn msg="Response size large" path="/api/export/%d" size=%dmb`,

		// DB warnings
		`time="%s" level=warn msg="Slow query detected" table=orders duration=%dms threshold=200ms`,
		`time="%s" level=warn msg="Connection pool pressure" active=%d idle=1 max=20`,
		`time="%s" level=warn msg="Table scan detected" table=events rows_scanned=%d`,

		// Cache warnings
		`time="%s" level=warn msg="Cache miss" key="user_profile_%d" fallback=db`,
		`time="%s" level=warn msg="Cache eviction" key="session_%d" reason=memory_pressure`,
		`time="%s" level=warn msg="High memory usage" threshold=%d percent=%d`,

		// Workers
		`time="%s" level=warn msg="Job retry" queue=email job_id=%d attempt=%d max_retries=3`,
		`time="%s" level=warn msg="Queue depth high" queue=notifications depth=%d threshold=1000`,
		`time="%s" level=warn msg="Worker slow" worker_id=%d job_duration=%dms threshold=5000ms`,

		// System
		`time="%s" level=warn msg="Disk usage high" mount=/data used_percent=%d threshold=80`,
		`time="%s" level=warn msg="CPU spike detected" percent=%d duration=%ds`,
	}

	errs := []string{
		// Auth errors
		`time="%s" level=error msg="Account locked" user_id=%d failed_attempts=%d`,
		`time="%s" level=error msg="JWT validation failed" user_id=%d reason=expired`,
		`time="%s" level=error msg="OAuth token exchange failed" provider=google status=%d`,

		// HTTP errors
		`time="%s" level=error msg="HTTP request finished" method=POST path="/api/checkout" status=500 duration=%dms`,
		`time="%s" level=error msg="Upstream timeout" service=payment_gateway timeout=%dms`,
		`time="%s" level=error msg="Circuit breaker open" service=inventory failures=%d`,

		// Database errors
		`time="%s" level=error msg="Query timeout" table=orders duration=%dms`,
		`time="%s" level=error msg="Deadlock detected" table=inventory tx_id=%d`,
		`time="%s" level=error msg="Connection refused" host=db-primary port=5432 attempt=%d`,
		`time="%s" level=error msg="Unique constraint violation" table=users field=email`,

		// Storage errors
		`time="%s" level=error msg="S3 upload failed" bucket=assets key="img_%d.png" status=%d`,
		`time="%s" level=error msg="S3 download failed" bucket=exports key="report_%d.csv" reason=not_found`,

		// Workers
		`time="%s" level=error msg="Job failed" queue=email job_id=%d error="SMTP connection refused"`,
		`time="%s" level=error msg="Max retries exceeded" queue=webhooks job_id=%d attempts=%d`,

		// Payments
		`time="%s" level=error msg="Payment declined" order_id=%d reason=insufficient_funds amount=%d`,
		`time="%s" level=error msg="Stripe API error" status=%d order_id=%d`,

		// System
		`time="%s" level=error msg="Out of memory" process=worker_pool pid=%d rss_mb=%d`,
		`time="%s" level=error msg="Panic recovered" goroutine=%d error="index out of range"`,
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	interval := time.Second / (normalLogsPerSecond / normalBatchSize)

	fmt.Println("🚀 Normal Generator active. Rate:", normalLogsPerSecond, "logs/sec → ", logFile)

	for {
		start := time.Now()
		ts := start.Format(time.RFC3339)

		buf := make([]byte, 0, 128*normalBatchSize)

		for i := 0; i < normalBatchSize; i++ {
			a := rng.Intn(1000)
			b := rng.Intn(1000)
			chance := rng.Intn(1000)

			var line string
			if chance < 940 {
				tmpl := healthy[rng.Intn(len(healthy))]
				line = fmt.Sprintf(tmpl, ts, a, b)
			} else if chance < 990 {
				tmpl := warn[rng.Intn(len(warn))]
				line = fmt.Sprintf(tmpl, ts, a, b)
			} else {
				tmpl := errs[rng.Intn(len(errs))]
				line = fmt.Sprintf(tmpl, ts, a, b)
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
// time="2026-04-06T10:00:00+02:00" level=info msg="User logged in" user_id=742 ip="10.0.1.33"
// time="2026-04-06T10:00:00+02:00" level=warn msg="Slow query detected" table=orders duration=312ms threshold=200ms
// time="2026-04-06T10:00:00+02:00" level=error msg="Query timeout" table=orders duration=5012ms
//
// Format: logfmt key=value — parsed by RegexParser logfmt fast-path.
// Two random ints (a, b) are substituted into each template — the fingerprinter
// replaces them with * so variants cluster correctly in the top errors panel.
// Healthy:Error ratio is roughly 94:1:5 (info/debug : warn : error).
