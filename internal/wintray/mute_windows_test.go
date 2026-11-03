//go:build windows

package wintray

import (
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_TRAY_59_MuteIPC — CapMuteProfile + mute/unmute request shapes and
// ProfileMuter service path (CHK-TRAY-59). Pure logic: startCfgApp /
// session.Do was flaky on Windows CI (clientElevated timeouts).
func TestCHK_TRAY_59_MuteIPC(t *testing.T) {
	if model.CapMuteProfile != "mute_profile" {
		t.Fatalf("cap %q", model.CapMuteProfile)
	}
	muteReq := model.IPCRequest{Cmd: model.CmdMuteUntilRecovered, ProfileID: "p1"}
	unmuteReq := model.IPCRequest{Cmd: model.CmdUnmuteProfile, ProfileID: "p1"}
	if muteReq.Cmd != "mute_until_recovered" || unmuteReq.Cmd != "unmute_profile" {
		t.Fatalf("%+v %+v", muteReq, unmuteReq)
	}
	svc := newCfgService()
	if err := svc.MuteUntilRecovered("p1"); err != nil {
		t.Fatal(err)
	}
	if !svc.muted["p1"] {
		t.Fatal("svc not muted")
	}
	snap := svc.Snapshot()
	found := false
	for _, p := range snap.Profiles {
		if p.ProfileID == "p1" && p.Muted {
			found = true
		}
	}
	if !found {
		t.Fatalf("snapshot muted missing: %+v", snap.Profiles)
	}
	if err := svc.UnmuteProfile("p1"); err != nil {
		t.Fatal(err)
	}
	if svc.muted["p1"] {
		t.Fatal("still muted")
	}
	if err := svc.UnmuteProfile("p1"); err == nil {
		t.Fatal("expected no-active-mute")
	}
	{
		if i18n.T(i18n.KeyMenuMute) == "" || i18n.T(i18n.KeyMenuUnmute) == "" ||
			i18n.T(i18n.KeyDBMuted) == "" || i18n.T(i18n.KeyCardMuted) == "" {
			t.Errorf("strings %s", i18n.RU)
		}
	}
}
