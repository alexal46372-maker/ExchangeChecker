package trayui

import (
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

func countRGB(px []uint32, c Color) int {
	r, g, b := RGB(c)
	n := 0
	for _, p := range px {
		if p>>24 == 0xFF && uint8(p>>16) == r && uint8(p>>8) == g && uint8(p) == b {
			n++
		}
	}
	return n
}

// TestCHK_TRAY_91_DatabaseCheckMarks — the left menu: every database item
// is a Check item (Checked = polling on) colored red (error) / yellow
// (warning) / green (OK), grey when off / pending / stale; ToggleCmd only
// with profile_enabled and clientElevated; no mute items and no
// «Шифрование отключено…» hint; the check bitmap is a bold mark.
func TestCHK_TRAY_91_DatabaseCheckMarks(t *testing.T) {
	snap := model.StatusSnapshot{Profiles: []model.ProfileSnapshot{
		{ProfileID: "e", Name: "E", Enabled: true, Polled: true, Level: model.LevelOK, ErrorKey: model.MsgMonitorConnError,
			Warnings: []string{model.MsgSQLWarnPlaintextLogin}, Muted: true},
		{ProfileID: "w", Name: "W", Enabled: true, Polled: true, Level: model.LevelWarning},
		{ProfileID: "o", Name: "O", Enabled: true, Polled: true, Level: model.LevelOK},
		{ProfileID: "d", Name: "D", Enabled: false, Polled: true, Level: model.LevelError},
		{ProfileID: "n", Name: "N", Enabled: true},
	}}
	yes := true
	st := connected(&snap)
	st.Capabilities = append(st.Capabilities, model.CapProfileEnabled, model.CapMuteProfile)
	st.ClientElevated = &yes
	items := databases(BuildLeftMenu(st))
	want := []struct {
		c  Color
		on bool
	}{{ColorRed, true}, {ColorYellow, true}, {ColorGreen, true}, {ColorGrey, false}, {ColorGrey, true}}
	for i, w := range want {
		it := items[i]
		if !it.Check || it.Color != w.c || it.Checked != w.on || it.ID != ToggleCmd(i) {
			t.Errorf("item %d %+v", i, it)
		}
		if j, ok := ToggleProfileIndex(it.ID); !ok || j != i {
			t.Errorf("ToggleProfileIndex(%d) = %d %v", it.ID, j, ok)
		}
		for _, s := range it.Sub {
			if strings.Contains(s.Text, "Шифрование отключено") || strings.Contains(s.Text, "Приглушить") || s.Text == "Снять" {
				t.Errorf("item %d: removed line %q", i, s.Text)
			}
		}
		if strings.Contains(it.Text, "приглушена") {
			t.Errorf("mute indicator %q", it.Text)
		}
	}
	st.ClientElevated = nil
	if it := databases(BuildLeftMenu(st))[0]; it.ID != 0 || !it.Check {
		t.Errorf("not elevated: %+v", it)
	}
	st.ClientElevated = &yes
	st.Capabilities = []string{}
	if CanToggle(st) {
		t.Error("CanToggle without profile_enabled")
	}
	if _, ok := ToggleProfileIndex(CmdToggleBase + 1000); ok {
		t.Error("slot overflow")
	}
	for _, size := range []int{16, 20, 24} {
		on, off := CheckPixels(ColorRed, true, size), CheckPixels(ColorRed, false, size)
		if n := countRGB(on, ColorRed); n < size*size/8 {
			t.Errorf("size %d: bold mark has %d red pixels", size, n)
		}
		if countRGB(off, ColorRed) != 0 || countRGB(off, ColorGrey) != 0 {
			t.Errorf("size %d: empty box has a mark", size)
		}
	}
	b := ToggleFailedBalloon("E", model.IPCMessage{Type: model.MsgError, ErrorKey: model.MsgMonitorConfigConflict}, nil)
	if !strings.HasPrefix(b.Text, "Не удалось переключить опрос базы: ") || b.Title != "E" {
		t.Errorf("balloon %+v", b)
	}
}
