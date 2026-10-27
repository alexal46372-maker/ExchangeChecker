//go:build windows

package wininstall

import (
	"github.com/alexal46372-maker/ExchangeChecker/internal/version"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// setupChrome owns GDI objects for the modern wizard / uninstall chrome
// (CHK-SETUP-14). Created once per top-level window; destroyed on WM_DESTROY.
type setupChrome struct {
	brandBrush   uintptr
	contentBrush uintptr
	barBrush     uintptr
	lineBrush    uintptr
	fontTitle    uintptr
	fontBody     uintptr
	fontBrand    uintptr
	fontStep     uintptr
	fontStepOn   uintptr
	icon         uintptr
}

func newSetupChrome() *setupChrome {
	c := &setupChrome{
		brandBrush:   winapi.CreateSolidBrush(ColorBrandDark),
		contentBrush: winapi.CreateSolidBrush(ColorContentBg),
		barBrush:     winapi.CreateSolidBrush(ColorBtnBarBg),
		lineBrush:    winapi.CreateSolidBrush(ColorBtnBarLine),
		icon:         winapi.LoadAppIcon(1),
	}
	// Segoe UI with Tahoma fallback (CreateFont returns 0 if face missing —
	// try the second face). Negative height = character cell.
	c.fontTitle = fontOr("Segoe UI", "Tahoma", -18, winapi.FW_SEMIBOLD)
	c.fontBody = fontOr("Segoe UI", "Tahoma", -13, winapi.FW_NORMAL)
	c.fontBrand = fontOr("Segoe UI", "Tahoma", -16, winapi.FW_BOLD)
	c.fontStep = fontOr("Segoe UI", "Tahoma", -12, winapi.FW_NORMAL)
	c.fontStepOn = fontOr("Segoe UI", "Tahoma", -12, winapi.FW_SEMIBOLD)
	return c
}

func fontOr(face, fallback string, height, weight int32) uintptr {
	if h := winapi.CreateFontWeight(face, height, weight); h != 0 {
		return h
	}
	return winapi.CreateFontWeight(fallback, height, weight)
}

func (c *setupChrome) destroy() {
	if c == nil {
		return
	}
	for _, h := range []uintptr{c.brandBrush, c.contentBrush, c.barBrush, c.lineBrush,
		c.fontTitle, c.fontBody, c.fontBrand, c.fontStep, c.fontStepOn} {
		if h != 0 {
			winapi.DeleteObject(h)
		}
	}
	*c = setupChrome{}
}

func (c *setupChrome) applyFont(hwnd uintptr, title bool) {
	f := c.fontBody
	if title {
		f = c.fontTitle
	}
	if f != 0 {
		winapi.SendMessage(hwnd, winapi.WM_SETFONT, f, 1)
	}
}

// paintWizard draws the brand panel, content background and button bar.
func (c *setupChrome) paintWizard(hwnd uintptr, lang string, page Page) {
	var ps winapi.PAINTSTRUCT
	dc := winapi.BeginPaint(hwnd, &ps)
	if dc == 0 {
		return
	}
	defer winapi.EndPaint(hwnd, &ps)
	rc := winapi.GetClientRect(hwnd)
	// content background
	winapi.FillRect(dc, rc, c.contentBrush)
	// brand panel
	brand := winapi.RECT{Left: 0, Top: 0, Right: WizardBrandW, Bottom: rc.Bottom}
	winapi.FillRect(dc, brand, c.brandBrush)
	// accent strip on the right edge of the brand panel
	strip := winapi.RECT{Left: WizardBrandW - 3, Top: 0, Right: WizardBrandW, Bottom: rc.Bottom}
	mid := winapi.CreateSolidBrush(ColorBrandMid)
	winapi.FillRect(dc, strip, mid)
	winapi.DeleteObject(mid)
	// button bar
	barTop := ButtonBarTop()
	line := winapi.RECT{Left: WizardBrandW, Top: barTop, Right: rc.Right, Bottom: barTop + WizardLineH}
	winapi.FillRect(dc, line, c.lineBrush)
	bar := winapi.RECT{Left: WizardBrandW, Top: barTop + WizardLineH, Right: rc.Right, Bottom: rc.Bottom}
	winapi.FillRect(dc, bar, c.barBrush)

	winapi.SetBkMode(dc, winapi.TRANSPARENT)
	y := int32(WizardPad)
	if c.icon != 0 {
		winapi.DrawIconEx(dc, WizardPad, y, c.icon, WizardBrandIcon, WizardBrandIcon)
		y += WizardBrandIcon + WizardGrid
	}
	winapi.SetTextColor(dc, ColorBrandText)
	if c.fontBrand != 0 {
		old := winapi.SelectObject(dc, c.fontBrand)
		winapi.TextOut(dc, WizardPad, y, version.ProductName)
		winapi.SelectObject(dc, old)
		y += 22
	}
	winapi.SetTextColor(dc, ColorBrandMuted)
	if c.fontStep != 0 {
		old := winapi.SelectObject(dc, c.fontStep)
		tag := T(lang, "setup.brand.tagline")
		tr := winapi.RECT{Left: WizardPad, Top: y, Right: WizardBrandW - WizardPad, Bottom: y + 40}
		winapi.DrawText(dc, tag, tr, winapi.DT_WORDBREAK|winapi.DT_NOPREFIX)
		winapi.SelectObject(dc, old)
		y += 48
	}
	cur := StepIndex(page)
	for i, key := range StepKeys {
		label := T(lang, key)
		on := i == cur
		font := c.fontStep
		col := ColorBrandMuted
		if on {
			font = c.fontStepOn
			col = ColorStepActive
		}
		if font != 0 {
			old := winapi.SelectObject(dc, font)
			winapi.SetTextColor(dc, col)
			prefix := "  "
			if on {
				prefix = "▸ "
			}
			winapi.TextOut(dc, WizardPad, y, prefix+label)
			winapi.SelectObject(dc, old)
		}
		y += 22
	}
}

func (c *setupChrome) paintUninstall(hwnd uintptr) {
	var ps winapi.PAINTSTRUCT
	dc := winapi.BeginPaint(hwnd, &ps)
	if dc == 0 {
		return
	}
	defer winapi.EndPaint(hwnd, &ps)
	rc := winapi.GetClientRect(hwnd)
	winapi.FillRect(dc, rc, c.contentBrush)
	top := winapi.RECT{Left: 0, Top: 0, Right: rc.Right, Bottom: 8}
	winapi.FillRect(dc, top, c.brandBrush)
	barTop := rc.Bottom - WizardBtnBarH
	line := winapi.RECT{Left: 0, Top: barTop, Right: rc.Right, Bottom: barTop + WizardLineH}
	winapi.FillRect(dc, line, c.lineBrush)
	bar := winapi.RECT{Left: 0, Top: barTop + WizardLineH, Right: rc.Right, Bottom: rc.Bottom}
	winapi.FillRect(dc, bar, c.barBrush)
}

func (c *setupChrome) ctlColor(dc uintptr, content bool) uintptr {
	winapi.SetBkMode(dc, winapi.TRANSPARENT)
	if content {
		winapi.SetTextColor(dc, ColorTextPrimary)
		winapi.SetBkColor(dc, ColorContentBg)
		return c.contentBrush
	}
	winapi.SetTextColor(dc, ColorTextPrimary)
	winapi.SetBkColor(dc, ColorBtnBarBg)
	return c.barBrush
}

const pbmSetBarColor = winapi.WM_USER + 9
