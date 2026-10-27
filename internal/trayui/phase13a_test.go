package trayui

import (
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
)

// TestCHK_TRAY_63_MenuOrder — Settings, work (no Sound), Theme, About, Exit
// with separators (CHK-TRAY-63).
func TestCHK_TRAY_63_MenuOrder(t *testing.T) {
	for _, lang := range []string{i18n.RU, i18n.EN} {
		m := BuildTrayRightMenu(RightMenuOpts{
			Lang: lang, Pref: "", Svc: SvcStateRunning, Theme: ThemeDark,
			Balloons: true, Connected: true, SummaryAvailable: true,
		})
		if len(m) < 8 {
			t.Fatalf("%s: short menu %+v", lang, m)
		}
		if m[0].ID != CmdSettings {
			t.Fatalf("%s: first want Settings, got %+v", lang, m[0])
		}
		var sound, theme, about, exit, seps int
		var aboutAt, exitAt, themeAt = -1, -1, -1
		for i, it := range m {
			if it.ID == CmdSound {
				sound++
			}
			if it.Separator {
				seps++
			}
			if len(it.Sub) == 2 && (it.Sub[0].ID == CmdThemeLight || it.Sub[1].ID == CmdThemeDark) {
				theme++
				themeAt = i
				if !it.Sub[1].Checked || it.Sub[0].Checked {
					t.Fatalf("%s: dark should be checked: %+v", lang, it.Sub)
				}
			}
			if it.ID == CmdAbout {
				about++
				aboutAt = i
			}
			if it.ID == CmdExit {
				exit++
				exitAt = i
			}
		}
		if sound != 0 {
			t.Fatalf("%s: CmdSound must leave the tray menu (CHK-TRAY-62)", lang)
		}
		if theme != 1 || about != 1 || exit != 1 {
			t.Fatalf("%s: theme=%d about=%d exit=%d", lang, theme, about, exit)
		}
		if seps < 3 {
			t.Fatalf("%s: want ≥3 separators, got %d", lang, seps)
		}
		// About immediately above Exit, separated
		if aboutAt < 0 || exitAt != aboutAt+2 || !m[aboutAt+1].Separator {
			t.Fatalf("%s: About@%d Exit@%d (want About, sep, Exit)", lang, aboutAt, exitAt)
		}
		if themeAt < 0 || themeAt >= aboutAt {
			t.Fatalf("%s: Theme must be above About (theme@%d about@%d)", lang, themeAt, aboutAt)
		}
		// Volume / Pin stay; Summary present
		var vol, pin, sum bool
		for _, it := range m {
			switch it.ID {
			case CmdVolume:
				vol = true
			case CmdPinHelp:
				pin = true
			case CmdSummary:
				sum = true
			}
		}
		if !vol || !pin || !sum {
			t.Fatalf("%s: volume=%v pin=%v summary=%v", lang, vol, pin, sum)
		}
	}
}

// TestCHK_TRAY_62_Phase10OmitsSound — Phase10Items no longer lists CmdSound.
func TestCHK_TRAY_62_Phase10OmitsSound(t *testing.T) {
	for _, it := range Phase10Items(i18n.RU) {
		if it.ID == CmdSound {
			t.Fatal("Phase10Items must omit CmdSound")
		}
	}
	if len(Phase10Items(i18n.EN)) != 2 {
		t.Fatalf("want Volume+PinHelp, got %+v", Phase10Items(i18n.EN))
	}
}
