//go:build windows

package wintray

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/traysettings"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// selCombo picks a combo box item like the user (CB_SETCURSEL does not
// notify).
func (w *settingsWindow) selCombo(id int32, i int) {
	winapi.SendMessage(w.ctl[id], winapi.CB_SETCURSEL, uintptr(i), 0)
	w.command(id, winapi.CBN_SELCHANGE)
}

func plain(key string) string { return strings.TrimSuffix(i18n.T(key, i18n.RU), ":") }

// TestCHK_TRAY_12_SettingsLimitsFields — the settings window edits the
// profile's thresholds, timeouts and server preset: values shown, saved
// with save_profile, a broken pair is refused locally with the real
// labels and the focus on «Ошибка, мин».
func TestCHK_TRAY_12_SettingsLimitsFields(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := newCfgService()
	app := startCfgApp(t, svc, true, nil)
	w := openAdmin(t, app)
	if w.text(idSetWarn) != "30" || w.text(idSetError) != "60" || w.text(idSetConnTO) != "15" || w.text(idSetQueryTO) != "30" ||
		winapi.SendMessage(w.ctl[idSetPreset], winapi.CB_GETCURSEL, 0, 0) != 0 {
		t.Fatalf("shown %q %q %q %q", w.text(idSetWarn), w.text(idSetError), w.text(idSetConnTO), w.text(idSetQueryTO))
	}
	w.set(idSetWarn, "45")
	w.set(idSetError, "90")
	w.set(idSetConnTO, "20")
	w.selCombo(idSetPreset, 1)
	w.click(t, idSetSave)
	pumpUntil(t, "saved", func() bool { return svc.count("save") == 1 && !w.busy })
	p := svc.lastSave().p
	if p.WarnMinutes != 45 || p.ErrorMinutes != 90 || p.ConnectTimeoutSec != 20 || p.QueryTimeoutSec != 0 || p.ServerPreset != "" {
		t.Errorf("saved %+v", p)
	}
	w.set(idSetWarn, "100")
	w.click(t, idSetSave)
	PumpPending()
	st := w.statusText()
	if svc.count("save") != 1 || w.lastFocus != traysettings.FieldProfError || strings.Contains(st, "warnMinutes") ||
		!strings.Contains(st, plain(i18n.KeySetWarnMin)) || !strings.Contains(st, plain(i18n.KeySetErrorMin)) {
		t.Errorf("pair error: saves %d focus %q status %q", svc.count("save"), w.lastFocus, st)
	}
	w.set(idSetWarn, "45")
	w.set(idSetQueryTO, "700")
	w.click(t, idSetSave)
	PumpPending()
	if w.lastFocus != traysettings.FieldQueryTimeout || !strings.Contains(w.statusText(), plain(i18n.KeySetQueryTimeout)) {
		t.Errorf("timeout error: focus %q status %q", w.lastFocus, w.statusText())
	}
}

// TestCHK_TRAY_17_SettingsDefaults — the empty port box shows the grey
// cue banner, the thresholds / timeouts boxes show the defaults (and a
// cue when emptied).
func TestCHK_TRAY_17_SettingsDefaults(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := newCfgService()
	app := startCfgApp(t, svc, true, nil)
	w := openAdmin(t, app)
	w.click(t, idSetAdd)
	PumpPending()
	if w.text(idSetPort) != "" || w.cues[idSetPort] != "1433 или пусто для SQL Browser" {
		t.Errorf("port %q cue %q", w.text(idSetPort), w.cues[idSetPort])
	}
	// EM_GETCUEBANNER answers only with comctl32 v6 (the exchtray
	// manifest, CHK-RES-01); the test binary has none, so it is checked
	// when available
	for id, want := range w.cues {
		if got := winapi.CueBanner(w.ctl[id]); got != "" && got != want {
			t.Errorf("cue %d: %q, want %q", id, got, want)
		}
	}
	if w.text(idSetWarn) != "30" || w.text(idSetError) != "60" ||
		w.text(idSetConnTO) != fmt.Sprint(model.DefaultConnectTimeoutSec) || w.text(idSetQueryTO) != fmt.Sprint(model.DefaultQueryTimeoutSec) {
		t.Errorf("defaults %q %q %q %q", w.text(idSetWarn), w.text(idSetError), w.text(idSetConnTO), w.text(idSetQueryTO))
	}
	if w.cues[idSetWarn] != i18n.T(i18n.KeySetThreshCue, i18n.RU) || w.cues[idSetError] != w.cues[idSetWarn] ||
		!strings.Contains(w.cues[idSetConnTO], "15") || !strings.Contains(w.cues[idSetQueryTO], "30") {
		t.Errorf("cues %q", w.cues)
	}
}

// TestCHK_TRAY_13_SettingsTrustDisabled — «Доверять сертификату» is
// enabled only with «Весь сеанс (TLS)»; elsewhere disabled and cleared.
func TestCHK_TRAY_13_SettingsTrustDisabled(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := newCfgService()
	app := startCfgApp(t, svc, true, nil)
	w := openAdmin(t, app)
	for i, enc := range traysettings.EncryptChoices {
		w.selCombo(idSetEncrypt, i)
		on := enc == model.EncryptOn
		if winapi.IsWindowEnabled(w.ctl[idSetTrust]) != on {
			t.Errorf("%s: trust enabled %v", enc, !on)
		}
		if on {
			w.click(t, idSetTrust)
			if !w.ed.Form.TrustServerCert || !w.check(idSetTrust) {
				t.Error("trust not taken with encrypt on")
			}
		} else if w.check(idSetTrust) {
			t.Errorf("%s: trust shown checked", enc)
		}
	}
	w.selCombo(idSetEncrypt, traysettings.EncryptIndex(model.EncryptOn))
	if !w.check(idSetTrust) {
		t.Error("the trust value is kept for encrypt on")
	}
}

// checkTips: every control of ids (except outputs) has its tooltip.
func checkTips(t *testing.T, what string, tips *tooltips, ctl map[int32]uintptr, want map[int32]string, outputs map[int32]bool) {
	t.Helper()
	if tips == nil {
		t.Fatalf("%s: no tooltip window", what)
	}
	n := 0
	for id, h := range ctl {
		if outputs[id] {
			continue
		}
		n++
		key, ok := want[id]
		if !ok {
			t.Errorf("%s: control %d (%s) has no tooltip key", what, id, winapi.GetClassName(h))
			continue
		}
		if got := tips.text(h); got == "" || got != i18n.T(key, i18n.RU) {
			t.Errorf("%s: control %d tooltip %q, want %s", what, id, got, key)
		}
	}
	if tips.count() != n {
		t.Errorf("%s: TTM_GETTOOLCOUNT %d, controls %d", what, tips.count(), n)
	}
}

// TestCHK_TRAY_15_Tooltips — TOOLTIPS_CLASS tooltips on every field,
// button, check box, combo and list box of the settings window (both
// modes, both pages), the Discovery dialog, the alert and About windows;
// RU / EN texts differ.
func TestCHK_TRAY_15_Tooltips(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	for _, m := range []map[int32]string{settingsTips, discoveryTips} {
		for id, k := range m {
			if ru, en := i18n.T(k, i18n.RU), i18n.T(k, i18n.EN); ru == "" || en == "" || ru == en {
				t.Errorf("tooltip %d %s: ru %q en %q", id, k, ru, en)
			}
		}
	}
	if !strings.Contains(i18n.T(i18n.KeyTipSetInstance, i18n.RU), "Только имя именованного экземпляра, например SQLEXPRESS. Не сервер и не IP") {
		t.Error("instance tooltip text")
	}
	func() {
		ds := newDiscService()
		app := startCfgApp(t, ds.cfgService, true, nil)
		defer app.Exit() // the Discovery dialog closes on this locked thread (CHK-TRAY-11 lesson)
		w := openAdmin(t, app)
		checkTips(t, "settings admin", w.tips, w.ctl, settingsTips, settingsOutputs)
		dd := openDisc(t, w)
		checkTips(t, "discovery", dd.tips, dd.ctl, discoveryTips, discoveryOutputs)
	}()

	ro := startCfgApp(t, newCfgService(), false, &fakeElevator{})
	defer ro.Exit()
	ro.Command(trayui.CmdSettings)
	if ro.settings == nil {
		t.Fatal("read-only window")
	}
	checkTips(t, "settings read-only", ro.settings.tips, ro.settings.ctl, settingsTips, settingsOutputs)

	aw, err := newAlertWindow(0, i18n.RU, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer winapi.DestroyWindow(aw.hwnd)
	if aw.tips.text(aw.ack) != i18n.T(i18n.KeyTipAlertAck, i18n.RU) || aw.tips.text(aw.close) != i18n.T(i18n.KeyTipAlertClose, i18n.RU) ||
		aw.tips.text(aw.mute) != i18n.T(i18n.KeyTipAlertMute, i18n.RU) || aw.tips.count() != 3 {
		t.Error("alert tooltips")
	}
	aw.setLang(i18n.EN)
	if aw.tips.text(aw.ack) != i18n.T(i18n.KeyTipAlertAck, i18n.EN) || aw.tips.count() != 3 {
		t.Error("alert tooltips after the language switch")
	}
	h, err := newAboutWindow(0, "About", "text", "OK", i18n.T(i18n.KeyTipAboutOK, i18n.EN), false, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer winapi.DestroyWindow(h)
	if aboutTips.count() != 1 || aboutTips.text(winapi.GetDlgItem(h, winapi.IDOK)) != i18n.T(i18n.KeyTipAboutOK, i18n.EN) {
		t.Error("about tooltip")
	}
}

// fakeSvc is a ServiceControl of the tests.
type fakeSvc struct {
	mu          sync.Mutex
	state       trayui.SvcState
	err         error
	starts, sts int
}

func (f *fakeSvc) Name() string { return model.ServiceName }
func (f *fakeSvc) State() (trayui.SvcState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state, nil
}
func (f *fakeSvc) Start() error { f.mu.Lock(); defer f.mu.Unlock(); f.starts++; return f.err }
func (f *fakeSvc) Stop() error  { f.mu.Lock(); defer f.mu.Unlock(); f.sts++; return f.err }

// TestCHK_TRAY_16_ServiceMenu — «Служба»: the state from the SCM, Start
// and Stop off the UI thread with a message; ERROR_ACCESS_DENIED gives
// the rights message (no elevation); the real SCM wrappers on CI.
func TestCHK_TRAY_16_ServiceMenu(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	app := startCfgApp(t, newCfgService(), false, nil)
	fs := &fakeSvc{state: trayui.SvcStateStopped}
	var msgs []string
	var warns []bool
	app.opt.Service = fs
	app.opt.Notify = func(text string, warn bool) { msgs, warns = append(msgs, text), append(warns, warn) }
	m := app.RightMenu()
	var svc *trayui.Item
	for i := range m {
		if m[i].Text == i18n.T(i18n.KeyMenuService, i18n.RU) {
			svc = &m[i]
			break
		}
	}
	if svc == nil || !strings.Contains(svc.Sub[0].Text, i18n.T(i18n.KeySvcStopped, i18n.RU)) ||
		svc.Sub[2].Disabled || !svc.Sub[3].Disabled {
		t.Fatalf("menu svc=%+v all=%+v", svc, m)
	}
	app.Command(trayui.CmdSvcStart)
	pumpUntil(t, "start message", func() bool { return len(msgs) == 1 })
	if fs.starts != 1 || warns[0] || msgs[0] != fmt.Sprintf(i18n.T(i18n.KeySvcStartSent, i18n.RU), model.ServiceName) {
		t.Errorf("start: %d %q", fs.starts, msgs)
	}
	fs.mu.Lock()
	fs.err = fmt.Errorf("OpenService %s: %w", model.ServiceName, syscall.ERROR_ACCESS_DENIED)
	fs.mu.Unlock()
	app.Command(trayui.CmdSvcStop)
	pumpUntil(t, "access message", func() bool { return len(msgs) == 2 })
	if fs.sts != 1 || !warns[1] || msgs[1] != fmt.Sprintf(i18n.T(i18n.KeySvcErrAccess, i18n.RU), model.ServiceName) {
		t.Errorf("access denied: %q", msgs)
	}
	for _, c := range []struct {
		err  error
		key  string
		warn bool
	}{
		{errServiceDoesNotExist, i18n.KeySvcErrNotInstalled, true},
		{errServiceAlreadyRunning, i18n.KeySvcErrRunning, false},
		{errServiceNotActive, i18n.KeySvcErrNotActive, false},
	} {
		for _, lang := range []string{i18n.RU, i18n.EN} {
			if txt, warn := ServiceResultText("X", true, c.err, lang); txt != fmt.Sprintf(i18n.T(c.key, lang), "X") || warn != c.warn {
				t.Errorf("%v %s: %q", c.err, lang, txt)
			}
		}
	}
	if txt, _ := ServiceResultText("X", false, syscall.Errno(1053), i18n.EN); !strings.Contains(txt, "X") {
		t.Errorf("other error %q", txt)
	}
	// real SCM: a running system service and a missing one
	if st, err := SCMService("EventLog").State(); err != nil || st != trayui.SvcStateRunning {
		t.Errorf("EventLog: %v %v", st, err)
	}
	missing := SCMService("ExchangeCheckerNoSuchService")
	if st, _ := missing.State(); st != trayui.SvcStateNotInstalled {
		t.Errorf("missing state %v", st)
	}
	if txt, warn := ServiceResultText(missing.Name(), true, missing.Start(), i18n.RU); !warn || !strings.Contains(txt, "не установлена") {
		t.Errorf("missing start %q", txt)
	}
	if app.service().Name() == "" || SCMService(model.ServiceName).Name() != "ExchangeChecker" {
		t.Error("service name")
	}
	// no elevation and no configuration change from the tray
	src, err := os.ReadFile("service_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"ShellExecute", "runas", "ChangeServiceConfig", "SetServiceObjectSecurity", "CreateService", "DeleteService"} {
		if strings.Contains(string(src), bad) {
			t.Errorf("service_windows.go uses %s", bad)
		}
	}
}
