package main

import (
	"context"
	"io"
	"sync/atomic"
	"testing"
	"time"

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
