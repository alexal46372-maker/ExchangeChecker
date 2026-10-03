package monitor

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/alert"
	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

type changeRec struct {
	mu sync.Mutex
	cs []model.ConfigChange
}

func (r *changeRec) add(c model.ConfigChange) { r.mu.Lock(); r.cs = append(r.cs, c); r.mu.Unlock() }
func (r *changeRec) all() []model.ConfigChange {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]model.ConfigChange(nil), r.cs...)
}

type cfgEnv struct {
	t   *testing.T
	dir string
	st  *config.Store
	m   *Monitor
	lg  *memLog
	ev  *changeRec
}

func newCfgEnv(t *testing.T) *cfgEnv {
	t.Helper()
	dir := t.TempDir()
	st := config.NewStore(dir)
	st.Protector = plainProtector{}
	f := newFactory()
	lg := &memLog{}
	ev := &changeRec{}
	opt := baseOpt(f, FileConfig{Store: st}, &fakeClock{t: t0})
	opt.Log, opt.OnConfigChange = lg, ev.add
	opt.CommandInterval = -1
	m := New(opt)
	m.reloadConfig(true)
	return &cfgEnv{t: t, dir: dir, st: st, m: m, lg: lg, ev: ev}
}

func sqlProf6(id string) model.Profile {
	p := prof(id)
	p.AuthMode, p.Login, p.Server = model.AuthSQL, "user1", `srv\INST`
	return p
}

func (e *cfgEnv) secrets() config.Secrets {
	e.t.Helper()
	s, err := e.st.LoadSecrets()
	if err != nil {
		e.t.Fatal(err)
	}
	return s
}

func (e *cfgEnv) files() string {
	var b strings.Builder
	for _, n := range []string{config.SettingsFile, config.ProfilesFile, config.SecretsFile} {
		raw, _ := os.ReadFile(e.st.Path(n))
		b.Write(raw)
		b.WriteString("|")
	}
	return b.String()
}

func wantCode(t *testing.T, what string, err error, code, key string) *model.IPCError {
	t.Helper()
	var ie *model.IPCError
	if !errors.As(err, &ie) || ie.Code != code || ie.Key != key {
		t.Fatalf("%s: %v (want %s / %s)", what, err, code, key)
	}
	return ie
}

// CHK-CFG-03: password rules of save_profile (Вика): new SQL profile
// without password -> invalid_request + test_password_required; empty
// password keeps the saved one only when CanReuseSavedPassword; clear;
// Windows auth deletes; HasPassword is derived, never trusted. Nothing is
// written on a refusal; passwords never reach the log, reply or event.
func TestCHK_CFG_03_SaveProfilePasswordRules(t *testing.T) {
	e := newCfgEnv(t)
	m := e.m
	p := sqlProf6("s1")
	before := e.files()
	_, err := m.SaveProfile(p, "", false, "", `HOST\admin`)
	wantCode(t, "new SQL profile without password", err, model.ErrCodeInvalidRequest, model.MsgTestPasswordRequired)
	if e.files() != before {
		t.Fatal("refused save wrote files")
	}
	p.HasPassword = true // client lies: ignored
	res, err := m.SaveProfile(p, "Pw-One!", false, "", `HOST\admin`)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Profile.HasPassword || !res.PollScheduled || res.Revision == "" || e.secrets()["s1"] != "Pw-One!" {
		t.Fatalf("save: %+v secrets %v", res, e.secrets())
	}
	// synchronous reload: in the snapshot and accepted by poll_now at once
	ps := profileByID(m.Snapshot(), "s1")
	if ps.ProfileID != "s1" || ps.Connection == nil || !ps.Connection.HasPassword || ps.Connection.Login != "user1" {
		t.Fatalf("snapshot after save: %+v", ps)
	}
	if pend := m.takePending(); !pend["s1"] {
		t.Errorf("poll of the saved profile not scheduled: %v", pend)
	}
	if _, err := m.PollNow("s1"); err != nil {
		t.Errorf("poll_now right after save_profile: %v", err)
	}
	raw, _ := os.ReadFile(e.st.Path(config.ProfilesFile))
	if bytes.Contains(raw, []byte("Pw-One!")) {
		t.Fatal("password in profiles.json")
	}
	keep := func(what string, mut func(*model.Profile)) {
		t.Helper()
		q := sqlProf6("s1")
		mut(&q)
		if _, err := m.SaveProfile(q, "", false, "", "a"); err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		if e.secrets()["s1"] != "Pw-One!" {
			t.Fatalf("%s: password lost", what)
		}
	}
	keep("identical", func(*model.Profile) {})
	keep("renamed + other database", func(q *model.Profile) { q.Name, q.Database = "New name", "other" })
	keep("host case + stronger encrypt", func(q *model.Profile) { q.Server, q.Encrypt = `SRV\inst`, model.EncryptOn })
	refuse := func(what string, mut func(*model.Profile)) {
		t.Helper()
		q := sqlProf6("s1")
		q.Encrypt = model.EncryptOn
		mut(&q)
		before := e.files()
		_, err := m.SaveProfile(q, "", false, "", "a")
		wantCode(t, what, err, model.ErrCodeInvalidRequest, model.MsgTestPasswordRequired)
		if e.files() != before || e.secrets()["s1"] != "Pw-One!" {
			t.Fatalf("%s: written", what)
		}
	}
	refuse("host swap", func(q *model.Profile) { q.Server = `evil\INST` })
	refuse("port swap", func(q *model.Profile) { q.Port = 1500 })
	refuse("login swap", func(q *model.Profile) { q.Login = "sa" })
	refuse("weaker encrypt", func(q *model.Profile) { q.Encrypt = model.EncryptDisable })
	refuse("trust cert (weaker)", func(q *model.Profile) { q.TrustServerCert = true })
	// replace
	q := sqlProf6("s1")
	if _, err := m.SaveProfile(q, "Pw-Two!", false, "", "a"); err != nil || e.secrets()["s1"] != "Pw-Two!" {
		t.Fatalf("replace: %v", err)
	}
	// both password and clear
	_, err = m.SaveProfile(q, "x", true, "", "a")
	if ie := wantCode(t, "password+clear", err, model.ErrCodeInvalidRequest, model.MsgMonitorProfileInvalid); ie.Args[0] != "clearPassword" || ie.Args[1] != model.ValConflict {
		t.Errorf("args %v", ie.Args)
	}
	// clear
	res, err = m.SaveProfile(q, "", true, "", "a")
	if _, ok := e.secrets()["s1"]; err != nil || ok || res.Profile.HasPassword {
		t.Fatalf("clear: %v %+v", err, res)
	}
	if profileByID(m.Snapshot(), "s1").Connection.HasPassword {
		t.Error("snapshot hasPassword after clear")
	}
	// no saved password any more: empty password -> enter the password
	_, err = m.SaveProfile(q, "", false, "", "a")
	wantCode(t, "empty after clear", err, model.ErrCodeInvalidRequest, model.MsgTestPasswordRequired)
	// Windows auth deletes the password (also one sent along)
	if _, err := m.SaveProfile(q, "Pw-3", false, "", "a"); err != nil {
		t.Fatal(err)
	}
	w := q
	w.AuthMode, w.Login = model.AuthWindows, ""
	res, err = m.SaveProfile(w, "ignored-pw", false, "", "a")
	if _, ok := e.secrets()["s1"]; err != nil || ok || res.Profile.HasPassword {
		t.Fatalf("windows auth: %v %v", err, e.secrets())
	}
	// generated UUIDv4 id
	n := sqlProf6("")
	n.Database = "newdb"
	res, err = m.SaveProfile(n, "pw", false, "", "a")
	if err != nil || !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(res.Profile.ID) {
		t.Fatalf("generated id %q %v", res.Profile.ID, err)
	}
	// invalid profile -> profile_invalid with errorArgs
	bad := sqlProf6("s1")
	bad.Port = 70000
	_, err = m.SaveProfile(bad, "pw", false, "", "a")
	if ie := wantCode(t, "invalid", err, model.ErrCodeInvalidRequest, model.MsgMonitorProfileInvalid); strings.Join(ie.Args, "|") != "port|validation.range|0|65535|70000" {
		t.Errorf("args %v", ie.Args)
	}
	bad = sqlProf6("s1")
	bad.Server = `\INST`
	_, err = m.SaveProfile(bad, "pw", false, "", "a")
	if ie := wantCode(t, "empty host", err, model.ErrCodeInvalidRequest, model.MsgMonitorProfileInvalid); ie.Args[0] != "server" || ie.Args[1] != model.ValServerFormat {
		t.Errorf("args %v", ie.Args)
	}
	for _, pw := range []string{"Pw-One!", "Pw-Two!", "Pw-3", "ignored-pw"} {
		if e.lg.has(pw) {
			t.Errorf("password %q in the log", pw)
		}
		for _, c := range e.ev.all() {
			if strings.Contains(fmt.Sprintf("%+v", c), pw) {
				t.Errorf("password in event %+v", c)
			}
		}
	}
}

// CHK-CFG-03: revision = first 16 bytes of SHA-256 over settings ||0||
// profiles ||0|| secrets; expectedRevision mismatch -> conflict and
// nothing written; save_* return the new revision; a manual edit changes
// it; the snapshot carries the applied revision.
func TestCHK_CFG_03_Revision(t *testing.T) {
	e := newCfgEnv(t)
	m := e.m
	_, rev0, err := m.GetConfig()
	if err != nil || len(rev0) != 32 {
		t.Fatalf("revision %q %v", rev0, err)
	}
	res, err := m.SaveProfile(sqlProf6("a"), "pw", false, rev0, "x")
	if err != nil || res.Revision == rev0 {
		t.Fatalf("save with current revision: %v %+v", err, res)
	}
	h := sha256.New()
	for i, n := range []string{config.SettingsFile, config.ProfilesFile, config.SecretsFile} {
		if i > 0 {
			h.Write([]byte{0})
		}
		b, _ := os.ReadFile(e.st.Path(n))
		h.Write(b)
	}
	if want := hex.EncodeToString(h.Sum(nil)[:16]); res.Revision != want {
		t.Fatalf("revision %s, want %s", res.Revision, want)
	}
	if s := m.Snapshot(); s.ConfigRevision != res.Revision {
		t.Errorf("snapshot revision %s, want %s", s.ConfigRevision, res.Revision)
	}
	before := e.files()
	_, err = m.SaveProfile(sqlProf6("b"), "pw", false, rev0, "x")
	wantCode(t, "stale save_profile", err, model.ErrCodeConflict, model.MsgMonitorConfigConflict)
	_, err = m.DeleteProfile("a", rev0, "x")
	wantCode(t, "stale delete", err, model.ErrCodeConflict, model.MsgMonitorConfigConflict)
	_, err = m.SaveSettings(model.DefaultSettings(), rev0, "x")
	wantCode(t, "stale settings", err, model.ErrCodeConflict, model.MsgMonitorConfigConflict)
	if e.files() != before {
		t.Fatal("conflict wrote files")
	}
	// manual edit of a file changes the revision
	if err := e.st.SaveSettings(model.Settings{PollIntervalSec: 120, WarnMinutes: 10, ErrorMinutes: 20, Beep: model.DefaultSettings().Beep}); err != nil {
		t.Fatal(err)
	}
	if _, rev2, _ := m.GetConfig(); rev2 == res.Revision {
		t.Error("manual edit did not change the revision")
	}
	_, err = m.SaveSettings(model.DefaultSettings(), res.Revision, "x")
	wantCode(t, "after manual edit", err, model.ErrCodeConflict, model.MsgMonitorConfigConflict)
	// without expectedRevision the last writer wins
	if _, err := m.SaveSettings(model.DefaultSettings(), "", "x"); err != nil {
		t.Fatal(err)
	}
}

// CHK-IPC-06: delete_profile removes the profile and its password
// (synchronous reload: gone from the snapshot, poll_now -> not_found);
// unknown id -> not_found; settings: validation with errorArgs, applied
// synchronously incl. trayAutostart; get_config never carries a password;
// every change -> one ConfigChange (event 105) with account / action /
// profile id.
func TestCHK_IPC_06_DeleteSettingsGetConfig(t *testing.T) {
	e := newCfgEnv(t)
	m := e.m
	if _, err := m.SaveProfile(sqlProf6("a"), "Secret-A", false, "", `D\u1`); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SaveProfile(prof("w"), "", false, "", `D\u1`); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := m.GetConfig()
	if err != nil || len(cfg.Profiles) != 2 || !cfg.Profiles[0].HasPassword || cfg.Profiles[1].HasPassword {
		t.Fatalf("get_config: %+v %v", cfg, err)
	}
	if b, _ := json.Marshal(cfg); bytes.Contains(b, []byte("Secret-A")) {
		t.Fatal("password in get_config")
	}
	if _, err := m.DeleteProfile("nope", "", "x"); !errors.Is(err, ErrUnknownProfile) {
		t.Errorf("unknown delete: %v", err)
	}
	if _, err := m.DeleteProfile("a", "", `D\u2`); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.secrets()["a"]; ok {
		t.Error("secret of the deleted profile kept")
	}
	if profileByID(m.Snapshot(), "a").ProfileID != "" {
		t.Error("deleted profile still in the snapshot")
	}
	if _, err := m.PollNow("a"); !errors.Is(err, ErrUnknownProfile) {
		t.Errorf("poll_now deleted: %v", err)
	}
	st := model.DefaultSettings()
	st.Beep.ErrorFreqHz = 5
	_, err = m.SaveSettings(st, "", "x")
	if ie := wantCode(t, "invalid settings", err, model.ErrCodeInvalidRequest, model.MsgMonitorSettingsInvalid); ie.Args[0] != "beep.errorFreqHz" || ie.Args[1] != model.ValRange {
		t.Errorf("args %v", ie.Args)
	}
	st = model.DefaultSettings()
	st.TrayAutostart, st.PollIntervalSec, st.Language = true, 300, model.LangEN
	if _, err := m.SaveSettings(st, "", `D\u3`); err != nil {
		t.Fatal(err)
	}
	s := m.Snapshot()
	if s.Settings == nil || !s.Settings.TrayAutostart || s.Settings.Language != model.LangEN || s.Settings.PollIntervalSec != 300 {
		t.Fatalf("settings not applied synchronously: %+v", s.Settings)
	}
	want := []model.ConfigChange{
		{Account: `D\u1`, Action: model.CmdSaveProfile, ProfileID: "a"},
		{Account: `D\u1`, Action: model.CmdSaveProfile, ProfileID: "w"},
		{Account: `D\u2`, Action: model.CmdDeleteProfile, ProfileID: "a"},
		{Account: `D\u3`, Action: model.CmdSaveSettings},
	}
	if got := e.ev.all(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("changes %v, want %v", got, want)
	}
}

// CHK-MON-05 (extension): the text of event 105 has the account, the
// action and the profile id, never a password, server or connection string.
func TestCHK_MON_05_ConfigChangedEventText(t *testing.T) {
	c := model.ConfigChange{Account: `CORP\alex`, Action: model.CmdSaveProfile, ProfileID: "p-1"}
	for _, lang := range []string{model.LangRU, model.LangEN} {
		s := alert.FormatConfigChange(c, lang)
		for _, want := range []string{`CORP\alex`, "save_profile", "p-1"} {
			if !strings.Contains(s, want) {
				t.Errorf("%s: %q lacks %q", lang, s, want)
			}
		}
	}
	if alert.EventIDConfig != 105 || alert.EventIDAck != 104 {
		t.Error("event ids")
	}
	s := alert.FormatConfigChange(model.ConfigChange{Action: model.CmdSaveSettings}, model.LangRU)
	if !strings.Contains(s, "Конфигурация изменена") || strings.Contains(s, "профиль") {
		t.Errorf("settings text %q", s)
	}
}

// CHK-IPC-06: the status snapshot carries the read-only settings view
// (settings, revision, connection without password, threshold override,
// mapping summary) and never a password or manual SQL.
func TestCHK_IPC_06_SnapshotReadOnlyView(t *testing.T) {
	e := newCfgEnv(t)
	p := sqlProf6("a")
	p.WarnMinutes, p.ErrorMinutes, p.Port = 15, 0, 1500
	p.Mappings = []model.Mapping{{Mode: model.MappingManual, ManualSQL: "SELECT 'TOP-SECRET-SQL' AS PointName", PlanLabel: "Ручной"}}
	if _, err := e.m.SaveProfile(p, "Hidden-PW", false, "", "x"); err != nil {
		t.Fatal(err)
	}
	s := e.m.Snapshot()
	ps := profileByID(s, "a")
	if ps.Connection == nil || ps.Connection.Server != `srv\INST` || ps.Connection.Port != 1500 || !ps.Connection.HasPassword ||
		ps.ThresholdOverride == nil || ps.ThresholdOverride.WarnMinutes != 15 || ps.ThresholdOverride.ErrorMinutes != 0 ||
		ps.MappingSummary == nil || len(ps.MappingSummary.Plans) != 2 || ps.MappingSummary.Plans[0].PlanTable != "_Node1" ||
		!ps.MappingSummary.Manual || s.Settings == nil || s.ConfigRevision == "" {
		t.Fatalf("read-only view: %+v / %+v", ps, s)
	}
	b, _ := json.Marshal(s)
	if bytes.Contains(b, []byte("Hidden-PW")) || bytes.Contains(b, []byte("TOP-SECRET-SQL")) {
		t.Fatalf("snapshot leaks: %s", b)
	}
}

// CHK-CFG-03: an unreadable profiles.json / secrets refuses the write
// (save_failed) instead of dropping the other profiles; no store ->
// save_failed.
func TestCHK_CFG_03_UnreadableConfigRefusesWrite(t *testing.T) {
	e := newCfgEnv(t)
	if err := os.WriteFile(e.st.Path(config.ProfilesFile), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := e.files()
	_, err := e.m.SaveProfile(sqlProf6("a"), "pw", false, "", "x")
	wantCode(t, "broken profiles.json", err, model.ErrCodeSaveFailed, model.MsgMonitorConfigSaveFailed)
	_, err = e.m.DeleteProfile("a", "", "x")
	wantCode(t, "broken profiles.json delete", err, model.ErrCodeSaveFailed, model.MsgMonitorConfigSaveFailed)
	if e.files() != before {
		t.Fatal("written")
	}
	os.Remove(e.st.Path(config.ProfilesFile))
	if err := os.WriteFile(e.st.Path(config.SecretsFile), []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = e.m.SaveProfile(sqlProf6("a"), "pw", false, "", "x")
	wantCode(t, "broken secrets", err, model.ErrCodeSaveFailed, model.MsgMonitorConfigSaveFailed)
	m := New(Options{Source: NewMemConfig(Config{Settings: model.DefaultSettings()})})
	_, _, err = m.GetConfig()
	wantCode(t, "no store", err, model.ErrCodeSaveFailed, model.MsgMonitorConfigSaveFailed)
}

// CHK-CFG-03: concurrent saves are serialized by the write mutex: none is
// lost (each re-reads the file under the lock).
func TestCHK_CFG_03_ConcurrentSaves(t *testing.T) {
	e := newCfgEnv(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := e.m.SaveProfile(sqlProf6(fmt.Sprintf("c%d", i)), fmt.Sprintf("pw%d", i), false, "", "x"); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	cfg, _, _ := e.m.GetConfig()
	sec := e.secrets()
	if len(cfg.Profiles) != 8 || len(sec) != 8 || len(e.m.Snapshot().Profiles) != 8 {
		t.Fatalf("lost writes: %d profiles, %d secrets", len(cfg.Profiles), len(sec))
	}
}
