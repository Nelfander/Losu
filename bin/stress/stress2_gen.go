package main

import (
	"bufio"
	"math/rand"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

const (
	logsPerSecond = 5000 // Reduced slightly for OS stability; ramp up once verified
	batchSize     = 250  // Larger batches = fewer channel operations
	channelSize   = 50   // Enough buffer to handle disk spikes
)

func main() {
	_ = godotenv.Load()

	logFile := os.Getenv("LOSU_LOG_PATH")
	if logFile == "" {
		logFile = "test.log"
	}

	// Open with O_SYNC or O_DSYNC removed to allow OS-level write caching
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	// 1MB buffer is the "Sweet Spot" for modern SSDs
	writer := bufio.NewWriterSize(f, 1024*1024)
	defer writer.Flush()

	logChan := make(chan []byte, channelSize)

	// --- WRITER GOROUTINE ---
	// This goroutine's only job is to move bytes from RAM to Disk
	go func() {
		// Flush much more frequently to "push" data into the OS buffer
		flushTicker := time.NewTicker(50 * time.Millisecond)
		defer flushTicker.Stop()

		for {
			select {
			case batch := <-logChan:
				_, _ = writer.Write(batch)
			case <-flushTicker.C:
				_ = writer.Flush()
				// CRITICAL: This gives the OS a tiny window to let
				// other processes (Losu) read the file.
				time.Sleep(1 * time.Millisecond)
			}
		}
	}()

	// --- DATA TEMPLATES ---
	healthy := [][]byte{
		[]byte(" level=INFO msg=\"User logged in\" user_id="),
		[]byte(" level=DEBUG msg=\"Cache hit\" key=\"user_profile_"),
		[]byte(" level=INFO msg=\"HTTP request finished\" status=200 duration="),
		[]byte(" level=INFO msg=\"Order processed\" amount=$"),
	}
	warn := [][]byte{
		[]byte(" level=WARN msg=\"Failed login attempt\" ip=192.168.1."),
		[]byte(" level=WARN msg=\"High memory usage\" threshold="),
	}
	errs := [][]byte{
		[]byte(" level=ERROR msg=\"Query timeout\" duration="),
		[]byte(" level=ERROR msg=\"S3 upload failed\" key=\"img_"),
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	// Calculate the necessary sleep to maintain the rate
	// (1 sec / (total logs / batch size))
	interval := time.Second / (logsPerSecond / batchSize)

	println("🚀 Generator active. Rate:", logsPerSecond, "logs/sec")

	for {
		start := time.Now()
		ts := start.Format("2006-01-02T15:04:05Z")

		// One allocation per batch
		buf := make([]byte, 0, 128*batchSize)

		for i := 0; i < batchSize; i++ {
			val := rng.Intn(1000)
			chance := rng.Intn(1000)

			var msg []byte
			if chance < 995 {
				msg = healthy[rng.Intn(len(healthy))]
			} else if chance < 999 {
				msg = warn[rng.Intn(len(warn))]
			} else {
				msg = errs[rng.Intn(len(errs))]
			}

			buf = append(buf, "time="...)
			buf = append(buf, ts...)
			buf = append(buf, msg...)
			buf = append(buf, strconv.Itoa(val)...)
			buf = append(buf, '\n')
		}

		// NON-BLOCKING SEND
		select {
		case logChan <- buf:
		default:
			// If channel is full, disk is saturated.
			// We skip this batch and sleep extra to let the OS catch up.
			time.Sleep(10 * time.Millisecond)
		}

		// Pacing logic to ensure we don't melt the CPU
		elapsed := time.Since(start)
		if elapsed < interval {
			time.Sleep(interval - elapsed)
		}
	}
}
