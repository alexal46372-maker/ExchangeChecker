package traysettings

import (
	"context"
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/ipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
)

func savedSQL() model.Profile {
	p := model.NewProfile("p1")
	p.Name, p.Server, p.Database, p.Login, p.HasPassword = "Base1", `srv\SQL1`, "db1", "mon", true
	p.Mapping = model.Mapping{Mode: model.MappingAuto}
	p.WarnMinutes, p.ErrorMinutes, p.ConnectTimeoutSec = 10, 20, 30
	return p
}

func adminEditor(ps ...model.Profile) *Editor {
	e := NewEditor(i18n.RU, Admin)
	e.LoadConfig(model.ConfigView{Profiles: ps}, "rev1")
	return e
}

// TestCHK_TRAY_04_FormRoundTrip — host\instance split / joined, port,
// encryption, TLS 1.0, other fields of the saved profile kept.
func TestCHK_TRAY_04_FormRoundTrip(t *testing.T) {
	base := savedSQL()
	f := FromProfile(base)
	if f.Host != "srv" || f.Instance != "SQL1" || f.Port != "" || !f.TLS10 || f.Encrypt != model.EncryptDisable || f.Auth != model.AuthSQL {
		t.Fatalf("form %+v", f)
	}
	p, fe := f.Apply(base)
	if fe != nil {
		t.Fatal(fe)
	}
	if p.Server != `srv\SQL1` || p.Mapping.Mode != model.MappingAuto || p.WarnMinutes != 10 || p.ConnectTimeoutSec != 30 || p.TLSMinVersion != "1.0" {
		t.Errorf("apply %+v", p)
	}
	f.Port, f.Instance, f.TLS10, f.Encrypt = " 1500 ", "", false, model.EncryptOn
	p, fe = f.Apply(base)
	if fe != nil || p.Port != 1500 || p.Server != "srv" || p.TLSMinVersion != model.TLSMin12 || p.Encrypt != model.EncryptOn {
		t.Errorf("apply2 %+v %v", p, fe)
	}
	f.Auth = model.AuthWindows
	if p, fe = f.Apply(base); fe != nil || p.Login != "" {
		t.Errorf("windows login %q %v", p.Login, fe)
	}
}

// TestCHK_TRAY_04_FormValidation — empty host, host only with a
// backslash, extra backslash, bad port, empty database / login: the
// field error points at the right control.
func TestCHK_TRAY_04_FormValidation(t *testing.T) {
	base := model.NewProfile("")
	ok := Form{Host: "h", Database: "d", Auth: model.AuthSQL, Login: "l", Encrypt: model.EncryptDisable, Preset: model.ServerPresetMSSQL2008}
	cases := []struct {
		mod   func(*Form)
		key   string
		focus Field
	}{
		{func(f *Form) { f.Host = " " }, model.ValRequired, FieldHost},
		{func(f *Form) { f.Host, f.Instance = "", "I" }, model.ValServerFormat, FieldHost},
		{func(f *Form) { f.Host = `h\x` }, model.ValServerFormat, FieldHost},
		{func(f *Form) { f.Instance = `a\b` }, model.ValServerFormat, FieldHost},
		{func(f *Form) { f.Port = "70000" }, model.ValRange, FieldPort},
		{func(f *Form) { f.Port = "x" }, model.ValRange, FieldPort},
		{func(f *Form) { f.Database = "" }, model.ValRequired, FieldDatabase},
		{func(f *Form) { f.Login = "" }, model.ValRequired, FieldLogin},
		{func(f *Form) { f.Encrypt = model.EncryptStrict }, model.ValPresetConflict, FieldEncrypt},
	}
	for i, c := range cases {
		f := ok
		c.mod(&f)
		_, fe := f.Apply(base)
		if fe == nil || fe.Key != c.key || FocusField(fe.Field, fe.Args) != c.focus {
			t.Errorf("case %d: %+v", i, fe)
		}
	}
	if _, fe := ok.Apply(base); fe != nil {
		t.Errorf("valid form: %v", fe)
	}
	if FocusField("server", []string{"empty_instance"}) != FieldInstance || FocusField("mapping.table", nil) != FieldDiscoverButton {
		t.Error("FocusField")
	}
}

// TestCHK_TRAY_04_PasswordHints — new / keep / re-enter / replace /
// clear / Windows drops; read-only shows only whether one is saved.
func TestCHK_TRAY_04_PasswordHints(t *testing.T) {
	e := adminEditor(savedSQL())
	want := func(key string) {
		t.Helper()
		if got := e.PasswordHint(); got != i18n.T(key, i18n.RU) {
			t.Errorf("hint %q, want %s", got, key)
		}
	}
	want(i18n.KeySetPwKeep)
	e.Form.Host = "other"
	want(i18n.KeySetPwReenter)
	e.Form.Host = "srv"
	e.Form.Encrypt = model.EncryptOptional // stronger: may reuse
	want(i18n.KeySetPwKeep)
	e.Form.PasswordTyped = true
	want(i18n.KeySetPwReplace)
	e.Form.PasswordTyped, e.Form.ClearPassword = false, true
	want(i18n.KeySetPwClear)
	e.Form.Auth = model.AuthWindows
	want(i18n.KeySetPwWindowsDrop)
	e.New()
	want(i18n.KeySetPwNew)
	r := NewEditor(i18n.EN, ReadOnly)
	r.LoadSnapshot(model.StatusSnapshot{Profiles: []model.ProfileSnapshot{{ProfileID: "p1", Connection: &model.ProfileConnection{Server: "s", AuthMode: model.AuthSQL, HasPassword: true}}}})
	if r.PasswordHint() != i18n.T(i18n.KeySetPwSaved, i18n.EN) {
		t.Errorf("read-only hint %q", r.PasswordHint())
	}
}

// TestCHK_TRAY_04_Hints — the plain-text warning next to "no
// encryption" (SQL authentication only) and "port wins over instance".
func TestCHK_TRAY_04_Hints(t *testing.T) {
	e := adminEditor(savedSQL())
	h := strings.Join(e.Hints(), "|")
	if !strings.Contains(h, "db_datareader") || strings.Contains(h, "SQL Browser") {
		t.Errorf("hints %q", h)
	}
	e.Form.Port = "1433"
	e.Form.Auth = model.AuthWindows
	h = strings.Join(e.Hints(), "|")
	if strings.Contains(h, "db_datareader") || !strings.Contains(h, "SQL Browser") {
		t.Errorf("hints2 %q", h)
	}
}

// TestCHK_TRAY_04_ReadOnlyFromSnapshot — read-only mode is built from
// get_status: connection, thresholds, revision; no save / delete.
func TestCHK_TRAY_04_ReadOnlyFromSnapshot(t *testing.T) {
	e := NewEditor(i18n.RU, ReadOnly)
	e.LoadSnapshot(model.StatusSnapshot{ConfigRevision: "r9", Profiles: []model.ProfileSnapshot{{
		ProfileID: "a", Name: "A", Enabled: true,
		Connection:        &model.ProfileConnection{Server: `h\i`, Port: 1500, Database: "d", AuthMode: model.AuthWindows, Encrypt: model.EncryptOn, TLSMinVersion: "1.0"},
		ThresholdOverride: &model.ThresholdOverride{WarnMinutes: 5, ErrorMinutes: 9},
		MappingSummary:    &model.MappingSummary{Configured: true},
	}}})
	if e.Revision != "r9" || e.Sel != 0 || e.Form.Host != "h" || e.Form.Instance != "i" || e.Form.Port != "1500" || !e.Form.TLS10 || e.Form.Encrypt != model.EncryptOn {
		t.Fatalf("editor %+v form %+v", e, e.Form)
	}
	if e.Base.WarnMinutes != 5 || e.MapSummary["a"] == nil {
		t.Error("thresholds / mapping summary")
	}
	if e.CanEdit() {
		t.Error("read-only editor can edit")
	}
	if _, pr := e.SaveRequest(""); pr == nil {
		t.Error("save in read-only mode")
	}
	if _, pr := e.DeleteRequest(); pr == nil {
		t.Error("delete in read-only mode")
	}
	e.Form.Host = "changed"
	req, pr := e.TestRequest("")
	if pr != nil || req.Profile != nil || req.ProfileID != "a" {
		t.Errorf("read-only test request %+v %v", req, pr)
	}
	if req, pr = e.PollRequest(); pr != nil || req.Cmd != model.CmdPollNow || req.ProfileID != "a" {
		t.Errorf("poll %+v", req)
	}
}

// TestCHK_TRAY_06_SaveRequest — expectedRevision is mandatory, the
// password rules (keep / required / clear / conflict / Windows), the
// size limit; SaveDone takes the new revision and the saved profile.
func TestCHK_TRAY_06_SaveRequest(t *testing.T) {
	e := adminEditor(savedSQL())
	pw := ""
	req, pr := e.SaveRequest(pw)
	if pr != nil || req.Cmd != model.CmdSaveProfile || req.ExpectedRevision != "rev1" || req.Password != "" || req.ClearPassword || req.Profile.ID != "p1" || req.Profile.HasPassword {
		t.Fatalf("keep: %+v %v", req, pr)
	}
	e.Form.Host = "other"
	if _, pr = e.SaveRequest(pw); pr == nil || pr.Field != FieldPassword {
		t.Errorf("re-enter: %+v", pr)
	}
	pw, e.Form.PasswordTyped = "secret", true
	if req, pr = e.SaveRequest(pw); pr != nil || req.Password != "secret" {
		t.Errorf("password: %+v %v", req, pr)
	}
	e.Form.ClearPassword = true
	if _, pr = e.SaveRequest(pw); pr == nil || pr.Field != FieldClearPassword {
		t.Errorf("conflict: %+v", pr)
	}
	pw, e.Form.PasswordTyped = "", false
	if req, pr = e.SaveRequest(pw); pr != nil || !req.ClearPassword {
		t.Errorf("clear: %+v %v", req, pr)
	}
	e.Form.Auth, e.Form.ClearPassword = model.AuthWindows, false
	if req, pr = e.SaveRequest(pw); pr != nil || req.Password != "" || req.Profile.Login != "" {
		t.Errorf("windows: %+v %v", req, pr)
	}
	e.New()
	e.Form.Host, e.Form.Database, e.Form.Login = "h", "d", "l"
	if _, pr = e.SaveRequest(pw); pr == nil || pr.Field != FieldPassword {
		t.Errorf("new without password: %+v", pr)
	}
	pw, e.Form.PasswordTyped = "pw", true
	if req, pr = e.SaveRequest(pw); pr != nil || req.Profile.ID != "" {
		t.Fatalf("new: %+v %v", req, pr)
	}
	saved := *req.Profile
	saved.ID, saved.HasPassword = "new-id", true
	txt := e.SaveDone(model.IPCMessage{Type: model.MsgOK, Result: model.ResultSaved, Profile: &saved, Revision: "rev2", PollScheduled: true})
	if e.Revision != "rev2" || e.IsNew || e.Sel != 1 || e.Base.ID != "new-id" || e.Form.PasswordTyped || txt != i18n.T(i18n.KeySetSavedPoll, i18n.RU) {
		t.Errorf("save done: rev %s new %v sel %d %q", e.Revision, e.IsNew, e.Sel, txt)
	}
	if e.Dirty() {
		t.Error("dirty after save")
	}
	e.Revision = ""
	if _, pr = e.SaveRequest(pw); pr == nil || !pr.Reload {
		t.Errorf("no revision: %+v", pr)
	}
	e.Revision = "r"
	e.Form.Name = strings.Repeat("я", model.IPCMaxRequest/2)
	if _, pr = e.SaveRequest(pw); pr == nil || pr.Text != i18n.T(i18n.KeySetTooLarge, i18n.RU) {
		t.Errorf("too large: %+v", pr)
	}
}

// TestCHK_TRAY_06_DeleteRequest — delete_profile with the revision;
// DeleteDone removes the profile and selects the next one.
func TestCHK_TRAY_06_DeleteRequest(t *testing.T) {
	p2 := savedSQL()
	p2.ID = "p2"
	e := adminEditor(savedSQL(), p2)
	req, pr := e.DeleteRequest()
	if pr != nil || req.ProfileID != "p1" || req.ExpectedRevision != "rev1" {
		t.Fatalf("%+v %v", req, pr)
	}
	e.DeleteDone(model.IPCMessage{Type: model.MsgOK, Result: model.ResultDeleted, ProfileID: "p1", Revision: "rev3"})
	if len(e.Profiles) != 1 || e.Base.ID != "p2" || e.Revision != "rev3" {
		t.Errorf("after delete %+v", e.Profiles)
	}
	e.New()
	if _, pr = e.DeleteRequest(); pr == nil {
		t.Error("delete of a new profile")
	}
}

// TestCHK_TRAY_05_TestRequestAndText — saved profile by id when not
// edited, the form with the password otherwise (no mapping); result
// texts with version / compat level / category.
func TestCHK_TRAY_05_TestRequestAndText(t *testing.T) {
	e := adminEditor(savedSQL())
	req, pr := e.TestRequest("")
	if pr != nil || req.Profile != nil || req.ProfileID != "p1" {
		t.Fatalf("unchanged: %+v", req)
	}
	e.Form.Database, e.Form.PasswordTyped = "db2", true
	req, pr = e.TestRequest("pw")
	if pr != nil || req.Profile == nil || req.Password != "pw" || req.Profile.Database != "db2" || req.Profile.Mapping.Mode != "" {
		t.Fatalf("edited: %+v %v", req, pr)
	}
	ok := e.TestText(model.TestProfileResult{OK: true, Category: model.TestOK, ServerVersion: "10.50.1600.1", ProductLevel: "RTM", Edition: "Standard", CompatLevel: 100, Encrypt: "disable", DurationMs: 42})
	for _, s := range []string{"42", "10.50.1600.1 RTM Standard", "100", "disable"} {
		if !strings.Contains(ok, s) {
			t.Errorf("ok text %q lacks %q", ok, s)
		}
	}
	bad := e.TestText(model.TestProfileResult{Category: model.TestTimeout, ErrorKey: model.MsgTestTimeout, Error: "dial timeout", Encrypt: "on", TLSMin: "1.0"})
	if !strings.Contains(bad, i18n.Message(model.MsgTestTimeout, i18n.RU)) || !strings.Contains(bad, "15") || !strings.Contains(bad, "dial timeout") || !strings.Contains(bad, "1.0") {
		t.Errorf("timeout text %q", bad)
	}
}

// TestCHK_TRAY_05_ErrorTexts — busy, rate_limited (10 s), canceled,
// timeouts, not connected, unsupported, too large.
func TestCHK_TRAY_05_ErrorTexts(t *testing.T) {
	e := adminEditor(savedSQL())
	if pr := e.TestReply(model.IPCMessage{Type: model.MsgError, Code: model.ErrCodeBusy, ErrorKey: model.MsgTestBusy}, nil); pr.Text != i18n.Message(model.MsgTestBusy, i18n.RU) {
		t.Errorf("busy %q", pr.Text)
	}
	if pr := e.TestReply(model.IPCMessage{Type: model.MsgError, Code: model.ErrCodeRateLimited, ErrorKey: model.MsgMonitorRateLimited}, nil); !strings.Contains(pr.Text, "10") {
		t.Errorf("rate limited %q", pr.Text)
	}
	if pr := e.TestReply(model.IPCMessage{Type: model.MsgError, Code: model.ErrCodeCanceled, ErrorKey: model.MsgMonitorCanceled}, nil); pr.Text != i18n.Message(model.MsgMonitorCanceled, i18n.RU) {
		t.Errorf("canceled %q", pr.Text)
	}
	if pr := e.TestReply(model.IPCMessage{Type: model.MsgError, Code: model.ErrCodeInvalidRequest, ErrorKey: model.MsgTestPasswordRequired, ErrorArgs: []string{"password", model.ValRequired}}, nil); pr.Field != FieldPassword || pr.Text != "Введите пароль." {
		t.Errorf("password required %+v", pr)
	}
	for err, key := range map[error]string{
		trayipc.ErrNotConnected:  i18n.KeySetNotConnected,
		trayipc.ErrUnsupported:   i18n.KeySetUnsupported,
		ipc.ErrTooLarge:          i18n.KeySetTooLarge,
		context.Canceled:         i18n.KeySetTestCanceled,
		ipc.ErrClientClosed:      i18n.KeySetNotConnected,
		context.DeadlineExceeded: i18n.KeySetTestNoReply,
	} {
		pr := e.TestReply(model.IPCMessage{}, err)
		want := i18n.T(key, i18n.RU)
		if key == i18n.KeySetTestNoReply {
			want = strings.Replace(want, "%d", "25", 1)
		}
		if pr.Text != want {
			t.Errorf("%v: %q, want %q", err, pr.Text, want)
		}
	}
}

// TestCHK_TRAY_06_ReplyErrors — access_denied, conflict (reload),
// field errors via errorKey / errorArgs (focus, localized field name),
// a missing revision and another invalid profile in the file.
func TestCHK_TRAY_06_ReplyErrors(t *testing.T) {
	other := savedSQL()
	other.ID, other.Name = "bad", "Ручной профиль"
	e := adminEditor(savedSQL(), other)
	sent, _ := e.Candidate()
	pr := e.Reply(model.IPCMessage{Type: model.MsgError, Code: model.ErrCodeAccessDenied, ErrorKey: model.MsgMonitorAccessDenied})
	if !strings.Contains(pr.Text, "администратора") {
		t.Errorf("access denied %q", pr.Text)
	}
	pr = e.Reply(model.IPCMessage{Type: model.MsgError, Code: model.ErrCodeConflict, ErrorKey: model.MsgMonitorConfigConflict})
	if !pr.Reload || !strings.Contains(pr.Text, "Перечитать") {
		t.Errorf("conflict %+v", pr)
	}
	bad := sent
	bad.Database = ""
	pr = e.Reply(model.IPCMessage{Type: model.MsgError, Code: model.ErrCodeInvalidRequest, ErrorKey: model.MsgMonitorProfileInvalid,
		ErrorArgs: []string{"database", model.ValRequired}, Error: `monitor: profile "p1": database is required`})
	if pr.Field != FieldDatabase || !strings.Contains(pr.Text, "«База данных»") {
		t.Errorf("field error %+v", pr)
	}
	pr = e.Reply(model.IPCMessage{Type: model.MsgError, Code: model.ErrCodeInvalidRequest, ErrorKey: model.MsgMonitorProfileInvalid,
		ErrorArgs: []string{"server", model.ValServerFormat, "empty_instance"}})
	if pr.Field != FieldInstance {
		t.Errorf("server field %+v", pr)
	}
	pr = e.Reply(model.IPCMessage{Type: model.MsgError, Code: model.ErrCodeInvalidRequest, ErrorKey: model.MsgMonitorBadRequest,
		ErrorArgs: []string{"expectedRevision", model.ValRequired}})
	if !pr.Reload {
		t.Errorf("revision %+v", pr)
	}
	// another profile of the file: profileId (errorArgs[0] is its field)
	pr = e.Reply(model.IPCMessage{Type: model.MsgError, Code: model.ErrCodeInvalidRequest, ErrorKey: model.MsgMonitorProfileInvalid,
		ErrorArgs: []string{"mapping.lastSendColumn", model.ValRequired}, ProfileID: "bad", Error: `monitor: profile "bad": mapping: at least one column`})
	if pr.Field != FieldNone || !strings.Contains(pr.Text, "Ручной профиль") || !strings.Contains(pr.Text, "Таблицы обмена — Колонка отправки") {
		t.Errorf("other profile %+v", pr)
	}
	// unknown id (e.g. duplicate id): named by the id itself
	pr = e.Reply(model.IPCMessage{Type: model.MsgError, Code: model.ErrCodeInvalidRequest, ErrorKey: model.MsgMonitorProfileInvalid,
		ErrorArgs: []string{"id", model.ValDuplicate}, ProfileID: "dup"})
	if pr.Field != FieldNone || !strings.Contains(pr.Text, "dup") {
		t.Errorf("other profile by id %+v", pr)
	}
	// no profileId: always the current profile, even when the text names
	// another one (the text is not parsed any more)
	pr = e.Reply(model.IPCMessage{Type: model.MsgError, Code: model.ErrCodeInvalidRequest, ErrorKey: model.MsgMonitorProfileInvalid,
		ErrorArgs: []string{"port", model.ValRange, "0", "65535", "99999"}, Error: `monitor: profile "bad": port 99999 out of range`})
	if pr.Field != FieldPort || strings.Contains(pr.Text, "Ручной профиль") {
		t.Errorf("own field error %+v", pr)
	}
	pr = e.Reply(model.IPCMessage{Type: model.MsgError, Code: model.ErrCodeInvalidRequest, ErrorKey: model.MsgMonitorProfileInvalid,
		ErrorArgs: []string{"mapping.table", model.ValRequired}})
	if pr.Field == FieldNone && strings.Contains(pr.Text, "Ручной профиль") {
		t.Errorf("no profileId must not be another profile %+v", pr)
	}
	pr = e.Reply(model.IPCMessage{Type: model.MsgError, Code: model.ErrCodeSaveFailed, Error: "disk full"})
	if !strings.Contains(pr.Text, "disk full") {
		t.Errorf("fallback %q", pr.Text)
	}
	if e.Reply(model.IPCMessage{Type: model.MsgOK}) != nil {
		t.Error("ok is not an error")
	}
	r := NewEditor(i18n.RU, ReadOnly)
	pr = r.Reply(model.IPCMessage{Type: model.MsgError, Code: model.ErrCodeAccessDenied, ErrorKey: model.MsgMonitorAccessDenied})
	if !strings.Contains(pr.Text, "Изменить (администратор)") {
		t.Errorf("read-only access denied %q", pr.Text)
	}
}

// TestCHK_TRAY_04_SelectionAndReload — LoadConfig keeps the selected
// profile by id, Select drops edits, New uses the defaults.
func TestCHK_TRAY_04_SelectionAndReload(t *testing.T) {
	p2 := savedSQL()
	p2.ID, p2.Name = "p2", "B"
	e := adminEditor(savedSQL(), p2)
	e.Select(1)
	e.Form.Name = "edited"
	if !e.Dirty() {
		t.Error("not dirty")
	}
	e.LoadConfig(model.ConfigView{Profiles: []model.Profile{p2, savedSQL()}}, "rev5")
	if e.Sel != 0 || e.Base.ID != "p2" || e.Form.Name != "B" || e.Revision != "rev5" {
		t.Errorf("reload: sel %d %+v", e.Sel, e.Form)
	}
	e.New()
	if !e.IsNew || e.Form.Encrypt != model.EncryptDisable || !e.Form.TLS10 || e.Form.Auth != model.AuthSQL || !e.Form.Enabled {
		t.Errorf("new %+v", e.Form)
	}
	e.LoadConfig(model.ConfigView{}, "rev6")
	if e.HasProfile() {
		t.Error("empty config has a profile")
	}
}

// TestCHK_TRAY_04_Unsaved — unsaved edits (asks «Отменить изменения?»):
// an edited saved profile, a typed-in new profile; not an untouched new
// one, not read-only mode.
func TestCHK_TRAY_04_Unsaved(t *testing.T) {
	e := adminEditor(savedSQL())
	if e.Unsaved() {
		t.Error("loaded profile is unsaved")
	}
	e.Form.PasswordTyped = true
	if !e.Unsaved() {
		t.Error("typed password is not an edit")
	}
	e.New()
	if e.Unsaved() || !e.Dirty() {
		t.Error("untouched new profile: not unsaved, but sent as a form")
	}
	e.Form.Host = "h"
	if !e.Unsaved() {
		t.Error("typed new profile")
	}
	r := NewEditor(i18n.RU, ReadOnly)
	r.LoadSnapshot(model.StatusSnapshot{Profiles: []model.ProfileSnapshot{{ProfileID: "a"}}})
	r.Form.Host = "x"
	if r.Unsaved() {
		t.Error("read-only mode has no edits")
	}
}
