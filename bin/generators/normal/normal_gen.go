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
	normalLogsPerSecond = 250
	normalBatchSize     = 250
	normalChannelSize   = 50
)

// Demo generator — designed to showcase LOSU's fingerprinting engine.
//
// Two error patterns with varying IPs/IDs:
//   "Failed login attempt" from different IPs → clusters into 1 pattern
//   "Connection refused" from different ports → clusters into 1 pattern
//
// Open the Top Errors inspector to see each unique IP as a separate variant.

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

	go func() {
		flushTicker := time.NewTicker(50 * time.Millisecond)
		defer flushTicker.Stop()
		for {
			select {
			case batch := <-logChan:
				_, _ = writer.Write(batch)
			case <-flushTicker.C:
				_ = writer.Flush()
			}
		}
	}()

	healthy := []string{
		`time="%s" level=info msg="User logged in" user_id=%d ip="10.0.1.%d"`,
		`time="%s" level=info msg="HTTP request finished" method=GET path="/api/users" status=200 duration=%dms`,
		`time="%s" level=info msg="Health check passed" service=api latency=%dms`,
	}

	// Warning patterns — same IP pool, clusters into 2 warn patterns
	warns := []string{
		`time="%s" level=warn msg="Rate limit approaching" ip="192.168.1.%d" requests=%d limit=100`,
		`time="%s" level=warn msg="Slow query detected" table=orders duration=%dms threshold=200ms`,
	}

	// Two error patterns — IPs vary so fingerprinter clusters them.
	// In the inspector you'll see each exact IP as a separate variant.
	errs := []string{
		`time="%s" level=error msg="Failed login attempt" ip="192.168.1.%d" attempts=%d`,
		`time="%s" level=error msg="Connection refused" host=db-primary port=543%d attempt=%d`,
	}

	// Fixed pool of IPs — small enough to see clear clustering
	ips := []int{12, 45, 87, 133, 201}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	interval := time.Second / (normalLogsPerSecond / normalBatchSize)

	fmt.Println("🚀 Demo Generator active. Rate:", normalLogsPerSecond, "logs/sec →", logFile)
	fmt.Println("   Two error patterns with", len(ips), "IPs — watch Top Errors cluster them!")

	for {
		start := time.Now()
		ts := start.Format(time.RFC3339)
		buf := make([]byte, 0, 128*normalBatchSize)

		for i := 0; i < normalBatchSize; i++ {
			chance := rng.Intn(100)
			ip := ips[rng.Intn(len(ips))]
			b := rng.Intn(5) + 1

			var line string
			if chance < 98 {
				tmpl := healthy[rng.Intn(len(healthy))]
				line = fmt.Sprintf(tmpl, ts, ip, b)
			} else if chance < 99 {
				tmpl := warns[rng.Intn(len(warns))]
				line = fmt.Sprintf(tmpl, ts, ip, b)
			} else {
				tmpl := errs[rng.Intn(len(errs))]
				line = fmt.Sprintf(tmpl, ts, ip, b)
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
