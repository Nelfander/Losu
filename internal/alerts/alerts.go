package alerts

import (
	"fmt"
	"net/http"
	"os"
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

// Trigger executes all alert actions
func (a *Alerter) Trigger(event model.LogEvent) {
	a.mu.Lock()
	last, exists := a.LastSent[event.Message]

	// If was sent this recently, just log it to file (quietly) and skip notification
	if exists && time.Since(last) < a.Cooldown {
		a.mu.Unlock()
		a.writeToLog(event, false) // Log but don't ping
		return
	}

	// Update the timestamp and unlock
	a.LastSent[event.Message] = time.Now()
	a.mu.Unlock()

	//  Log it with a "NOTIFIED" tag
	a.writeToLog(event, true)

	//  Visual Notification
	title := fmt.Sprintf("🚨 %s: Critical Pattern", event.Level)
	go func() {
		// Native popup
		_ = beeep.Alert(title, event.Message, "")
		// Phone alert
		if event.Level == "ERROR" {
			go a.SendToPhone(event.Message)
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
	//  send the message
	req, _ := http.NewRequest("POST", url, strings.NewReader(message))

	// Add some sexy text for the mobile app
	req.Header.Set("Title", "🚨 LOSU Critical Alert")
	req.Header.Set("Priority", "high") // Makes it bypass some do not disturb settings
	req.Header.Set("Tags", "warning,skull")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return // dont crash the app if the internet is down
	}
	defer resp.Body.Close()
}
