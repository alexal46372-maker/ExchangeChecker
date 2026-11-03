package main

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/ipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/sqlcheck"
)

type flashRig struct {
	t     *testing.T
	s     *svc
	p     model.Profile
	lag   int64 // minutes behind
	admin int32 // 1 = Authorize reports an administrator
	lis   *ipc.MemListener
	ctx   context.Context
}

func newFlashRig(t *testing.T, poll time.Duration, delayFlash time.Duration) *flashRig {
	dir := t.TempDir()
	st := config.NewStore(dir)
	st.Protector = plainProtector{}
	r := &flashRig{t: t, lag: 0}
	r.p = model.NewProfile("db1")
	r.p.Name, r.p.Server, r.p.Database, r.p.AuthMode = "Склад", "srv", "db", model.AuthWindows
	r.p.Mapping = model.Mapping{Table: "_Node1", PointNameColumn: "_Description", LastSendColumn: "s"}
	if err := st.SaveProfiles([]model.Profile{r.p}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, model.DBZone)
	r.s = startSvcWith(t, dir, 0, nil, &fakeEvents{}, func(ac *appConfig) {
		ac.Authorize = func(io.ReadWriteCloser) (ipc.Peer, error) {
			if atomic.LoadInt32(&r.admin) == 1 {
				return ipc.Peer{Admin: true, Account: `TEST\admin`}, nil
			}
			return ipc.Peer{Account: `TEST\user`}, nil
		}
		ac.PollInterval = poll
		ac.NewReader = func(model.Profile, string, sqlcheck.Options) (sqlcheck.ReadCloser, error) {
			return &stubRC{list: func() []model.ExchangePoint {
				ts := now.Add(-time.Duration(atomic.LoadInt64(&r.lag)) * time.Minute)
				return []model.ExchangePoint{{Name: "Магазин 1", LastSend: &ts}}
			}, onClose: func() {}}, nil
		}
		r.lis, _ = ac.Listener.(*ipc.MemListener)
		if delayFlash > 0 { // hold a computed flash so a newer mute publishes its stop first
			ac.BeforeFlashDelivery = func(fs []model.FlashEvent) {
				for _, f := range fs {
					if !f.Stop {
						time.Sleep(delayFlash)
						return
					}
				}
			}
		}
	})
	return r
}

func (r *flashRig) client() chan model.IPCMessage {
	rw, err := r.lis.Dial()
	if err != nil {
		r.t.Fatal(err)
	}
	ch := make(chan model.IPCMessage, 4096)
	c := ipc.NewClient(rw, func(m model.IPCMessage) {
		select {
		case ch <- m:
		default:
		}
	})
	r.t.Cleanup(func() { c.Close() })
	return ch
}

func nextPush(ch chan model.IPCMessage, typ string, d time.Duration) (model.IPCMessage, bool) {
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

// countFlash counts flash pushes (of eventID, 0 = any) during d.
func countFlash(ch chan model.IPCMessage, ev uint64, d time.Duration) int {
	n := 0
	to := time.After(d)
	for {
		select {
		case m := <-ch:
			if m.Type == model.MsgFlash && (ev == 0 || m.EventID == ev) {
				n++
			}
		case <-to:
			return n
		}
	}
}

func drain(ch chan model.IPCMessage) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

func (r *flashRig) do(req model.IPCRequest) model.IPCMessage {
	r.t.Helper()
	m, err := r.s.c.Do(r.ctx, req)
	if err != nil {
		r.t.Fatalf("%s: %v", req.Cmd, err)
	}
	return m
}

func (r *flashRig) ok(req model.IPCRequest) model.IPCMessage {
	r.t.Helper()
	m := r.do(req)
	if m.Type != model.MsgOK {
		r.t.Fatalf("%s: %+v", req.Cmd, m)
	}
	return m
}

// TestCHK_MON_22_FlashE2E — the real service with a fast poll (one
// non-admin client + an admin switch for config commands):
// warning flashes with level warning; a profile muted before its error
// alert never flashes; error flashes on every failing check (both
// clients); ack_alert does not stop it; dismiss_flash from a non-admin
// client does (unknown eventId → not_found); recovery → flash_stop and a
// new error flashes again; set_enabled=false, mute and delete_profile
// during flashing → flash_stop.
func TestCHK_MON_22_FlashE2E(t *testing.T) {
	r := newFlashRig(t, 150*time.Millisecond, 0)
	defer r.s.stop()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	r.ctx = ctx
	p := r.p
	hello := r.s.waitPush(t, func(m model.IPCMessage) bool { return m.Type == model.MsgHello })
	has := false
	for _, c := range hello.Capabilities {
		has = has || c == model.CapFlash
	}
	if !has {
		t.Fatalf("caps %v", hello.Capabilities)
	}
	p2 := r.client()
	pushes := r.s.pushes

	// warning (lag 45 min: between 30 and 60) → flash level warning
	atomic.StoreInt64(&r.lag, 45)
	fw, ok := nextPush(pushes, model.MsgFlash, 8*time.Second)
	if !ok || fw.ProfileID != p.ID || fw.PointKey == "" || fw.Level == nil || *fw.Level != model.LevelWarning {
		t.Fatalf("warning flash %+v %v", fw, ok)
	}
	// mute while warning (before the error alert) → flash_stop; the error
	// alert raised while muted never flashes
	r.ok(model.IPCRequest{Cmd: model.CmdMuteUntilRecovered, ProfileID: p.ID})
	if _, ok := nextPush(pushes, model.MsgFlashStop, 5*time.Second); !ok {
		t.Fatal("no flash_stop on mute")
	}
	atomic.StoreInt64(&r.lag, 600)
	if n := countFlash(pushes, 0, 1500*time.Millisecond); n != 0 {
		t.Fatalf("muted profile flashed %d times", n)
	}
	r.ok(model.IPCRequest{Cmd: model.CmdUnmuteProfile, ProfileID: p.ID})

	f, ok := nextPush(pushes, model.MsgFlash, 8*time.Second)
	if !ok || f.EventID == 0 || f.Level == nil || *f.Level != model.LevelError || f.PointKey != fw.PointKey {
		t.Fatalf("error flash %+v %v", f, ok)
	}
	ev := f.EventID
	if n := countFlash(pushes, ev, time.Second); n < 3 {
		t.Fatalf("flash not repeated per failing check: %d", n)
	}
	if _, ok := nextPush(p2, model.MsgFlash, 3*time.Second); !ok {
		t.Fatal("second client got no flash")
	}
	// ack does not stop flashing
	r.ok(model.IPCRequest{Cmd: model.CmdAckAlert, EventID: ev})
	if n := countFlash(pushes, ev, time.Second); n < 3 {
		t.Fatalf("flash stopped by ack: %d", n)
	}
	// dismiss from the non-admin client
	if m := r.do(model.IPCRequest{Cmd: model.CmdDismissFlash, EventID: 987654}); m.Code != model.ErrCodeNotFound || m.ErrorKey != model.MsgMonitorUnknownEvent {
		t.Fatalf("unknown event %+v", m)
	}
	if m := r.do(model.IPCRequest{Cmd: model.CmdDismissFlash}); m.Code != model.ErrCodeInvalidRequest {
		t.Fatalf("no eventId %+v", m)
	}
	r.ok(model.IPCRequest{Cmd: model.CmdDismissFlash, EventID: ev})
	countFlash(pushes, ev, 200*time.Millisecond)
	if n := countFlash(pushes, ev, 1200*time.Millisecond); n != 0 {
		t.Fatalf("flash after dismiss: %d", n)
	}
	// recovery → flash_stop on both clients
	drain(p2)
	atomic.StoreInt64(&r.lag, 0)
	stop, ok := nextPush(pushes, model.MsgFlashStop, 8*time.Second)
	if !ok || stop.ProfileID != p.ID || stop.PointKey != f.PointKey {
		t.Fatalf("flash_stop %+v %v", stop, ok)
	}
	if _, ok := nextPush(p2, model.MsgFlashStop, 3*time.Second); !ok {
		t.Fatal("second client got no flash_stop")
	}
	// a new error flashes again (suppression reset)
	atomic.StoreInt64(&r.lag, 600)
	f2, ok := nextPush(pushes, model.MsgFlash, 8*time.Second)
	if !ok || f2.EventID == ev || f2.PointKey != f.PointKey {
		t.Fatalf("flash after recovery %+v %v", f2, ok)
	}

	// set_enabled=false during flashing → flash_stop, then silence
	atomic.StoreInt32(&r.admin, 1)
	no, yes := false, true
	r.ok(model.IPCRequest{Cmd: model.CmdSetEnabled, ProfileID: p.ID, Enabled: &no})
	if _, ok := nextPush(pushes, model.MsgFlashStop, 5*time.Second); !ok {
		t.Fatal("no flash_stop on set_enabled=false")
	}
	if n := countFlash(pushes, 0, time.Second); n != 0 {
		t.Fatalf("disabled profile flashed %d times", n)
	}
	r.ok(model.IPCRequest{Cmd: model.CmdSetEnabled, ProfileID: p.ID, Enabled: &yes})
	if _, ok := nextPush(pushes, model.MsgFlash, 8*time.Second); !ok {
		t.Fatal("no flash after set_enabled=true")
	}
	// mute during flashing → flash_stop, silence
	atomic.StoreInt32(&r.admin, 0)
	r.ok(model.IPCRequest{Cmd: model.CmdMuteUntilRecovered, ProfileID: p.ID})
	if _, ok := nextPush(pushes, model.MsgFlashStop, 5*time.Second); !ok {
		t.Fatal("no flash_stop on mute")
	}
	countFlash(pushes, 0, 200*time.Millisecond)
	if n := countFlash(pushes, 0, time.Second); n != 0 {
		t.Fatalf("flash while muted: %d", n)
	}
	r.ok(model.IPCRequest{Cmd: model.CmdUnmuteProfile, ProfileID: p.ID})
	if _, ok := nextPush(pushes, model.MsgFlash, 8*time.Second); !ok {
		t.Fatal("no flash after unmute")
	}
	// delete_profile during flashing → flash_stop
	atomic.StoreInt32(&r.admin, 1)
	cfg := r.do(model.IPCRequest{Cmd: model.CmdGetConfig})
	r.ok(model.IPCRequest{Cmd: model.CmdDeleteProfile, ProfileID: p.ID, ExpectedRevision: cfg.Revision})
	if st, ok := nextPush(pushes, model.MsgFlashStop, 5*time.Second); !ok || st.ProfileID != p.ID {
		t.Fatalf("no flash_stop on delete_profile %+v", st)
	}
	if n := countFlash(pushes, 0, time.Second); n != 0 {
		t.Fatalf("deleted profile flashed %d times", n)
	}
}

// TestCHK_MON_22_FlashMuteRace — mute / unmute race a fast poll (5 ms;
// run with -race in CI): after every mute the LAST flash-type push the
// client sees for the point is flash_stop (no stale flash delivered after
// the stop of a newer publish).
func TestCHK_MON_22_FlashMuteRace(t *testing.T) {
	r := newFlashRig(t, 5*time.Millisecond, 30*time.Millisecond)
	defer r.s.stop()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	r.ctx = ctx
	atomic.StoreInt64(&r.lag, 600)
	ch := r.client()
	var mu sync.Mutex
	last := ""
	done := make(chan struct{})
	go func() {
		for {
			select {
			case m := <-ch:
				if m.Type == model.MsgFlash || m.Type == model.MsgFlashStop {
					mu.Lock()
					last = m.Type
					mu.Unlock()
				}
			case <-done:
				return
			}
		}
	}()
	defer close(done)
	if _, ok := nextPush(r.s.pushes, model.MsgFlash, 8*time.Second); !ok {
		t.Fatal("no flash")
	}
	go func() { // the main client's push handler must never block
		for {
			select {
			case <-r.s.pushes:
			case <-done:
				return
			}
		}
	}()
	for i := 0; i < 25; i++ {
		// parallel: mute from one goroutine while the pollers publish
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.ok(model.IPCRequest{Cmd: model.CmdMuteUntilRecovered, ProfileID: r.p.ID})
		}()
		_, _ = r.s.c.Do(ctx, model.IPCRequest{Cmd: model.CmdGetStatus})
		wg.Wait()
		// let in-flight polls / pushes settle (pushes are FIFO per client:
		// a get_status round trip on the second client flushes its queue)
		time.Sleep(120 * time.Millisecond)
		mu.Lock()
		got := last
		mu.Unlock()
		if got != model.MsgFlashStop {
			t.Fatalf("round %d: last push %q while muted", i, got)
		}
		r.ok(model.IPCRequest{Cmd: model.CmdUnmuteProfile, ProfileID: r.p.ID})
		deadline := time.Now().Add(5 * time.Second)
		for {
			mu.Lock()
			got = last
			mu.Unlock()
			if got == model.MsgFlash {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("round %d: no flash after unmute", i)
			}
			time.Sleep(2 * time.Millisecond)
		}
	}
}
