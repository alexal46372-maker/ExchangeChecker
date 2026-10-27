package trayui

import (
	"strings"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
)

func muteStatus(muted bool) trayipc.Status {
	now := time.Now()
	ps := model.ProfileSnapshot{ProfileID: "p1", Name: "Склад", Enabled: true, Polled: true, Level: model.LevelWarning,
		Muted: muted, Points: []model.PointStatus{{ExchangePoint: model.ExchangePoint{Name: "ЦБ", Level: model.LevelWarning}, FreshAt: &now, LagMinutes: -1}}}
	if muted {
		ps.MutedSince = &now
	}
	return trayipc.Status{State: trayipc.StateConnected, Version: 2, Capabilities: []string{model.CapMuteProfile},
		Snapshot: &model.StatusSnapshot{Profiles: []model.ProfileSnapshot{ps}}}
}

// TestCHK_TRAY_59_MuteMenu — left-menu mute/unmute items by capability and state.
func TestCHK_TRAY_59_MuteMenu(t *testing.T) {
	for _, lang := range []string{i18n.RU, i18n.EN} {
		items := BuildLeftMenu(muteStatus(false), lang)
		if len(items) < 1 || items[0].Sub == nil {
			t.Fatalf("%s: %#v", lang, items)
		}
		sub := items[0].Sub
		if sub[0].ID != MuteCmd(0) || sub[0].Text != i18n.T(i18n.KeyMenuMute, lang) {
			t.Fatalf("%s mute item %#v", lang, sub[0])
		}
		items = BuildLeftMenu(muteStatus(true), lang)
		sub = items[0].Sub
		if sub[0].ID != UnmuteCmd(0) || sub[0].Text != i18n.T(i18n.KeyMenuUnmute, lang) {
			t.Fatalf("%s unmute %#v", lang, sub[0])
		}
		if !strings.Contains(items[0].Text, i18n.T(i18n.KeyDBMuted, lang)) {
			t.Fatalf("%s muted indicator missing in %q", lang, items[0].Text)
		}
		// no capability → no mute items
		st := muteStatus(false)
		st.Capabilities = nil
		items = BuildLeftMenu(st, lang)
		for _, it := range items[0].Sub {
			if it.ID == MuteCmd(0) || it.ID == UnmuteCmd(0) {
				t.Fatalf("%s mute without cap", lang)
			}
		}
	}
}

// TestCHK_TRAY_59_MuteCmdIndex — encode/decode profile slot.
func TestCHK_TRAY_59_MuteCmdIndex(t *testing.T) {
	if MuteCmd(3) != CmdMuteBase+3 || UnmuteCmd(3) != CmdUnmuteBase+3 {
		t.Fatal(MuteCmd(3), UnmuteCmd(3))
	}
	i, mute, ok := MuteProfileIndex(MuteCmd(7))
	if !ok || !mute || i != 7 {
		t.Fatal(i, mute, ok)
	}
	i, mute, ok = MuteProfileIndex(UnmuteCmd(2))
	if !ok || mute || i != 2 {
		t.Fatal(i, mute, ok)
	}
	if _, _, ok := MuteProfileIndex(CmdSettings); ok {
		t.Fatal("settings")
	}
}

// TestCHK_TRAY_59_MutedCard — status/card show muted.
func TestCHK_TRAY_59_MutedCard(t *testing.T) {
	st := muteStatus(true)
	rows := StatusRows(st, i18n.RU, time.Now())
	found := false
	for _, r := range rows {
		if strings.Contains(r.Note, i18n.T(i18n.KeyCardMuted, i18n.RU)) {
			found = true
		}
	}
	if !found {
		t.Fatalf("rows %#v", rows)
	}
	cards := Cards(rows, i18n.RU)
	if len(cards) == 0 || !cards[0].Muted || !strings.Contains(cards[0].State, i18n.T(i18n.KeyCardMuted, i18n.RU)) {
		t.Fatalf("card %#v", cards)
	}
}
