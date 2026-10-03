package trayipc

import (
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/ipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// fakeClock: After registers a waiter; tests fire waiters by duration.
type fakeClock struct {
	mu      sync.Mutex
	waiters []fakeWaiter
	seen    map[time.Duration]int
}

type fakeWaiter struct {
	d  time.Duration
	ch chan time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{seen: map[time.Duration]int{}} }

func (f *fakeClock) After(d time.Duration) <-chan time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	ch := make(chan time.Time, 1)
	f.waiters = append(f.waiters, fakeWaiter{d, ch})
	f.seen[d]++
	return ch
}

// pending reports whether a waiter with duration d is registered.
func (f *fakeClock) pending(d time.Duration) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, w := range f.waiters {
		if w.d == d {
			return true
		}
	}
	return false
}

// fire waits (real time, bounded) for a waiter of duration d and fires
// the oldest one.
func (f *fakeClock) fire(t *testing.T, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		f.mu.Lock()
		for i, w := range f.waiters {
			if w.d == d {
				f.waiters = append(f.waiters[:i], f.waiters[i+1:]...)
				f.mu.Unlock()
				w.ch <- time.Now()
				return
			}
		}
		f.mu.Unlock()
		if time.Now().After(deadline) {
			t.Fatalf("no timer of %s registered", d)
		}
		time.Sleep(time.Millisecond)
	}
}

func (f *fakeClock) count(d time.Duration) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.seen[d]
}

// waitFor polls cond (real time, bounded).
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// fakeHandler is a version-2 service handler (heartbeat + ack + test).
type fakeHandler struct {
	mu      sync.Mutex
	snap    model.StatusSnapshot
	hb      model.Heartbeat
	hbCalls int
	acks    int
}

func (h *fakeHandler) Snapshot() model.StatusSnapshot {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.snap
}
func (h *fakeHandler) PollNow(string) (bool, error) { return false, nil }
func (h *fakeHandler) ReloadConfig() bool           { return false }
func (h *fakeHandler) Heartbeat() model.Heartbeat {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.hbCalls++
	return h.hb
}
func (h *fakeHandler) AckAlert(string, string) (int, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.acks++
	return 1, nil
}
func (h *fakeHandler) TestProfile(context.Context, model.IPCRequest) (model.TestProfileResult, error) {
	return model.TestProfileResult{OK: true, Category: model.TestOK}, nil
}
func (h *fakeHandler) calls() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.hbCalls
}

// plainHandler has no version-2 interfaces.
type plainHandler struct{ snap model.StatusSnapshot }

func (h *plainHandler) Snapshot() model.StatusSnapshot { return h.snap }
func (h *plainHandler) PollNow(string) (bool, error)   { return false, nil }
func (h *plainHandler) ReloadConfig() bool             { return false }

// startServer runs an ipc.Server over an in-memory listener.
func startServer(t *testing.T, h ipc.Handler) (*ipc.Server, Dialer) {
	t.Helper()
	l := ipc.NewMemListener()
	srv := ipc.NewServer(h, nil)
	go srv.Serve(l)
	t.Cleanup(func() { srv.Close() })
	return srv, func(context.Context) (io.ReadWriteCloser, error) { return l.Dial() }
}

// manualServer is a hand-written service: hello first, then reply(req)
// for every request (nil = no answer). Requests are recorded.
type manualServer struct {
	mu   sync.Mutex
	cmds []string
}

func (m *manualServer) got() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.cmds...)
}

func (m *manualServer) dialer(hello model.IPCMessage, reply func(model.IPCRequest) *model.IPCMessage) Dialer {
	return func(context.Context) (io.ReadWriteCloser, error) {
		srv, cli := net.Pipe()
		go func() {
			defer srv.Close()
			if err := ipc.WriteMessage(srv, hello); err != nil {
				return
			}
			for {
				var req model.IPCRequest
				if err := ipc.ReadFrame(srv, &req, model.IPCMaxRequest); err != nil {
					return
				}
				m.mu.Lock()
				m.cmds = append(m.cmds, req.Cmd)
				m.mu.Unlock()
				if r := reply(req); r != nil {
					r.ID = req.ID
					if err := ipc.WriteMessage(srv, *r); err != nil {
						return
					}
				}
			}
		}()
		return cli, nil
	}
}

// runSession starts s.Run until the test ends.
func runSession(t *testing.T, s *Session) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); s.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("Session.Run did not stop")
		}
	})
}

func connected(s *Session) func() bool {
	return func() bool { st := s.Status(); return st.State == StateConnected && st.Snapshot != nil }
}
