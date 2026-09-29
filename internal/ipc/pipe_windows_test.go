//go:build windows

package ipc

import (
	"fmt"
	"io"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

var pipeSeq int32

func testPipeName() string {
	return fmt.Sprintf(`\\.\pipe\ExchangeChecker-test-%d-%d`, os.Getpid(), atomic.AddInt32(&pipeSeq, 1))
}

// TestCHK_MON_06_NamedPipe — the protocol over a real named pipe
// (CreateNamedPipeW with the PipeSDDL DACL, overlapped I/O).
func TestCHK_MON_06_NamedPipe(t *testing.T) {
	name := testPipeName()
	l, err := ListenPipe(name, "")
	if err != nil {
		t.Fatalf("ListenPipe: %v", err)
	}
	exerciseServer(t, l, func() (io.ReadWriteCloser, error) { return DialPipe(name, 5*time.Second) })
	if _, err := DialPipe(name, 200*time.Millisecond); err == nil {
		t.Error("dial after close must fail")
	}
}

// TestCHK_MON_06_NamedPipeFirstInstance — a second server on the same
// name is refused (FILE_FLAG_FIRST_PIPE_INSTANCE, anti-squatting); Close
// interrupts a pending Accept and a pending Read.
func TestCHK_MON_06_NamedPipeFirstInstance(t *testing.T) {
	name := testPipeName()
	l, err := ListenPipe(name, "")
	if err != nil {
		t.Fatal(err)
	}
	if l2, err := ListenPipe(name, ""); err == nil {
		l2.Close()
		t.Error("second server on the same pipe name must fail")
	}
	acc := make(chan error, 1)
	go func() {
		c, err := l.Accept()
		if err == nil {
			// block in Read, then get interrupted by Close
			buf := make([]byte, 8)
			_, err = c.Read(buf)
			c.Close()
			acc <- err
			return
		}
		acc <- err
	}()
	cli, err := DialPipe(name, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	cli.Close() // server Read must end (EOF)
	select {
	case err := <-acc:
		if err != io.EOF {
			t.Errorf("server read after client close: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server read not interrupted by client close")
	}
	// pending Accept interrupted by Close
	go func() { _, err := l.Accept(); acc <- err }()
	time.Sleep(100 * time.Millisecond)
	start := time.Now()
	l.Close()
	select {
	case err := <-acc:
		if err != ErrListenerClosed {
			t.Errorf("accept after close: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Accept not interrupted by Close")
	}
	if time.Since(start) > 3*time.Second {
		t.Error("Close too slow")
	}
	if _, err := ListenPipe(name, "D:bad-sddl"); err == nil {
		t.Error("bad SDDL must fail")
	}
}
