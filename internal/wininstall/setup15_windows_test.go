//go:build windows

package wininstall

import (
	"fmt"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// folderHintFits compares GetClientRect of the hint STATIC with
// DrawText DT_CALCRECT|DT_WORDBREAK (winapi.TextSize) for text using the
// control's HFONT and width. Returns an error when the text would clip.
func folderHintFits(hint uintptr, text string) error {
	rc := winapi.GetClientRect(hint)
	clientW, clientH := rc.Right-rc.Left, rc.Bottom-rc.Top
	if clientW <= 0 || clientH <= 0 {
		return fmt.Errorf("GetClientRect empty: %+v", rc)
	}
	font := winapi.SendMessage(hint, winapi.WM_GETFONT, 0, 0)
	if font == 0 {
		return fmt.Errorf("hint has no font")
	}
	_, need := winapi.TextSize(font, text, clientW)
	if need <= 0 {
		return fmt.Errorf("TextSize h=%d", need)
	}
	if need > clientH {
		return fmt.Errorf("hint clipped: DrawText need=%d > GetClientRect h=%d w=%d text=%q", need, clientH, clientW, text)
	}
	return nil
}

func startFolderHintWizard(t *testing.T) (hwnd, hint uintptr, done <-chan struct{}, cancel func()) {
	t.Helper()
	text := T("setup.folder.upgrade")
	if text == "" || text == "setup.folder.upgrade" {
		t.Fatal("missing Russian setup.folder.upgrade")
	}
	st := NewWizardState("1.1.0", ModeUpgrade, "1.0.0", `C:\Program Files\Exchange Checker`, true, `C:\ProgramData\ExchangeChecker`, false)
	ch := make(chan struct{})
	go func() {
		_, _ = RunWizard(WizardConfig{State: st, LogPath: `C:\x.log`, NoCancelConfirm: true,
			Run: func(o Options, progress func(string)) Result {
				return Result{Code: ExitOK, Dir: o.Dir}
			}})
		close(ch)
	}()
	hwnd = waitWindow(t, WizardClass)
	click(hwnd, IDNext) // Welcome → Folder
	if page(hwnd) != PageFolder {
		t.Fatalf("page %d, want folder", page(hwnd))
	}
	hint = winapi.GetDlgItem(hwnd, IDFolderHint)
	if hint == 0 {
		t.Fatal("IDFolderHint missing")
	}
	if !winapi.IsWindowVisible(hint) {
		t.Fatal("folder hint must be visible when DirLocked")
	}
	cancel = func() {
		click(hwnd, IDCancel)
		select {
		case <-ch:
		case <-time.After(5 * time.Second):
			t.Fatal("wizard did not exit")
		}
	}
	return hwnd, hint, ch, cancel
}

// TestCHK_SETUP_15_FolderHintFits drives a real wizard Folder page with
// DirLocked (upgrade), then compares GetClientRect of the hint STATIC with
// DrawText DT_CALCRECT|DT_WORDBREAK (winapi.TextSize) for the Russian upgrade
// text using the control's HFONT and width.
//
// 144 dpi: testHintFontPx scales the hint font by 144/96; the live control
// grows (hh = need+4) and GetClientRect must still fit TextSize.
// Negative case of the same check: after shrinking the control to
// FolderHintMinH, folderHintFits must report clipping.
func TestCHK_SETUP_15_FolderHintFits(t *testing.T) {
	text := T("setup.folder.upgrade")
	if text == "" || text == "setup.folder.upgrade" {
		t.Fatal("missing Russian setup.folder.upgrade")
	}

	t.Run("live_default", func(t *testing.T) {
		_, hint, _, cancel := startFolderHintWizard(t)
		defer cancel()
		if err := folderHintFits(hint, text); err != nil {
			t.Fatal(err)
		}
		rc := winapi.GetClientRect(hint)
		t.Logf("default: clientH=%d clientW=%d", rc.Bottom-rc.Top, rc.Right-rc.Left)

		// Negative case of THE SAME check: force height = FolderHintMinH
		// (no growth). Text must clip so folderHintFits returns an error.
		hx, hy, hw, _ := FolderHintRect()
		winapi.MoveWindow(hint, hx, hy, hw, FolderHintMinH)
		rc = winapi.GetClientRect(hint)
		gotH := rc.Bottom - rc.Top
		if gotH > FolderHintMinH+2 {
			t.Fatalf("MoveWindow did not shrink hint: h=%d want ~%d", gotH, FolderHintMinH)
		}
		err := folderHintFits(hint, text)
		if err == nil {
			t.Fatalf("expected clip at FolderHintMinH=%d (same GetClientRect vs TextSize check must fail)", FolderHintMinH)
		}
		t.Logf("negative FolderHintMinH: %v", err)
	})

	t.Run("live_144dpi_font", func(t *testing.T) {
		testHintFontPx = int32(int(float64(FolderHintFontPx)*144.0/96.0 + 0.5))
		defer func() { testHintFontPx = 0 }()
		if testHintFontPx <= FolderHintFontPx {
			t.Fatalf("144dpi font px %d not larger than default %d", testHintFontPx, FolderHintFontPx)
		}
		_, hint, _, cancel := startFolderHintWizard(t)
		defer cancel()
		if err := folderHintFits(hint, text); err != nil {
			t.Fatal(err)
		}
		rc := winapi.GetClientRect(hint)
		font := winapi.SendMessage(hint, winapi.WM_GETFONT, 0, 0)
		_, need := winapi.TextSize(font, text, rc.Right-rc.Left)
		t.Logf("144dpi font px=%d: need=%d clientH=%d clientW=%d", testHintFontPx, need, rc.Bottom-rc.Top, rc.Right-rc.Left)
		if rc.Bottom-rc.Top < need {
			t.Fatalf("144dpi GetClientRect h=%d < TextSize need=%d", rc.Bottom-rc.Top, need)
		}
	})
}
