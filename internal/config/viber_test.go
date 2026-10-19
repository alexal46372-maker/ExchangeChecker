package config

import (
	"os"
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_SVC_06_ViberStore — settings.viber defaults / round trip /
// validation in settings.json (hasToken and status never stored), the
// token only in viber.dpapi (encrypted), "" removes it.
func TestCHK_SVC_06_ViberStore(t *testing.T) {
	s := newTestStore(t)
	st, err := s.LoadSettings()
	v := st.Viber
	if err != nil || v == nil || v.Enabled || v.Schedule.From != "08:30" || v.Schedule.To != "17:00" || len(v.Schedule.Days) != 5 ||
		v.ReminderMinutes != 60 || strings.Join(v.Events, ",") != "error,warning,recovered,no_connection" || v.Recipients == nil {
		t.Fatalf("defaults %+v %v", v, err)
	}
	v.Enabled, v.Recipients, v.HasToken = true, []model.ViberRecipient{{ID: "abc==", Name: "Иван"}}, true
	v.Status = &model.ViberStatus{Sent: 3}
	if err := s.SaveSettings(st); err != nil {
		t.Fatal(err)
	}
	js, _ := os.ReadFile(s.Path(SettingsFile))
	if strings.Contains(string(js), `"hasToken": true`) || strings.Contains(string(js), "status") {
		t.Fatalf("view fields stored: %s", js)
	}
	if got, err := s.LoadSettings(); err != nil || !got.Viber.Enabled || got.Viber.Recipients[0].Name != "Иван" || got.Viber.HasToken || got.Viber.Status != nil {
		t.Fatalf("load %+v %v", got.Viber, err)
	}
	// a settings.json from before phase 10: viber defaults
	if err := os.WriteFile(s.Path(SettingsFile), []byte(`{"version":1,"settings":{"pollIntervalSec":60,"warnMinutes":15,"errorMinutes":30,"beep":{"warningFreqHz":800,"warningDurMs":200,"errorFreqHz":1200,"errorDurMs":400,"gapMs":100}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := s.LoadSettings(); err != nil || got.Viber == nil || got.Viber.Enabled || len(got.Viber.Events) != 4 {
		t.Fatalf("old file %+v %v", got.Viber, err)
	}
	bad := model.DefaultSettings()
	bad.Viber.Schedule.To = "08:00"
	if err := s.SaveSettings(bad); model.AsFieldError(err) == nil || model.AsFieldError(err).Field != "viber.schedule.to" {
		t.Fatalf("to <= from: %v", err)
	}
	bad = model.DefaultSettings()
	bad.Viber.Events = []string{"overdue"}
	if err := s.SaveSettings(bad); model.AsFieldError(err) == nil || model.AsFieldError(err).Field != "viber.events[0]" {
		t.Fatalf("unknown event: %v", err)
	}

	if tok, err := s.LoadViberToken(); err != nil || tok != "" || s.HasViberToken() {
		t.Fatalf("no token %q %v", tok, err)
	}
	if err := s.SaveViberToken("SECRET-tok-123"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(s.Path(ViberTokenFile))
	js, _ = os.ReadFile(s.Path(SettingsFile))
	if len(raw) == 0 || strings.Contains(string(js), "SECRET") || !s.HasViberToken() {
		t.Fatalf("token placement %q", js)
	}
	if tok, err := s.LoadViberToken(); err != nil || tok != "SECRET-tok-123" {
		t.Fatalf("token %q %v", tok, err)
	}
	if err := s.SaveViberToken(""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.Path(ViberTokenFile)); !os.IsNotExist(err) || s.HasViberToken() {
		t.Fatalf("token file kept: %v", err)
	}
}
