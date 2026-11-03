package trayui

import (
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
)

// TestCHK_TRAY_63_MenuOrder — Settings, work (no Sound), About, Exit
// with separators (CHK-TRAY-63; no «Тема» since CHK-TRAY-88).
func TestCHK_TRAY_63_MenuOrder(t *testing.T) {
	{
		m := BuildTrayRightMenu(RightMenuOpts{
			Svc:      SvcStateRunning,
			Balloons: true, Connected: true, SummaryAvailable: true,
		})
		if len(m) < 8 {
			t.Fatalf("%s: short menu %+v", i18n.RU, m)
		}
		if m[0].ID != CmdSettings {
			t.Fatalf("%s: first want Settings, got %+v", i18n.RU, m[0])
		}
		var sound, about, exit, seps int
		var aboutAt, exitAt = -1, -1
		for i, it := range m {
			if it.ID == CmdSound {
				sound++
			}
			if it.Separator {
				seps++
			}
			if it.Text == "Тема" || strings.Contains(it.Text, "Тем") && len(it.Sub) > 0 {
				t.Fatalf("%s: theme menu must be gone (CHK-TRAY-88): %+v", i18n.RU, it)
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
			t.Fatalf("%s: CmdSound must leave the tray menu (CHK-TRAY-62)", i18n.RU)
		}
		if about != 1 || exit != 1 {
			t.Fatalf("%s: about=%d exit=%d", i18n.RU, about, exit)
		}
		if seps < 2 {
			t.Fatalf("%s: want ≥2 separators, got %d", i18n.RU, seps)
		}
		// About immediately above Exit, separated
		if aboutAt < 0 || exitAt != aboutAt+2 || !m[aboutAt+1].Separator {
			t.Fatalf("%s: About@%d Exit@%d (want About, sep, Exit)", i18n.RU, aboutAt, exitAt)
		}
		// work items, sep, About (no Theme between)
		if aboutAt < 2 || !m[aboutAt-1].Separator || m[aboutAt-2].Separator || len(m[aboutAt-2].Sub) == 2 && m[aboutAt-2].Text != i18n.T(i18n.KeyMenuService) {
			t.Fatalf("%s: rest, sep, About (about@%d) %+v", i18n.RU, aboutAt, m)
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
			t.Fatalf("%s: volume=%v pin=%v summary=%v", i18n.RU, vol, pin, sum)
		}
	}
}

// TestCHK_TRAY_62_Phase10OmitsSound — Phase10Items no longer lists CmdSound.
func TestCHK_TRAY_62_Phase10OmitsSound(t *testing.T) {
	for _, it := range Phase10Items() {
		if it.ID == CmdSound {
			t.Fatal("Phase10Items must omit CmdSound")
		}
	}
	if len(Phase10Items()) != 2 {
		t.Fatalf("want Volume+PinHelp, got %+v", Phase10Items())
	}
}

// TestCHK_TRAY_79_LanguageUnderTheme — bottom→top: Exit → sep → About → sep → rest
// (phase 13: no Language submenu, CHK-TRAY-86; no Theme submenu, CHK-TRAY-88).
func TestCHK_TRAY_79_LanguageUnderTheme(t *testing.T) {
	m := BuildTrayRightMenu(RightMenuOpts{Connected: true})
	if len(m) < 5 || m[len(m)-1].ID != CmdExit {
		t.Fatalf("Exit must be last: %+v", m)
	}
	n := len(m)
	if !m[n-2].Separator || m[n-3].ID != CmdAbout || !m[n-4].Separator || m[n-5].Separator || len(m[n-5].Sub) == 2 && m[n-5].Sub[0].ID == 0 && m[n-5].Text != i18n.T(i18n.KeyMenuService) {
		t.Fatalf("want rest, sep, About, sep, Exit; got %+v", m)
	}
}
