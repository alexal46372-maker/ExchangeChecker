//go:build windows

package wininstall

import (
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// TestCHK_SETUP_14_ChromeSmoke — Segoe UI fonts and solid brushes for the
// modern chrome resolve without falling back to DEFAULT_GUI_FONT.
func TestCHK_SETUP_14_ChromeSmoke(t *testing.T) {
	c := newSetupChrome()
	defer c.destroy()
	if c.contentBrush == 0 || c.brandBrush == 0 || c.barBrush == 0 {
		t.Fatal("chrome brushes missing")
	}
	if c.fontTitle == 0 || c.fontBody == 0 {
		t.Fatal("chrome fonts missing (Segoe UI / Tahoma)")
	}
	if c.fontTitle == winapi.GetStockObject(winapi.DEFAULT_GUI_FONT) {
		t.Fatal("title font must not be DEFAULT_GUI_FONT")
	}
	// CreateFontWeight bold path
	bold := winapi.CreateFontWeight("Segoe UI", -16, winapi.FW_BOLD)
	if bold == 0 {
		bold = winapi.CreateFontWeight("Tahoma", -16, winapi.FW_BOLD)
	}
	if bold == 0 {
		t.Fatal("CreateFontWeight failed")
	}
	winapi.DeleteObject(bold)
}
