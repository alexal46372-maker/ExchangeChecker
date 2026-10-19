package config

import (
	"os"
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_SVC_06_ViberStore — viber.json defaults / round trip /
// validation, the token only in viber.dpapi (encrypted), "" removes it,
// and neither is part of export_config.
func TestCHK_SVC_06_ViberStore(t *testing.T) {
	s := newTestStore(t)
	v, err := s.LoadViber()
	if err != nil || v.Enabled || v.Schedule.From != "08:30" || v.Schedule.To != "17:00" || len(v.Schedule.Days) != 5 || v.ReminderMinutes != 60 {
		t.Fatalf("defaults %+v %v", v, err)
	}
	v.Enabled, v.Receivers = true, []string{"abc=="}
	if err := s.SaveViber(v); err != nil {
		t.Fatal(err)
	}
	if got, err := s.LoadViber(); err != nil || !got.Enabled || got.Receivers[0] != "abc==" {
		t.Fatalf("load %+v %v", got, err)
	}
	bad := v
	bad.Schedule.To = "08:00"
	if err := s.SaveViber(bad); err == nil {
		t.Fatal("to <= from saved")
	}
	if tok, err := s.LoadViberToken(); err != nil || tok != "" {
		t.Fatalf("no token %q %v", tok, err)
	}
	if err := s.SaveViberToken("SECRET-tok-123"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(s.Path(ViberTokenFile))
	js, _ := os.ReadFile(s.Path(ViberFile))
	if len(raw) == 0 || strings.Contains(string(js), "SECRET") {
		t.Fatalf("token placement %q", js)
	}
	if tok, err := s.LoadViberToken(); err != nil || tok != "SECRET-tok-123" {
		t.Fatalf("token %q %v", tok, err)
	}
	// export_config (ec-config/1) has no Viber field: settings and
	// receivers live outside settings.json, the token in viber.dpapi
	b, _ := marshalJSON(model.ConfigExport{Format: model.ConfigFormatV1})
	if strings.Contains(strings.ToLower(string(b)), "viber") {
		t.Fatalf("export shape contains Viber: %s", b)
	}
	if err := s.SaveViberToken(""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.Path(ViberTokenFile)); !os.IsNotExist(err) {
		t.Fatalf("token file kept: %v", err)
	}
}
