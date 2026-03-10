package main

import (
	"fmt"
	"math/rand"
	"os"
	"time"
)

func main() {
	logFile := "test.log"
	formats := []string{
		// 1. Logfmt style
		"time=%s level=INFO msg=\"User logged in\" user_id=%d\n",
		// 2. Brackets style
		"%s [ERROR] Connection failed to database_%d\n",
		// 3. Prefix/Complex style (The playingfield-app one)
		"playingfield-app | time=%s level=DEBUG msg=\"websocket: close 1001\" user_id=%d\n",
		"time=%s level=WARN msg=\"High memory usage\" threshold=%d%%\n",
	}

	// Open file once for performance
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	fmt.Println("🏗 Log Generator started. Press Ctrl+C to stop.")

	for {
		template := formats[rand.Intn(len(formats))]
		timestamp := time.Now().Format("2006-01-02T15:04:05Z")
		val := rand.Intn(100)

		logLine := fmt.Sprintf(template, timestamp, val)
		f.WriteString(logLine)

		f.Sync() // This forces the OS to tell the Watcher that the file changed

		// 10ms = 100 logs per second.
		// 1ms  = 1,000 logs per second.
		time.Sleep(1 * time.Millisecond)
	}
}
