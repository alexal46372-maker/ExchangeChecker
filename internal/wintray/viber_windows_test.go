//go:build windows

package wintray

import (
	"context"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/traysettings"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// viberCfg is a service with capability viber: save_settings with
// viberToken / clearViberToken and viber_test.
type viberCfg struct {
	*cfgService
	vmu      sync.Mutex
	viber    model.ViberSettings
	hasToken bool
	saves    []viberSave
	tests    []model.IPCRequest
	saveErr  error
}

type viberSave struct {
	v     model.ViberSettings
	token string
	clear bool
}

func newViberCfg() *viberCfg {
	v := model.DefaultViberSettings()
	v.Recipients = []model.ViberRecipient{{ID: "abc=", Name: "Дежурный"}}
	v.Status = &model.ViberStatus{Sent: 4, Queued: 1}
	f := &viberCfg{cfgService: newCfgService(), viber: v, hasToken: true}
	f.cfgService.handler = f
	return f
}

func (f *viberCfg) settings() model.Settings {
	f.vmu.Lock()
	defer f.vmu.Unlock()
	s := model.DefaultSettings()
	v := f.viber
	v.HasToken = f.hasToken
	s.Viber = &v
	return s
}

func (f *viberCfg) GetConfig() (model.ConfigView, string, error) {
	cv, rev, err := f.cfgService.GetConfig()
	cv.Settings = f.settings()
	return cv, rev, err
}

func (f *viberCfg) Snapshot() model.StatusSnapshot {
	snap := f.cfgService.Snapshot()
	s := f.settings()
	snap.Settings = &s
	return snap
}

func (f *viberCfg) SaveSettingsViber(st model.Settings, token string, clear bool, rev, account string) (string, error) {
	f.vmu.Lock()
	if err := f.saveErr; err != nil {
		f.saveErr = nil
		f.vmu.Unlock()
		return "", err
	}
	f.saves = append(f.saves, viberSave{*st.Viber, token, clear})
	f.viber = st.Viber.Stored()
	f.viber.Status = &model.ViberStatus{Sent: 4}
	switch {
	case clear:
		f.hasToken = false
	case token != "":
		f.hasToken = true
	}
	f.vmu.Unlock()
	return f.cfgService.SaveSettings(st, rev, account)
}

func (f *viberCfg) ViberTest(ctx context.Context, recipientID, token string) (model.ViberTestResult, error) {
	f.vmu.Lock()
	defer f.vmu.Unlock()
	f.tests = append(f.tests, model.IPCRequest{RecipientID: recipientID, ViberToken: token})
	if len(f.tests) > 1 {
		return model.ViberTestResult{}, &model.IPCError{Code: model.ErrCodeRateLimited, Key: model.MsgViberRateLimited, Message: "rate"}
	}
	return model.ViberTestResult{OK: true, Results: []model.ViberTestItem{{ID: "abc=", OK: true}}}, nil
}

func (f *viberCfg) counts() (int, int) {
	f.vmu.Lock()
	defer f.vmu.Unlock()
	return len(f.saves), len(f.tests)
}

func visible(h uintptr) bool { return winapi.WindowStyle(h)&winapi.WS_VISIBLE != 0 }

// TestCHK_TRAY_43_ViberHidden — without capability viber there is no
// «Уведомления Viber…» button.
func TestCHK_TRAY_43_ViberHidden(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	app := startCfgApp(t, newCfgService(), true, nil)
	w := openAdmin(t, app)
	for _, pg := range w.tabPages {
		if pg == pageViber {
			t.Error("viber tab without capability")
		}
	}
}

// TestCHK_TRAY_43_ViberPage — the page with capability viber: load,
// save with the write-only token, «Тест» (viber_test {recipientId,
// viberToken}), rate_limited, service field errors, simple mode hides it,
// layout at 96/120/144 dpi in RU and EN.
func TestCHK_TRAY_43_ViberPage(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := newViberCfg()
	app := startCfgApp(t, svc.cfgService, true, nil)
	w := openAdmin(t, app)
	pumpUntil(t, "viber tab", func() bool {
		w.update()
		for _, pg := range w.tabPages {
			if pg == pageViber {
				return true
			}
		}
		return false
	})
	w.click(t, idSetViber)
	if w.page != pageViber || !visible(w.ctl[idVbRecipients]) || visible(w.ctl[idSetName]) {
		t.Fatal("viber page")
	}
	if w.text(idVbRecipients) != "abc= Дежурный" || w.text(idVbFrom) != "08:30" || !w.check(idVbDay1) || w.check(idVbDay1+6) ||
		w.text(idVbReminder) != "60" || w.text(idVbToken) != "" || w.text(idVbTokenState) != i18n.T(i18n.KeyVbTokenSaved, i18n.RU) ||
		!strings.Contains(w.text(idVbStatus), "4") {
		t.Fatalf("filled: %q %q %q %q", w.text(idVbRecipients), w.text(idVbFrom), w.text(idVbTokenState), w.text(idVbStatus))
	}
	// save: new token and recipient
	w.set(idVbToken, "new-token")
	w.set(idVbRecipients, "abc= Дежурный\r\nxyz Иван")
	w.click(t, idVbEnabled)
	w.click(t, idSetSave)
	pumpUntil(t, "viber saved", func() bool { n, _ := svc.counts(); return n == 1 && !w.busy })
	sv := svc.saves[0]
	if sv.token != "new-token" || sv.clear || !sv.v.Enabled || len(sv.v.Recipients) != 2 || sv.v.Recipients[1].Name != "Иван" {
		t.Errorf("save %+v", sv)
	}
	if w.text(idVbToken) != "" || w.text(idVbTokenState) != i18n.T(i18n.KeyVbTokenSaved, i18n.RU) || w.ed.ViberUnsaved() {
		t.Errorf("after save: token box %q state %q", w.text(idVbToken), w.text(idVbTokenState))
	}
	// test to one recipient
	winapi.SendMessage(w.ctl[idVbTestTo], winapi.CB_SETCURSEL, 2, 0)
	w.click(t, idVbTest)
	pumpUntil(t, "viber test", func() bool { return w.vbTesting == nil && strings.Contains(w.text(idVbResult), "доставлено") })
	if _, n := svc.counts(); n != 1 || svc.tests[0].RecipientID != "xyz" || svc.tests[0].ViberToken != "" {
		t.Errorf("viber_test %+v", svc.tests)
	}
	w.click(t, idVbTest)
	pumpUntil(t, "rate limited", func() bool {
		return w.vbTesting == nil && strings.Contains(w.text(idVbResult), i18n.Message(model.MsgViberRateLimited, i18n.RU))
	})
	// a field error of the service focuses the field
	svc.vmu.Lock()
	svc.saveErr = &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgMonitorSettingsInvalid, Message: "bad",
		Args: []string{"viber.events[0]", model.ValEnum, "x"}}
	svc.vmu.Unlock()
	w.click(t, idSetSave)
	pumpUntil(t, "field error", func() bool { return !w.busy && w.lastFocus == traysettings.FieldViberEvents })
	svc.vmu.Lock()
	svc.saveErr = &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgViberNoToken, Message: "no token"}
	svc.vmu.Unlock()
	w.click(t, idSetSave)
	pumpUntil(t, "no token", func() bool { return !w.busy && w.lastFocus == traysettings.FieldViberToken })
	// a local error
	w.set(idVbReminder, "5")
	w.click(t, idSetSave)
	if w.lastFocus != traysettings.FieldViberReminder || !strings.Contains(w.status, "1440") {
		t.Errorf("reminder 5: focus %s status %q", w.lastFocus, w.status)
	}
	w.set(idVbReminder, "60")
	// simple mode hides the page and the Viber tab
	w.toggleUIMode()
	hasVb := false
	for _, pg := range w.tabPages {
		if pg == pageViber {
			hasVb = true
		}
	}
	if w.page != 0 || hasVb {
		t.Error("simple mode shows viber")
	}
	w.toggleUIMode()

	defer func() { testDPI = 0 }()
	for _, dpi := range []int32{96, 120, 144} {
		for _, lang := range []string{i18n.RU, i18n.EN} {
			testDPI = dpi
			vw, err := newSettingsWindow(settingsConfig{lang: lang, mode: traysettings.Admin, session: app.session, hidden: true,
				uiMode: traysettings.UIExpert})
			if err != nil {
				t.Fatal(err)
			}
			vw.Reload()
			pumpUntil(t, "loaded", func() bool { return !vw.busy && vw.ed.HasViber })
			vw.setPage(pageViber)
			checkLayout(t, "viber "+lang+" "+string(rune('0'+dpi/100))+string(rune('0'+dpi/10%10))+string(rune('0'+dpi%10))+" dpi", vw.hwnd)
			vw.close()
		}
	}
}
