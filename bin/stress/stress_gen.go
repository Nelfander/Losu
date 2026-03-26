/* package main

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
	formats := []string{
		"time=%s level=INFO msg=\"User logged in\" user_id=%d\n",
		"time=%s level=WARN msg=\"Failed login attempt\" ip=192.168.1.%d user=\"admin\"\n",
		"time=%s level=ERROR msg=\"Query timeout\" duration=%dms query_id=q_99\n",
		"time=%s level=DEBUG msg=\"Cache hit\" key=\"user_profile_%d\"\n",
		"time=%s level=WARN msg=\"High memory usage\" threshold=%d%%\n",
		"time=%s level=INFO msg=\"HTTP request finished\" status=200 duration=%dms\n",
		"time=%s level=ERROR msg=\"S3 upload failed\" bucket=\"assets\" key=\"img_%d.png\"\n",
		"time=%s level=INFO msg=\"Order processed\" amount=$%d.99 currency=\"USD\"\n",
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
