package sqlcheck

import (
	"context"
	"errors"
	"io"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"
)

// CHK-SQL-01: the session setup is bounded and leaves nothing behind
// (PLAN.md §12 п. 40): go-mssqldb v1.8.2 reads prelogin / login without a
// deadline and ignores the context there.

// stubServer accepts TCP connections, optionally answers the first packet
// with reply, and reports every connection the client closes.
type stubServer struct {
	ln     net.Listener
	reply  []byte
	closed chan time.Time // one value per connection closed by the client
	wg     sync.WaitGroup
}

func newStubServer(t *testing.T, reply []byte) *stubServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &stubServer{ln: ln, reply: reply, closed: make(chan time.Time, 16)}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				defer c.Close()
				buf := make([]byte, 4096)
				replied := s.reply == nil
				for {
					if _, err := c.Read(buf); err != nil {
						s.closed <- time.Now() // EOF / reset: the client closed the socket
						return
					}
					if !replied {
						replied = true
						_, _ = c.Write(s.reply)
					}
				}
			}()
		}
	}()
	t.Cleanup(func() { ln.Close(); s.wg.Wait() })
	return s
}

func (s *stubServer) port() int { return s.ln.Addr().(*net.TCPAddr).Port }

// waitClosed waits for the server to see the client close a connection.
func (s *stubServer) waitClosed(t *testing.T, what string) {
	t.Helper()
	select {
	case <-s.closed:
	case <-time.After(3 * time.Second):
		t.Errorf("%s: the server still sees the connection open (socket leaked until GC)", what)
	}
}

// settleGoroutines waits until the goroutine count is back to base.
func settleGoroutines(t *testing.T, base int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() > base {
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<16)
			t.Fatalf("goroutines: %d > %d\n%s", runtime.NumGoroutine(), base, buf[:runtime.Stack(buf, true)])
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestCHK_SQL_01_HandshakeCancelSilentServer(t *testing.T) {
	srv := newStubServer(t, nil) // accepts, never answers
	p := connProfile()
	p.Server, p.Port, p.ConnectTimeoutSec = "127.0.0.1", srv.port(), 30
	base := runtime.NumGoroutine()
	// two cancels in a row (IPC test_profile): each returns promptly and
	// closes its socket; nothing hangs, nothing accumulates
	for i := 0; i < 2; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(300*time.Millisecond, cancel)
		start := time.Now()
		err := TestConnection(ctx, p, "x")
		if d := time.Since(start); d > 3*time.Second {
			t.Errorf("cancel %d: TestConnection returned after %v", i, d)
		}
		if err == nil || !errors.Is(err, context.Canceled) {
			t.Errorf("cancel %d: %v", i, err)
		}
		srv.waitClosed(t, "cancel")
		cancel()
	}
	settleGoroutines(t, base)
}

func TestCHK_SQL_01_HandshakeDeadlineSilentServer(t *testing.T) {
	srv := newStubServer(t, nil)
	p := connProfile()
	p.Server, p.Port, p.ConnectTimeoutSec = "127.0.0.1", srv.port(), 30
	base := runtime.NumGoroutine()
	// the caller's deadline (DeadlineExceeded)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	start := time.Now()
	_, err := TestConnectionInfo(ctx, p, "x")
	cancel()
	if d := time.Since(start); d > 3*time.Second || ConnCategory(err) != ConnCategoryTimeout {
		t.Errorf("deadline: %v after %v -> %q", err, d, ConnCategory(err))
	}
	srv.waitClosed(t, "deadline")
	// the handshake budget (Profile.ConnectTimeout) without a caller deadline
	p.ConnectTimeoutSec = 1
	start = time.Now()
	err = TestConnection(context.Background(), p, "x")
	if d := time.Since(start); d > 4*time.Second || ConnCategory(err) != ConnCategoryTimeout {
		t.Errorf("budget: %v after %v -> %q", err, d, ConnCategory(err))
	}
	srv.waitClosed(t, "budget")
	settleGoroutines(t, base)
}

func TestCHK_SQL_01_HandshakeFailureClosesSocket(t *testing.T) {
	// a TDS packet of the wrong type instead of the PRELOGIN response
	srv := newStubServer(t, []byte{0x01, 0x01, 0x00, 0x08, 0x00, 0x00, 0x01, 0x00})
	p := connProfile()
	p.Server, p.Port, p.ConnectTimeoutSec = "127.0.0.1", srv.port(), 30
	base := runtime.NumGoroutine()
	start := time.Now()
	err := TestConnection(context.Background(), p, "x")
	if err == nil || time.Since(start) > 3*time.Second {
		t.Fatalf("bad prelogin: %v after %v", err, time.Since(start))
	}
	srv.waitClosed(t, "prelogin failure")
	settleGoroutines(t, base)
}

// tcpPair returns both ends of a loopback TCP connection.
func tcpPair(t *testing.T) (client, server net.Conn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	ch := make(chan net.Conn, 1)
	go func() { c, _ := ln.Accept(); ch <- c }()
	client, err = net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	server = <-ch
	t.Cleanup(func() { client.Close(); server.Close() })
	return client, server
}

// TestCHK_SQL_01_HandshakeStateMachine: after a successful login the
// deadline is cleared and neither the caller's context nor the budget can
// touch the session any more (no race with the driver's deferred cancel);
// a kill before the end closes the sockets and wins over a late success.
func TestCHK_SQL_01_HandshakeStateMachine(t *testing.T) {
	// success: ctx ends and the budget expires right after login
	c, s := tcpPair(t)
	h := newHSGuard()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { h.watch(ctx, 50*time.Millisecond); close(done) }()
	if err := h.add(c); err != nil {
		t.Fatal(err)
	}
	_ = c.SetDeadline(time.Now().Add(10 * time.Millisecond)) // a deadline left by the handshake
	if !h.finish(nil) {
		t.Fatal("finish(nil) on a running handshake must succeed")
	}
	cancel()
	<-done
	if h.kill(context.Canceled) {
		t.Error("kill after success must be a no-op")
	}
	time.Sleep(150 * time.Millisecond) // past the budget and the old deadline
	go func() { time.Sleep(200 * time.Millisecond); _, _ = s.Write([]byte("late")) }()
	buf := make([]byte, 4)
	if n, err := io.ReadFull(c, buf); err != nil || string(buf[:n]) != "late" {
		t.Errorf("session after success: %q %v (deadline not cleared?)", buf[:n], err)
	}

	// kill while handshaking: sockets closed, a late success is refused,
	// later dials are closed at once
	c2, s2 := tcpPair(t)
	h2 := newHSGuard()
	if err := h2.add(c2); err != nil {
		t.Fatal(err)
	}
	if !h2.kill(context.DeadlineExceeded) {
		t.Fatal("kill while handshaking must act")
	}
	if h2.finish(nil) {
		t.Error("finish after kill must report the session unusable")
	}
	_ = s2.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := s2.Read(buf); err == nil {
		t.Error("killed socket still open")
	}
	c3, s3 := tcpPair(t)
	if err := h2.add(c3); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("dial after kill: %v", err)
	}
	_ = s3.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := s3.Read(buf); err == nil {
		t.Error("socket dialed after the kill still open")
	}

	// failure: sockets closed at once
	c4, s4 := tcpPair(t)
	h4 := newHSGuard()
	_ = h4.add(c4)
	if h4.finish(errors.New("login failed")) {
		t.Error("finish(err) must report failure")
	}
	_ = s4.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := s4.Read(buf); err == nil {
		t.Error("socket of a failed handshake still open")
	}
}
