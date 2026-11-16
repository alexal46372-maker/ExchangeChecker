//go:build windows

package wintray

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// TestCHK_TRAY_104_AlertWindowNoClose — phase 16 C: the alert window of a
// real alarm (ipc.Server → App) cannot be closed. Its buttons are exactly
// «Подтвердить» and «Звук выкл» (no «Закрыть», no IDCANCEL control); the
// system menu has no «Закрыть» (SC_CLOSE deleted — the caption close box is
// inactive) and no trailing separator, but keeps «Свернуть» (SC_MINIMIZE,
// WS_MINIMIZEBOX); WM_CLOSE, SC_CLOSE (what Alt+F4, the system menu and the
// close box send) and Esc leave it shown and sounding, unacknowledged;
// «Свернуть» minimizes it; the hint names the remaining actions and no
// longer mentions «Закрыть».
const wsMaximizeBox = 0x00010000

func TestCHK_TRAY_104_AlertWindowNoClose(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := &alertService{}
	svc.set(1, ptAlert("a", "Склад", "_Node1/Склад", model.LevelError))
	b := &testBeeper{}
	app, _ := startAlertApp(t, svc, b)
	pumpUntil(t, "alert window", func() bool {
		v := app.AlertView()
		return v.Visible && v.Sound && app.AlertWindow() != 0
	})
	h := app.AlertWindow()
	var buttons []string
	for _, c := range winapi.Children(h) {
		if strings.EqualFold(winapi.GetClassName(c), "Button") {
			buttons = append(buttons, winapi.GetWindowText(c))
		}
	}
	has := map[string]bool{}
	for _, s := range buttons {
		has[s] = true
	}
	if len(buttons) != 2 || !has[i18n.T(i18n.KeyAlertAck)] || !has[i18n.T(i18n.KeyAlertMute)] {
		t.Fatalf("buttons %q, want «Подтвердить» and «Звук выкл» only", buttons)
	}
	for _, s := range buttons {
		if s == "Закрыть" {
			t.Fatal("«Закрыть» button still there")
		}
	}
	if winapi.GetDlgItem(h, winapi.IDCANCEL) != 0 || winapi.GetDlgItem(h, idAlertAck) == 0 || winapi.GetDlgItem(h, idAlertMute) == 0 {
		t.Error("control ids")
	}
	st := winapi.WindowStyle(h)
	if st&winapi.WS_MINIMIZEBOX == 0 || st&winapi.WS_SYSMENU == 0 || st&wsMaximizeBox != 0 {
		t.Errorf("style %x: minimize box + system menu, no maximize", st)
	}
	m := winapi.GetSystemMenu(h, false)
	n := winapi.GetMenuItemCount(m)
	if m == 0 || n <= 0 {
		t.Fatalf("system menu %x n %d", m, n)
	}
	minimize := false
	for i := 0; i < n; i++ {
		switch winapi.GetMenuItemID(m, i) {
		case winapi.SC_CLOSE:
			t.Error("SC_CLOSE still in the system menu (close box active)")
		case winapi.SC_MINIMIZE:
			minimize = true
		}
	}
	if !minimize {
		t.Error("«Свернуть» (SC_MINIMIZE) missing from the system menu")
	}
	if winapi.GetMenuItemID(m, n-1) == 0 {
		t.Error("trailing separator left in the system menu")
	}
	hint := winapi.GetWindowText(winapi.GetDlgItem(h, idAlertHint))
	if hint != i18n.T(i18n.KeyAlertCloseHint) || strings.Contains(hint, "Закрыть") ||
		!strings.Contains(hint, "Подтвердить") || !strings.Contains(hint, "свернуть") {
		t.Errorf("hint %q", hint)
	}
	// WM_CLOSE, SC_CLOSE (Alt+F4 / system menu / close box), Esc
	winapi.SendMessage(h, winapi.WM_CLOSE, 0, 0)
	winapi.SendMessage(h, winapi.WM_SYSCOMMAND, winapi.SC_CLOSE, 0)
	winapi.SendMessage(h, winapi.WM_COMMAND, winapi.IDCANCEL, 0)
	pumpFor(150 * time.Millisecond)
	v := app.AlertView()
	if !winapi.IsWindow(h) || !v.Visible || !v.Sound || !app.awin.shown || len(svc.ackCalls()) != 0 {
		t.Fatalf("closed / muted / acknowledged: window %v view %+v acks %d", winapi.IsWindow(h), v, len(svc.ackCalls()))
	}
	if app.awin.closeAttempts != 2 {
		t.Errorf("close attempts %d, want 2", app.awin.closeAttempts)
	}
	// «Свернуть» still works (the window is hidden in tests: show it first)
	winapi.ShowWindow(h, winapi.SW_SHOWNOACTIVATE)
	winapi.SendMessage(h, winapi.WM_SYSCOMMAND, winapi.SC_MINIMIZE, 0)
	pumpFor(100 * time.Millisecond)
	if !winapi.IsIconic(h) {
		t.Error("SC_MINIMIZE did not minimize")
	}
	winapi.ShowWindow(h, winapi.SW_HIDE)
}
