package main

import (
	"context"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/ipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/sqlcheck"
)

// TestCHK_MON_22_FlashE2E — the real service with a fast poll: a point in
// error flashes on every failing check (both clients); ack_alert does not
// stop it; dismiss_flash does (unknown eventId → not_found); recovery
// pushes flash_stop and a new error flashes again with a new eventId;
// mute_until_recovered stops flashing with flash_stop.
func TestCHK_MON_22_FlashE2E(t *testing.T) {
	dir := t.TempDir()
	st := config.NewStore(dir)
	st.Protector = plainProtector{}
	p := model.NewProfile("db1")
	p.Name, p.Server, p.Database, p.AuthMode = "Склад", "srv", "db", model.AuthWindows
	p.Mapping = model.Mapping{Table: "_Node1", PointNameColumn: "_Description", LastSendColumn: "s"}
	if err := st.SaveProfiles([]model.Profile{p}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, model.DBZone)
	var lag int64 = 600
	var lis *ipc.MemListener
	s := startSvcWith(t, dir, 0, nil, &fakeEvents{}, func(ac *appConfig) {
		ac.Authorize = func(io.ReadWriteCloser) (ipc.Peer, error) { return ipc.Peer{Account: `TEST\user`}, nil } // not admin
		ac.PollInterval = 150 * time.Millisecond
		ac.NewReader = func(model.Profile, string, sqlcheck.Options) (sqlcheck.ReadCloser, error) {
			return &stubRC{list: func() []model.ExchangePoint {
				ts := now.Add(-time.Duration(atomic.LoadInt64(&lag)) * time.Minute)
				return []model.ExchangePoint{{Name: "Магазин 1", LastSend: &ts}}
			}, onClose: func() {}}, nil
		}
		lis, _ = ac.Listener.(*ipc.MemListener)
	})
	defer s.stop()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	hello := s.waitPush(t, func(m model.IPCMessage) bool { return m.Type == model.MsgHello })
	has := false
	for _, c := range hello.Capabilities {
		has = has || c == model.CapFlash
	}
	if !has {
		t.Fatalf("caps %v", hello.Capabilities)
	}
	rw, err := lis.Dial()
	if err != nil {
		t.Fatal(err)
	}
	p2 := make(chan model.IPCMessage, 1024)
	c2 := ipc.NewClient(rw, func(m model.IPCMessage) {
		select {
		case p2 <- m:
		default:
		}
	})
	defer c2.Close()

	next := func(ch chan model.IPCMessage, typ string, d time.Duration) (model.IPCMessage, bool) {
		to := time.After(d)
		for {
			select {
			case m := <-ch:
				if m.Type == typ {
					return m, true
				}
			case <-to:
				return model.IPCMessage{}, false
			}
		}
	}
	f, ok := next(s.pushes, model.MsgFlash, 8*time.Second)
	if !ok || f.ProfileID != p.ID || f.PointKey == "" || f.EventID == 0 || f.Level == nil || *f.Level != model.LevelError {
		t.Fatalf("first flash %+v %v", f, ok)
	}
	ev := f.EventID
	countFlashes := func(ch chan model.IPCMessage, d time.Duration) int {
		n := 0
		to := time.After(d)
		for {
			select {
			case m := <-ch:
				if m.Type == model.MsgFlash && m.EventID == ev {
					n++
				}
			case <-to:
				return n
			}
		}
	}
	if n := countFlashes(s.pushes, time.Second); n < 3 {
		t.Fatalf("flash not repeated per failing check: %d", n)
	}
	if _, ok := next(p2, model.MsgFlash, 3*time.Second); !ok {
		t.Fatal("second client got no flash")
	}
	// ack does not stop flashing
	if m, err := s.c.Do(ctx, model.IPCRequest{Cmd: model.CmdAckAlert, EventID: ev}); err != nil || m.Type != model.MsgOK {
		t.Fatalf("ack %+v %v", m, err)
	}
	if n := countFlashes(s.pushes, time.Second); n < 3 {
		t.Fatalf("flash stopped by ack: %d", n)
	}
	// dismiss
	if m, _ := s.c.Do(ctx, model.IPCRequest{Cmd: model.CmdDismissFlash, EventID: 987654}); m.Code != model.ErrCodeNotFound || m.ErrorKey != model.MsgMonitorUnknownEvent {
		t.Fatalf("unknown event %+v", m)
	}
	if m, err := s.c.Do(ctx, model.IPCRequest{Cmd: model.CmdDismissFlash, EventID: ev}); err != nil || m.Type != model.MsgOK {
		t.Fatalf("dismiss %+v %v", m, err)
	}
	countFlashes(s.pushes, 200*time.Millisecond) // in-flight pushes
	if n := countFlashes(s.pushes, 1200*time.Millisecond); n != 0 {
		t.Fatalf("flash after dismiss: %d", n)
	}
	// recovery → flash_stop on both clients
	for len(p2) > 0 {
		<-p2
	}
	atomic.StoreInt64(&lag, 0)
	stop, ok := next(s.pushes, model.MsgFlashStop, 8*time.Second)
	if !ok || stop.ProfileID != p.ID || stop.PointKey != f.PointKey {
		t.Fatalf("flash_stop %+v %v", stop, ok)
	}
	if _, ok := next(p2, model.MsgFlashStop, 3*time.Second); !ok {
		t.Fatal("second client got no flash_stop")
	}
	// a new error flashes again (suppression reset)
	atomic.StoreInt64(&lag, 600)
	f2, ok := next(s.pushes, model.MsgFlash, 8*time.Second)
	if !ok || f2.EventID == ev || f2.PointKey != f.PointKey {
		t.Fatalf("flash after recovery %+v %v", f2, ok)
	}
	// mute → flash_stop, no more flashes
	if m, err := s.c.Do(ctx, model.IPCRequest{Cmd: model.CmdMuteUntilRecovered, ProfileID: p.ID}); err != nil || m.Type != model.MsgOK {
		t.Fatalf("mute %+v %v", m, err)
	}
	if _, ok := next(s.pushes, model.MsgFlashStop, 5*time.Second); !ok {
		t.Fatal("no flash_stop on mute")
	}
	ev = f2.EventID
	countFlashes(s.pushes, 200*time.Millisecond)
	if n := countFlashes(s.pushes, time.Second); n != 0 {
		t.Fatalf("flash while muted: %d", n)
	}
}
