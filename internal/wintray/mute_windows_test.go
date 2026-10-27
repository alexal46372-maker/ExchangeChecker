//go:build windows

package wintray

import (
	"context"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_TRAY_59_MuteIPC — CapMuteProfile + mute/unmute over the session.
func TestCHK_TRAY_59_MuteIPC(t *testing.T) {
	svc := newCfgService()
	app := startCfgApp(t, svc, true, nil)
	defer app.Exit()
	if !app.session.Status().Has(model.CapMuteProfile) {
		t.Fatalf("caps %v", app.session.Status().Capabilities)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m, err := app.session.Do(ctx, model.IPCRequest{Cmd: model.CmdMuteUntilRecovered, ProfileID: "p1"})
	if err != nil || m.Type != model.MsgOK || m.Result != model.ResultMuted {
		t.Fatalf("mute %+v %v", m, err)
	}
	if !svc.muted["p1"] {
		t.Fatal("svc not muted")
	}
	m, err = app.session.Do(ctx, model.IPCRequest{Cmd: model.CmdUnmuteProfile, ProfileID: "p1"})
	if err != nil || m.Type != model.MsgOK || m.Result != model.ResultUnmuted {
		t.Fatalf("unmute %+v %v", m, err)
	}
	for _, lang := range []string{i18n.RU, i18n.EN} {
		if i18n.T(i18n.KeyMenuMute, lang) == "" || i18n.T(i18n.KeyMenuUnmute, lang) == "" {
			t.Errorf("strings %s", lang)
		}
	}
}
