package ipc

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

type fakeHandler struct {
	polls, reloads int32
	snap           model.StatusSnapshot
	coalesce       bool          // report every command as coalesced
	block          chan struct{} // Snapshot blocks until closed
	lastProfile    atomic.Value
}

func (h *fakeHandler) Snapshot() model.StatusSnapshot {
	if h.block != nil {
		<-h.block
	}
	return h.snap
}

func (h *fakeHandler) PollNow(id string) (bool, error) {
	h.lastProfile.Store(id)
	if id == "missing" || len(id) > 1000 {
		return false, errors.New("monitor: unknown profile")
	}
	atomic.AddInt32(&h.polls, 1)
	return h.coalesce, nil
}

func (h *fakeHandler) ReloadConfig() bool { atomic.AddInt32(&h.reloads, 1); return h.coalesce }

func testSnapshot() model.StatusSnapshot {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, model.DBZone)
	fresh := now.Add(-35 * time.Minute)
	return model.StatusSnapshot{Seq: 7, Now: now, Level: model.LevelWarning, PollIntervalSec: 60, Profiles: []model.ProfileSnapshot{{
		ProfileID: "db1", Name: "База 1", Enabled: true, Level: model.LevelWarning, Polled: true, FreshAt: &fresh, Baseline: true,
		Points: []model.PointStatus{{ExchangePoint: model.ExchangePoint{Name: "Магазин 1", LastSend: &fresh, Level: model.LevelWarning,
			Source: model.PointSourceCounters, SendBaseline: true}, FreshAt: &fresh, LagMinutes: 35, Baseline: true}},
	}, {ProfileID: "db2", Name: "DB2", Enabled: true, Level: model.LevelError, Error: "login failed", ErrorKey: model.MsgMonitorConnError, Points: []model.PointStatus{}}}}
}

// TestCHK_MON_06_Framing — length prefix, limits, truncated frames.
func TestCHK_MON_06_Framing(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteMessage(&buf, model.IPCRequest{ID: 5, Cmd: model.CmdGetStatus}); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	if n := binary.LittleEndian.Uint32(b); int(n) != len(b)-4 {
		t.Fatalf("length prefix %d, payload %d", n, len(b)-4)
	}
	var req model.IPCRequest
	if err := ReadMessage(bytes.NewReader(b), &req); err != nil || req.ID != 5 || req.Cmd != model.CmdGetStatus {
		t.Fatalf("roundtrip %+v %v", req, err)
	}
	if err := ReadMessage(bytes.NewReader(b[:len(b)-1]), &req); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("truncated: %v", err)
	}
	big := make([]byte, 4)
	binary.LittleEndian.PutUint32(big, model.IPCMaxMessage+1)
	if err := ReadMessage(bytes.NewReader(big), &req); !errors.Is(err, ErrTooLarge) {
		t.Errorf("too large: %v", err)
	}
	bad := []byte{2, 0, 0, 0, '{', 'x'}
	if err := ReadMessage(bytes.NewReader(bad), &req); err == nil {
		t.Error("bad JSON accepted")
	}
	if err := ReadMessage(bytes.NewReader(nil), &req); !errors.Is(err, io.EOF) {
		t.Errorf("empty: %v", err)
	}
}

// exerciseServer runs the protocol checks over any dial function (memory
// pipe on every OS, the real named pipe on Windows).
func exerciseServer(t *testing.T, l Listener, dial func() (io.ReadWriteCloser, error)) {
	t.Helper()
	h := &fakeHandler{snap: testSnapshot()}
	srv := NewServer(h, nil)
	served := make(chan error, 1)
	go func() { served <- srv.Serve(l) }()

	rw, err := dial()
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	pushes := make(chan model.IPCMessage, 16)
	c := NewClient(rw, func(m model.IPCMessage) { pushes <- m })
	select {
	case m := <-pushes:
		if m.Type != model.MsgHello || m.Version != model.IPCVersion {
			t.Fatalf("hello %+v", m)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no hello")
	}
	m, err := c.CallTimeout(model.CmdPing, 5*time.Second)
	if err != nil || m.Type != model.MsgPong {
		t.Fatalf("ping %+v %v", m, err)
	}
	m, err = c.CallTimeout(model.CmdGetStatus, 5*time.Second)
	if err != nil || m.Type != model.MsgStatus || m.Status == nil {
		t.Fatalf("status %+v %v", m, err)
	}
	s := m.Status
	if s.Seq != 7 || len(s.Profiles) != 2 || s.Profiles[0].Points[0].LagMinutes != 35 || !s.Profiles[0].Points[0].Baseline ||
		!s.Profiles[0].FreshAt.Equal(*testSnapshot().Profiles[0].FreshAt) || s.Profiles[1].ErrorKey != model.MsgMonitorConnError ||
		s.Profiles[0].Level != model.LevelWarning {
		t.Errorf("snapshot content %+v", s)
	}
	if _, off := s.Now.Zone(); off != 3*3600 {
		t.Errorf("now offset %d", off)
	}
	if m, err = c.CallTimeout(model.CmdReloadConfig, 5*time.Second); err != nil || m.Type != model.MsgOK || atomic.LoadInt32(&h.reloads) != 1 {
		t.Errorf("reload %+v %v", m, err)
	}
	if m, err = c.CallTimeout(model.CmdPollNow, 5*time.Second); err != nil || m.Type != model.MsgOK || atomic.LoadInt32(&h.polls) != 1 {
		t.Errorf("poll now %+v %v", m, err)
	}
	if m, err = c.CallTimeout("format_disk", 5*time.Second); err != nil || m.Type != model.MsgError {
		t.Errorf("unknown command %+v %v", m, err)
	}
	// concurrent calls are matched by ID
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if m, err := c.CallTimeout(model.CmdPing, 5*time.Second); err != nil || m.Type != model.MsgPong {
				t.Errorf("concurrent ping %+v %v", m, err)
			}
		}()
	}
	wg.Wait()
	// pushes: status after a poll and alert events
	waitClients(t, srv, 1)
	snap := testSnapshot()
	snap.Seq = 8
	srv.PublishStatus(snap)
	srv.Alert(model.AlertEvent{ID: 3, ProfileID: "db1", PointName: "Магазин 1", From: model.LevelOK, To: model.LevelWarning})
	got := map[string]model.IPCMessage{}
	for len(got) < 2 {
		select {
		case m := <-pushes:
			got[m.Type] = m
		case <-time.After(5 * time.Second):
			t.Fatalf("pushes: %v", got)
		}
	}
	if got[model.MsgStatus].Status.Seq != 8 || got[model.MsgAlert].Alert.ID != 3 || got[model.MsgAlert].ID != 0 {
		t.Errorf("pushes %+v", got)
	}
	// a second client
	rw2, err := dial()
	if err != nil {
		t.Fatalf("second dial: %v", err)
	}
	c2 := NewClient(rw2, nil)
	if m, err := c2.CallTimeout(model.CmdGetStatus, 5*time.Second); err != nil || m.Status == nil {
		t.Errorf("second client %+v %v", m, err)
	}
	waitClients(t, srv, 2)
	// client disconnect is noticed
	c2.Close()
	waitClients(t, srv, 1)
	// server close disconnects clients and ends Serve promptly
	done := make(chan struct{})
	go func() { srv.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("server Close hung")
	}
	select {
	case err := <-served:
		if err != nil {
			t.Errorf("Serve: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return")
	}
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("client not disconnected by server Close")
	}
	if _, err := c.CallTimeout(model.CmdPing, 5*time.Second); err == nil {
		t.Error("call after server close must fail")
	}
	c.Close()
}

func waitClients(t *testing.T, s *Server, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for s.Clients() != n {
		if time.Now().After(deadline) {
			t.Fatalf("clients = %d, want %d", s.Clients(), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestCHK_MON_06_ServerMemory — full protocol over the in-memory listener.
func TestCHK_MON_06_ServerMemory(t *testing.T) {
	l := NewMemListener()
	exerciseServer(t, l, l.Dial)
	if _, err := l.Dial(); !errors.Is(err, ErrListenerClosed) {
		t.Errorf("dial after close: %v", err)
	}
}

// TestCHK_MON_06_SlowClientDropped — a client that never reads is
// disconnected instead of blocking broadcasts.
func TestCHK_MON_06_SlowClientDropped(t *testing.T) {
	l := NewMemListener()
	srv := NewServer(&fakeHandler{snap: testSnapshot()}, nil)
	go srv.Serve(l)
	defer srv.Close()
	rw, err := l.Dial()
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()
	waitClients(t, srv, 1)
	done := make(chan struct{})
	go func() {
		for i := 0; i < outQueue*4; i++ {
			srv.PublishStatus(testSnapshot())
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("broadcast blocked on a slow client")
	}
	waitClients(t, srv, 0)
	// Serve after Close returns immediately
	srv.Close()
	if err := srv.Serve(NewMemListener()); err != nil {
		t.Errorf("Serve after Close: %v", err)
	}
}

func jsonSize(v interface{}) (int, error) {
	var b bytes.Buffer
	err := WriteMessage(&b, v)
	return b.Len() - 4, err
}
