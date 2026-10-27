package monitor

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_CFG_06_ExportImport — export_config: format ec-config/1,
// settings + profiles with hasPassword, never a password; import_config:
// dryRun writes nothing, merge (default) / replace, added / updated /
// removed / needPassword, saved secrets kept only for the same target,
// expectedRevision mandatory, tray block ignored, full validation before
// any write.
func TestCHK_CFG_06_ExportImport(t *testing.T) {
	e := newCfgEnv(t)
	m := e.m
	if x, err := m.ExportConfig(); err != nil || x.ContainsLogins {
		t.Fatalf("empty export containsLogins %v %v", x.ContainsLogins, err)
	}
	if _, err := m.SaveProfile(prof("w0"), "", false, e.rev(), "a"); err != nil {
		t.Fatal(err)
	}
	x, _ := m.ExportConfig()
	if b, _ := json.Marshal(x); x.ContainsLogins || !strings.Contains(string(b), `"containsLogins":false`) {
		t.Fatalf("windows-only export %s", b)
	}
	if _, err := m.DeleteProfile("w0", e.rev(), "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SaveProfile(sqlProf6("s1"), "Pw-Secret!", false, e.rev(), "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SaveProfile(prof("w1"), "", false, e.rev(), "a"); err != nil {
		t.Fatal(err)
	}
	exp, err := m.ExportConfig()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(exp)
	if !exp.ContainsLogins || exp.Profiles[0].Login != "user1" || !strings.Contains(string(raw), `"containsLogins":true`) {
		t.Fatalf("containsLogins / login %s", raw)
	}
	if exp.Format != model.ConfigFormatV1 || exp.Settings == nil || len(exp.Profiles) != 2 || !exp.Profiles[0].HasPassword ||
		strings.Contains(string(raw), "Pw-Secret!") || strings.Contains(string(raw), `"tray"`) {
		t.Fatalf("export %s", raw)
	}

	// round trip, replace: nothing changes, the password stays
	exp.Tray = json.RawMessage(`{"trayMarkerXYZ":1}`)
	before := e.files()
	r, err := m.ImportConfig(&exp, model.ImportReplace, e.rev(), true, "a")
	if err != nil || !r.DryRun || len(r.Added)+len(r.Updated)+len(r.Removed)+len(r.NeedPassword) != 0 || r.Revision != e.rev() {
		t.Fatalf("dry run %+v %v", r, err)
	}
	r, err = m.ImportConfig(&exp, model.ImportReplace, e.rev(), false, "a")
	if err != nil || len(r.Added)+len(r.Updated)+len(r.Removed)+len(r.NeedPassword) != 0 || e.files() != before {
		t.Fatalf("round trip %+v %v", r, err)
	}
	if strings.Contains(e.files(), "trayMarkerXYZ") {
		t.Fatal("tray block stored")
	}

	// merge (default): s1 moves to another server -> needPassword, n1 added
	in := exp
	in.Tray = nil
	s1 := in.Profiles[0]
	s1.Server = "other"
	n1 := prof("n1")
	in.Profiles = []model.Profile{s1, n1}
	before = e.files()
	r, err = m.ImportConfig(&in, "", e.rev(), true, "a")
	if err != nil || r.Mode != model.ImportMerge || strings.Join(r.Added, ",") != "n1" || strings.Join(r.Updated, ",") != "s1" ||
		strings.Join(r.NeedPassword, ",") != "s1" || len(r.Removed) != 0 || e.files() != before {
		t.Fatalf("merge dry run %+v %v", r, err)
	}
	if _, err := m.ImportConfig(&in, "", "", false, "a"); err == nil {
		t.Fatal("import without expectedRevision")
	}
	wantCode(t, "conflict", func() error { _, err := m.ImportConfig(&in, "", "deadbeef", false, "a"); return err }(),
		model.ErrCodeConflict, model.MsgMonitorConfigConflict)
	r, err = m.ImportConfig(&in, "", e.rev(), false, "a")
	if err != nil || r.Revision != e.rev() {
		t.Fatalf("merge %+v %v", r, err)
	}
	if _, ok := e.secrets()["s1"]; ok || len(m.Snapshot().Profiles) != 3 {
		t.Fatalf("after merge: secrets %v profiles %d", len(e.secrets()), len(m.Snapshot().Profiles))
	}

	// replace with n1 only: s1, w1 removed
	in.Profiles = []model.Profile{n1}
	r, err = m.ImportConfig(&in, model.ImportReplace, e.rev(), false, "a")
	if err != nil || strings.Join(r.Removed, ",") != "w1,s1" && strings.Join(r.Removed, ",") != "s1,w1" || len(m.Snapshot().Profiles) != 1 {
		t.Fatalf("replace %+v %v", r, err)
	}

	// refusals write nothing
	before = e.files()
	bad := in
	bad.Format = "ec-config/2"
	wantCode(t, "format", func() error { _, err := m.ImportConfig(&bad, "", e.rev(), false, "a"); return err }(),
		model.ErrCodeInvalidRequest, model.MsgImportFormat)
	wantCode(t, "mode", func() error { _, err := m.ImportConfig(&in, "upsert", e.rev(), false, "a"); return err }(),
		model.ErrCodeInvalidRequest, model.MsgImportMode)
	dup := in
	dup.Profiles = []model.Profile{n1, n1}
	ie := wantCode(t, "dup", func() error { _, err := m.ImportConfig(&dup, "", e.rev(), false, "a"); return err }(),
		model.ErrCodeInvalidRequest, model.MsgImportDuplicateID)
	if ie.ProfileID != "n1" {
		t.Fatalf("dup profileId %q", ie.ProfileID)
	}
	inv := in
	p := n1
	p.Server = ""
	inv.Profiles = []model.Profile{prof("ok2"), p}
	ie = wantCode(t, "invalid", func() error { _, err := m.ImportConfig(&inv, "", e.rev(), false, "a"); return err }(),
		model.ErrCodeInvalidRequest, model.MsgMonitorProfileInvalid)
	if ie.ProfileID != "n1" || len(ie.Args) < 2 || ie.Args[0] != "server" {
		t.Fatalf("invalid %+v", ie)
	}
	st := *in.Settings
	st.PollIntervalSec = 1
	inv = in
	inv.Settings = &st
	wantCode(t, "settings", func() error { _, err := m.ImportConfig(&inv, "", e.rev(), false, "a"); return err }(),
		model.ErrCodeInvalidRequest, model.MsgMonitorSettingsInvalid)
	if e.files() != before {
		t.Fatal("refused import wrote files")
	}
	if e.lg.has("Pw-Secret!") {
		t.Fatal("password in the log")
	}
}

// failStore fails chosen writes of a config.Store.
type failStore struct {
	*config.Store
	failSecrets   bool
	failProfilesN int // fail the n-th SaveProfiles call (1-based), 0 = never
	profCalls     int
}

func (f *failStore) SaveSecrets(s config.Secrets) error {
	if f.failSecrets {
		return errors.New("disk full")
	}
	return f.Store.SaveSecrets(s)
}

func (f *failStore) SaveProfiles(ps []model.Profile) error {
	f.profCalls++
	if f.failProfilesN == f.profCalls {
		return errors.New("disk gone")
	}
	return f.Store.SaveProfiles(ps)
}

// TestCHK_CFG_06_TrayLimitAndRollback — a tray block above 256 KiB is
// refused (validation.too_long); write order profiles -> settings ->
// secrets: a failed secrets write restores profiles.json (save_failed),
// a failed restore is reported as monitor.import_rollback_failed.
func TestCHK_CFG_06_TrayLimitAndRollback(t *testing.T) {
	e := newCfgEnv(t)
	if _, err := e.m.SaveProfile(sqlProf6("s1"), "Pw-Secret!", false, e.rev(), "a"); err != nil {
		t.Fatal(err)
	}
	exp, _ := e.m.ExportConfig()
	big := exp
	big.Tray = json.RawMessage(`"` + strings.Repeat("x", model.ImportTrayMax) + `"`)
	ie := wantCode(t, "tray", func() error { _, err := e.m.ImportConfig(&big, "", e.rev(), true, "a"); return err }(),
		model.ErrCodeInvalidRequest, model.MsgImportTrayTooLarge)
	if len(ie.Args) != 3 || ie.Args[0] != "tray" || ie.Args[1] != model.ValTooLong || ie.Args[2] != "262144" {
		t.Fatalf("tray args %v", ie.Args)
	}
	ok := exp
	ok.Tray = json.RawMessage(`"` + strings.Repeat("x", model.ImportTrayMax-2) + `"`)
	if _, err := e.m.ImportConfig(&ok, "", e.rev(), true, "a"); err != nil {
		t.Fatalf("tray at the limit: %v", err)
	}

	fs := &failStore{Store: e.st, failSecrets: true}
	e.m.opt.ConfigStore = fs
	in := exp
	s1 := in.Profiles[0]
	s1.Server = "other" // the password must go: secrets are written
	in.Profiles = []model.Profile{s1}
	before := e.files()
	wantCode(t, "secrets fail", func() error { _, err := e.m.ImportConfig(&in, "", e.rev(), false, "a"); return err }(),
		model.ErrCodeSaveFailed, model.MsgMonitorConfigSaveFailed)
	if e.files() != before {
		t.Fatal("profiles not restored after the failed secrets write")
	}
	fs.profCalls, fs.failProfilesN = 0, 2 // the restore of profiles.json fails too
	wantCode(t, "rollback fails", func() error { _, err := e.m.ImportConfig(&in, "", e.rev(), false, "a"); return err }(),
		model.ErrCodeSaveFailed, model.MsgImportRollbackFailed)
	if e.secrets()["s1"] != "Pw-Secret!" {
		t.Fatal("secrets written although the import failed")
	}
}

// TestCHK_CFG_07_ExportImportViberRecipients — includeViberRecipients
// (default false): export omits settings.viber and sets
// containsViberRecipients false; with the flag, export carries non-secret
// viber (recipients [{id,name}], enabled, schedule, events,
// reminderMinutes) and never token / hasToken / status / viber.dpapi;
// containsViberRecipients is true when recipients are present. Import
// with the flag applies non-secret viber and leaves the saved token;
// without the flag (or without settings.viber) the stored viber is kept.
func TestCHK_CFG_07_ExportImportViberRecipients(t *testing.T) {
	e := newCfgEnv(t)
	m := e.m
	st := model.DefaultSettings()
	st.Viber.Enabled = true
	st.Viber.Recipients = []model.ViberRecipient{{ID: "r1", Name: "Иван"}, {ID: "r2", Name: "Пётр"}}
	if _, err := m.SaveSettingsViber(st, "TOKEN-SECRET", false, e.rev(), "a"); err != nil {
		t.Fatal(err)
	}

	// default export: no viber
	x, err := m.ExportConfig()
	raw, _ := json.Marshal(x)
	if err != nil || x.Settings.Viber != nil || x.ContainsViberRecipients ||
		strings.Contains(string(raw), "viber") || strings.Contains(string(raw), "TOKEN") ||
		strings.Contains(string(raw), `"hasToken"`) || !strings.Contains(string(raw), `"containsViberRecipients":false`) {
		t.Fatalf("default export %s %v", raw, err)
	}

	// includeViberRecipients: non-secret viber only
	x, err = m.ExportConfigOpts(true)
	raw, _ = json.Marshal(x)
	if err != nil || x.Settings == nil || x.Settings.Viber == nil || !x.ContainsViberRecipients ||
		len(x.Settings.Viber.Recipients) != 2 || x.Settings.Viber.Recipients[0] != (model.ViberRecipient{ID: "r1", Name: "Иван"}) ||
		!x.Settings.Viber.Enabled || x.Settings.Viber.HasToken || x.Settings.Viber.Status != nil ||
		strings.Contains(string(raw), "TOKEN") || strings.Contains(string(raw), `"hasToken"`) ||
		strings.Contains(string(raw), `"status"`) || !strings.Contains(string(raw), `"containsViberRecipients":true`) {
		t.Fatalf("include export %s %v", raw, err)
	}
	// exact settings.viber shape (no hasToken / status)
	vb, _ := json.Marshal(x.Settings.Viber)
	wantV := `{"enabled":true,"recipients":[{"id":"r1","name":"Иван"},{"id":"r2","name":"Пётр"}],"schedule":{"days":[1,2,3,4,5],"from":"08:30","to":"17:00"},"events":["error","warning","recovered","no_connection"],"reminderMinutes":60}`
	if string(vb) != wantV {
		t.Fatalf("viber json\n got %s\nwant %s", vb, wantV)
	}

	// import without flag: keep stored recipients + token even if file has other viber
	imp := x
	is := *x.Settings
	is.WarnMinutes = 33
	is.Viber = &model.ViberSettings{Enabled: false, Recipients: []model.ViberRecipient{{ID: "other", Name: "X"}}}
	imp.Settings = &is
	if _, err := m.ImportConfig(&imp, model.ImportReplace, e.rev(), false, "a"); err != nil {
		t.Fatal(err)
	}
	cur, _ := e.st.LoadSettings()
	tok, _ := e.st.LoadViberToken()
	if cur.WarnMinutes != 33 || !cur.Viber.Enabled || len(cur.Viber.Recipients) != 2 || cur.Viber.Recipients[0].ID != "r1" || tok != "TOKEN-SECRET" {
		t.Fatalf("import without flag %+v tok %q", cur.Viber, tok)
	}

	// import with flag: apply non-secret viber, token untouched
	is2 := *x.Settings
	is2.Viber = &model.ViberSettings{
		Enabled: true, HasToken: true, // must be ignored
		Recipients: []model.ViberRecipient{{ID: "new1", Name: "Новый"}},
		Schedule:   model.ViberSchedule{Days: []int{1}, From: "09:00", To: "18:00"},
		Events:     []string{model.ViberEventError}, ReminderMinutes: 30,
		Status: &model.ViberStatus{Queued: 99},
	}
	imp2 := model.ConfigExport{Format: model.ConfigFormatV1, Settings: &is2, Profiles: x.Profiles}
	if _, err := m.ImportConfigOpts(&imp2, model.ImportReplace, e.rev(), false, true, "a"); err != nil {
		t.Fatal(err)
	}
	cur, _ = e.st.LoadSettings()
	tok, _ = e.st.LoadViberToken()
	rawSet, _ := json.Marshal(cur.Viber.Stored())
	if tok != "TOKEN-SECRET" || !cur.Viber.Enabled || len(cur.Viber.Recipients) != 1 || cur.Viber.Recipients[0].ID != "new1" ||
		cur.Viber.ReminderMinutes != 30 || strings.Contains(string(rawSet), "hasToken") && cur.Viber.HasToken {
		t.Fatalf("import with flag %+v tok %q stored %s", cur.Viber, tok, rawSet)
	}
	if e.st.HasViberToken() != true {
		t.Fatal("token file touched")
	}

	// empty recipients -> containsViberRecipients false
	st.Viber.Recipients = nil
	st.Viber.Enabled = false
	if _, err := m.SaveSettingsViber(st, "", true, e.rev(), "a"); err != nil {
		t.Fatal(err)
	}
	x, _ = m.ExportConfigOpts(true)
	if x.ContainsViberRecipients || x.Settings.Viber == nil || len(x.Settings.Viber.Recipients) != 0 {
		t.Fatalf("empty recipients %+v", x)
	}
}
