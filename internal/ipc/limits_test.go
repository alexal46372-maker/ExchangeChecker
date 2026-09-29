package ipc

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

type testLog struct {
	mu    sync.Mutex
	lines []string
}

func (l *testLog) Printf(f string, a ...interface{}) {
	l.mu.Lock()
	l.lines = append(l.lines, fmt.Sprintf(f, a...))
	l.mu.Unlock()
}

func (l *testLog) has(sub string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, s := range l.lines {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// readHello reads the server's hello from a raw connection.
func readHello(t *testing.T, rw io.Reader) {
	t.Helper()
	var m model.IPCMessage
	if err := ReadMessage(rw, &m); err != nil || m.Type != model.MsgHello {
		t.Fatalf("hello %+v %v", m, err)
	}
}

// expectClosed: the server closes the connection within d.
func expectClosed(t *testing.T, rw io.Reader, d time.Duration) {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		var m model.IPCMessage
		for {
			if err := ReadMessage(rw, &m); err != nil {
				done <- err
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatal("server did not drop the client")
	}
}

// TestCHK_MON_11_RequestFrameLimit — requests are limited to
// IPCMaxRequest (64 KB): a larger announced frame drops the client without
// allocating it, the client refuses to send one; responses may be larger
// (a status snapshot above 64 KB is delivered).
func TestCHK_MON_11_RequestFrameLimit(t *testing.T) {
	if model.IPCMaxRequest != 64<<10 || model.IPCMaxMessage <= model.IPCMaxRequest {
		t.Fatalf("limits %d / %d", model.IPCMaxRequest, model.IPCMaxMessage)
	}
	big := testSnapshot()
	for i := 0; i < 2000; i++ {
		big.Profiles = append(big.Profiles, model.ProfileSnapshot{ProfileID: fmt.Sprintf("p%04d", i),
			Name: strings.Repeat("База ", 10), Points: []model.PointStatus{}})
	}
	lg := &testLog{}
	h := &fakeHandler{snap: big}
	l := NewMemListener()
	srv := NewServer(h, lg)
	go srv.Serve(l)
	defer srv.Close()

	// oversized request frame -> dropped
	rw, err := l.Dial()
	if err != nil {
		t.Fatal(err)
	}
	readHello(t, rw)
	var hdr [4]byte
	binary.LittleEndian.PutUint32(hdr[:], model.IPCMaxRequest+1)
	go rw.Write(hdr[:])
	expectClosed(t, rw, 5*time.Second)
	rw.Close()
	waitClients(t, srv, 0)
	deadline := time.Now().Add(5 * time.Second)
	for !lg.has("too large") {
		if time.Now().After(deadline) {
			t.Fatalf("log %v", lg.lines)
		}
		time.Sleep(5 * time.Millisecond)
	}

	// the client: a request above the limit is refused locally; one just
	// below is sent; the large response arrives
	rw, _ = l.Dial()
	c := NewClient(rw, nil)
	defer c.Close()
	ctx := context.Background()
	if _, err := c.Do(ctx, model.IPCRequest{Cmd: model.CmdPollNow, ProfileID: strings.Repeat("x", model.IPCMaxRequest)}); !errors.Is(err, ErrTooLarge) {
		t.Errorf("oversized request: %v", err)
	}
	m, err := c.Do(ctx, model.IPCRequest{Cmd: model.CmdPollNow, ProfileID: strings.Repeat("x", 60<<10)})
	if err != nil || m.Type != model.MsgError {
		t.Errorf("60 KB request: %+v %v", m.Type, err)
	}
	m, err = c.CallTimeout(model.CmdGetStatus, 10*time.Second)
	if err != nil || m.Status == nil || len(m.Status.Profiles) != len(big.Profiles) {
		t.Fatalf("large status: %v", err)
	}
	b, _ := jsonSize(m)
	if b <= model.IPCMaxRequest {
		t.Errorf("response only %d bytes, want > 64 KB", b)
	}
}

// TestCHK_MON_11_Timeouts — idle timeout (no request at all) and read
// timeout (a started frame that never completes) drop the client; a
// client that keeps pinging stays connected.
func TestCHK_MON_11_Timeouts(t *testing.T) {
	lg := &testLog{}
	l := NewMemListener()
	srv := NewServer(&fakeHandler{snap: testSnapshot()}, lg)
	srv.IdleTimeout, srv.ReadTimeout = 300*time.Millisecond, 100*time.Millisecond
	go srv.Serve(l)
	defer srv.Close()

	idle, _ := l.Dial()
	readHello(t, idle)
	start := time.Now()
	expectClosed(t, idle, 5*time.Second)
	if d := time.Since(start); d < 200*time.Millisecond {
		t.Errorf("idle drop too early: %s", d)
	}
	idle.Close()

	slow, _ := l.Dial()
	readHello(t, slow)
	go slow.Write([]byte{10, 0}) // half a header, then silence
	start = time.Now()
	expectClosed(t, slow, 5*time.Second)
	slow.Close()
	deadline := time.Now().Add(5 * time.Second)
	for !lg.has("idle timeout") || !lg.has("read timeout") {
		if time.Now().After(deadline) {
			t.Fatalf("log %v", lg.lines)
		}
		time.Sleep(5 * time.Millisecond)
	}

	rw, _ := l.Dial()
	c := NewClient(rw, nil)
	defer c.Close()
	for i := 0; i < 8; i++ { // 8 x 100 ms > idle timeout: heartbeat keeps it
		if m, err := c.CallTimeout(model.CmdPing, 5*time.Second); err != nil || m.Type != model.MsgPong {
			t.Fatalf("ping %d: %v", i, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	select {
	case <-c.Done():
		t.Fatal("pinging client dropped")
	default:
	}
}

// TestCHK_MON_11_MaxClients — at most MaxClients connections: the next
// client is not accepted until one disconnects.
func TestCHK_MON_11_MaxClients(t *testing.T) {
	if model.IPCMaxClients != 8 {
		t.Fatalf("IPCMaxClients %d", model.IPCMaxClients)
	}
	l := NewMemListener()
	srv := NewServer(&fakeHandler{snap: testSnapshot()}, nil)
	if srv.MaxClients != model.IPCMaxClients {
		t.Fatalf("default MaxClients %d", srv.MaxClients)
	}
	srv.MaxClients = 2
	go srv.Serve(l)
	defer srv.Close()
	var conns []io.ReadWriteCloser
	for i := 0; i < 2; i++ {
		rw, err := l.Dial()
		if err != nil {
			t.Fatal(err)
		}
		readHello(t, rw)
		conns = append(conns, rw)
	}
	third := make(chan io.ReadWriteCloser, 1)
	go func() {
		rw, err := l.Dial()
		if err == nil {
			third <- rw
		}
	}()
	select {
	case <-third:
		t.Fatal("third client accepted above MaxClients")
	case <-time.After(300 * time.Millisecond):
	}
	conns[0].Close()
	select {
	case rw := <-third:
		readHello(t, rw)
		rw.Close()
	case <-time.After(5 * time.Second):
		t.Fatal("slot not released after disconnect")
	}
	conns[1].Close()
}

// TestCHK_MON_11_CallContext — Call honours its context: a reply that
// does not come in time ends with DeadlineExceeded, the connection stays
// usable.
func TestCHK_MON_11_CallContext(t *testing.T) {
	h := &fakeHandler{snap: testSnapshot(), block: make(chan struct{})}
	l := NewMemListener()
	srv := NewServer(h, nil)
	go srv.Serve(l)
	defer srv.Close()
	rw, _ := l.Dial()
	c := NewClient(rw, nil)
	defer c.Close()
	start := time.Now()
	if _, err := c.CallTimeout(model.CmdGetStatus, 100*time.Millisecond); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout: %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Error("Call did not honour the timeout")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Call(ctx, model.CmdPing); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled ctx: %v", err)
	}
	close(h.block)
	// the late status reply is discarded; the next call works
	deadline := time.Now().Add(5 * time.Second)
	for {
		m, err := c.CallTimeout(model.CmdPing, 5*time.Second)
		if err == nil && m.Type == model.MsgPong {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("ping after timeout: %+v %v", m, err)
		}
	}
}

// TestCHK_MON_12_CoalescedReply — poll_now / reload_config replies carry
// "accepted" or "accepted, coalesced"; poll_now may name one profile.
func TestCHK_MON_12_CoalescedReply(t *testing.T) {
	h := &fakeHandler{snap: testSnapshot()}
	l := NewMemListener()
	srv := NewServer(h, nil)
	go srv.Serve(l)
	defer srv.Close()
	rw, _ := l.Dial()
	c := NewClient(rw, nil)
	defer c.Close()
	ctx := context.Background()
	m, err := c.Do(ctx, model.IPCRequest{Cmd: model.CmdPollNow, ProfileID: "db1"})
	if err != nil || m.Type != model.MsgOK || m.Result != model.ResultAccepted || h.lastProfile.Load() != "db1" {
		t.Errorf("accepted %+v %v", m, err)
	}
	h.coalesce = true
	for _, cmd := range []string{model.CmdPollNow, model.CmdReloadConfig} {
		m, err := c.CallTimeout(cmd, 5*time.Second)
		if err != nil || m.Type != model.MsgOK || m.Result != model.ResultCoalesced || model.ResultCoalesced != "accepted, coalesced" {
			t.Errorf("%s coalesced %+v %v", cmd, m, err)
		}
	}
	if m, _ := c.Do(ctx, model.IPCRequest{Cmd: model.CmdPollNow, ProfileID: "missing"}); m.Type != model.MsgError {
		t.Errorf("unknown profile %+v", m)
	}
}
