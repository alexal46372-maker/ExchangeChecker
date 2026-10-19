package monitor

import (
	"encoding/json"
	"strings"
	"testing"

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
