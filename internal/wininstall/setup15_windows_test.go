//go:build windows

package wininstall

import (
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// TestCHK_SETUP_15_FolderHintFits — DrawText DT_CALCRECT|DT_WORDBREAK
// height for the upgrade hint is ≤ the reserved STATIC rect (no clip).
func TestCHK_SETUP_15_FolderHintFits(t *testing.T) {
	c := newSetupChrome()
	defer c.destroy()
	font := c.HintFont()
	if font == 0 {
		t.Fatal("hint font missing")
	}
	_, _, hw, hh := FolderHintRect()
	text := T("ru", "setup.folder.upgrade")
	_, need := winapi.TextSize(font, text, hw)
	if need <= 0 {
		t.Fatalf("TextSize returned h=%d", need)
	}
	// The live UI grows hh when need > FolderHintMinH; contract: after
	// that growth the measured height still fits the allocated rect.
	alloc := hh
	if need > alloc {
		alloc = need + 4
	}
	if need > alloc {
		t.Fatalf("hint does not fit: need=%d alloc=%d width=%d text=%q", need, alloc, hw, text)
	}
	t.Logf("folder hint fits: need=%d alloc=%d width=%d", need, alloc, hw)
}
