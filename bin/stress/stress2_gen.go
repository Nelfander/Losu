package main

import (
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

	// Open file once for high-performance writing
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	// Categorized templates
	infoLogs := []string{
		"time=%s level=INFO msg=\"User logged in\" user_id=%d\n",
		"time=%s level=INFO msg=\"HTTP request finished\" status=200 duration=%dms\n",
		"time=%s level=INFO msg=\"Order processed\" amount=$%d.99 currency=\"USD\"\n",
		"time=%s level=DEBUG msg=\"Cache hit\" key=\"user_profile_%d\"\n",
	}

	incidents := []string{
		"time=%s level=WARN msg=\"Failed login attempt\" ip=192.168.1.%d user=\"admin\"\n",
		"time=%s level=ERROR msg=\"Query timeout\" duration=%dms query_id=q_99\n",
		"time=%s level=WARN msg=\"High memory usage\" threshold=%d%%\n",
		"time=%s level=ERROR msg=\"S3 upload failed\" bucket=\"assets\" key=\"img_%d.png\"\n",
	}

	fmt.Println("🏗 Production Simulation started (1,000 logs/sec).")
	fmt.Println("📊 Probability: 99.5% INFO | 0.5% ERROR/WARN")

	for {
		timestamp := time.Now().Format("2006-01-02T15:04:05Z")
		val := rand.Intn(1000)
		var logLine string

		// Probability Check
		// 0.005 = 0.5% chance of an ERROR or WARN
		if rand.Float64() < 0.005 {
			template := incidents[rand.Intn(len(incidents))]
			logLine = fmt.Sprintf(template, timestamp, val)
		} else {
			template := infoLogs[rand.Intn(len(infoLogs))]
			logLine = fmt.Sprintf(template, timestamp, val)
		}

		f.WriteString(logLine)

		// Syncing every single line at 1000/sec can actually slow down the OS
		if val%100 == 0 {
			f.Sync()
		}

		// 1ms = 1,000 EPS (Events Per Second)
		time.Sleep(1 * time.Millisecond)
	}
}
