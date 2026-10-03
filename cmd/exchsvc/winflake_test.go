package main

import (
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/ipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/monitor"
)

// TestCHK_IPC_06_ConfigLoadedBeforeServe — the first configuration load
// happens before the pipe serves clients (windows CI 37123714167: a
// get_status right after start returned a snapshot without settings /
// configRevision): when the service is ready, the snapshot already has
// the settings with beep.enabled and the revision. Deterministic: Ready
// runs before Monitor.Run, so without LoadConfig it always fails.
func TestCHK_IPC_06_ConfigLoadedBeforeServe(t *testing.T) {
	var snap model.StatusSnapshot
	s := startSvcWith(t, t.TempDir(), 40, nil, &fakeEvents{}, func(ac *appConfig) {
		ready := ac.Ready
		ac.Ready = func(m *monitor.Monitor, srv *ipc.Server) {
			snap = m.Snapshot()
			ready(m, srv)
		}
	})
	defer s.stop()
	if snap.Settings == nil || snap.Settings.Beep.Enabled != model.OptTrue || snap.ConfigRevision == "" {
		t.Fatalf("snapshot at ready: settings %+v revision %q", snap.Settings, snap.ConfigRevision)
	}
}
