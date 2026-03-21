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
		// --- AUTH & USERS ---
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
		"time=%s level=INFO msg=\"Database vacuum complete\" table=\"logs_%d\"\n",

		// --- INFRASTRUCTURE & RESOURCE ---
		"time=%s level=WARN msg=\"High memory usage\" threshold=%d%%\n",
		"playingfield-app | time=%s level=DEBUG msg=\"websocket: close 1001\" user_id=%d\n",
		"time=%s level=ERROR msg=\"Disk space critical\" partition=\"/var/log\" usage=%d%%\n",
		"time=%s level=INFO msg=\"CPU scaling governor changed\" cpu=%d\n",
		"time=%s level=WARN msg=\"Context deadline exceeded\" service=\"payment-gate\" trace_id=%d\n",
		"time=%s level=ERROR msg=\"Panic recovered\" stack_trace=\"0x%x 0x%x 0x%x\"\n",
		"time=%s level=DEBUG msg=\"GC cycle completed\" heap_size=%dMB\n",

		// --- NETWORKING & API ---
		"time=%s level=INFO msg=\"HTTP request started\" method=GET path=\"/api/v1/products/%d\"\n",
		"time=%s level=INFO msg=\"HTTP request finished\" status=200 duration=%dms\n",
		"time=%s level=WARN msg=\"HTTP request failed\" status=404 path=\"/favicon.ico\" ip=%d.%d.%d.%d\n",
		"time=%s level=ERROR msg=\"Upstream service unavailable\" service=\"billing\" code=%d\n",
		"time=%s level=DEBUG msg=\"CORS preflight allowed\" origin=\"https://app.%d.com\"\n",
		"time=%s level=INFO msg=\"Rate limit reached for API key\" key=\"pk_live_%d\"\n",
		"time=%s level=ERROR msg=\"Invalid TLS handshake\" remote_addr=\"%d.%d.%d.%d:%d\"\n",

		// --- MICROSERVICES & WORKERS ---
		"time=%s level=INFO msg=\"Kafka message consumed\" topic=\"orders\" partition=%d offset=%d\n",
		"time=%s level=ERROR msg=\"Worker crashed\" worker_id=worker_%d restart_count=%d\n",
		"time=%s level=DEBUG msg=\"Image processing started\" asset_id=%d format=\"webp\"\n",
		"time=%s level=INFO msg=\"Email sent successfully\" template=\"welcome\" user_id=%d\n",
		"time=%s level=WARN msg=\"Job retrying\" job_id=%d attempt=%d\n",
		"time=%s level=ERROR msg=\"S3 upload failed\" bucket=\"assets\" key=\"img_%d.png\"\n",
		"time=%s level=DEBUG msg=\"Heartbeat sent to orchestrator\" node_id=\"node-%d\"\n",

		// --- SECURITY & ANOMALIES ---
		"time=%s level=ERROR msg=\"SQL Injection attempt blocked\" input=\"' OR 1=1--\" ip=%d.%d.%d.%d\n",
		"time=%s level=WARN msg=\"Unrecognized file change\" path=\"/etc/shadow\" pid=%d\n",
		"time=%s level=INFO msg=\"SSH login successful\" user=\"root\" ip=%d.%d.%d.%d\n",
		"time=%s level=ERROR msg=\"Buffer overflow prevented\" service=\"parser\" offset=%d\n",

		// --- RANDOM BUSINESS EVENTS ---
		"time=%s level=INFO msg=\"Order processed\" amount=$%d.99 currency=\"USD\"\n",
		"time=%s level=INFO msg=\"Subscription canceled\" plan=\"pro\" user_id=%d\n",
		"time=%s level=DEBUG msg=\"Feature flag toggled\" flag=\"new_ui\" enabled=true user_id=%d\n",
		"time=%s level=WARN msg=\"Inventory low for SKU\" sku=\"SKU-%d\" stock=%d\n",
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
