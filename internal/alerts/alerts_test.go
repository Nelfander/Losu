package alerts

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nelfander/losu/internal/model"
)

func TestAlerts_Cooldown(t *testing.T) {
	tmpLog := "test_alerts.log"
	defer os.Remove(tmpLog)

	alerter := NewAlerter(tmpLog)
	alerter.Cooldown = 500 * time.Millisecond

	event := model.LogEvent{Level: "ERROR", Message: "First Error"}

	alerter.Trigger(event, 5.0) // EPS above threshold — ensures phone path is exercised
	firstSent := alerter.LastSent["GLOBAL_ERROR_COOLDOWN"]

	alerter.Trigger(model.LogEvent{Level: "ERROR", Message: "Second Error"}, 5.0)
	secondSent := alerter.LastSent["GLOBAL_ERROR_COOLDOWN"]

	if !firstSent.Equal(secondSent) {
		t.Error("Cooldown failed: LastSent timestamp was updated during cooldown period")
	}
}

func TestAlerts_RaceCondition(t *testing.T) {
	tmpLog := "test_race.log"
	defer os.Remove(tmpLog)

	alerter := NewAlerter(tmpLog)
	alerter.Cooldown = 1 * time.Millisecond

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(val int) {
			defer wg.Done()
			alerter.Trigger(model.LogEvent{Level: "ERROR", Message: "Race test"}, 0.0)
		}(i)
	}
	wg.Wait()
}

func TestAlerts_LevelIsolation(t *testing.T) {
	tmpLog := "test_iso.log"
	defer os.Remove(tmpLog)

	alerter := NewAlerter(tmpLog)

	alerter.Trigger(model.LogEvent{Level: "ERROR", Message: "Err"}, 0.0)
	alerter.Trigger(model.LogEvent{Level: "WARN", Message: "Wrn"}, 0.0)

	if len(alerter.LastSent) != 2 {
		t.Errorf("Expected 2 distinct cooldown keys, got %d", len(alerter.LastSent))
	}
}

func TestAlerts_LogFileWritten(t *testing.T) {
	tmpLog := "test_written.log"
	defer os.Remove(tmpLog)

	alerter := NewAlerter(tmpLog)
	alerter.Trigger(model.LogEvent{Level: "ERROR", Message: "disk failure"}, 0.0)

	time.Sleep(50 * time.Millisecond)

	data, err := os.ReadFile(tmpLog)
	if err != nil {
		t.Fatalf("log file was not created: %v", err)
	}
	if len(data) == 0 {
		t.Error("log file is empty — writeToLog did not write anything")
	}
	content := string(data)
	if !strings.Contains(content, "disk failure") {
		t.Errorf("log file missing message, got: %q", content)
	}
	if !strings.Contains(content, "NOTIFIED") {
		t.Errorf("first trigger should write NOTIFIED status, got: %q", content)
	}
}

func TestAlerts_CooldownResetAfterExpiry(t *testing.T) {
	tmpLog := "test_reset.log"
	defer os.Remove(tmpLog)

	alerter := NewAlerter(tmpLog)
	alerter.Cooldown = 50 * time.Millisecond

	alerter.Trigger(model.LogEvent{Level: "ERROR", Message: "first"}, 0.0)
	firstSent := alerter.LastSent["GLOBAL_ERROR_COOLDOWN"]

	time.Sleep(100 * time.Millisecond)

	alerter.Trigger(model.LogEvent{Level: "ERROR", Message: "second"}, 0.0)
	secondSent := alerter.LastSent["GLOBAL_ERROR_COOLDOWN"]

	if !secondSent.After(firstSent) {
		t.Error("LastSent was not updated after cooldown expired")
	}
}

func TestAlerts_EmptyNtfyTopicSkipsPhone(t *testing.T) {
	tmpLog := "test_ntfy.log"
	defer os.Remove(tmpLog)

	alerter := NewAlerter(tmpLog)
	alerter.NtfyTopic = ""

	done := make(chan struct{})
	go func() {
		alerter.SendToPhone("should not be sent")
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Error("SendToPhone did not return immediately for empty NtfyTopic")
	}
}
