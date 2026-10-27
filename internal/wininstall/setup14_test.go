package wininstall

import (
	"testing"
)

// TestCHK_SETUP_14_LayoutContract — modern wizard geometry / palette
// (CHK-SETUP-14). Pure Go: no HWND. Keeps the phase-7 control-ID and
// page-order contracts stable while documenting the chrome sizes.
func TestCHK_SETUP_14_LayoutContract(t *testing.T) {
	if WizardClientW != 640 || WizardClientH != 420 {
		t.Fatalf("client size %dx%d want 640x420", WizardClientW, WizardClientH)
	}
	if WizardBrandW < 200 || WizardBrandW > 220 {
		t.Fatalf("brand width %d want 200–220", WizardBrandW)
	}
	if WizardPad != 24 || WizardGrid != 8 {
		t.Fatalf("pad/grid %d/%d want 24/8", WizardPad, WizardGrid)
	}
	if WizardBtnW != 110 || WizardBtnH != 32 {
		t.Fatalf("button %dx%d want 110x32", WizardBtnW, WizardBtnH)
	}
	if WizardBtnBarH < 48 {
		t.Fatalf("button bar height %d too small", WizardBtnBarH)
	}
	if ContentLeft() != WizardBrandW+WizardPad {
		t.Fatalf("content left %d", ContentLeft())
	}
	if ContentWidth() <= 0 || ContentWidth() > WizardClientW-WizardBrandW {
		t.Fatalf("content width %d", ContentWidth())
	}
	if ButtonBarTop() >= WizardClientH || ButtonBarTop() <= ContentTop() {
		t.Fatalf("button bar top %d", ButtonBarTop())
	}
	// Palette: brand dark ≠ content bg ≠ COLOR_BTNFACE classic feel
	if ColorBrandDark == ColorContentBg || ColorBrandDark == 0 {
		t.Fatal("brand/content colors collapsed")
	}
	if ColorContentBg == 0x00F0F0F0 { // classic COLOR_BTNFACE-ish
		t.Fatal("content bg must not be COLOR_BTNFACE gray")
	}
	if ColorBtnBarLine == 0 || ColorAccent == 0 {
		t.Fatal("line/accent colors missing")
	}
	// Control IDs (E2E / SendMessage contract)
	if IDBack != 101 || IDNext != 102 || IDCancel != 103 || IDBrowse != 104 ||
		IDDir != 105 || IDShortcut != 106 || IDService != 107 || IDTray != 108 ||
		IDText != 109 || IDProgress != 110 || IDStatus != 111 || IDError != 112 ||
		IDPurge != 113 || IDUninstall != 114 {
		t.Fatal("control IDs changed — breaks CHK-SETUP-10/12 E2E")
	}
	// Page → step index (brand panel highlight)
	for _, tc := range []struct {
		p Page
		i int
	}{
		{PageWelcome, 0}, {PageFolder, 1}, {PageOptions, 2},
		{PageProgress, 3}, {PageFinish, 4},
	} {
		if StepIndex(tc.p) != tc.i {
			t.Fatalf("StepIndex(%d)=%d want %d", tc.p, StepIndex(tc.p), tc.i)
		}
	}
	if len(StepKeys) != 5 {
		t.Fatalf("StepKeys len %d", len(StepKeys))
	}
	ru, en := CatalogKeys("ru"), CatalogKeys("en")
	for _, k := range append([]string{"setup.brand.tagline"}, StepKeys...) {
		if ru[k] == "" || en[k] == "" {
			t.Errorf("missing catalog key %s", k)
		}
	}
}
