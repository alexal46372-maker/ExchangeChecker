package trayipc

import (
	"encoding/json"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_TRAY_36_RepeatNested — {"type":"alert_repeat","alert":{…}}: the
// nested alert is parsed; only with the alert_repeat capability.
func TestCHK_TRAY_36_RepeatNested(t *testing.T) {
	raw := `{"type":"alert_repeat","alert":{"id":77,"eventId":77,"kind":"alert_repeat","repeatOf":5,"profileId":"a","pointKey":"k1","level":"error","checkSeq":9,"stillFailing":true}}`
	var m model.IPCMessage
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	var got []Event
	s := New(Options{OnEvent: func(e Event) { got = append(got, e) }})
	s.opt.OnEvent = nil // collect synchronously below
	var evs []Event
	s.opt.OnEvent = func(e Event) {}
	s.st.Version, s.st.Capabilities = 2, []string{model.CapAlertRepeat}
	s.onPush(m)
	s.evMu.Lock()
	evs = append(evs, s.events...)
	s.events = nil
	s.evMu.Unlock()
	if len(evs) != 1 || evs[0].Kind != EventAlertRepeat || evs[0].Alert == nil || evs[0].Alert.ProfileID != "a" ||
		evs[0].Alert.RepeatOf != 5 || evs[0].Alert.Level != model.LevelError || evs[0].Alert.CheckSeq != 9 {
		t.Fatalf("%+v", evs)
	}
	s.st.Capabilities = nil
	s.onPush(m)
	s.evMu.Lock()
	n := len(s.events)
	s.evMu.Unlock()
	if n != 0 || len(got) != 0 {
		t.Error("without the capability the repeat is ignored")
	}
}
