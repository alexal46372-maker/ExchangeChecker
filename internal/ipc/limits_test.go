package ipc

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"runtime"
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

// flakyListener fails the first failN Accept calls, then serves.
type flakyListener struct {
	*MemListener
	mu    sync.Mutex
	n     int
	failN int
}

func (l *flakyListener) Accept() (io.ReadWriteCloser, error) {
	l.mu.Lock()
	l.n++
	fail := l.n <= l.failN
	l.mu.Unlock()
	if fail {
		return nil, errors.New("CreateNamedPipe: All pipe instances are busy")
	}
	return l.MemListener.Accept()
}

// fakeTimer is the accept loop's injected clock: after() records the
// requested pause, advances the fake time by it and fires at once, so
// the test does not depend on real sleeps.
type fakeTimer struct {
	mu     sync.Mutex
	now    time.Time
	delays []time.Duration
}

func (f *fakeTimer) Now() time.Time { f.mu.Lock(); defer f.mu.Unlock(); return f.now }
func (f *fakeTimer) After(d time.Duration) <-chan time.Time {
	f.mu.Lock()
	f.delays = append(f.delays, d)
	f.now = f.now.Add(d)
	now := f.now
	f.mu.Unlock()
	ch := make(chan time.Time, 1)
	ch <- now
	return ch
}

func (f *fakeTimer) Delays() []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]time.Duration(nil), f.delays...)
}

func acceptLines(lg *testLog) (n int) {
	lg.mu.Lock()
	defer lg.mu.Unlock()
	for _, s := range lg.lines {
		if strings.Contains(s, "ipc: accept:") {
			n++
		}
	}
	return n
}

func waitLog(t *testing.T, lg *testLog, sub string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !lg.has(sub) {
		if time.Now().After(deadline) {
			t.Fatalf("log line %q missing: %v", sub, lg.lines)
		}
		runtime.Gosched()
		time.Sleep(time.Millisecond)
	}
}

// TestCHK_MON_11_AcceptBackoff — a persistent Accept failure is retried
// with exponential backoff capped at BackoffMax and logged once (a Server
// literal without NewServer uses the default LogEvery); after it heals
// the client is accepted and the recovery line reports the number of
// failures and of unlogged repeats. With a short LogEvery the failure is
// logged again periodically. Fake clock: no timing assumptions.
func TestCHK_MON_11_AcceptBackoff(t *testing.T) {
	if AcceptBackoffMin != 200*time.Millisecond || AcceptBackoffMax < 10*time.Second || AcceptLogEvery != 10*time.Minute {
		t.Fatalf("defaults %s / %s / %s", AcceptBackoffMin, AcceptBackoffMax, AcceptLogEvery)
	}
	const failN = 12
	lg := &testLog{}
	ft := &fakeTimer{now: time.Date(2026, 9, 29, 12, 0, 0, 0, model.DBZone)}
	l := &flakyListener{MemListener: NewMemListener(), failN: failN}
	srv := &Server{H: &fakeHandler{snap: testSnapshot()}, Log: lg, // no NewServer: LogEvery etc. zero
		BackoffMin: 10 * time.Millisecond, BackoffMax: 80 * time.Millisecond, now: ft.Now, after: ft.After}
	go srv.Serve(l)
	defer srv.Close()
	rw, err := l.Dial() // accepted after the failN failures
	if err != nil {
		t.Fatal(err)
	}
	readHello(t, rw)
	rw.Close()
	waitLog(t, lg, "accept works again")
	want := []time.Duration{10, 20, 40, 80, 80, 80, 80, 80, 80, 80, 80, 80}
	got := ft.Delays()
	if len(got) != len(want) {
		t.Fatalf("delays %v", got)
	}
	for i := range want {
		if got[i] != want[i]*time.Millisecond {
			t.Fatalf("delays %v, want %v ms", got, want)
		}
	}
	if n := acceptLines(lg); n != 1 {
		t.Errorf("failure logged %d times, want 1: %v", n, lg.lines)
	}
	if !lg.has(fmt.Sprintf("after %d failure(s), %d of them not logged", failN, failN-1)) {
		t.Errorf("recovery line: %v", lg.lines)
	}

	// short LogEvery: repeated periodically with the suppressed count
	lg2 := &testLog{}
	ft2 := &fakeTimer{now: ft.Now()}
	l2 := &flakyListener{MemListener: NewMemListener(), failN: failN}
	srv2 := &Server{H: &fakeHandler{snap: testSnapshot()}, Log: lg2, BackoffMin: 10 * time.Millisecond,
		BackoffMax: 80 * time.Millisecond, LogEvery: 200 * time.Millisecond, now: ft2.Now, after: ft2.After}
	go srv2.Serve(l2)
	defer srv2.Close()
	rw2, err := l2.Dial()
	if err != nil {
		t.Fatal(err)
	}
	readHello(t, rw2)
	rw2.Close()
	waitLog(t, lg2, "accept works again")
	// failures at fake t = 0,10,30,70,150,230,310,390,470,550,630,710 ms:
	// logged at 0, 230, 470, 710 (>= 200 ms apart)
	if n := acceptLines(lg2); n != 4 || !lg2.has("repeat(s) not logged") {
		t.Errorf("periodic log: %d lines %v", n, lg2.lines)
	}
}
