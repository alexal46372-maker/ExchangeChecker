//go:build windows

package wintray

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// TestCHK_TRAY_46_Screens — CI screenshots (phase 11/12): every window of
// uiScenes, RU and EN, at 96 and 144 dpi, light and dark (CHK-TRAY-55),
// as PNG into $EC_UI_SCREENS (the ui-screens artifact of the windows job,
// 7 days). The windows are shown off-screen (not hidden) and rendered
// with PrintWindow. Includes summary* and alarm_test* (CHK-TRAY-54).
// Dark scenes are named with a "_dark" suffix (CHK-TRAY-55/56).
func TestCHK_TRAY_46_Screens(t *testing.T) {
	dir := os.Getenv("EC_UI_SCREENS")
	if dir == "" {
		t.Skip("set EC_UI_SCREENS to a directory (CI windows job)")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer func() { testDPI = 0; trayui.SetTheme(trayui.ThemeLight) }()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, theme := range []trayui.Theme{trayui.ThemeLight, trayui.ThemeDark} {
		trayui.SetTheme(theme)
		suffix := ""
		if theme == trayui.ThemeDark {
			suffix = "_dark"
		}
		for _, dpi := range []int32{96, 144} {
			for _, lang := range []string{i18n.RU, i18n.EN} {
				testDPI = dpi
				uiScenes(t, lang, func(name string, hwnd uintptr) {
					ApplyWindowTheme(hwnd)
					file := fmt.Sprintf("%s%s-%s-%d", strings.ReplaceAll(name, " ", "_"), suffix, lang, dpi)
					if err := screenshot(hwnd, filepath.Join(dir, file)); err != nil {
						t.Errorf("%s: %v", file, err)
						return
					}
					n++
				})
			}
		}
	}
	t.Logf("%d screenshots in %s", n, dir)
	if n == 0 {
		t.Fatal("no screenshots")
	}
}

// hwndNoTopmost is HWND_NOTOPMOST of SetWindowPos.
var hwndNoTopmost = ^uintptr(1) // (HWND)-2

// offscreenX / offscreenY: where the windows are shown for the capture.
const offscreenX, offscreenY = -20000, -20000

// screenshot shows hwnd off-screen, renders it and writes a PNG; the
// window is hidden again afterwards.
func screenshot(hwnd uintptr, path string) error {
	r := winapi.GetWindowRect(hwnd)
	winapi.SetWindowPos(hwnd, 0, offscreenX, offscreenY, 0, 0,
		winapi.SWP_NOSIZE|winapi.SWP_NOZORDER|winapi.SWP_NOACTIVATE|winapi.SWP_SHOWWINDOW)
	defer func() {
		winapi.ShowWindow(hwnd, winapi.SW_HIDE)
		winapi.SetWindowPos(hwnd, 0, r.Left, r.Top, 0, 0, winapi.SWP_NOSIZE|winapi.SWP_NOZORDER|winapi.SWP_NOACTIVATE)
	}()
	// paint now: windows shown off-screen may not have been composed yet
	winapi.RedrawWindow(hwnd, winapi.RDW_INVALIDATE|winapi.RDW_ERASE|winapi.RDW_ALLCHILDREN|winapi.RDW_UPDATENOW|winapi.RDW_FRAME)
	for i := 0; i < 3; i++ {
		PumpPending()
	}
	w, h, bgra, ok := winapi.CaptureWindow(hwnd)
	suffix := ""
	if !ok || blackClient(bgra, w, h) {
		// phase 11: DWM has not composed the off-screen window — bring it
		// on screen, on top, wait for a composition and print it again
		// (no classic WM_PRINT: that draws the Windows 7 Basic frame)
		work := workArea()
		winapi.SetWindowPos(hwnd, winapi.HWND_TOPMOST, work.Left, work.Top, 0, 0,
			winapi.SWP_NOSIZE|winapi.SWP_NOACTIVATE|winapi.SWP_SHOWWINDOW)
		defer winapi.SetWindowPos(hwnd, hwndNoTopmost, 0, 0, 0, 0, winapi.SWP_NOSIZE|winapi.SWP_NOMOVE|winapi.SWP_NOACTIVATE)
		winapi.RedrawWindow(hwnd, winapi.RDW_INVALIDATE|winapi.RDW_ERASE|winapi.RDW_ALLCHILDREN|winapi.RDW_UPDATENOW|winapi.RDW_FRAME)
		for i := 0; i < 3; i++ {
			PumpPending()
			winapi.DwmFlush()
		}
		w, h, bgra, ok = winapi.CaptureWindow(hwnd)
		if !ok || blackClient(bgra, w, h) {
			// the screen copy, marked in the file name
			w, h, bgra, ok = winapi.CaptureScreen(hwnd)
			suffix = "_bitblt"
		}
	}
	path += suffix + ".png"
	if !ok {
		return fmt.Errorf("capture failed (%dx%d)", w, h)
	}
	full := image.NewNRGBA(image.Rect(0, 0, int(w), int(h)))
	for i := 0; i+3 < len(bgra); i += 4 {
		full.Pix[i], full.Pix[i+1], full.Pix[i+2], full.Pix[i+3] = bgra[i+2], bgra[i+1], bgra[i], 255
	}
	// clip the invisible resize borders (DWM extended frame bounds)
	var img image.Image = full
	if vf, ok := winapi.VisibleFrame(hwnd); ok {
		wr := winapi.GetWindowRect(hwnd)
		crop := image.Rect(int(vf.Left-wr.Left), int(vf.Top-wr.Top), int(vf.Right-wr.Left), int(vf.Bottom-wr.Top)).Intersect(full.Bounds())
		if !crop.Empty() {
			img = full.SubImage(crop)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// blackClient reports a capture whose middle row is all black (an
// uncomposed client area).
func blackClient(bgra []byte, w, h int32) bool {
	row := int(h/2) * int(w) * 4
	for x := 0; x < int(w)*4 && row+x+2 < len(bgra); x += 4 {
		if bgra[row+x]|bgra[row+x+1]|bgra[row+x+2] != 0 {
			return false
		}
	}
	return true
}
