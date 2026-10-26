package trayipc

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/ipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// State is the connection state of the session.
type State int

const (
	// StateConnecting: dialing / waiting for hello (initial state).
	StateConnecting State = iota
	// StateConnected: hello received, commands can be sent.
	StateConnected
	// StateNotRunning: the pipe does not exist (service stopped / not
	// installed).
	StateNotRunning
	// StateUnavailable: dial failed for another reason, or the
	// connection was lost.
	StateUnavailable
	// StateNotResponding: connected, but no hello / pong / reply in time.
	StateNotResponding
	// StateUntrusted: the pipe server is not the service process (G7).
	StateUntrusted
)

func (s State) String() string {
	switch s {
	case StateConnecting:
		return "connecting"
	case StateConnected:
		return "connected"
	case StateNotRunning:
		return "not_running"
	case StateUnavailable:
		return "unavailable"
	case StateNotResponding:
		return "not_responding"
	case StateUntrusted:
		return "untrusted"
	}
	return "unknown"
}

// Errors.
var (
	// ErrServiceNotRunning is returned (wrapped) by a Dialer when the pipe
	// does not exist.
	ErrServiceNotRunning = errors.New("trayipc: service pipe not found")
	// ErrNotConnected: no connection right now.
	ErrNotConnected = errors.New("trayipc: not connected to the service")
	// ErrUnsupported: the service does not support the command (protocol
	// version 1, capability missing or unknown_command reply).
	ErrUnsupported = errors.New("trayipc: command not supported by the service")
)

// Status is a copy of the session state.
type Status struct {
	State State
	// Err is the last dial / verify / connection error (nil when
	// connected).
	Err error
	// Version and Capabilities from hello (0 / nil before the first one).
	Version      int
	Capabilities []string
	// Heartbeat is the latest heartbeat (hello, then every pong); nil for
	// a version-1 service.
	Heartbeat *model.Heartbeat
	// ServiceStartedAt from hello.heartbeat (zero for version 1).
	ServiceStartedAt time.Time
	// Restarts counts detected service restarts (serviceStartedAt changed).
	Restarts int
	// Snapshot is the latest status snapshot (kept while disconnected:
	// the UI shows it as last known data).
	Snapshot *model.StatusSnapshot
	// EverConnected: at least one hello was received.
	EverConnected bool
	// ClientElevated from the last get_status / ping reply of this
	// connection (phase 6, display only; nil = not reported): this
	// process runs as an elevated administrator. Admin commands are
	// re-checked by the service every time.
	ClientElevated *bool
	// TrayAutostartError: trayAutostartError of the last get_status reply
	// of this connection (capability tray_autostart_status, CHK-CFG-04;
	// "" = applied or not reported). Not carried by status_update / pong.
	TrayAutostartError string
}

// Elevated reports ClientElevated == true.
func (s Status) Elevated() bool { return s.ClientElevated != nil && *s.ClientElevated }

// Has reports whether the service announced capability c.
func (s Status) Has(c string) bool {
	for _, x := range s.Capabilities {
		if x == c {
			return true
		}
	}
	return false
}

// EventKind of an Event.
type EventKind int

const (
	// EventState: Status changed (state, hello, heartbeat).
	EventState EventKind = iota
	// EventStatus: a new snapshot (Status.Snapshot).
	EventStatus
	// EventAlert: an alert push (Alert).
	EventAlert
	// EventAlertRepeat: an alert_repeat push (phase 10, capability
	// alert_repeat; push only, never in events_since). Alert carries the
	// repeat (Kind alert_repeat, RepeatOf, Level, CheckSeq, Acknowledged).
	EventAlertRepeat
	// EventBrowseProgress / EventTestStepProgress: flat progress pushes of
	// sql_browse / sql_test_steps (phase 11, CHK-TRAY-52); Message carries
	// Ref + BrowseProgress / TestStep.
	EventBrowseProgress
	EventTestStepProgress
)

// Event is delivered to Options.OnEvent.
type Event struct {
	Kind   EventKind
	Status Status
	Alert  *model.AlertEvent
	// Message: progress push (BrowseProgress / TestStep); nil otherwise.
	Message *model.IPCMessage
}

// Dialer opens a connection to the service.
type Dialer func(ctx context.Context) (io.ReadWriteCloser, error)

// Options configure a Session. Zero durations mean the defaults.
type Options struct {
	Dial Dialer
	// Verify checks the connection before the first byte is sent (G7:
	// pipe server PID == service PID); nil = no check (tests).
	Verify func(io.ReadWriteCloser) error
	// OnEvent receives events on the dispatcher goroutine (may block; the
	// pipe reader is never blocked by it).
	OnEvent func(Event)

	Heartbeat    time.Duration // ping period, default model.IPCHeartbeat
	PingTimeout  time.Duration // default 10 s
	HelloTimeout time.Duration // default 5 s
	CallTimeout  time.Duration // get_status after hello, default 10 s
	BackoffMin   time.Duration // default 1 s
	BackoffMax   time.Duration // default 30 s

	// After is the timer source (tests inject a fake clock); nil =
	// time.After.
	After func(time.Duration) <-chan time.Time
}

// Defaults.
const (
	DefaultPingTimeout  = 10 * time.Second
	DefaultHelloTimeout = 5 * time.Second
	DefaultCallTimeout  = 10 * time.Second
	DefaultBackoffMin   = 1 * time.Second
	DefaultBackoffMax   = 30 * time.Second
)

// Session is a reconnecting connection to the service.
type Session struct {
	opt Options

	mu       sync.Mutex
	st       Status
	client   *ipc.Client
	disabled map[string]bool // commands answered unknown_command

	evMu   sync.Mutex
	evCond *sync.Cond
	events []Event
	closed bool

	// progress: UI callback for in-flight sql_browse / sql_test_steps
	// (CHK-TRAY-52); called from onPush on the reader path — must not block.
	progMu   sync.Mutex
	progress func(model.IPCMessage)
}

// New returns a session; call Run to connect.
func New(opt Options) *Session {
	if opt.Heartbeat <= 0 {
		opt.Heartbeat = model.IPCHeartbeat
	}
	if opt.PingTimeout <= 0 {
		opt.PingTimeout = DefaultPingTimeout
	}
	if opt.HelloTimeout <= 0 {
		opt.HelloTimeout = DefaultHelloTimeout
	}
	if opt.CallTimeout <= 0 {
		opt.CallTimeout = DefaultCallTimeout
	}
	if opt.BackoffMin <= 0 {
		opt.BackoffMin = DefaultBackoffMin
	}
	if opt.BackoffMax < opt.BackoffMin {
		opt.BackoffMax = DefaultBackoffMax
		if opt.BackoffMax < opt.BackoffMin {
			opt.BackoffMax = opt.BackoffMin
		}
	}
	if opt.After == nil {
		opt.After = time.After
	}
	s := &Session{opt: opt, disabled: map[string]bool{}}
	s.evCond = sync.NewCond(&s.evMu)
	return s
}

// Status returns a copy of the current state.
func (s *Session) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.copyLocked()
}

func (s *Session) copyLocked() Status {
	st := s.st
	st.Capabilities = append([]string(nil), s.st.Capabilities...)
	if s.st.Heartbeat != nil {
		hb := *s.st.Heartbeat
		st.Heartbeat = &hb
	}
	if s.st.ClientElevated != nil {
		v := *s.st.ClientElevated
		st.ClientElevated = &v
	}
	return st
}

// setElevatedLocked stores clientElevated of a get_status / ping reply.
func (s *Session) setElevatedLocked(v *bool) {
	if v != nil {
		b := *v
		s.st.ClientElevated = &b
	}
}

// commandCapability maps version-2 commands to the capability that the
// service must announce. Commands of version 1 need nothing.
var commandCapability = map[string]string{
	model.CmdTestProfile: model.CapTestProfile,
	model.CmdAckAlert:    model.CapAckAlert,
	// phase 6 (G5 / G6)
	model.CmdGetConfig:     model.CapConfigRead,
	model.CmdSaveProfile:   model.CapConfigWrite,
	model.CmdDeleteProfile: model.CapConfigWrite,
	model.CmdSaveSettings:  model.CapConfigWrite,
	model.CmdCancel:        model.CapCancel,
	// phase 9 (EC Service; tray phase 9b)
	model.CmdHistory:     model.CapHistory,
	model.CmdCheckNow:    model.CapCheckNow,
	model.CmdEventsSince: model.CapEventsSince,
	// phase 11 wizard (CHK-TRAY-52 / CHK-IPC-12/13)
	model.CmdSQLBrowse:    model.CapSQLBrowse,
	model.CmdSQLDatabases: model.CapSQLDatabases,
	model.CmdSQLDiagnose:  model.CapSQLDiagnose,
	model.CmdSQLGrantScript: model.CapSQLGrantScript,
	model.CmdSQLTestSteps: model.CapTestSteps,
	// phase 11 (CHK-TRAY-53)
	model.CmdAlarmTest:   model.CapAlarmTest,
	model.CmdSummary:     model.CapSummary,
	model.CmdClientHello: model.CapClientHello,
}

var v1Commands = map[string]bool{
	model.CmdPing: true, model.CmdGetStatus: true, model.CmdReloadConfig: true, model.CmdPollNow: true,
}

// Supported reports whether cmd may be sent to the connected service:
// version-1 commands always; a version-2 command only to a service with
// protocol >= 2, with its capability (if it has one) and not answered
// unknown_command since the last hello.
func (s *Session) Supported(cmd string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.supportedLocked(cmd)
}

func (s *Session) supportedLocked(cmd string) bool {
	if s.disabled[cmd] {
		return false
	}
	if v1Commands[cmd] {
		return true
	}
	if s.st.Version < 2 {
		return false
	}
	if c, ok := commandCapability[cmd]; ok {
		return s.st.Has(c)
	}
	return true
}

// IsUnknownCommand reports whether m is the reply of a service that does
// not know the command: code unknown_command (version 2) or the version-1
// error text "unknown command: ..." without a code.
func IsUnknownCommand(m model.IPCMessage) bool {
	if m.Type != model.MsgError {
		return false
	}
	if m.Code == model.ErrCodeUnknownCommand {
		return true
	}
	return m.Code == "" && strings.HasPrefix(m.Error, "unknown command:")
}

// Do sends req to the service and waits for the reply until ctx is done.
// Unsupported commands are refused locally with ErrUnsupported (nothing
// is sent); an unknown_command reply disables the command until the next
// hello and also returns ErrUnsupported (with the reply).
func (s *Session) Do(ctx context.Context, req model.IPCRequest) (model.IPCMessage, error) {
	s.mu.Lock()
	c := s.client
	if c == nil || s.st.State != StateConnected {
		s.mu.Unlock()
		return model.IPCMessage{}, ErrNotConnected
	}
	if !s.supportedLocked(req.Cmd) {
		s.mu.Unlock()
		return model.IPCMessage{}, ErrUnsupported
	}
	s.mu.Unlock()
	m, err := c.Do(ctx, req)
	if err != nil {
		return m, err
	}
	if IsUnknownCommand(m) {
		s.mu.Lock()
		if s.client == c {
			s.disabled[req.Cmd] = true
		}
		s.mu.Unlock()
		return m, ErrUnsupported
	}
	return m, nil
}

// Call is Do with a bare command.
func (s *Session) Call(ctx context.Context, cmd string) (model.IPCMessage, error) {
	return s.Do(ctx, model.IPCRequest{Cmd: cmd})
}

// Refresh re-reads the status (get_status) and publishes it, e.g. after
// ack_alert changed the alerts (phase 6b).
func (s *Session) Refresh(ctx context.Context) error {
	m, err := s.Call(ctx, model.CmdGetStatus)
	if err != nil {
		return err
	}
	if m.Type != model.MsgStatus || m.Status == nil {
		return errors.New("trayipc: bad get_status reply: " + m.Type)
	}
	s.mu.Lock()
	s.setElevatedLocked(m.ClientElevated)
	s.st.TrayAutostartError = m.TrayAutostartError // get_status only
	s.mu.Unlock()
	s.applySnapshot(*m.Status, true)
	return nil
}

// DoCancel is Do for an asynchronous command (test_profile): when ctx
// ends before the reply, the client sends cancel {ref} to the service
// (G6, only if the service announced the cancel capability). Request IDs
// are never reused (ipc.Client numbers them).
func (s *Session) DoCancel(ctx context.Context, req model.IPCRequest) (model.IPCMessage, error) {
	s.mu.Lock()
	c := s.client
	if c == nil || s.st.State != StateConnected {
		s.mu.Unlock()
		return model.IPCMessage{}, ErrNotConnected
	}
	if !s.supportedLocked(req.Cmd) {
		s.mu.Unlock()
		return model.IPCMessage{}, ErrUnsupported
	}
	cancel := s.supportedLocked(model.CmdCancel)
	s.mu.Unlock()
	var m model.IPCMessage
	var err error
	if cancel {
		m, err = c.DoCancel(ctx, req)
	} else {
		m, err = c.Do(ctx, req)
	}
	if err == nil && IsUnknownCommand(m) {
		s.mu.Lock()
		if s.client == c {
			s.disabled[req.Cmd] = true
		}
		s.mu.Unlock()
		return m, ErrUnsupported
	}
	return m, err
}

// SetProgress registers fn for sql_browse_progress / sql_test_steps_progress
// pushes (CHK-TRAY-52). fn must not block (post to the UI thread). nil clears.
func (s *Session) SetProgress(fn func(model.IPCMessage)) {
	s.progMu.Lock()
	s.progress = fn
	s.progMu.Unlock()
}

// Run connects and keeps the session alive until ctx is done.
func (s *Session) Run(ctx context.Context) {
	done := make(chan struct{})
	go s.dispatch(done)
	defer func() {
		s.evMu.Lock()
		s.closed = true
		s.evCond.Broadcast()
		s.evMu.Unlock()
		<-done
	}()
	var delay time.Duration
	for ctx.Err() == nil {
		ok := s.connectOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		if ok {
			delay = 0
		}
		if delay == 0 {
			delay = s.opt.BackoffMin
		} else if delay *= 2; delay > s.opt.BackoffMax {
			delay = s.opt.BackoffMax
		}
		select {
		case <-ctx.Done():
			return
		case <-s.opt.After(delay):
		}
	}
}

// setState updates the state and emits EventState.
func (s *Session) setState(state State, err error) {
	s.mu.Lock()
	changed := s.st.State != state || errText(s.st.Err) != errText(err)
	s.st.State, s.st.Err = state, err
	st := s.copyLocked()
	s.mu.Unlock()
	if changed {
		s.emit(Event{Kind: EventState, Status: st})
	}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// connectOnce runs one connection; it reports whether a hello was
// received (the backoff is reset then).
func (s *Session) connectOnce(ctx context.Context) bool {
	// the previous state (not running / unavailable / ...) is kept while
	// reconnecting, so the UI does not flicker on every attempt
	rw, err := s.opt.Dial(ctx)
	if err != nil {
		if errors.Is(err, ErrServiceNotRunning) {
			s.setState(StateNotRunning, err)
		} else {
			s.setState(StateUnavailable, err)
		}
		return false
	}
	if s.opt.Verify != nil {
		if err := s.opt.Verify(rw); err != nil {
			_ = rw.Close()
			s.setState(StateUntrusted, err)
			return false
		}
	}
	hello := make(chan model.IPCMessage, 1)
	var gotHello bool
	var hmu sync.Mutex
	c := ipc.NewClient(rw, func(m model.IPCMessage) {
		if m.Type == model.MsgHello {
			hmu.Lock()
			first := !gotHello
			gotHello = true
			hmu.Unlock()
			if first {
				hello <- m
			}
			return
		}
		s.onPush(m)
	})
	defer c.Close()

	var h model.IPCMessage
	select {
	case <-ctx.Done():
		return false
	case h = <-hello:
	case <-c.Done():
		s.setState(StateUnavailable, errors.New("trayipc: connection closed before hello"))
		return false
	case <-s.opt.After(s.opt.HelloTimeout):
		s.setState(StateNotResponding, errors.New("trayipc: no hello from the service"))
		return false
	}
	s.onHello(c, h)
	s.sendClientHello(ctx, c)

	// snapshot right after hello
	if !s.refreshStatus(ctx, c) {
		return true
	}
	for {
		select {
		case <-ctx.Done():
			return true
		case <-c.Done():
			s.dropClient(c, StateUnavailable, errors.New("trayipc: connection to the service lost"))
			return true
		case <-s.opt.After(s.opt.Heartbeat):
			if !s.ping(ctx, c) {
				return true
			}
		}
	}
}

// sendClientHello declares tray capabilities (alarm_test) so the service
// can push test alerts only to us (CHK-TRAY-53 / CHK-IPC-11). Skipped on
// version 1 or when the service has no client_hello / alarm_test.
func (s *Session) sendClientHello(ctx context.Context, c *ipc.Client) {
	st := s.Status()
	if st.Version < 2 || !(st.Has(model.CapClientHello) || st.Has(model.CapAlarmTest)) {
		return
	}
	m, err := c.Do(ctx, model.IPCRequest{Cmd: model.CmdClientHello, Capabilities: []string{model.ClientCapAlarmTest, model.CapSummary, model.CapAckEvent}})
	if err != nil || m.Type == model.MsgError {
		return // older service / unknown_command: stay silent
	}
}

func (s *Session) onHello(c *ipc.Client, h model.IPCMessage) {
	s.mu.Lock()
	ver := h.Version
	if ver <= 0 {
		ver = 1
	}
	s.st.Version = ver
	s.st.Capabilities = append([]string(nil), h.Capabilities...)
	s.st.ClientElevated = nil // per connection, from get_status / ping
	s.st.TrayAutostartError = ""
	s.disabled = map[string]bool{}
	if h.Heartbeat != nil {
		hb := *h.Heartbeat
		s.st.Heartbeat = &hb
		if !s.st.ServiceStartedAt.IsZero() && !hb.ServiceStartedAt.Equal(s.st.ServiceStartedAt) {
			s.st.Restarts++
		}
		s.st.ServiceStartedAt = hb.ServiceStartedAt
	} else {
		s.st.Heartbeat = nil
	}
	s.st.EverConnected = true
	s.st.State, s.st.Err = StateConnected, nil
	s.client = c
	st := s.copyLocked()
	s.mu.Unlock()
	s.emit(Event{Kind: EventState, Status: st})
}

func (s *Session) dropClient(c *ipc.Client, state State, err error) {
	s.mu.Lock()
	if s.client == c {
		s.client = nil
	}
	s.mu.Unlock()
	s.setState(state, err)
}

// callTimeout runs one request with a timeout from the session clock.
func (s *Session) callTimeout(ctx context.Context, c *ipc.Client, cmd string, d time.Duration) (model.IPCMessage, error) {
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	t := s.opt.After(d)
	go func() {
		select {
		case <-t:
			cancel()
		case <-cctx.Done():
		}
	}()
	return c.Call(cctx, cmd)
}

func (s *Session) refreshStatus(ctx context.Context, c *ipc.Client) bool {
	m, err := s.callTimeout(ctx, c, model.CmdGetStatus, s.opt.CallTimeout)
	if err != nil || m.Type != model.MsgStatus || m.Status == nil {
		if ctx.Err() == nil {
			if err == nil {
				err = errors.New("trayipc: bad get_status reply: " + m.Type)
			}
			s.dropClient(c, StateNotResponding, err)
		}
		return false
	}
	s.mu.Lock()
	s.setElevatedLocked(m.ClientElevated)
	s.st.TrayAutostartError = m.TrayAutostartError // get_status only
	s.mu.Unlock()
	s.applySnapshot(*m.Status, true)
	return true
}

func (s *Session) ping(ctx context.Context, c *ipc.Client) bool {
	m, err := s.callTimeout(ctx, c, model.CmdPing, s.opt.PingTimeout)
	if err != nil || m.Type != model.MsgPong {
		if ctx.Err() == nil {
			if err == nil {
				err = errors.New("trayipc: bad ping reply: " + m.Type)
			}
			s.dropClient(c, StateNotResponding, err)
		}
		return false
	}
	if m.Heartbeat != nil || m.ClientElevated != nil {
		s.mu.Lock()
		if m.Heartbeat != nil {
			hb := *m.Heartbeat
			s.st.Heartbeat = &hb
		}
		s.setElevatedLocked(m.ClientElevated)
		st := s.copyLocked()
		s.mu.Unlock()
		s.emit(Event{Kind: EventState, Status: st})
	}
	return true
}

// applySnapshot stores a snapshot: the reply of get_status always (new
// connection, possibly a restarted service with a new sequence), pushes
// only when not older than the current one.
func (s *Session) applySnapshot(snap model.StatusSnapshot, force bool) {
	s.mu.Lock()
	if !force && s.st.Snapshot != nil && snap.Seq < s.st.Snapshot.Seq {
		s.mu.Unlock()
		return
	}
	cp := snap
	s.st.Snapshot = &cp
	st := s.copyLocked()
	s.mu.Unlock()
	s.emit(Event{Kind: EventStatus, Status: st})
}

func (s *Session) onPush(m model.IPCMessage) {
	switch m.Type {
	case model.MsgStatus:
		if m.Status != nil {
			s.applySnapshot(*m.Status, false)
		}
	case model.MsgAlert:
		if m.Alert != nil {
			a := *m.Alert
			s.emit(Event{Kind: EventAlert, Status: s.Status(), Alert: &a})
		}
	case model.MsgAlertRepeat:
		if st := s.Status(); m.Alert != nil && st.Has(model.CapAlertRepeat) {
			a := *m.Alert
			s.emit(Event{Kind: EventAlertRepeat, Status: st, Alert: &a})
		}
	case model.MsgSQLBrowseProgress, model.MsgSQLTestStepsProgress:
		s.progMu.Lock()
		cb := s.progress
		s.progMu.Unlock()
		if cb != nil {
			cb(m)
		}
		cp := m
		kind := EventBrowseProgress
		if m.Type == model.MsgSQLTestStepsProgress {
			kind = EventTestStepProgress
		}
		s.emit(Event{Kind: kind, Status: s.Status(), Message: &cp})
	}
}

// emit queues an event (never blocks).
func (s *Session) emit(e Event) {
	if s.opt.OnEvent == nil {
		return
	}
	s.evMu.Lock()
	if !s.closed {
		s.events = append(s.events, e)
		s.evCond.Signal()
	}
	s.evMu.Unlock()
}

func (s *Session) dispatch(done chan struct{}) {
	defer close(done)
	for {
		s.evMu.Lock()
		for len(s.events) == 0 && !s.closed {
			s.evCond.Wait()
		}
		if len(s.events) == 0 && s.closed {
			s.evMu.Unlock()
			return
		}
		evs := s.events
		s.events = nil
		s.evMu.Unlock()
		for _, e := range evs {
			s.opt.OnEvent(e)
		}
	}
}
