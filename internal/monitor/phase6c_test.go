package monitor

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_IPC_06_BeepEnabledSettings — beep.enabled (PLAN §12 p.65):
// true by default in get_status.settings and get_config; save_settings
// with false is applied (snapshot, get_config, file); save_settings
// without the field (a client that does not know it) keeps the stored
// value; switching back to true works.
func TestCHK_IPC_06_BeepEnabledSettings(t *testing.T) {
	e := newCfgEnv(t)
	m := e.m
	beep := func() (snap, cfg model.OptBool) {
		t.Helper()
		s := m.Snapshot()
		if s.Settings == nil {
			t.Fatal("no settings in the snapshot")
		}
		c, _, err := m.GetConfig()
		if err != nil {
			t.Fatal(err)
		}
		return s.Settings.Beep.Enabled, c.Settings.Beep.Enabled
	}
	if s, c := beep(); s != model.OptTrue || c != model.OptTrue {
		t.Fatalf("default: snapshot %v get_config %v", s, c)
	}
	off := model.DefaultSettings()
	off.Beep.Enabled = model.OptFalse
	if _, err := m.SaveSettings(off, e.rev(), "x"); err != nil {
		t.Fatal(err)
	}
	if s, c := beep(); s != model.OptFalse || c != model.OptFalse {
		t.Fatalf("after false: snapshot %v get_config %v", s, c)
	}
	// field not sent: the stored false is kept
	unset := model.DefaultSettings()
	unset.Beep.Enabled = model.OptUnset
	unset.WarnMinutes = 25
	if _, err := m.SaveSettings(unset, e.rev(), "x"); err != nil {
		t.Fatal(err)
	}
	if s, c := beep(); s != model.OptFalse || c != model.OptFalse || m.Snapshot().Settings.WarnMinutes != 25 {
		t.Fatalf("unset kept: snapshot %v get_config %v", s, c)
	}
	on := model.DefaultSettings()
	if _, err := m.SaveSettings(on, e.rev(), "x"); err != nil {
		t.Fatal(err)
	}
	if s, c := beep(); s != model.OptTrue || c != model.OptTrue {
		t.Fatalf("back to true: snapshot %v get_config %v", s, c)
	}
	// JSON of the snapshot always carries the field
	b, _ := json.Marshal(m.Snapshot().Settings)
	var raw struct {
		Beep map[string]interface{} `json:"beep"`
	}
	if err := json.Unmarshal(b, &raw); err != nil || raw.Beep["enabled"] != true {
		t.Fatalf("snapshot JSON: %s", b)
	}
}

// writeProfilesByHand writes profiles.json without validation (a manual
// edit), so it may contain an invalid profile.
func writeProfilesByHand(t *testing.T, st *config.Store, ps ...model.Profile) {
	t.Helper()
	b, err := json.Marshal(struct {
		Version  int             `json:"version"`
		Profiles []model.Profile `json:"profiles"`
	}{config.SchemaVersion, ps})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.Path(config.ProfilesFile), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestCHK_IPC_06_OtherInvalidProfileID — a write refused because ANOTHER
// profile in profiles.json is invalid names it: IPCError.ProfileID
// (-> reply profileId); errorArgs[0] is its field as before, the text is
// unchanged; an error of the saved profile itself has no profileId; the
// broken profile itself can be deleted / fixed.
func TestCHK_IPC_06_OtherInvalidProfileID(t *testing.T) {
	e := newCfgEnv(t)
	m := e.m
	bad := prof("bad")
	bad.Server = `host\`
	writeProfilesByHand(t, e.st, prof("a"), bad)
	wantText := bad.Validate().Error()
	check := func(what string, err error) {
		t.Helper()
		ie := wantCode(t, what, err, model.ErrCodeInvalidRequest, model.MsgMonitorProfileInvalid)
		if ie.ProfileID != "bad" || len(ie.Args) < 2 || ie.Args[0] != "server" || ie.Args[1] != model.ValServerFormat || ie.Message != wantText {
			t.Errorf("%s: %+v (want profileId bad, errorArgs [server %s ...], text %q)", what, ie, model.ValServerFormat, wantText)
		}
	}
	before := e.files()
	_, err := m.SaveProfile(prof("c"), "", false, e.rev(), "x")
	check("save_profile (new)", err)
	_, err = m.SaveProfile(prof("a"), "", false, e.rev(), "x")
	check("save_profile (existing)", err)
	_, err = m.DeleteProfile("a", e.rev(), "x")
	check("delete_profile", err)
	if e.files() != before {
		t.Fatal("refused write changed files")
	}
	// the saved profile's own error: no profileId
	self := prof("c")
	self.Database = ""
	_, err = m.SaveProfile(self, "", false, e.rev(), "x")
	if ie := wantCode(t, "own error", err, model.ErrCodeInvalidRequest, model.MsgMonitorProfileInvalid); ie.ProfileID != "" || ie.Args[0] != "database" {
		t.Errorf("own error: %+v", ie)
	}
	// save_settings does not validate profiles: never refused by them
	if _, err := m.SaveSettings(model.DefaultSettings(), e.rev(), "x"); err != nil {
		t.Errorf("save_settings: %v", err)
	}
	// fixing the broken profile itself is allowed (it is not "another")
	fixed := prof("bad")
	if _, err := m.SaveProfile(fixed, "", false, e.rev(), "x"); err != nil {
		t.Fatalf("fix the broken profile: %v", err)
	}
	if _, err := m.DeleteProfile("a", e.rev(), "x"); err != nil {
		t.Fatalf("delete after the fix: %v", err)
	}
	// duplicate id written by hand
	writeProfilesByHand(t, e.st, prof("a"), prof("d"), prof("d"))
	_, err = m.SaveProfile(prof("a"), "", false, e.rev(), "x")
	ie := wantCode(t, "duplicate", err, model.ErrCodeInvalidRequest, model.MsgMonitorProfileInvalid)
	if ie.ProfileID != "d" || ie.Args[0] != "id" || ie.Args[1] != model.ValDuplicate {
		t.Errorf("duplicate: %+v", ie)
	}
}
