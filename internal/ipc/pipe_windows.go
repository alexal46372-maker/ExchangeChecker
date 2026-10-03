//go:build windows

package ipc

import (
	"errors"
	"io"
	"sync"
	"syscall"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// PipeSDDL is the pipe DACL: SYSTEM and Administrators full access,
// interactive users FILE_GENERIC_READ | FILE_WRITE_DATA |
// FILE_WRITE_ATTRIBUTES (0x12018b) — no FILE_CREATE_PIPE_INSTANCE, so a
// user process cannot create a rogue instance of the service's pipe.
const PipeSDDL = "D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;0x12018b;;;IU)"

const pipeBuf = 64 << 10

// clientAccess is the access a client requests: FILE_GENERIC_READ |
// FILE_WRITE_DATA | FILE_WRITE_ATTRIBUTES = exactly the 0x12018b granted
// to interactive users by PipeSDDL. GENERIC_WRITE (FILE_GENERIC_WRITE)
// would also ask for FILE_APPEND_DATA (= FILE_CREATE_PIPE_INSTANCE) and
// FILE_WRITE_EA, which PipeSDDL deliberately does not grant: a non-admin
// client would get ERROR_ACCESS_DENIED (phase 6 fix, CHK-IPC-06).
const clientAccess = syscall.GENERIC_READ | fileWriteData | fileWriteAttributes

const (
	fileWriteData       = 0x2
	fileWriteAttributes = 0x100
)

// ClientIdentity identifies the client of a server end (phase 6,
// CHK-IPC-06): ImpersonateNamedPipeClient + CheckTokenMembership
// (BUILTIN\Administrators) + RevertToSelf on a dedicated OS thread. It
// fails before the server has read data from the client
// (ERROR_CANNOT_IMPERSONATE) and for an anonymous client.
func (c *pipeConn) ClientIdentity() (Peer, error) {
	if !c.server {
		return Peer{}, errors.New("ipc: not a server end")
	}
	if !c.begin() {
		return Peer{}, errPipeClosed
	}
	defer c.active.Done()
	pc, err := winapi.ImpersonatePipeClient(c.h)
	if err != nil {
		return Peer{}, err
	}
	return Peer{Admin: pc.Admin, Account: pc.Account}, nil
}

// maxInstances is nMaxInstances of CreateNamedPipeW: the OS refuses a
// ninth instance even if a caller bypasses ipc.Server's own slot limit
// (CHK-MON-11).
const maxInstances = model.IPCMaxClients

var errPipeClosed = errors.New("ipc: pipe closed")

// pipeConn is one end of a named pipe opened with FILE_FLAG_OVERLAPPED.
// Reads and writes wait on (io event, stop event), so Close interrupts
// them from any goroutine (CancelIoEx + wait for the cancellation).
type pipeConn struct {
	h      syscall.Handle
	server bool
	stop   syscall.Handle
	rov    syscall.Overlapped
	wov    syscall.Overlapped
	// rbuf / wbuf: heap buffers owned by the connection. Overlapped I/O
	// keeps using the buffer after ReadFile/WriteFile return, so caller
	// slices (possibly on a goroutine stack that may move) are never
	// handed to the kernel.
	rbuf []byte
	wbuf []byte
	rmu  sync.Mutex
	wmu  sync.Mutex

	mu     sync.Mutex
	closed bool
	active sync.WaitGroup
}

func newPipeConn(h syscall.Handle, server bool) (*pipeConn, error) {
	c := &pipeConn{h: h, server: server, rbuf: make([]byte, pipeBuf), wbuf: make([]byte, pipeBuf)}
	var err error
	if c.stop, err = winapi.CreateEvent(true, false); err != nil {
		return nil, err
	}
	if c.rov.HEvent, err = winapi.CreateEvent(true, false); err != nil {
		syscall.CloseHandle(c.stop)
		return nil, err
	}
	if c.wov.HEvent, err = winapi.CreateEvent(true, false); err != nil {
		syscall.CloseHandle(c.stop)
		syscall.CloseHandle(c.rov.HEvent)
		return nil, err
	}
	return c, nil
}

func (c *pipeConn) begin() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return false
	}
	c.active.Add(1)
	return true
}

// wait completes an overlapped operation started with err.
func (c *pipeConn) wait(ov *syscall.Overlapped, err error) (uint32, error) {
	if err != nil && err != winapi.ErrorIOPending {
		return 0, err
	}
	i, werr := winapi.WaitAny(ov.HEvent, c.stop)
	if werr != nil || i == 1 {
		_ = syscall.CancelIoEx(c.h, ov)
		_, _ = winapi.GetOverlappedResult(c.h, ov, true)
		return 0, errPipeClosed
	}
	return winapi.GetOverlappedResult(c.h, ov, true)
}

func mapPipeErr(err error) error {
	switch err {
	case syscall.ERROR_BROKEN_PIPE, winapi.ErrorPipeNotConnected, winapi.ErrorNoData, winapi.ErrorOperationAborted, errPipeClosed:
		return io.EOF
	}
	return err
}

// Read implements io.Reader.
func (c *pipeConn) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	c.rmu.Lock()
	defer c.rmu.Unlock()
	if !c.begin() {
		return 0, io.EOF
	}
	defer c.active.Done()
	_ = winapi.ResetEvent(c.rov.HEvent)
	want := len(p)
	if want > len(c.rbuf) {
		want = len(c.rbuf)
	}
	var n uint32
	err := syscall.ReadFile(c.h, c.rbuf[:want], &n, &c.rov)
	n, err = c.wait(&c.rov, err)
	if err != nil {
		return 0, mapPipeErr(err)
	}
	if n == 0 {
		return 0, io.EOF
	}
	return copy(p, c.rbuf[:n]), nil
}

// Write implements io.Writer (loops until p is written).
func (c *pipeConn) Write(p []byte) (int, error) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if !c.begin() {
		return 0, io.ErrClosedPipe
	}
	defer c.active.Done()
	total := 0
	for total < len(p) {
		_ = winapi.ResetEvent(c.wov.HEvent)
		chunk := copy(c.wbuf, p[total:])
		var n uint32
		err := syscall.WriteFile(c.h, c.wbuf[:chunk], &n, &c.wov)
		n, err = c.wait(&c.wov, err)
		total += int(n)
		if err != nil {
			if mapPipeErr(err) == io.EOF {
				return total, io.ErrClosedPipe
			}
			return total, err
		}
		if n == 0 {
			return total, io.ErrShortWrite
		}
	}
	return total, nil
}

// Close interrupts pending I/O, disconnects (server side) and closes.
func (c *pipeConn) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()
	_ = winapi.SetEvent(c.stop)
	c.active.Wait()
	if c.server {
		_ = winapi.DisconnectNamedPipe(c.h)
	}
	err := syscall.CloseHandle(c.h)
	syscall.CloseHandle(c.rov.HEvent)
	syscall.CloseHandle(c.wov.HEvent)
	syscall.CloseHandle(c.stop)
	return err
}

// PipeListener accepts clients on a named pipe (one instance per client,
// PIPE_UNLIMITED_INSTANCES).
type PipeListener struct {
	name   string
	sa     *syscall.SecurityAttributes
	freeSD func()
	stop   syscall.Handle

	// ov is the ConnectNamedPipe OVERLAPPED, on the heap for the whole
	// pending operation (Accept is called by one goroutine at a time).
	ov *syscall.Overlapped

	mu      sync.Mutex
	closed  bool
	pending syscall.Handle // next instance (created ahead)
	active  sync.WaitGroup
}

// ListenPipe creates the first pipe instance with
// FILE_FLAG_FIRST_PIPE_INSTANCE (fails if another server already owns the
// name) and the PipeSDDL DACL. sddl "" = PipeSDDL.
func ListenPipe(name, sddl string) (*PipeListener, error) {
	if sddl == "" {
		sddl = PipeSDDL
	}
	sa, free, err := winapi.SecurityAttributesFromSDDL(sddl)
	if err != nil {
		return nil, err
	}
	stop, err := winapi.CreateEvent(true, false)
	if err != nil {
		free()
		return nil, err
	}
	l := &PipeListener{name: name, sa: sa, freeSD: free, stop: stop, pending: syscall.InvalidHandle, ov: new(syscall.Overlapped)}
	h, err := l.create(true)
	if err != nil {
		free()
		syscall.CloseHandle(stop)
		return nil, err
	}
	l.pending = h
	return l, nil
}

func (l *PipeListener) create(first bool) (syscall.Handle, error) {
	mode := uint32(winapi.PipeAccessDuplex | winapi.FileFlagOverlapped)
	if first {
		mode |= winapi.FileFlagFirstPipeInst
	}
	return winapi.CreateNamedPipe(l.name, mode,
		winapi.PipeTypeByte|winapi.PipeReadmodeByte|winapi.PipeWait|winapi.PipeRejectRemoteClients,
		maxInstances, pipeBuf, pipeBuf, 0, l.sa)
}

// Accept implements Listener. It must not be called concurrently
// (ipc.Server calls it from its single accept loop).
func (l *PipeListener) Accept() (io.ReadWriteCloser, error) {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil, ErrListenerClosed
	}
	l.active.Add(1)
	h := l.pending
	l.pending = syscall.InvalidHandle
	l.mu.Unlock()
	defer l.active.Done()
	if h == syscall.InvalidHandle {
		var err error
		if h, err = l.create(false); err != nil {
			return nil, err
		}
	}
	ev, err := winapi.CreateEvent(true, false)
	if err != nil {
		syscall.CloseHandle(h)
		return nil, err
	}
	defer syscall.CloseHandle(ev)
	ov := l.ov
	*ov = syscall.Overlapped{HEvent: ev}
	err = winapi.ConnectNamedPipe(h, ov)
	switch err {
	case nil, winapi.ErrorPipeConnected:
	case winapi.ErrorIOPending:
		i, werr := winapi.WaitAny(ev, l.stop)
		if werr != nil || i == 1 {
			_ = syscall.CancelIoEx(h, ov)
			_, _ = winapi.GetOverlappedResult(h, ov, true)
			syscall.CloseHandle(h)
			return nil, ErrListenerClosed
		}
		if _, err = winapi.GetOverlappedResult(h, ov, true); err != nil {
			syscall.CloseHandle(h)
			return nil, err
		}
	default:
		syscall.CloseHandle(h)
		return nil, err
	}
	c, err := newPipeConn(h, true)
	if err != nil {
		_ = winapi.DisconnectNamedPipe(h)
		syscall.CloseHandle(h)
		return nil, err
	}
	return c, nil
}

// Close stops Accept and releases the pipe (connected clients are closed
// by their owners, i.e. ipc.Server.Close).
func (l *PipeListener) Close() error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	pending := l.pending
	l.pending = syscall.InvalidHandle
	l.mu.Unlock()
	_ = winapi.SetEvent(l.stop)
	l.active.Wait()
	if pending != syscall.InvalidHandle {
		syscall.CloseHandle(pending)
	}
	syscall.CloseHandle(l.stop)
	l.freeSD()
	return nil
}

// DialPipe connects to a pipe server, waiting up to timeout while all
// instances are busy or the server is not listening yet.
func DialPipe(name string, timeout time.Duration) (io.ReadWriteCloser, error) {
	n, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	for {
		h, err := syscall.CreateFile(n, clientAccess, 0, nil, syscall.OPEN_EXISTING,
			winapi.FileFlagOverlapped|winapi.SecuritySQOSPresent|winapi.SecurityIdentification, 0)
		if err == nil {
			c, cerr := newPipeConn(h, false)
			if cerr != nil {
				syscall.CloseHandle(h)
				return nil, cerr
			}
			return c, nil
		}
		left := time.Until(deadline)
		if left <= 0 || (err != winapi.ErrorPipeBusy && err != winapi.ErrorFileNotFound) {
			return nil, err
		}
		if err == winapi.ErrorPipeBusy {
			ms := uint32(left / time.Millisecond)
			if ms == 0 {
				ms = 1
			}
			_ = winapi.WaitNamedPipe(name, ms)
		} else {
			time.Sleep(20 * time.Millisecond)
		}
	}
}
