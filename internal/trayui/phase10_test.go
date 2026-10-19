package trayui

import (
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_TRAY_36_TrackerSkipsRepeats — alert_repeat is push only: an
// events_since page never feeds it to balloons.
func TestCHK_TRAY_36_TrackerSkipsRepeats(t *testing.T) {
	var tr EventTracker
	last := uint64(2)
	fresh, _ := tr.Apply(model.IPCMessage{RunID: "r", LastID: &last, Events: []model.AlertEvent{
		{ID: 1, EventID: 1, ProfileID: "a", To: model.LevelError},
		{ID: 2, EventID: 2, ProfileID: "a", Kind: model.AlertKindRepeat, Level: model.LevelError}}})
	if len(fresh) != 1 || fresh[0].ID != 1 {
		t.Fatalf("%+v", fresh)
	}
}
