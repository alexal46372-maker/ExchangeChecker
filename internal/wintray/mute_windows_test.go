//go:build windows

package wintray

import (
	"context"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
)

// TestCHK_TRAY_59_MuteIPC — mute_until_recovered / unmute via App.Command;
// capability mute_profile from Muter.
func TestCHK_TRAY_59_MuteIPC(t *testing.T) {
	svc := newCfgService()
	app := startCfgApp(t, svc, true, nil)
	defer app.Exit()
	if !app.session.Supported(model.CmdMuteUntilRecovered) {
		t.Fatalf("missing mute_profile cap: %v", app.session.Status().Capabilities)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.session.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	st := app.session.Status()
	if st.Snapshot == nil || len(st.Snapshot.Profiles) == 0 {
		t.Fatal("no profiles in status")
	}
	// seed App.status so muteProfile index resolves (Refresh publishes async)
	pumpUntil(t, "app status", func() bool {
		return app.Status().Snapshot != nil && len(app.Status().Snapshot.Profiles) > 0
	})

	app.Command(trayui.MuteCmd(0))
	pumpUntil(t, "muted", func() bool { return svc.muted != nil && svc.muted["p1"] })
	app.Command(trayui.UnmuteCmd(0))
	pumpUntil(t, "unmuted", func() bool { return svc.muted == nil || !svc.muted["p1"] })

	for _, lang := range []string{i18n.RU, i18n.EN} {
		if i18n.T(i18n.KeyMenuMute, lang) == i18n.KeyMenuMute {
			t.Errorf("missing mute %s", lang)
		}
	}
}
