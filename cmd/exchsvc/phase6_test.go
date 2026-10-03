package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/ipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_IPC_06_ServiceConfigWrite — end to end through the service: an
// administrator saves a new profile over IPC; the reply comes after the
// synchronous reload (poll_now works at once, the scheduled poll runs and
// the profile shows up polled in the status), event 105 is written with
// the account; a non-admin client is refused and nothing changes.
func TestCHK_IPC_06_ServiceConfigWrite(t *testing.T) {
	dir := t.TempDir()
	ev := &fakeEvents{}
	var admin int32 = 1
	s := startSvcWith(t, dir, 40, nil, ev, func(ac *appConfig) {
		ac.Authorize = func(io.ReadWriteCloser) (ipc.Peer, error) {
			return ipc.Peer{Admin: atomic.LoadInt32(&admin) == 1, Account: `TEST\admin`}, nil
		}
	})
	defer s.stop()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	m, err := s.c.Call(ctx, model.CmdGetStatus)
	if err != nil || m.ClientElevated == nil || !*m.ClientElevated || len(m.Status.Profiles) != 0 {
		t.Fatalf("get_status: %+v %v", m, err)
	}
	cfg, err := s.c.GetConfig(ctx)
	if err != nil || cfg.Type != model.MsgConfig || cfg.Revision == "" {
		t.Fatalf("get_config: %+v %v", cfg, err)
	}
	p := model.NewProfile("db2")
	p.Name, p.Server, p.Database, p.AuthMode = "Офис", "srv", "db", model.AuthWindows
	p.Mapping = model.Mapping{Table: "_Node1", PointNameColumn: "_Description", LastSendColumn: "s"}
	r, err := s.c.SaveProfile(ctx, p, "", false, cfg.Revision)
	if err != nil || r.Type != model.MsgOK || r.Result != model.ResultSaved || !r.Reloaded || !r.PollScheduled || r.Revision == cfg.Revision {
		t.Fatalf("save_profile: %+v %v", r, err)
	}
	if pr, err := s.c.Do(ctx, model.IPCRequest{Cmd: model.CmdPollNow, ProfileID: "db2"}); err != nil || pr.Type != model.MsgOK {
		t.Fatalf("poll_now right after save_profile: %+v %v", pr, err)
	}
	s.waitPush(t, func(m model.IPCMessage) bool {
		return m.Type == model.MsgStatus && len(m.Status.Profiles) == 1 && m.Status.Profiles[0].Polled &&
			m.Status.ConfigRevision == r.Revision
	})
	if !ev.has("save_profile") || !ev.has(`TEST\admin`) || !ev.has("db2") {
		t.Errorf("event 105 missing: %v", ev.got)
	}
	// stale revision -> conflict
	if c, _ := s.c.SaveSettings(ctx, model.DefaultSettings(), cfg.Revision); c.Code != model.ErrCodeConflict {
		t.Errorf("stale revision: %+v", c)
	}
	atomic.StoreInt32(&admin, 0)
	for _, call := range []func() (model.IPCMessage, error){
		func() (model.IPCMessage, error) { return s.c.GetConfig(ctx) },
		func() (model.IPCMessage, error) { return s.c.DeleteProfile(ctx, "db2", "") },
		func() (model.IPCMessage, error) { return s.c.SaveSettings(ctx, model.DefaultSettings(), "") },
	} {
		if d, err := call(); err != nil || d.Code != model.ErrCodeAccessDenied {
			t.Errorf("non-admin: %+v %v", d, err)
		}
	}
	if st, _ := s.c.Call(ctx, model.CmdGetStatus); len(st.Status.Profiles) != 1 {
		t.Error("non-admin changed the configuration")
	}
}

// TestCHK_IPC_06_ServiceRevisionRequired — over IPC, save_profile,
// delete_profile and save_settings without expectedRevision get
// invalid_request + monitor.bad_request with errorArgs
// ["expectedRevision", "validation.required"]; nothing is written; the
// same request with the revision of get_config succeeds.
func TestCHK_IPC_06_ServiceRevisionRequired(t *testing.T) {
	dir := t.TempDir()
	s := startSvcWith(t, dir, 40, nil, &fakeEvents{}, func(ac *appConfig) {
		ac.Authorize = func(io.ReadWriteCloser) (ipc.Peer, error) { return ipc.Peer{Admin: true, Account: `TEST\admin`}, nil }
	})
	defer s.stop()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p := model.NewProfile("db1")
	p.Name, p.Server, p.Database, p.AuthMode = "Офис", "srv", "db", model.AuthWindows
	p.Mapping = model.Mapping{Table: "_Node1", PointNameColumn: "_Description", LastSendColumn: "s"}
	for what, call := range map[string]func() (model.IPCMessage, error){
		"save_profile":   func() (model.IPCMessage, error) { return s.c.SaveProfile(ctx, p, "", false, "") },
		"delete_profile": func() (model.IPCMessage, error) { return s.c.DeleteProfile(ctx, "db1", "") },
		"save_settings":  func() (model.IPCMessage, error) { return s.c.SaveSettings(ctx, model.DefaultSettings(), "") },
	} {
		m, err := call()
		if err != nil || m.Type != model.MsgError || m.Code != model.ErrCodeInvalidRequest || m.ErrorKey != model.MsgMonitorBadRequest ||
			len(m.ErrorArgs) != 2 || m.ErrorArgs[0] != "expectedRevision" || m.ErrorArgs[1] != model.ValRequired {
			t.Errorf("%s without expectedRevision: %+v %v", what, m, err)
		}
	}
	for _, n := range []string{config.SettingsFile, config.ProfilesFile, config.SecretsFile} {
		if _, err := os.Stat(filepath.Join(dir, n)); !os.IsNotExist(err) {
			t.Errorf("%s written without expectedRevision", n)
		}
	}
	cfg, err := s.c.GetConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if r, err := s.c.SaveProfile(ctx, p, "", false, cfg.Revision); err != nil || r.Type != model.MsgOK {
		t.Fatalf("save_profile with revision: %+v %v", r, err)
	}
}

// TestCHK_IPC_06_ServiceBeepAndOtherProfileID — over IPC through the
// service: get_status.settings and get_config carry beep.enabled (true by
// default), save_settings with false is applied and returned; a
// save_profile / delete_profile refused because of ANOTHER invalid profile
// in profiles.json (edited by hand) carries "profileId" of that profile,
// errorArgs[0] its field.
func TestCHK_IPC_06_ServiceBeepAndOtherProfileID(t *testing.T) {
	dir := t.TempDir()
	s := startSvcWith(t, dir, 40, nil, &fakeEvents{}, func(ac *appConfig) {
		ac.Authorize = func(io.ReadWriteCloser) (ipc.Peer, error) { return ipc.Peer{Admin: true, Account: `TEST\admin`}, nil }
	})
	defer s.stop()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	st, err := s.c.Call(ctx, model.CmdGetStatus)
	if err != nil || st.Status.Settings == nil || st.Status.Settings.Beep.Enabled != model.OptTrue {
		t.Fatalf("get_status: %+v %v", st.Status, err)
	}
	if b, _ := json.Marshal(st.Status.Settings); !strings.Contains(string(b), `"enabled":true`) {
		t.Fatalf("get_status settings JSON: %s", b)
	}
	cfg, err := s.c.GetConfig(ctx)
	if err != nil || cfg.Config == nil || cfg.Config.Settings.Beep.Enabled != model.OptTrue {
		t.Fatalf("get_config: %+v %v", cfg, err)
	}
	off := model.DefaultSettings()
	off.Beep.Enabled = model.OptFalse
	if r, err := s.c.SaveSettings(ctx, off, cfg.Revision); err != nil || r.Type != model.MsgOK {
		t.Fatalf("save_settings false: %+v %v", r, err)
	}
	cfg, _ = s.c.GetConfig(ctx)
	if cfg.Config.Settings.Beep.Enabled != model.OptFalse {
		t.Fatalf("get_config after false: %v", cfg.Config.Settings.Beep.Enabled)
	}
	if b, _ := json.Marshal(cfg.Config.Settings); !strings.Contains(string(b), `"enabled":false`) {
		t.Fatalf("get_config settings JSON: %s", b)
	}
	// another invalid profile, written by hand
	good := model.NewProfile("a")
	good.Name, good.Server, good.Database, good.AuthMode = "A", "srv", "db", model.AuthWindows
	good.Mapping = model.Mapping{Table: "_Node1", PointNameColumn: "_Description", LastSendColumn: "s"}
	bad := good
	bad.ID, bad.Server = "bad", `host\`
	raw, _ := json.Marshal(map[string]interface{}{"version": config.SchemaVersion, "profiles": []model.Profile{good, bad}})
	if err := os.WriteFile(filepath.Join(dir, config.ProfilesFile), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _ = s.c.GetConfig(ctx)
	newer := good
	newer.ID = "c"
	for what, call := range map[string]func() (model.IPCMessage, error){
		"save_profile":   func() (model.IPCMessage, error) { return s.c.SaveProfile(ctx, newer, "", false, cfg.Revision) },
		"delete_profile": func() (model.IPCMessage, error) { return s.c.DeleteProfile(ctx, "a", cfg.Revision) },
	} {
		m, err := call()
		if err != nil || m.Type != model.MsgError || m.Code != model.ErrCodeInvalidRequest || m.ErrorKey != model.MsgMonitorProfileInvalid ||
			m.ProfileID != "bad" || len(m.ErrorArgs) < 2 || m.ErrorArgs[0] != "server" || m.Error != bad.Validate().Error() {
			t.Errorf("%s: %+v %v", what, m, err)
		}
		if b, _ := json.Marshal(m); !strings.Contains(string(b), `"profileId":"bad"`) {
			t.Errorf("%s JSON: %s", what, b)
		}
	}
}
