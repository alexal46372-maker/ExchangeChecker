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

// emailCfg is a service with capability email.
type emailCfg struct {
	*cfgService
	emu      sync.Mutex
	email    model.EmailSettings
	hasPass  bool
	saves    []emailSave
	tests    []model.IPCRequest
	saveErr  error
}

type emailSave struct {
	e     model.EmailSettings
	pass  string
	clear bool
}

func newEmailCfg() *emailCfg {
	e := model.DefaultEmailSettings()
	e.Host, e.Port, e.From = "smtp.example.com", 587, "mon@example.com"
	e.TLSMode = model.EmailTLSStartTLS
	e.Recipients = []model.EmailRecipient{{ID: "ops", Addr: "ops@example.com", Name: "Дежурный"}}
	e.Status = &model.ViberStatus{Sent: 4, Queued: 1}
	f := &emailCfg{cfgService: newCfgService(), email: e, hasPass: true}
	f.cfgService.handler = f
	return f
}

func (f *emailCfg) settings() model.Settings {
	f.emu.Lock()
	defer f.emu.Unlock()
	s := model.DefaultSettings()
	e := f.email
	e.HasPassword = f.hasPass
	s.Email = &e
	return s
}

func (f *emailCfg) GetConfig() (model.ConfigView, string, error) {
	cv, rev, err := f.cfgService.GetConfig()
	cv.Settings = f.settings()
	return cv, rev, err
}

func (f *emailCfg) Snapshot() model.StatusSnapshot {
	snap := f.cfgService.Snapshot()
	s := f.settings()
	snap.Settings = &s
	return snap
}

func (f *emailCfg) SaveSettingsEmail(st model.Settings, viberToken string, clearViber bool, smtpPassword string, clearSmtp bool, rev, account string) (string, error) {
	f.emu.Lock()
	if err := f.saveErr; err != nil {
		f.saveErr = nil
		f.emu.Unlock()
		return "", err
	}
	f.saves = append(f.saves, emailSave{*st.Email, smtpPassword, clearSmtp})
	f.email = st.Email.Stored()
	f.email.Status = &model.ViberStatus{Sent: 4}
	switch {
	case clearSmtp:
		f.hasPass = false
	case smtpPassword != "":
		f.hasPass = true
	}
	f.emu.Unlock()
	return f.cfgService.SaveSettings(st, rev, account)
}

func (f *emailCfg) EmailTest(ctx context.Context, recipientID, addr, smtpPassword string) (model.EmailTestResult, error) {
	f.emu.Lock()
	defer f.emu.Unlock()
	f.tests = append(f.tests, model.IPCRequest{RecipientID: recipientID, Addr: addr, SmtpPassword: smtpPassword})
	if len(f.tests) > 1 {
		return model.EmailTestResult{}, &model.IPCError{Code: model.ErrCodeRateLimited, Key: model.MsgEmailRateLimited, Message: "rate"}
	}
	return model.EmailTestResult{OK: true, Results: []model.EmailTestItem{{ID: "ops", Addr: "ops@example.com", OK: true}}}, nil
}

func (f *emailCfg) counts() (int, int) {
	f.emu.Lock()
	defer f.emu.Unlock()
	return len(f.saves), len(f.tests)
}

// TestCHK_TRAY_64_EmailHidden — without capability email there is no tab;
// Simple mode also hides it when the capability is present.
func TestCHK_TRAY_64_EmailHidden(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	app := startCfgApp(t, newCfgService(), true, nil)
	w := openAdmin(t, app)
	for _, pg := range w.tabPages {
		if pg == pageEmail {
			t.Error("email tab without capability")
		}
	}
	// with capability: Simple hides the tab
	svc := newEmailCfg()
	app2 := startCfgApp(t, svc.cfgService, true, nil)
	w2, err := newSettingsWindow(settingsConfig{lang: i18n.RU, mode: traysettings.Admin, session: app2.session, hidden: true,
		uiMode: traysettings.UISimple})
	if err != nil {
		t.Fatal(err)
	}
	w2.Reload()
	pumpUntil(t, "loaded", func() bool { w2.update(); return !w2.busy && w2.ed.HasEmail })
	for _, pg := range w2.tabPages {
		if pg == pageEmail {
			t.Error("email tab in Simple")
		}
	}
	w2.close()
	app2.Exit()
}

// TestCHK_TRAY_64_EmailPage — Expert+cap: tab, tlsMode password gating,
// save/test, overlap clear vs other tabs.
func TestCHK_TRAY_64_EmailPage(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := newEmailCfg()
	app := startCfgApp(t, svc.cfgService, true, nil)
	w := openAdmin(t, app)
	pumpUntil(t, "email tab", func() bool {
		w.update()
		for _, pg := range w.tabPages {
			if pg == pageEmail {
				return true
			}
		}
		return false
	})
	w.setPage(pageEmail)
	if w.page != pageEmail || !visible(w.ctl[idEmRecipients]) || visible(w.ctl[idSetName]) {
		t.Fatal("email page")
	}
	if why := w.assertPageClear(); why != "" {
		t.Fatal("clear on email:", why)
	}
	if w.text(idEmRecipients) != "ops ops@example.com Дежурный" || w.text(idEmHost) != "smtp.example.com" ||
		w.text(idEmPassword) != "" || w.text(idEmPasswordState) != i18n.T(i18n.KeyEmPasswordSaved, i18n.RU) {
		t.Fatalf("filled: %q %q %q", w.text(idEmRecipients), w.text(idEmHost), w.text(idEmPasswordState))
	}
	// starttls: password enabled
	if !winapi.IsWindowEnabled(w.ctl[idEmPassword]) {
		t.Error("password should be enabled for starttls")
	}
	// switch to none → password disabled
	winapi.SendMessage(w.ctl[idEmTLSMode], winapi.CB_SETCURSEL, 0, 0) // none
	w.emailCommand(idEmTLSMode, winapi.CBN_SELCHANGE)
	if winapi.IsWindowEnabled(w.ctl[idEmPassword]) {
		t.Error("password must be disabled for tlsMode=none")
	}
	if !strings.Contains(w.text(idEmPasswordState), "none") && w.text(idEmPasswordState) != i18n.T(i18n.KeyEmPasswordNoneTLS, i18n.RU) {
		t.Errorf("none state %q", w.text(idEmPasswordState))
	}
	// back to starttls, save with new password
	winapi.SendMessage(w.ctl[idEmTLSMode], winapi.CB_SETCURSEL, 1, 0)
	w.emailCommand(idEmTLSMode, winapi.CBN_SELCHANGE)
	w.set(idEmPassword, "new-secret")
	w.set(idEmRecipients, "ops ops@example.com Дежурный\r\nalert alerts@example.com")
	w.click(t, idEmEnabled)
	w.click(t, idSetSave)
	pumpUntil(t, "email saved", func() bool { n, _ := svc.counts(); return n == 1 && !w.busy })
	sv := svc.saves[0]
	if sv.pass != "new-secret" || sv.clear || !sv.e.Enabled || len(sv.e.Recipients) != 2 {
		t.Errorf("save %+v", sv)
	}
	// test
	winapi.SendMessage(w.ctl[idEmTestTo], winapi.CB_SETCURSEL, 1, 0)
	w.click(t, idEmTest)
	pumpUntil(t, "email test", func() bool { return w.emTesting == nil && strings.Contains(w.text(idEmResult), "доставлено") })
	if _, n := svc.counts(); n != 1 || svc.tests[0].RecipientID != "ops" {
		t.Errorf("email_test %+v", svc.tests)
	}
	// overlap: switch Databases ↔ Email
	w.setPage(0)
	if why := w.assertPageClear(); why != "" {
		t.Fatal("clear on databases:", why)
	}
	w.setPage(pageEmail)
	if why := w.assertPageClear(); why != "" {
		t.Fatal("clear back on email:", why)
	}
	// Simple hides tab
	w.toggleUIMode()
	for _, pg := range w.tabPages {
		if pg == pageEmail {
			t.Error("simple shows email")
		}
	}
}
