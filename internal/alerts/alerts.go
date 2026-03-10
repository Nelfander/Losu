package alerts

import (
	"fmt"
	"os"
	"time"

	"github.com/nelfander/losu/internal/model"
)

type Alerter struct {
	LogPath string
}

func NewAlerter(path string) *Alerter {
	return &Alerter{LogPath: path}
}

// Trigger executes all alert actions
func (a *Alerter) Trigger(event model.LogEvent) {
	a.writeToLog(event)

	// Play System Sound (The "Bell" character)
	// \a is the ASCII Bell character which triggers the system beep
	fmt.Print("\a")
}

func (a *Alerter) writeToLog(event model.LogEvent) {
	f, err := os.OpenFile(a.LogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	alertLine := fmt.Sprintf("[%s] CRITICAL ERROR: %s\n",
		time.Now().Format("2006-01-02 15:04:05"),
		event.Message,
	)
	f.WriteString(alertLine)
}
