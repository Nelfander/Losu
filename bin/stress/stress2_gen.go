package main

import (
	"bufio"
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()
	logFile := os.Getenv("LOSU_LOG_PATH")
	if logFile == "" {
		logFile = "test.log"
	}

	// Templates categorized by health
	healthyLogs := []string{
		"time=%s level=INFO msg=\"User logged in\" user_id=%d\n",
		"time=%s level=DEBUG msg=\"Cache hit\" key=\"user_profile_%d\"\n",
		"time=%s level=INFO msg=\"HTTP request finished\" status=200 duration=%dms\n",
		"time=%s level=INFO msg=\"Order processed\" amount=$%d.99 currency=\"USD\"\n",
	}

	warningLogs := []string{
		"time=%s level=WARN msg=\"Failed login attempt\" ip=192.168.1.%d user=\"admin\"\n",
		"time=%s level=WARN msg=\"High memory usage\" threshold=%d%%\n",
	}

	errorLogs := []string{
		"time=%s level=ERROR msg=\"Query timeout\" duration=%dms query_id=q_99\n",
		"time=%s level=ERROR msg=\"S3 upload failed\" bucket=\"assets\" key=\"img_%d.png\"\n",
	}

	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	// Use a buffered writer to handle high throughput
	writer := bufio.NewWriterSize(f, 1024*1024) // 1MB buffer
	defer writer.Flush()

	fmt.Println("🚀 Nitro Log Generator: Aiming for 50k logs/sec")
	fmt.Println("Press Ctrl+C to stop.")

	// Statistics for the generator
	totalGenerated := 0
	//startTime := time.Now()

	// Ticker for stats output
	statsTicker := time.NewTicker(time.Second)
	defer statsTicker.Stop()

	for {
		// Batch processing: write 500 logs per small sleep to hit the target
		// This reduces the overhead of time.Sleep and time.Now()
		for {
			// Increase batch to 2500, but sleep longer (50ms)
			// 2500 logs / 0.05 seconds = 50,000 logs/sec
			for i := 0; i < 2500; i++ {
				timestamp := time.Now().Format("2006-01-02T15:04:05Z")
				val := rand.Intn(1000)

				chance := rand.Intn(1000) // Using 1000 for finer control
				var template string

				if chance < 995 { // 99.5% Healthy
					template = healthyLogs[rand.Intn(len(healthyLogs))]
				} else if chance < 999 { // 0.4% Warning
					template = warningLogs[rand.Intn(len(warningLogs))]
				} else { // 0.1% Error (A real needle in a haystack)
					template = errorLogs[rand.Intn(len(errorLogs))]
				}

				fmt.Fprintf(writer, template, timestamp, val)
				totalGenerated++
			}

			// Flush less often to give the OS a break
			writer.Flush()

			// 50ms gives the UI/Tailer a chance to grab the CPU
			time.Sleep(500 * time.Millisecond)
		}
	}
}
