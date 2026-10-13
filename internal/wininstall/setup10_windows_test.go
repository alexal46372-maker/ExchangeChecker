//go:build windows

package wininstall

import (
	"path/filepath"
	"testing"
	"time"
	"unsafe"

	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// waitWindow returns the window of class once it is ready to be driven.
// FindWindow alone is not enough: CreateWindowEx returns (and the window
// becomes findable) before RunWizard/ConfirmUninstall have created the
// child controls (build) and applied the page state (render: e.g. Next
// disabled on a blocked downgrade). Reads such as IsWindowEnabled/GetDlgItem
// are not messages, so they are not serialized behind that work and can see
// a freshly created, still enabled Next button. The window is created
// without WS_VISIBLE and ShowWindow(SW_SHOW) is called only after build and
// render, so "visible" is the readiness signal.
func waitWindow(t *testing.T, class string) uintptr {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if h := winapi.FindWindow(class); h != 0 && winapi.IsWindowVisible(h) {
			return h
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("window %s not found", class)
	return 0
}

func click(hwnd uintptr, id int) {
	winapi.SendMessage(hwnd, winapi.WM_COMMAND, uintptr(id)|uintptr(winapi.BN_CLICKED)<<16, winapi.GetDlgItem(hwnd, int32(id)))
}

func setText(hwnd uintptr, id int, s string) {
	winapi.SendMessage(winapi.GetDlgItem(hwnd, int32(id)), wmSetText, 0, uintptr(unsafe.Pointer(winapi.UTF16Ptr(s))))
}

func isChecked(hwnd uintptr, id int) bool {
	return winapi.SendMessage(winapi.GetDlgItem(hwnd, int32(id)), BM_GETCHECK, 0, 0) == 1
}

func page(hwnd uintptr) Page { return Page(winapi.SendMessage(hwnd, MsgGetPage, 0, 0)) }

// TestCHK_SETUP_10_WizardUI — drives the real wizard window with
// SendMessage: Next/Back, folder validation error, checkbox defaults,
// Install → progress → Finish, the tray flag; Cancel → exit code 2.
func TestCHK_SETUP_10_WizardUI(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "EC")
	st := NewWizardState("en", "1.1.0", ModeFresh, "", dir, false, `C:\ProgramData\ExchangeChecker`, false)
	ran := make(chan Options, 1)
	done := make(chan WizardOutcome, 1)
	go func() {
		out, err := RunWizard(WizardConfig{State: st, LogPath: `C:\x.log`, NoCancelConfirm: true,
			Run: func(o Options, progress func(string)) Result {
				progress("step 1")
				ran <- o
				return Result{Code: ExitOK, Dir: o.Dir}
			}})
		if err != nil {
			t.Error(err)
		}
		done <- out
	}()
	h := waitWindow(t, WizardClass)
	if page(h) != PageWelcome || winapi.IsWindowEnabled(winapi.GetDlgItem(h, IDBack)) {
		t.Fatal("welcome: Back must be disabled")
	}
	click(h, IDNext)
	if page(h) != PageFolder {
		t.Fatalf("page %d, want folder", page(h))
	}
	setText(h, IDDir, `relative\path`)
	click(h, IDNext)
	if page(h) != PageFolder || winapi.GetWindowText(winapi.GetDlgItem(h, IDError)) == "" {
		t.Fatal("invalid folder must keep the page and show an error")
	}
	setText(h, IDDir, dir)
	click(h, IDNext)
	if page(h) != PageOptions {
		t.Fatalf("page %d, want options", page(h))
	}
	if !isChecked(h, IDShortcut) || !isChecked(h, IDService) {
		t.Fatal("options default: shortcut and service checked")
	}
	click(h, IDBack)
	if page(h) != PageFolder {
		t.Fatal("back to folder")
	}
	click(h, IDNext)
	winapi.SendMessage(winapi.GetDlgItem(h, IDShortcut), BM_SETCHECK, 0, 0)
	click(h, IDNext) // Install
	var o Options
	select {
	case o = <-ran:
	case <-time.After(10 * time.Second):
		t.Fatal("install not started")
	}
	if !o.NoShortcut || o.NoStart || CleanDir(o.Dir) != CleanDir(dir) || o.Silent {
		t.Fatalf("options passed: %+v", o)
	}
	deadline := time.Now().Add(10 * time.Second)
	for page(h) != PageFinish {
		if time.Now().After(deadline) {
			t.Fatal("finish page not reached")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !isChecked(h, IDTray) {
		t.Fatal("finish: tray checkbox default on")
	}
	click(h, IDNext) // Finish
	out := <-done
	if out.Cancelled || !out.LaunchTray || out.Result.Code != ExitOK {
		t.Fatalf("outcome %+v", out)
	}

	// Cancel → exit code 2, nothing run
	st2 := NewWizardState("ru", "1.1.0", ModeFresh, "", dir, false, "", false)
	go func() {
		out, _ := RunWizard(WizardConfig{State: st2, NoCancelConfirm: true,
			Run: func(Options, func(string)) Result { t.Error("must not run"); return Result{} }})
		done <- out
	}()
	h = waitWindow(t, WizardClass)
	click(h, IDNext)
	click(h, IDCancel)
	out = <-done
	if !out.Cancelled || out.Result.Code != ExitCancelled || out.LaunchTray {
		t.Fatalf("cancel outcome %+v", out)
	}
}

// TestCHK_SETUP_10_UninstallConfirm — the "remove data" checkbox is off by
// default; Cancel → not ok; checked + Uninstall → ok, purge.
func TestCHK_SETUP_10_UninstallConfirm(t *testing.T) {
	type res struct{ ok, purge bool }
	done := make(chan res, 1)
	run := func() {
		go func() {
			ok, p, err := ConfirmUninstall("en")
			if err != nil {
				t.Error(err)
			}
			done <- res{ok, p}
		}()
	}
	run()
	h := waitWindow(t, UninstallClass)
	if isChecked(h, IDPurge) {
		t.Fatal("purge must be off by default")
	}
	click(h, IDCancel)
	if r := <-done; r.ok {
		t.Fatal("cancel returned ok")
	}
	run()
	h = waitWindow(t, UninstallClass)
	// the class window can be found before its children exist (WM_CREATE)
	var purge uintptr
	for i := 0; i < 250 && purge == 0; i++ {
		if purge = winapi.GetDlgItem(h, IDPurge); purge == 0 {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if purge == 0 {
		t.Fatal("purge checkbox not created")
	}
	winapi.SendMessage(purge, BM_SETCHECK, 1, 0)
	if !isChecked(h, IDPurge) {
		t.Fatal("purge checkbox did not take the check")
	}
	click(h, IDUninstall)
	if r := <-done; !r.ok || !r.purge {
		t.Fatalf("uninstall+purge: %+v", r)
	}
}

// TestCHK_SETUP_10_WizardUpgradeAndDowngrade — upgrade: service checkbox
// disabled, shows the previous state, and the run gets the same NoStart;
// downgrade: Next disabled on Welcome, Cancel → exit 3, nothing run.
func TestCHK_SETUP_10_WizardUpgradeAndDowngrade(t *testing.T) {
	for _, was := range []bool{true, false} {
		st := NewWizardState("en", "1.1.0", ModeUpgrade, "1.0.0", `C:\EC`, true, "", was)
		ran := make(chan Options, 1)
		done := make(chan WizardOutcome, 1)
		go func() {
			out, _ := RunWizard(WizardConfig{State: st, NoCancelConfirm: true,
				Run: func(o Options, _ func(string)) Result { ran <- o; return Result{Code: ExitOK} }})
			done <- out
		}()
		h := waitWindow(t, WizardClass)
		click(h, IDNext)
		click(h, IDNext)
		if page(h) != PageOptions {
			t.Fatalf("page %d", page(h))
		}
		svc := winapi.GetDlgItem(h, IDService)
		if winapi.IsWindowEnabled(svc) || isChecked(h, IDService) != was {
			t.Fatalf("was=%v: service checkbox enabled=%v checked=%v", was, winapi.IsWindowEnabled(svc), isChecked(h, IDService))
		}
		winapi.SendMessage(svc, BM_SETCHECK, map[bool]uintptr{true: 0, false: 1}[was], 0) // try to flip
		click(h, IDNext)
		o := <-ran
		if o.NoStart != !was {
			t.Fatalf("was=%v: NoStart=%v", was, o.NoStart)
		}
		for page(h) != PageFinish {
			time.Sleep(20 * time.Millisecond)
		}
		click(h, IDNext)
		<-done
	}

	st := NewWizardState("en", "1.0.0", ModeDowngrade, "1.1.0", `C:\EC`, true, "", true)
	done := make(chan WizardOutcome, 1)
	go func() {
		out, _ := RunWizard(WizardConfig{State: st,
			Run: func(Options, func(string)) Result { t.Error("must not run"); return Result{} }})
		done <- out
	}()
	h := waitWindow(t, WizardClass)
	if winapi.IsWindowEnabled(winapi.GetDlgItem(h, IDNext)) {
		t.Fatal("downgrade: Next must be disabled on Welcome")
	}
	click(h, IDNext)
	if page(h) != PageWelcome {
		t.Fatal("downgrade left Welcome")
	}
	click(h, IDCancel) // no confirmation needed: nothing was changed
	out := <-done
	if out.Result.Code != ExitDowngrade {
		t.Fatalf("downgrade exit %d, want %d", out.Result.Code, ExitDowngrade)
	}
}
