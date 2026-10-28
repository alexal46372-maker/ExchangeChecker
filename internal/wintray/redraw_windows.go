//go:build windows

package wintray

import "github.com/alexal46372-maker/ExchangeChecker/internal/winapi"

// eraseRedrawFlags is the one coherent host repaint contract for every
// tray scroll / custom window (CHK-TRAY-68). Installer never scrolls child
// HWNDs and keeps its own persistent brushes.
const eraseRedrawFlags = winapi.RDW_INVALIDATE | winapi.RDW_ERASE | winapi.RDW_ALLCHILDREN | winapi.RDW_UPDATENOW

// eraseRepaint forces a full erase + child repaint of hwnd now.
// Call once after a MoveWindowNoRedraw batch — never ScrollWindow.
func eraseRepaint(hwnd uintptr) {
	if hwnd == 0 || !winapi.IsWindow(hwnd) {
		return
	}
	winapi.InvalidateRect(hwnd, true)
	winapi.RedrawWindow(hwnd, eraseRedrawFlags)
}

// withSetRedraw suspends painting on hwnd while fn runs, then eraseRepaints.
// Optional around large scroll / relayout batches (Win7 smear defence).
func withSetRedraw(hwnd uintptr, fn func()) {
	if hwnd == 0 || !winapi.IsWindow(hwnd) {
		fn()
		return
	}
	winapi.SendMessage(hwnd, winapi.WM_SETREDRAW, 0, 0)
	fn()
	winapi.SendMessage(hwnd, winapi.WM_SETREDRAW, 1, 0)
	eraseRepaint(hwnd)
}
