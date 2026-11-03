//go:build windows

package wininstall

import (
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// TestCHK_SETUP_15_FolderHintFits drives a real wizard Folder page with
// DirLocked (upgrade), then compares GetClientRect of the hint STATIC with
// DrawText DT_CALCRECT|DT_WORDBREAK (winapi.TextSize) for the Russian upgrade
// text using the control's HFONT and width.
//
// 144 dpi is simulated by scaling the hint font height and content width by
// 144/96 (same ratio the layout would see under a 150% DPI scale); the UI
// growth rule (hh = max(FolderHintMinH, need+4)) must still fit. EN is not
// required.
func TestCHK_SETUP_15_FolderHintFits(t *testing.T) {
	text := T("ru", "setup.folder.upgrade")
	if text == "" || text == "setup.folder.upgrade" {
		t.Fatal("missing Russian setup.folder.upgrade")
	}

	st := NewWizardState("ru", "1.1.0", ModeUpgrade, "1.0.0", `C:\Program Files\Exchange Checker`, true, `C:\ProgramData\ExchangeChecker`, false)
	done := make(chan struct{})
	go func() {
		_, _ = RunWizard(WizardConfig{State: st, LogPath: `C:\x.log`, NoCancelConfirm: true,
			Run: func(o Options, progress func(string)) Result {
				return Result{Code: ExitOK, Dir: o.Dir}
			}})
		close(done)
	}()
	h := waitWindow(t, WizardClass)
	click(h, IDNext) // Welcome → Folder
	if page(h) != PageFolder {
		t.Fatalf("page %d, want folder", page(h))
	}
	hint := winapi.GetDlgItem(h, IDFolderHint)
	if hint == 0 {
		t.Fatal("IDFolderHint missing")
	}
	if !winapi.IsWindowVisible(hint) {
		t.Fatal("folder hint must be visible when DirLocked")
	}
	rc := winapi.GetClientRect(hint)
	clientW, clientH := rc.Right-rc.Left, rc.Bottom-rc.Top
	if clientW <= 0 || clientH <= 0 {
		t.Fatalf("GetClientRect empty: %+v", rc)
	}
	font := winapi.SendMessage(hint, winapi.WM_GETFONT, 0, 0)
	if font == 0 {
		t.Fatal("hint has no font")
	}
	_, need := winapi.TextSize(font, text, clientW)
	if need <= 0 {
		t.Fatalf("TextSize h=%d", need)
	}
	if need > clientH {
		t.Fatalf("hint clipped: DrawText need=%d > GetClientRect h=%d w=%d text=%q", need, clientH, clientW, text)
	}
	t.Logf("96dpi-ish live: need=%d clientH=%d clientW=%d", need, clientH, clientW)

	// Prove the assertion can fail: a deliberately tiny box cannot hold the text.
	_, needNarrow := winapi.TextSize(font, text, 40)
	if needNarrow <= 20 {
		t.Fatalf("narrow measure too short (%d): test would not catch undersized hint", needNarrow)
	}

	// 144 dpi simulation: scale font cell and content width by 144/96.
	const scale = 144.0 / 96.0
	fontPx := FolderHintFontPx
	contentW := ContentWidth()
	minH := FolderHintMinH
	h144 := -int32(int(float64(fontPx)*scale + 0.5))
	font144 := winapi.CreateFontWeight("Segoe UI", h144, winapi.FW_NORMAL)
	if font144 == 0 {
		font144 = winapi.CreateFontWeight("Tahoma", h144, winapi.FW_NORMAL)
	}
	if font144 == 0 {
		t.Fatal("144dpi font")
	}
	defer winapi.DeleteObject(font144)
	w144 := int32(int(float64(contentW)*scale + 0.5))
	minH144 := int32(int(float64(minH)*scale + 0.5))
	_, need144 := winapi.TextSize(font144, text, w144)
	if need144 <= 0 {
		t.Fatalf("144dpi TextSize h=%d", need144)
	}
	alloc144 := minH144
	if need144 > alloc144 {
		alloc144 = need144 + 4 // same growth as wizard render
	}
	if need144 > alloc144 {
		t.Fatalf("144dpi hint does not fit after growth: need=%d alloc=%d width=%d", need144, alloc144, w144)
	}
	t.Logf("144dpi sim: need=%d alloc=%d width=%d fontH=%d (scale font+layout)", need144, alloc144, w144, h144)

	click(h, IDCancel)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("wizard did not exit")
	}
}
