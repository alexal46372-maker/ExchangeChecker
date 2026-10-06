package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

func hasCap(m model.IPCMessage, c string) bool {
	for _, x := range m.Capabilities {
		if x == c {
			return true
		}
	}
	return false
}

func isHello(m model.IPCMessage) bool { return m.Type == model.MsgHello }

// rawStatus returns the get_status reply and its JSON.
func rawStatus(ctx context.Context, t *testing.T, s *svc) (model.IPCMessage, string) {
	t.Helper()
	m, err := s.c.Call(ctx, model.CmdGetStatus)
	if err != nil || m.Type != model.MsgStatus {
		t.Fatalf("get_status: %+v %v", m, err)
	}
	b, _ := json.Marshal(m)
	return m, string(b)
}

// TestCHK_CFG_04_TrayAutostartErrorField — get_status carries
// trayAutostartError (capability tray_autostart_status in hello): the code
// of the last failed application of trayAutostart to HKLM Run (at start or
// after save_settings); omitted after a successful one; no capability and
// no field when the service does not manage the Run value.
func TestCHK_CFG_04_TrayAutostartErrorField(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// start: access denied
	dir := t.TempDir()
	reg := &fakeRun{vals: map[string]string{}, setErr: fmt.Errorf("RegSetValueExW: %w", fs.ErrPermission)}
	s := startSvcWith(t, dir, 40, nil, &fakeEvents{}, adminSvc(reg, testTrayCmd))
	if h := s.waitPush(t, isHello); !hasCap(h, model.CapTrayAutostartStatus) {
		t.Errorf("hello capabilities %v", h.Capabilities)
	}
	m, raw := rawStatus(ctx, t, s)
	if m.TrayAutostartError != model.AutostartErrAccessDenied || !strings.Contains(raw, `"trayAutostartError":"access_denied"`) {
		t.Fatalf("start, access denied: %q %s", m.TrayAutostartError, raw)
	}
	// save_settings succeeds in the registry -> field omitted
	reg.mu.Lock()
	reg.setErr = nil
	reg.mu.Unlock()
	cfg, err := s.c.GetConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.c.SaveSettings(ctx, model.DefaultSettings(), cfg.Revision)
	if err != nil || r.Type != model.MsgOK {
		t.Fatalf("save_settings: %+v %v", r, err)
	}
	if m, raw = rawStatus(ctx, t, s); m.TrayAutostartError != "" || strings.Contains(raw, "trayAutostartError") {
		t.Fatalf("after a good save: %q %s", m.TrayAutostartError, raw)
	}
	// save_settings with another registry error -> registry_error
	reg.mu.Lock()
	reg.delErr = errors.New("RegDeleteValueW: The system cannot find the path specified.")
	reg.mu.Unlock()
	off := model.DefaultSettings()
	off.TrayAutostart = false
	if r2, err := s.c.SaveSettings(ctx, off, r.Revision); err != nil || r2.Type != model.MsgOK {
		t.Fatalf("save_settings with a registry error: %+v %v", r2, err)
	}
	if m, _ = rawStatus(ctx, t, s); m.TrayAutostartError != model.AutostartErrRegistry {
		t.Fatalf("after a failed save: %q", m.TrayAutostartError)
	}
	// ping does not carry it (get_status only)
	if p, err := s.c.Call(ctx, model.CmdPing); err != nil || p.TrayAutostartError != "" {
		t.Errorf("ping: %+v %v", p, err)
	}
	s.stop()

	// start: settings.json unreadable -> settings_unreadable
	dir2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir2, config.SettingsFile), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	s2 := startSvcWith(t, dir2, 40, nil, &fakeEvents{}, adminSvc(&fakeRun{vals: map[string]string{}}, testTrayCmd))
	if m, _ := rawStatus(ctx, t, s2); m.TrayAutostartError != model.AutostartErrSettings {
		t.Errorf("unreadable settings: %q", m.TrayAutostartError)
	}
	s2.stop()

	// start ok -> no field
	s3 := startSvcWith(t, t.TempDir(), 40, nil, &fakeEvents{}, adminSvc(&fakeRun{vals: map[string]string{}}, testTrayCmd))
	if m, raw := rawStatus(ctx, t, s3); m.TrayAutostartError != "" || strings.Contains(raw, "trayAutostartError") {
		t.Errorf("good start: %s", raw)
	}
	s3.stop()

	// not managed (console mode / no registry): no capability, no field
	s4 := startSvcWith(t, t.TempDir(), 40, nil, &fakeEvents{}, adminSvc(nil, ""))
	if h := s4.waitPush(t, isHello); hasCap(h, model.CapTrayAutostartStatus) {
		t.Errorf("capability without a managed Run value: %v", h.Capabilities)
	}
	if m, raw := rawStatus(ctx, t, s4); m.TrayAutostartError != "" || strings.Contains(raw, "trayAutostartError") {
		t.Errorf("not managed: %s", raw)
	}
	s4.stop()
}
