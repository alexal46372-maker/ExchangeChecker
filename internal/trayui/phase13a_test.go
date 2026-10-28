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

		// CHK-TRAY-79: Language submenu sits directly under Theme.
		langAt := -1
		for i, it := range m {
			if len(it.Sub) == 3 && (it.Sub[0].ID == CmdLangAuto || it.Sub[1].ID == CmdLangRU) {
				langAt = i
				break
			}
		}
		if langAt < 0 || langAt != themeAt+1 {
			t.Fatalf("%s: Language must follow Theme (theme@%d lang@%d)", lang, themeAt, langAt)
		}
		if aboutAt != langAt+2 || !m[langAt+1].Separator {
			t.Fatalf("%s: Language, sep, About (lang@%d about@%d)", lang, langAt, aboutAt)
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

// TestCHK_TRAY_79_LanguageUnderTheme — bottom→top: Exit → sep → About → sep → Language → Theme → sep → rest.
func TestCHK_TRAY_79_LanguageUnderTheme(t *testing.T) {
	m := BuildTrayRightMenu(RightMenuOpts{Lang: i18n.RU, Theme: ThemeLight, Connected: true})
	if len(m) < 6 || m[len(m)-1].ID != CmdExit {
		t.Fatalf("Exit must be last: %+v", m)
	}
	themeAt, langAt, aboutAt := -1, -1, -1
	for i, it := range m {
		if len(it.Sub) == 2 && it.Sub[0].ID == CmdThemeLight {
			themeAt = i
		}
		if len(it.Sub) >= 2 && it.Sub[0].ID == CmdLangAuto {
			langAt = i
		}
		if it.ID == CmdAbout {
			aboutAt = i
		}
	}
	if !(themeAt >= 0 && langAt == themeAt+1 && aboutAt == langAt+2) {
		t.Fatalf("want Theme, Language, sep, About; got theme=%d lang=%d about=%d menu=%+v", themeAt, langAt, aboutAt, m)
	}
	// Language must not appear in the upper work block (before Theme separator).
	for i := 0; i < themeAt; i++ {
		it := m[i]
		if len(it.Sub) >= 2 && it.Sub[0].ID == CmdLangAuto {
			t.Fatalf("Language still in work block at %d", i)
		}
	}
}
