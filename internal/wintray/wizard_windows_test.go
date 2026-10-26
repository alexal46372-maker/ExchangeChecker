//go:build windows

package wintray

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/traysettings"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// wizService is discService with the wizard commands (ipc.SQLWizard).
type wizService struct {
	*discService
	wmu       sync.Mutex
	browses   []model.IPCRequest
	dbReqs    []model.IPCRequest
	browseErr error
	dbErr     error
}

func newWizService() *wizService {
	w := &wizService{discService: newDiscService()}
	w.cfgService.handler = w
	return w
}

func (w *wizService) SQLBrowse(ctx context.Context, req model.IPCRequest) (model.SQLBrowseResult, error) {
	w.wmu.Lock()
	defer w.wmu.Unlock()
	w.browses = append(w.browses, req)
	if err := w.browseErr; err != nil {
		w.browseErr = nil
		return model.SQLBrowseResult{}, err
	}
	return model.SQLBrowseResult{Instances: []model.SQLServerInstance{{Host: "SRV", Address: "10.0.0.5", Instance: "SQL1", TCPPort: 1500}}, Responders: 1}, nil
}

func (w *wizService) SQLDatabases(ctx context.Context, req model.IPCRequest) (model.SQLDatabasesResult, error) {
	w.wmu.Lock()
	defer w.wmu.Unlock()
	w.dbReqs = append(w.dbReqs, req)
	if err := w.dbErr; err != nil {
		w.dbErr = nil
		return model.SQLDatabasesResult{}, err
	}
	return model.SQLDatabasesResult{Databases: []model.SQLDatabaseInfo{{Name: "master", CompatLevel: 100, State: "ONLINE", Probed: true},
		{Name: "ut", CompatLevel: 100, State: "ONLINE", Is1C: true, HasNodeTables: true, Probed: true, SizeMB: 500}}}, nil
}

func (wd *wizardDialog) set(id int32, s string) {
	winapi.SendMessageString(wd.ctl[id], winapi.WM_SETTEXT, 0, s)
}

func (wd *wizardDialog) click(t *testing.T, id int32) {
	t.Helper()
	h := wd.ctl[id]
	if h == 0 || !winapi.IsWindowEnabled(h) {
		t.Fatalf("wizard control %d missing or disabled", id)
	}
	winapi.SendMessage(h, winapi.BM_CLICK, 0, 0)
	if id == idWizCancel {
		PumpPending() // the posted WM_CLOSE
	}
}

func (wd *wizardDialog) pick(id int32, i int) {
	winapi.SendMessage(wd.ctl[id], winapi.LB_SETCURSEL, uintptr(i), 0)
	wd.command(id, winapi.LBN_SELCHANGE)
}

func (wd *wizardDialog) text() string { return winapi.GetWindowText(wd.ctl[idWizResult]) }

func (wd *wizardDialog) idle() bool { return wd.cancel == nil }

func openWiz(t *testing.T, svc *wizService) (*settingsWindow, *wizardDialog) {
	t.Helper()
	app := startCfgApp(t, svc.cfgService, true, nil)
	w := openAdmin(t, app)
	if !w.wizardAvailable() {
		t.Fatal("wizard not available")
	}
	w.click(t, idSetAdd)
	wd := w.wiz
	if wd == nil || !w.ed.IsNew || winapi.IsWindowEnabled(w.hwnd) {
		t.Fatal("wizard not opened")
	}
	for id, h := range wd.ctl {
		if _, ok := wizardTips[id]; !ok && !wizardOutputs[id] {
			t.Errorf("control %d has no tooltip", id)
		}
		if wd.tips.text(h) == "" && !wizardOutputs[id] {
			t.Errorf("control %d: empty tooltip", id)
		}
	}
	return w, wd
}

// TestCHK_TRAY_30_Wizard — CHK-TRAY-30: «Добавить» opens the wizard;
// server step (local check, «Найти» = sql_browse, a found instance fills
// the box), database step (sql_databases with the typed password), finish
// (test_profile, discover, the user picks: two low-score candidates),
// save_profile with expectedRevision; «Расширенные настройки» continues in
// the form; «Отмена» restores the profile shown before.
func TestCHK_TRAY_30_Wizard(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := newWizService()
	w, wd := openWiz(t, svc)
	wd.set(idWizServer, "np:srv")
	wd.set(idWizLogin, "mon")
	wd.click(t, idWizNext)
	if wd.z.Step != traysettings.WizServer || wd.lastField != traysettings.FieldHost || !strings.Contains(wd.text(), "np:") {
		t.Fatalf("np: %q", wd.text())
	}
	wd.set(idWizServer, "")
	wd.click(t, idWizFind)
	pumpUntil(t, "browse", func() bool { return wd.idle() && len(wd.z.Instances) == 1 })
	if svc.browses[0].Host != "" || winapi.SendMessage(wd.ctl[idWizInstances], winapi.LB_GETCOUNT, 0, 0) != 1 {
		t.Errorf("browse %+v", svc.browses)
	}
	wd.pick(idWizInstances, 0)
	if winapi.GetWindowText(wd.ctl[idWizServer]) != `SRV\SQL1` {
		t.Errorf("server %q", winapi.GetWindowText(wd.ctl[idWizServer]))
	}
	wd.set(idWizPassword, "s3cret")
	wd.click(t, idWizNext)
	pumpUntil(t, "databases", func() bool { return wd.idle() && len(wd.z.Databases) == 2 })
	if r := svc.dbReqs[0]; r.Profile == nil || r.Profile.Server != `SRV\SQL1` || r.Password != "s3cret" || wd.z.Step != traysettings.WizDatabase {
		t.Fatalf("sql_databases %+v", r)
	}
	if l := winapi.SendMessage(wd.ctl[idWizDBs], winapi.LB_GETCOUNT, 0, 0); l != 2 {
		t.Fatalf("db rows %d", l)
	}
	wd.click(t, idWizNext)
	if wd.text() != i18n.T(i18n.KeyWizDBNone, i18n.RU) {
		t.Errorf("no db %q", wd.text())
	}
	wd.pick(idWizDBs, 1)
	wd.click(t, idWizNext)
	pumpUntil(t, "discovered", func() bool { return wd.idle() && len(wd.z.Candidates) == 2 })
	if svc.count("test") != 1 || winapi.IsWindowEnabled(wd.ctl[idWizSave]) || wd.z.Chosen != -1 {
		t.Fatalf("finish: tests %d chosen %d %q", svc.count("test"), wd.z.Chosen, wd.text())
	}
	if !strings.Contains(wd.text(), i18n.T(i18n.KeyWizTestOK, i18n.RU)) {
		t.Errorf("result %q", wd.text())
	}
	wd.pick(idWizCands, 1)
	wd.click(t, idWizSave)
	pumpUntil(t, "saved", func() bool { return w.wiz == nil && svc.count("save") == 1 })
	sc := svc.lastSave()
	if sc.p.Server != `SRV\SQL1` || sc.p.Database != "ut" || sc.p.Name != "ut" || sc.p.Mapping.Table != "_Node20" || sc.password != "s3cret" || sc.rev != "r1" {
		t.Errorf("saved %+v", sc)
	}
	if !winapi.IsWindowEnabled(w.hwnd) || w.ed.IsNew || len(w.ed.Profiles) != 2 {
		t.Errorf("after save: new %v profiles %d", w.ed.IsNew, len(w.ed.Profiles))
	}

	// «Расширенные настройки»: the form with the entered data
	w.click(t, idSetAdd)
	wd = w.wiz
	wd.set(idWizServer, "srv2")
	wd.command(idWizAuth, 0)
	winapi.SendMessage(wd.ctl[idWizAuth], winapi.CB_SETCURSEL, 1, 0)
	wd.command(idWizAuth, winapi.CBN_SELCHANGE)
	if winapi.IsWindowEnabled(wd.ctl[idWizLogin]) {
		t.Error("login box enabled for the computer account")
	}
	wd.click(t, idWizAdvanced)
	if w.wiz != nil || !w.ed.IsNew || w.text(idSetHost) != "srv2" || w.ed.Form.Auth != model.AuthWindows || w.lastFocus != traysettings.FieldHost {
		t.Errorf("advanced: host %q auth %v", w.text(idSetHost), w.ed.Form.Auth)
	}
	// «Отмена» returns to the profile shown before
	w.ed.Select(0)
	w.fill()
	w.click(t, idSetAdd)
	w.wiz.click(t, idWizCancel)
	if w.wiz != nil || w.ed.IsNew || w.ed.Sel != 0 {
		t.Errorf("cancel: new %v sel %d", w.ed.IsNew, w.ed.Sel)
	}
}

// TestCHK_TRAY_30_WizardAutoPick — one candidate the service may apply:
// chosen without asking, «Сохранить» enabled right away.
func TestCHK_TRAY_30_WizardAutoPick(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := newWizService()
	_, wd := openWiz(t, svc)
	wd.z.Server, wd.z.Database, wd.z.Step = `srv`, "ut", traysettings.WizFinish
	wd.set(idWizServer, "srv")
	wd.set(idWizLogin, "mon")
	m := model.IPCMessage{Type: model.MsgDiscovery, Candidates: []model.DiscoveryCandidate{testCandidate("_Node10"), testCandidate("_Node20")},
		AutoPick: &model.SQLAutoPick{Index: 1, AutoPickMin: 0.6, ReasonKey: model.MsgSQLAutoPickOK,
			Ranks: []model.SQLCandidateRank{{Rank: 2, Confidence: 0.2}, {Rank: 1, Confidence: 0.9}}}}
	idx, _ := wd.z.DiscoveryDone(m)
	if idx != 1 || !wd.z.Choose(wd.w.ed, idx) {
		t.Fatalf("auto %d", idx)
	}
	wd.show()
	if !winapi.IsWindowEnabled(wd.ctl[idWizSave]) || wd.w.ed.MapEdit == nil || wd.w.ed.MapEdit.Table != "_Node20" {
		t.Error("auto pick not applied")
	}
	wd.click(t, idWizCancel)
}

// TestCHK_TRAY_31_WizardDiagnosis — CHK-TRAY-31: an error with errorInfo
// shows the code and the sql_diagnose title + steps; rate_limited shows
// the wait; a failed connection test of the form gets the diagnosis too.
func TestCHK_TRAY_31_WizardDiagnosis(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := newWizService()
	w, wd := openWiz(t, svc)
	svc.wmu.Lock()
	svc.browseErr = &model.IPCError{Code: model.ErrCodeRateLimited, Key: model.MsgBrowseRateLimited, Message: "rate", RetryAfterMs: 14200}
	svc.wmu.Unlock()
	wd.click(t, idWizFind)
	pumpUntil(t, "rate", func() bool { return wd.idle() && strings.Contains(wd.text(), "15 с") })
	info := model.SQLErrorInfo{Code: model.SQLCodeInstanceNotFound, Category: "network", ErrorKey: "sql.diag_instance_not_found"}
	svc.wmu.Lock()
	svc.dbErr = &model.IPCError{Code: model.ErrCodeFailed, Key: model.MsgDiscoverFailed, Message: "instance SQL9 not found", Info: &info}
	svc.wmu.Unlock()
	wd.set(idWizServer, `srv\SQL9`)
	wd.set(idWizLogin, "mon")
	wd.click(t, idWizNext)
	d := model.SQLDiagnoseCode(model.SQLCodeInstanceNotFound, "")
	pumpUntil(t, "diagnosis", func() bool { return wd.idle() && strings.Contains(wd.text(), d.Title.RU) })
	if !strings.Contains(wd.text(), "instance_not_found") || !strings.Contains(wd.text(), "1. ") || !strings.Contains(wd.text(), "SQL9") {
		t.Errorf("diagnosis %q", wd.text())
	}
	wd.click(t, idWizCancel)
	// the connection test of the form
	svc.mu.Lock()
	svc.test = "timeout"
	svc.mu.Unlock()
	w.ed.Select(0)
	w.fill()
	w.click(t, idSetTest)
	pumpUntil(t, "test diagnosis", func() bool { return w.testCancel == nil && w.diagnoses == 1 })
	dt := model.SQLDiagnose(model.TestTimeout, model.MsgTestTimeout)
	if st := w.statusText(); !strings.Contains(st, "timeout") || !strings.Contains(st, dt.Title.RU) || !strings.Contains(st, i18n.Message(model.MsgTestTimeout, i18n.RU)) {
		t.Errorf("test status %q", st)
	}
}

// TestCHK_TRAY_32_WizardGrant — CHK-TRAY-32: «Скопировать SQL-скрипт
// прав» puts sql_grant_script on the clipboard (only there) and shows the
// notes; a clipboard error is reported.
func TestCHK_TRAY_32_WizardGrant(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := newWizService()
	w, wd := openWiz(t, svc)
	var clip []string
	w.cfg.clipboard = func(owner uintptr, s string) error { clip = append(clip, s); return nil }
	if winapi.IsWindowEnabled(wd.ctl[idWizGrant]) {
		t.Error("grant without a database")
	}
	wd.set(idWizServer, "srv")
	wd.set(idWizLogin, "mon")
	wd.read()
	wd.z.Databases = []model.SQLDatabaseInfo{{Name: "ut"}}
	wd.z.PickDatabase(0)
	// phase 11: the grant button is hidden on the server step (step 1)
	wd.z.Step = traysettings.WizDatabase
	wd.show()
	if winapi.WindowStyle(wd.ctl[idWizGrant])&winapi.WS_VISIBLE == 0 {
		t.Error("grant hidden on the database step")
	}
	wd.click(t, idWizGrant)
	pumpUntil(t, "grant", func() bool { return wd.idle() && len(clip) == 1 })
	if !strings.Contains(clip[0], "mon") || !strings.Contains(clip[0], "ut") {
		t.Errorf("script %q", clip[0])
	}
	if strings.Contains(wd.text(), clip[0]) || !strings.HasPrefix(wd.text(), i18n.T(i18n.KeyWizGrantCopied, i18n.RU)) {
		t.Errorf("result %q", wd.text())
	}
	w.cfg.clipboard = func(uintptr, string) error { return errors.New("busy clipboard") }
	wd.click(t, idWizGrant)
	pumpUntil(t, "clip error", func() bool { return wd.idle() && strings.Contains(wd.text(), "busy clipboard") })
	wd.click(t, idWizCancel)
}

// TestCHK_TRAY_32_Clipboard — the real clipboard round trip (skipped
// when the session has no clipboard, e.g. a service desktop).
func TestCHK_TRAY_32_Clipboard(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := winapi.SetClipboardText(0, "SELECT 1 -- тест"); err != nil {
		t.Skipf("no clipboard: %v", err)
	}
}
