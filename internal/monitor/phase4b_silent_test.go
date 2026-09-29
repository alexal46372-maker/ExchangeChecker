package monitor

import (
	"context"
	"errors"
	"io"
	"net"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/sqlcheck"
)

// silentServer accepts TCP connections and never answers (a black-hole
// SQL port). It counts the connections that are still open.
type silentServer struct {
	ln       net.Listener
	accepted chan struct{}
	mu       sync.Mutex
	open     int
	wg       sync.WaitGroup
}

func newSilentServer(t *testing.T) *silentServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &silentServer{ln: ln, accepted: make(chan struct{}, 100)}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			s.mu.Lock()
			s.open++
			s.mu.Unlock()
			s.accepted <- struct{}{}
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				_, _ = io.Copy(io.Discard, c) // returns when the client closes
				c.Close()
				s.mu.Lock()
				s.open--
				s.mu.Unlock()
			}()
		}
	}()
	t.Cleanup(func() { ln.Close() })
	return s
}

func (s *silentServer) openConns() int { s.mu.Lock(); defer s.mu.Unlock(); return s.open }

func (s *silentServer) port() int { return s.ln.Addr().(*net.TCPAddr).Port }

// TestCHK_IPC_03_SilentServerCancelTimeout — phase 4b (review blocker 1):
// test_profile against a server that accepts TCP and never answers,
// through the REAL sqlcheck.TestConnectionInfo. Three client disconnects
// (caller context canceled) and three timeouts (the single test deadline
// expires, fake-clock context): every test answers at once (canceled /
// timeout), the driver returns (the abandoned slot is released), the
// socket is closed, and afterwards test_profile is not busy and no
// goroutines are left behind.
func TestCHK_IPC_03_SilentServerCancelTimeout(t *testing.T) {
	srv := newSilentServer(t)
	p := model.Profile{ID: "silent", Name: "silent", Server: "127.0.0.1", Port: srv.port(), Database: "db",
		AuthMode: model.AuthSQL, Login: "u", Encrypt: model.EncryptDisable, Enabled: true}
	fa := &fakeTimeouts{}
	opt := baseOpt(newFactory(), NewMemConfig(Config{Settings: model.DefaultSettings(), Profiles: []model.Profile{p},
		Passwords: map[string]string{"silent": savedPW}}), &fakeClock{t: t0})
	opt.Test, opt.TestCategory, opt.TestWithTimeout, opt.CommandInterval = SQLTest, sqlcheck.ConnCategory, fa.WithTimeout, -1
	m := New(opt)
	m.reloadConfig(true)
	runtime.GC()
	base := runtime.NumGoroutine()

	settled := func() {
		t.Helper()
		waitFor(t, func() bool {
			run, ab := m.tester.Busy()
			return run == 0 && ab == 0 && srv.openConns() == 0
		})
	}
	type out struct {
		r   model.TestProfileResult
		err error
	}
	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan out, 1)
		go func() { r, err := m.TestProfile(ctx, model.IPCRequest{ProfileID: "silent"}); done <- out{r, err} }()
		<-srv.accepted // the driver is connected and waits for the pre-login answer
		cancel()       // client disconnected
		o := <-done
		if o.err != nil || o.r.OK || o.r.Category != model.TestOther || o.r.Error != "connection test canceled" {
			t.Fatalf("cancel %d: %+v %v", i, o.r, o.err)
		}
		settled()
	}
	for i := 0; i < 3; i++ {
		done := make(chan out, 1)
		go func() {
			r, err := m.TestProfile(context.Background(), model.IPCRequest{ProfileID: "silent"})
			done <- out{r, err}
		}()
		<-srv.accepted
		fa.fire(fa.n() - 1) // the single deadline expires
		o := <-done
		if o.err != nil || o.r.Category != model.TestTimeout || o.r.ErrorKey != model.MsgTestTimeout {
			t.Fatalf("timeout %d: %+v %v", i, o.r, o.err)
		}
		if strings.Contains(o.r.Error, savedPW) {
			t.Fatal("password in the result")
		}
		settled()
	}
	// not busy afterwards: a new test is accepted (and ends by timeout)
	done := make(chan out, 1)
	go func() {
		r, err := m.TestProfile(context.Background(), model.IPCRequest{ProfileID: "silent"})
		done <- out{r, err}
	}()
	<-srv.accepted
	fa.fire(fa.n() - 1)
	if o := <-done; errors.Is(o.err, ErrTestBusy) || o.r.Category != model.TestTimeout {
		t.Fatalf("after 6 aborted tests: %+v %v", o.r, o.err)
	}
	settled()
	waitFor(t, func() bool { return runtime.NumGoroutine() <= base+1 })
}
