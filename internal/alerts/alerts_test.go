package alerts

import (
	"os"
	"sync"
	"testing"
	"time"

	"github.com/nelfander/losu/internal/model"
)

/*
	TestAlerts_Cooldown verifies that the throttling logic works.

If two alerts of the same level are triggered within the cooldown period,
the second one should be logged but NOT update the 'LastSent' timestamp
or trigger a new notification.
*/
func TestAlerts_Cooldown(t *testing.T) {
	tmpLog := "test_alerts.log"
	defer os.Remove(tmpLog)

	alerter := NewAlerter(tmpLog)
	alerter.Cooldown = 500 * time.Millisecond // Short cooldown for testing

	event := model.LogEvent{Level: "ERROR", Message: "First Error"}

	// First trigger
	alerter.Trigger(event)
	firstSent := alerter.LastSent["GLOBAL_ERROR_COOLDOWN"]

	// Immediate second trigger (should be ignored by cooldown)
	alerter.Trigger(model.LogEvent{Level: "ERROR", Message: "Second Error"})
	secondSent := alerter.LastSent["GLOBAL_ERROR_COOLDOWN"]

	if !firstSent.Equal(secondSent) {
		t.Error("Cooldown failed: LastSent timestamp was updated during cooldown period")
	}
}

/*
	TestAlerts_RaceCondition runs multiple triggers in parallel.

When run with 'go test -race', this ensures the Mutex is correctly
protecting the LastSent map from concurrent write access.
*/
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
			alerter.Trigger(model.LogEvent{Level: "ERROR", Message: "Race test"})
		}(i)
	}
	wg.Wait()
}

/*
	TestAlerts_LevelIsolation ensures that a WARN alert

does not trigger the cooldown for an ERROR alert.
They should be tracked independently.
*/
func TestAlerts_LevelIsolation(t *testing.T) {
	tmpLog := "test_iso.log"
	defer os.Remove(tmpLog)

	alerter := NewAlerter(tmpLog)

	// Trigger ERROR
	alerter.Trigger(model.LogEvent{Level: "ERROR", Message: "Err"})
	// Trigger WARN
	alerter.Trigger(model.LogEvent{Level: "WARN", Message: "Wrn"})

	if len(alerter.LastSent) != 2 {
		t.Errorf("Expected 2 distinct cooldown keys, got %d", len(alerter.LastSent))
	}
}
