package monitor

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_SVC_06_SettingsViber — settings.viber through save_settings:
// viberToken sets the token (write-only, viber.dpapi), absent = no
// change, clearViberToken removes it, enabled needs a token, viber
// absent = the stored one is kept; views (get_config, snapshot) carry
// hasToken and status; export_config omits viber and import_config keeps
// the stored one.
func TestCHK_SVC_06_SettingsViber(t *testing.T) {
	e := newCfgEnv(t)
	m := e.m
	tokens := 0
	m.opt.OnViberToken = func() { tokens++ }
	m.opt.ViberStatus = func() *model.ViberStatus {
		return &model.ViberStatus{Queued: 1, Sent: 7, LastErrorKey: model.MsgViberTLS}
	}

	st := model.DefaultSettings()
	st.Viber.Enabled, st.Viber.Recipients = true, []model.ViberRecipient{{ID: "r1", Name: "Иван"}}
	wantCode(t, "enabled without token", errOf(m.SaveSettingsViber(st, "", false, e.rev(), "a")), model.ErrCodeInvalidRequest, model.MsgViberNoToken)
	wantCode(t, "bad token", errOf(m.SaveSettingsViber(st, "a b", false, e.rev(), "a")), model.ErrCodeInvalidRequest, model.MsgViberInvalid)
	bad := model.DefaultSettings()
	bad.Viber.Events = []string{"overdue"}
	ie := wantCode(t, "bad event", errOf(m.SaveSettingsViber(bad, "", false, e.rev(), "a")), model.ErrCodeInvalidRequest, model.MsgMonitorSettingsInvalid)
	if len(ie.Args) < 2 || ie.Args[0] != "viber.events[0]" {
		t.Fatalf("args %v", ie.Args)
	}
	if _, err := m.SaveSettingsViber(st, "TOKEN-1", false, e.rev(), "a"); err != nil {
		t.Fatal(err)
	}
	if tok, _ := e.st.LoadViberToken(); tok != "TOKEN-1" || tokens != 1 {
		t.Fatalf("token %q %d", tok, tokens)
	}
	raw, _ := os.ReadFile(e.st.Path(config.SettingsFile))
	if strings.Contains(string(raw), "TOKEN") || !strings.Contains(string(raw), `"recipients"`) {
		t.Fatalf("settings.json %s", raw)
	}
	// absent token = no change; viber absent = kept
	old := st
	old.Viber = nil
	old.WarnMinutes = 20
	if _, err := m.SaveSettings(old, e.rev(), "a"); err != nil {
		t.Fatal(err)
	}
	view, _, _ := m.GetConfig()
	b, _ := json.Marshal(view.Settings.Viber)
	if string(b) != `{"enabled":true,"hasToken":true,"recipients":[{"id":"r1","name":"Иван"}],"schedule":{"days":[1,2,3,4,5],"from":"08:30","to":"17:00"},"events":["error","warning","recovered","no_connection"],"reminderMinutes":60,"status":{"queued":1,"sent":7,"dropped":0,"lastErrorKey":"notify.viber_tls"}}` || tokens != 1 {
		t.Fatalf("get_config viber %s (%d)", b, tokens)
	}
	m.reloadConfig(true)
	if s := m.Snapshot(); s.Settings == nil || s.Settings.Viber == nil || !s.Settings.Viber.HasToken || s.Settings.Viber.Status == nil || s.Settings.WarnMinutes != 20 {
		t.Fatalf("snapshot %+v", s.Settings)
	}
	// export omits viber
	x, err := m.ExportConfig()
	xb, _ := json.Marshal(x)
	if err != nil || x.Settings.Viber != nil || strings.Contains(string(xb), "viber") || strings.Contains(string(xb), "r1") {
		t.Fatalf("export %s %v", xb, err)
	}
	// import (replace, with a foreign viber block): the stored viber stays
	imp := x
	is := *x.Settings
	is.WarnMinutes = 25
	is.Viber = &model.ViberSettings{Enabled: false}
	imp.Settings = &is
	if _, err := m.ImportConfig(&imp, model.ImportReplace, e.rev(), false, "a"); err != nil {
		t.Fatal(err)
	}
	cur, _ := e.st.LoadSettings()
	if cur.WarnMinutes != 25 || !cur.Viber.Enabled || cur.Viber.Recipients[0].ID != "r1" {
		t.Fatalf("after import %+v %+v", cur, cur.Viber)
	}
	// clear: enabled must be off first
	wantCode(t, "clear while enabled", errOf(m.SaveSettingsViber(cur, "", true, e.rev(), "a")), model.ErrCodeInvalidRequest, model.MsgViberNoToken)
	cur.Viber.Enabled = false
	if _, err := m.SaveSettingsViber(cur, "", true, e.rev(), "a"); err != nil {
		t.Fatal(err)
	}
	if e.st.HasViberToken() || tokens != 2 {
		t.Fatalf("token kept %d", tokens)
	}
	if view, _, _ := m.GetConfig(); view.Settings.Viber.HasToken {
		t.Fatal("hasToken after clear")
	}
}

func errOf(_ string, err error) error { return err }

// TestCHK_MON_16_SameProfileNilVsEmpty — sameProfile treats nil and empty
// slices (mappings: [] vs absent) as equal, a real difference stays.
func TestCHK_MON_16_SameProfileNilVsEmpty(t *testing.T) {
	a := prof("p1")
	a.Mappings = nil
	b := a
	b.Mappings = []model.Mapping{}
	b.HasPassword = true
	if !sameProfile(a, b) || !sameProfile(b, a) {
		t.Fatal("nil vs empty mappings differ")
	}
	var c model.Profile
	if err := json.Unmarshal([]byte(`{"id":"p1","mappings":[]}`), &c); err != nil {
		t.Fatal(err)
	}
	var d model.Profile
	_ = json.Unmarshal([]byte(`{"id":"p1"}`), &d)
	if !sameProfile(c, d) {
		t.Fatal("json [] vs absent differ")
	}
	b.Name = a.Name + "x"
	if sameProfile(a, b) {
		t.Fatal("different names equal")
	}
	if a.Mappings != nil {
		t.Fatal("input modified")
	}
}
