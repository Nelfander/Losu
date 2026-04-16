package alerts

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gen2brain/beeep"
	"github.com/nelfander/losu/internal/model"
)

type Alerter struct {
	LogPath   string
	LastSent  map[string]time.Time // Track when last notified for a pattern
	mu        sync.Mutex
	Cooldown  time.Duration // How long to wait between pings
	NtfyTopic string
}

func NewAlerter(path string) *Alerter {
	return &Alerter{
		LogPath:  path,
		LastSent: make(map[string]time.Time),
		Cooldown: 20 * time.Second,
	}
}

// alertEPSThreshold reads LOSU_ALERT_EPS_THRESHOLD from env.
// Phone alerts are only sent when current EPS is at or above this value.
// Default: 1.0 — avoids spamming your phone for occasional single errors.
func alertEPSThreshold() float64 {
	if v := os.Getenv("LOSU_ALERT_EPS_THRESHOLD"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return 1.0
}

// Trigger executes all alert actions for an event.
// currentEPS is the rolling average EPS at the time of the alert —
// phone notifications are suppressed below LOSU_ALERT_EPS_THRESHOLD.
func (a *Alerter) Trigger(event model.LogEvent, currentEPS float64) {
	a.mu.Lock()

	alertKey := "GLOBAL_ERROR_COOLDOWN"
	if event.Level == "WARN" {
		alertKey = "GLOBAL_WARN_COOLDOWN"
	}

	last, exists := a.LastSent[alertKey]
	if exists && time.Since(last) < a.Cooldown {
		a.mu.Unlock()
		a.writeToLog(event, false)
		return
	}

	a.LastSent[alertKey] = time.Now()
	a.mu.Unlock()

	a.writeToLog(event, true)

	title := fmt.Sprintf("🚨 %s: System Alert", event.Level)
	go func() {
		// Native desktop popup — always fires regardless of EPS
		_ = beeep.Alert(title, event.Message, "")
		// Phone alert — only fires when EPS is at or above threshold
		if event.Level == "ERROR" && a.NtfyTopic != "" {
			if currentEPS >= alertEPSThreshold() {
				a.SendToPhone(event.Message)
			}
		}
	}()
}

func (a *Alerter) writeToLog(event model.LogEvent, notified bool) {
	f, err := os.OpenFile(a.LogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	status := "LOGGED"
	if notified {
		status = "NOTIFIED"
	}

	alertLine := fmt.Sprintf("[%s] [%s] %s: %s\n",
		time.Now().Format("2006-01-02 15:04:05"),
		status,
		event.Level,
		event.Message,
	)
	f.WriteString(alertLine)
}

func (a *Alerter) SendToPhone(message string) {
	if a.NtfyTopic == "" {
		return
	}

	url := "https://ntfy.sh/" + a.NtfyTopic
	req, _ := http.NewRequest("POST", url, strings.NewReader(message))
	req.Header.Set("Title", "🚨 LOSU Critical Alert")
	req.Header.Set("Priority", "high")
	req.Header.Set("Tags", "warning,skull")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
}

// PushNotification sends a plain text alert (used for Heartbeats/Summaries)
func (a *Alerter) PushNotification(title, message string) {
	f, _ := os.OpenFile(a.LogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if f != nil {
		defer f.Close()
		timestamp := time.Now().Format("2006-01-02 15:04:05")
		fmt.Fprintf(f, "[%s] %s: %s\n", timestamp, title, message)
	}

	if a.NtfyTopic != "" {
		url := "https://ntfy.sh/" + a.NtfyTopic
		req, _ := http.NewRequest("POST", url, strings.NewReader(message))
		req.Header.Set("Title", title)
		req.Header.Set("Tags", "bar_chart,heartbeat")

		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
		}
	}
}
