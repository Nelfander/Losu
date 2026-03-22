package main

import (
	"fmt"
	"math/rand"
	"os"
	"strings"
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
		// --- AUTH & USERS (Mostly INFO) ---
		"time=%s level=INFO msg=\"User logged in\" user_id=%d\n",
		"time=%s level=WARN msg=\"Failed login attempt\" ip=%d.%d.%d.%d user=\"admin\"\n",
		"time=%s level=INFO msg=\"Password reset requested\" user_id=%d\n",
		"time=%s level=ERROR msg=\"Session fixation attempt detected\" session_id=sess_%d\n",
		"time=%s level=INFO msg=\"User profile updated\" user_id=%d fields=\"bio,avatar\"\n",
		"time=%s level=DEBUG msg=\"OIDC callback received\" provider=\"google\" state=%d\n",

		// --- DATABASE & CACHE ---
		"%s [ERROR] Connection failed to database_%d\n",
		"time=%s level=ERROR msg=\"Query timeout\" duration=500ms query_id=q_%d\n",
		"time=%s level=WARN msg=\"Slow query detected\" duration=%dms sql=\"SELECT * FROM users\"\n",
		"time=%s level=DEBUG msg=\"Cache hit\" key=\"user_profile_%d\"\n",
		"time=%s level=DEBUG msg=\"Cache miss\" key=\"product_inventory_%d\"\n",
		"time=%s level=ERROR msg=\"Redis connection refused\" host=\"127.0.0.1:%d\"\n",
		"time=%s level=INFO msg=\"Database migration started\" version=%d\n",

		// --- INFRASTRUCTURE ---
		"time=%s level=WARN msg=\"High memory usage\" threshold=%d%%\n",
		"playingfield-app | time=%s level=DEBUG msg=\"websocket: close 1001\" user_id=%d\n",
		"time=%s level=ERROR msg=\"Disk space critical\" partition=\"/var/log\" usage=%d%%\n",
		"time=%s level=ERROR msg=\"Panic recovered\" stack_trace=\"0x%x 0x%x 0x%x\"\n",
		"time=%s level=DEBUG msg=\"GC cycle completed\" heap_size=%dMB\n",

		// --- NETWORKING ---
		"time=%s level=INFO msg=\"HTTP request started\" method=GET path=\"/api/v1/products/%d\"\n",
		"time=%s level=INFO msg=\"HTTP request finished\" status=200 duration=%dms\n",
		"time=%s level=WARN msg=\"HTTP request failed\" status=404 path=\"/favicon.ico\" ip=%d.%d.%d.%d\n",
		"time=%s level=ERROR msg=\"Upstream service unavailable\" service=\"billing\" code=%d\n",

		// --- BUSINESS ---
		"time=%s level=INFO msg=\"Order processed\" amount=$%d.99 currency=\"USD\"\n",
		"time=%s level=WARN msg=\"Inventory low for SKU\" sku=\"SKU-%d\" stock=%d\n",
	}

	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	fmt.Println("🏗 Normal Condition Generator started (Weighted Traffic).")
	fmt.Println("   - INFO/DEBUG: Constant")
	fmt.Println("   - WARN: ~10% chance")
	fmt.Println("   - ERROR: ~3% chance")

	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	for {
		sleep := time.Duration(r.Intn(750)+50) * time.Millisecond
		time.Sleep(sleep)

		template := formats[r.Intn(len(formats))]

		isError := strings.Contains(template, "ERROR")
		isWarn := strings.Contains(template, "WARN")

		roll := r.Intn(100)
		if isError && roll > 3 {
			continue
		}
		if isWarn && roll > 10 {
			continue
		}

		timestamp := time.Now().Format("2006-01-02T15:04:05Z")

		needed := strings.Count(template, "%") - (strings.Count(template, "%%") * 2)

		// Create a slice of interface{} to hold args
		args := make([]interface{}, needed)
		args[0] = timestamp // The first %s is always the timestamp

		for i := 1; i < needed; i++ {
			args[i] = r.Intn(1000) // Fill the rest with random numbers
		}

		// Use the ... operator to pass the slice as individual arguments
		logLine := fmt.Sprintf(template, args...)

		f.WriteString(logLine)
		f.Sync()
	}
}
