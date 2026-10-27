package ipc

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Listener accepts client connections (PipeListener on Windows; in-memory
// listeners in tests).
type Listener interface {
	Accept() (io.ReadWriteCloser, error)
	Close() error
}

// ErrListenerClosed is returned by Accept after Close.
var ErrListenerClosed = errors.New("ipc: listener closed")

// Handler is the service side (the monitor). PollNow / ReloadConfig are
// rate-limited by the handler; coalesced == true is answered with
// model.ResultCoalesced (CHK-MON-12).
type Handler interface {
	Snapshot() model.StatusSnapshot
	// PollNow polls every profile (profileID "") or one profile.
	PollNow(profileID string) (coalesced bool, err error)
	ReloadConfig() (coalesced bool)
}

// Optional handler interfaces (protocol version 2). A handler without
// them serves version-1 commands only; Hello.Capabilities lists what it
// supports and the other version-2 commands answer unknown_command.

// Tester runs test_profile (CHK-IPC-03). Called on its own goroutine: a
// test may take up to its timeout and must not block the client's other
// requests (ping). Errors of type *model.IPCError carry the reply code.
type Tester interface {
	TestProfile(ctx context.Context, req model.IPCRequest) (model.TestProfileResult, error)
}

// Acker runs ack_alert (CHK-IPC-02).
type Acker interface {
	AckAlert(profileID, pointKey string) (int, error)
}

// Muter runs mute_until_recovered / unmute_profile (phase 12, CHK-MON-20 /
// CHK-IPC-15). Access level is the same as ack_alert (any local client).
type Muter interface {
	MuteUntilRecovered(profileID string) error
	UnmuteProfile(profileID string) error
}

// EventAcker runs ack_alert {eventId} (phase 11, CHK-MON-18, capability
// ack_event): only that alert.
type EventAcker interface {
	AckEvent(eventID uint64) (int, error)
}

// Checker runs check_now (phase 9, CHK-SVC-04). Errors of type
// *model.IPCError carry the reply code (rate_limited + RetryAfterMs).
type Checker interface {
	CheckNow(profileID string) (model.CheckResult, error)
}

// EventFeed serves events_since (phase 9, CHK-SVC-05).
type EventFeed interface {
	EventsSince(runID string, since uint64, limit int) model.EventsPage
}

// HistorySource serves the history command (phase 9, CHK-SVC-03): called
// on its own goroutine (cancel {ref}); errors of type *model.IPCError
// carry the reply code.
type HistorySource interface {
	History(ctx context.Context, req model.IPCRequest) (model.HistoryPage, error)
}

// HeartbeatSource fills the heartbeat of Hello / Pong (CHK-IPC-04).
type HeartbeatSource interface {
	Heartbeat() model.Heartbeat
}

// ConfigManager runs the admin-only configuration commands (phase 6,
// CHK-IPC-06): the service is the only writer of the configuration. The
// server checks the client (Authorize) before every call; account is the
// client's DOMAIN\user for the log / event 105. Errors of type
// *model.IPCError carry the reply code, key and args.
type ConfigManager interface {
	GetConfig() (model.ConfigView, string, error)
	SaveProfile(p model.Profile, password string, clearPassword bool, expectedRevision, account string) (model.SavedProfile, error)
	DeleteProfile(profileID, expectedRevision, account string) (revision string, err error)
	SaveSettings(st model.Settings, expectedRevision, account string) (revision string, err error)
}

// ProfileCloner runs clone_profile (phase 12, CHK-CFG-08, admin only).
// Optional beside ConfigManager; Monitor implements both.
type ProfileCloner interface {
	CloneProfile(profileID, newID, newName, expectedRevision, account string) (model.SavedProfile, bool, error)
}

// ConfigTransfer runs export_config / import_config (admin only; phase
// 10, CHK-CFG-06). import_config requests may be up to
// model.IPCMaxImportRequest (1 MiB) from an elevated administrator; every
// other request keeps model.IPCMaxRequest.
type ConfigTransfer interface {
	ExportConfig() (model.ConfigExport, error)
	ImportConfig(cfg *model.ConfigExport, mode, expectedRevision string, dryRun bool, account string) (model.ImportResult, error)
}

// ConfigViberRecipientsTransfer: export_config / import_config with
// includeViberRecipients (phase 12, CHK-CFG-07 / CHK-IPC-14). Optional;
// when absent the flag is ignored (export omits viber, import keeps it).
type ConfigViberRecipientsTransfer interface {
	ExportConfigOpts(includeViberRecipients bool) (model.ConfigExport, error)
	ImportConfigOpts(cfg *model.ConfigExport, mode, expectedRevision string, dryRun, includeViberRecipients bool, account string) (model.ImportResult, error)
}

// ConfigEmailRecipientsTransfer: includeEmailRecipients (phase 13b,
// CHK-CFG-09). Preferred over ConfigViberRecipientsTransfer when present
// so both include flags are applied together.
type ConfigEmailRecipientsTransfer interface {
	ExportConfigBoth(includeViberRecipients, includeEmailRecipients bool) (model.ConfigExport, error)
	ImportConfigBoth(cfg *model.ConfigExport, mode, expectedRevision string, dryRun, includeViberRecipients, includeEmailRecipients bool, account string) (model.ImportResult, error)
}

// Discoverer runs the admin-only interactive SQL commands (phase 6,
// G2/G3; CHK-IPC-08 discover / discover_by_value / list_node_points,
// CHK-IPC-09 check_mapping). The server checks the client (admin) before
// every call and runs it on its own goroutine like test_profile (ping of
// the same client is not blocked; cancel {ref} and a disconnect cancel
// ctx). The string results are the truncated reason (model.Truncated*,
// "" = complete). Errors of type *model.IPCError carry the reply code,
// key and args.
type Discoverer interface {
	Discover(ctx context.Context, req model.IPCRequest) ([]model.DiscoveryCandidate, string, error)
	DiscoverByValue(ctx context.Context, req model.IPCRequest) ([]model.DiscoveryCandidate, string, error)
	ListNodePoints(ctx context.Context, req model.IPCRequest) ([]model.NodePoint, string, error)
	CheckMapping(ctx context.Context, req model.IPCRequest) (model.MappingCheck, error)
}

// SQLWizard runs the admin-only connection wizard commands of phase 9
// (CHK-IPC-10) that need I/O: sql_browse and sql_databases (async, cancel
// {ref}). sql_diagnose and sql_grant_script are answered by the server
// itself (model, no I/O) when the handler is a SQLWizard.
type SQLWizard interface {
	SQLBrowse(ctx context.Context, req model.IPCRequest) (model.SQLBrowseResult, error)
	SQLDatabases(ctx context.Context, req model.IPCRequest) (model.SQLDatabasesResult, error)
}

// SQLWizardV2 (phase 11, CHK-IPC-12/13): sql_browse with modes and
// progress, sql_test_steps with stage pushes. Capabilities browse_subnet,
// browse_local, test_steps.
type SQLWizardV2 interface {
	SQLBrowseProgress(ctx context.Context, req model.IPCRequest, progress func(model.SQLBrowseProgress)) (model.SQLBrowseResult, error)
	SQLTestSteps(ctx context.Context, req model.IPCRequest, step func(model.SQLCheckStep)) (model.SQLServerCheckResult, error)
}

// interactiveCommand reports whether cmd is one of the Discoverer
// commands.
func interactiveCommand(cmd string) bool {
	switch cmd {
	case model.CmdDiscover, model.CmdDiscoverByValue, model.CmdListNodePoints, model.CmdCheckMapping:
		return true
	}
	return false
}

// Peer is the identity of a connected client (phase 6).
type Peer struct {
	// Admin: elevated member of BUILTIN\Administrators.
	Admin bool
	// Account: DOMAIN\user ("" if unknown).
	Account string
}

// clientIdentifier is implemented by transports that can identify the
// client (the Windows pipe: ImpersonateNamedPipeClient).
type clientIdentifier interface {
	ClientIdentity() (Peer, error)
}

// ErrAccessDenied: admin command from a non-elevated client.
var ErrAccessDenied = &model.IPCError{Code: model.ErrCodeAccessDenied, Key: model.MsgMonitorAccessDenied,
	Message: "ipc: administrator rights required"}

// Logger is the line logger.
type Logger interface {
	Printf(format string, args ...interface{})
}

type nopLogger struct{}

func (nopLogger) Printf(string, ...interface{}) {}

// outQueue is the per-client push queue length; a client that does not
// read is disconnected instead of blocking the service.
const outQueue = 64

// Server serves the protocol to at most MaxClients clients at a time.
type Server struct {
	H   Handler
	Log Logger
	// closeHook (tests) runs in Close after the cancel, before the
	// clients are disconnected.
	closeHook func()
	// Limits (CHK-MON-11); NewServer sets the model.IPC* defaults.
	MaxRequest  int           // request frame limit (bytes)
	MaxClients  int           // concurrent connections
	ReadTimeout time.Duration // rest of a started request frame
	IdleTimeout time.Duration // no request at all
	// Accept failure backoff / log period (defaults AcceptBackoff*,
	// AcceptLogEvery; tests shorten them).
	// Zero values mean the defaults, also for a Server built without
	// NewServer.
	BackoffMin, BackoffMax, LogEvery time.Duration

	// clock of the accept loop (tests inject fakes; nil = time.Now /
	// time.After)
	now   func() time.Time
	after func(time.Duration) <-chan time.Time

	mu     sync.Mutex
	l      Listener
	conns  map[*conn]struct{}
	closed bool
	stop   chan struct{}
	slots  chan struct{}
	wg     sync.WaitGroup
	// ctx is canceled by Close (running test_profile calls end)
	ctx    context.Context
	cancel context.CancelFunc

	// Authorize identifies the client of a connection for admin commands
	// and clientElevated (phase 6). nil = the transport's own
	// ClientIdentity (Windows pipe), else "not an administrator". An
	// error means "not an administrator".
	Authorize func(rw io.ReadWriteCloser) (Peer, error)
	// TrayAutostartError returns the model.AutostartErr* code of the last
	// application of trayAutostart to HKLM Run ("" = ok) for get_status
	// replies (CHK-CFG-04); set only when the service manages the Run
	// value, which also announces CapTrayAutostartStatus.
	TrayAutostartError func() string
	// History serves the history command (phase 9); nil = no capability.
	History HistorySource
	// Viber (phase 10, CHK-SVC-06): viber_test and capability viber; nil
	// = not offered.
	Viber ViberTester
	// Email (phase 13b, CHK-IPC-16 / CHK-SVC-08): email_test and capability
	// email; nil = not offered.
	Email EmailTester
	// AlarmTest (phase 11, CHK-IPC-11): alarm_test and capability
	// alarm_test; nil = not offered.
	AlarmTest AlarmTester
	// Summary (phase 11, CHK-SVC-07): summary and capability summary;
	// nil = not offered.
	Summary     SummarySource
	alarmTestAt time.Time // last accepted alarm_test (s.mu)

	histN int // running history requests (s.mu)
}

type conn struct {
	rw      io.ReadWriteCloser
	out     chan model.IPCMessage
	once    sync.Once
	done    chan struct{}
	relOnce sync.Once
	release func()

	// asynchronous requests of this connection by request ID (G6 cancel,
	// CHK-IPC-07)
	opsMu sync.Mutex
	ops   map[uint64]*asyncOp
	// clientElevated of get_status / ping replies (display only; computed
	// once per connection, admin commands re-check every time)
	elevOnce sync.Once
	elev     bool

	// phase 9: running history requests of this connection (Server.mu),
	// last check_now accepted from it (read loop only)
	histN     int
	lastCheck time.Time
	// clientCaps: client_hello capabilities (phase 11, CHK-IPC-11; read
	// loop writes, broadcasts read: capMu)
	capMu      sync.Mutex
	clientCaps map[string]bool
}

type asyncOp struct {
	cancel   context.CancelFunc
	canceled bool // set by cancel {ref} (guarded by conn.opsMu)
}

func (c *conn) close() {
	c.once.Do(func() {
		close(c.done)
		_ = c.rw.Close()
	})
}

// send queues m; false when the queue is full or the connection closed.
func (c *conn) send(m model.IPCMessage) bool {
	select {
	case <-c.done:
		return false
	default:
	}
	select {
	case c.out <- m:
		return true
	case <-c.done:
		return false
	default:
		return false
	}
}

// NewServer returns a server for h.
func NewServer(h Handler, log Logger) *Server {
	if log == nil {
		log = nopLogger{}
	}
	return &Server{H: h, Log: log, conns: map[*conn]struct{}{}, stop: make(chan struct{}),
		MaxRequest: model.IPCMaxRequest, MaxClients: model.IPCMaxClients,
		ReadTimeout: model.IPCReadTimeout, IdleTimeout: model.IPCIdleTimeout,
		BackoffMin: AcceptBackoffMin, BackoffMax: AcceptBackoffMax, LogEvery: AcceptLogEvery}
}

// Accept failure handling (CHK-MON-11): exponential backoff from
// AcceptBackoffMin, doubling up to AcceptBackoffMax, reset after a
// successful Accept; the failure is logged once, then only when the error
// text changes or at most every AcceptLogEvery (with the number of
// suppressed repeats).
const (
	AcceptBackoffMin = 200 * time.Millisecond
	AcceptBackoffMax = 30 * time.Second
	AcceptLogEvery   = 10 * time.Minute
)

// initLocked fills what NewServer would have set, so a Server literal
// works too (s.mu held).
func (s *Server) initLocked() {
	if s.Log == nil {
		s.Log = nopLogger{}
	}
	if s.conns == nil {
		s.conns = map[*conn]struct{}{}
	}
	if s.stop == nil {
		s.stop = make(chan struct{})
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.after == nil {
		s.after = time.After
	}
	if s.ctx == nil {
		s.ctx, s.cancel = context.WithCancel(context.Background())
	}
}

// Capabilities lists the version-2 features of the handler.
func (s *Server) Capabilities() []string {
	var caps []string
	if _, ok := s.H.(Tester); ok {
		caps = append(caps, model.CapTestProfile)
	}
	if _, ok := s.H.(Acker); ok {
		caps = append(caps, model.CapAckAlert)
	}
	if _, ok := s.H.(Muter); ok {
		caps = append(caps, model.CapMuteProfile) // phase 12 (CHK-IPC-15)
	}
	if _, ok := s.H.(HeartbeatSource); ok {
		caps = append(caps, model.CapHeartbeat)
	}
	_, tester := s.H.(Tester)
	_, disc := s.H.(Discoverer)
	_, wiz := s.H.(SQLWizard)
	if tester || disc || wiz || s.History != nil {
		caps = append(caps, model.CapCancel)
	}
	if _, ok := s.H.(ConfigManager); ok {
		caps = append(caps, model.CapConfigRead, model.CapConfigWrite)
	}
	if _, ok := s.H.(ProfileCloner); ok {
		caps = append(caps, model.CapCloneProfile) // phase 12 (CHK-CFG-08)
	}
	if disc {
		caps = append(caps, model.CapDiscover, model.CapDiscoverByValue, model.CapNodePoints, model.CapCheckMapping)
	}
	if s.TrayAutostartError != nil {
		caps = append(caps, model.CapTrayAutostartStatus)
	}
	if s.History != nil {
		caps = append(caps, model.CapHistory)
	}
	if _, ok := s.H.(Checker); ok {
		caps = append(caps, model.CapCheckNow)
	}
	if _, ok := s.H.(EventFeed); ok {
		caps = append(caps, model.CapEventsSince)
	}
	if _, ok := s.H.(ConfigTransfer); ok {
		caps = append(caps, model.CapExportConfig, model.CapImportConfig) // phase 10 (CHK-CFG-06)
		if _, ok := s.H.(ConfigViberRecipientsTransfer); ok {
			caps = append(caps, model.CapExportViberRecipients) // phase 12 (CHK-IPC-14)
		}
		if _, ok := s.H.(ConfigEmailRecipientsTransfer); ok {
			caps = append(caps, model.CapExportEmailRecipients) // phase 13b (CHK-CFG-09)
		}
	}
	if pr, ok := s.H.(ProblemsReporter); ok && pr.ReportsProblems() {
		caps = append(caps, model.CapProblems, model.CapAlertRepeat) // phase 10 (CHK-MON-17)
	}
	if sr, ok := s.H.(StalledReporter); ok && sr.ReportsStalled() {
		caps = append(caps, model.CapStalled) // phase 11 (CHK-MON-18)
	}
	if wiz {
		caps = append(caps, model.CapSQLBrowse, model.CapSQLDatabases, model.CapSQLDiagnose, model.CapSQLGrantScript)
		if _, ok := s.H.(SQLWizardV2); ok { // phase 11 (CHK-IPC-12/13)
			caps = append(caps, model.CapBrowseSubnet, model.CapBrowseLocal, model.CapTestSteps)
		}
	}
	if s.Viber != nil {
		caps = append(caps, model.CapViber) // phase 10 (CHK-SVC-06)
	}
	if s.Email != nil {
		caps = append(caps, model.CapEmail) // phase 13b (CHK-CFG-09)
	}
	if s.AlarmTest != nil {
		caps = append(caps, model.CapAlarmTest) // phase 11 (CHK-IPC-11)
	}
	if s.Summary != nil {
		caps = append(caps, model.CapSummary) // phase 11 (CHK-SVC-07)
	}
	if s.AlarmTest != nil { // phase 11 (CHK-IPC-11): the only client capability used so far
		caps = append(caps, model.CapClientHello)
	}
	if _, ok := s.H.(EventAcker); ok {
		caps = append(caps, model.CapAckEvent) // phase 11 (CHK-MON-18)
	}
	return caps
}

// ProblemsReporter: the handler sends hasProblems / problems and repeat
// alert pushes (phase 10, CHK-MON-17).
type ProblemsReporter interface{ ReportsProblems() bool }

// StalledReporter: the handler fills status stalled / problems kind
// "stalled" (phase 11, CHK-MON-18).
type StalledReporter interface{ ReportsStalled() bool }

func (s *Server) heartbeat() *model.Heartbeat {
	if h, ok := s.H.(HeartbeatSource); ok {
		hb := h.Heartbeat()
		return &hb
	}
	return nil
}

// errorReply builds MsgError; *model.IPCError supplies Code / ErrorKey /
// ErrorArgs. Every error reply carries a code and an errorKey (phase 6).
func errorReply(id uint64, err error, code string) model.IPCMessage {
	m := model.IPCMessage{ID: id, Type: model.MsgError, Error: err.Error(), Code: code}
	var ie *model.IPCError
	if errors.As(err, &ie) {
		m.Code, m.ErrorKey, m.ErrorArgs, m.ProfileID = ie.Code, ie.Key, ie.Args, ie.ProfileID
		m.RetryAfterMs = ie.RetryAfterMs
		m.ErrorInfo = ie.Info
	}
	if m.Code == "" && errors.Is(err, context.Canceled) {
		m.Code = model.ErrCodeCanceled
	}
	if m.Code == "" {
		m.Code = model.ErrCodeInvalidRequest
	}
	if m.ErrorKey == "" {
		switch m.Code {
		case model.ErrCodeUnknownCommand:
			m.ErrorKey = model.MsgMonitorUnknownCommand
		case model.ErrCodeAccessDenied:
			m.ErrorKey = model.MsgMonitorAccessDenied
		case model.ErrCodeCanceled:
			m.ErrorKey = model.MsgMonitorCanceled
		case model.ErrCodeRateLimited:
			m.ErrorKey = model.MsgMonitorRateLimited
		case model.ErrCodeNotFound:
			m.ErrorKey = model.MsgMonitorUnknownProfile
		case model.ErrCodeSaveFailed:
			m.ErrorKey = model.MsgMonitorConfigSaveFailed
		case model.ErrCodeConflict:
			m.ErrorKey = model.MsgMonitorConfigConflict
		case model.ErrCodeBusy:
			m.ErrorKey = model.MsgTestBusy
		case model.ErrCodeInternal:
			m.ErrorKey = model.MsgMonitorInternal
		default:
			m.ErrorKey = model.MsgMonitorBadRequest
		}
	}
	return m
}

// identify returns the client of c (Authorize, else the transport's
// ClientIdentity, else not an administrator).
func (s *Server) identify(c *conn) (Peer, error) {
	if s.Authorize != nil {
		return s.Authorize(c.rw)
	}
	if ci, ok := c.rw.(clientIdentifier); ok {
		return ci.ClientIdentity()
	}
	return Peer{}, errors.New("ipc: transport cannot identify the client")
}

// elevated is clientElevated of c (computed on the first get_status /
// ping: ImpersonateNamedPipeClient fails before the server has read data
// from the client, so it cannot be in hello).
func (c *conn) elevated(s *Server) *bool {
	c.elevOnce.Do(func() {
		cl, err := s.identify(c)
		c.elev = err == nil && cl.Admin
	})
	v := c.elev
	return &v
}

// Serve accepts clients until Close; it returns nil after Close.
func (s *Server) Serve(l Listener) error {
	s.mu.Lock()
	s.initLocked()
	if s.closed {
		s.mu.Unlock()
		_ = l.Close()
		return nil
	}
	s.l = l
	if s.slots == nil {
		n := s.MaxClients
		if n <= 0 {
			n = model.IPCMaxClients
		}
		s.slots = make(chan struct{}, n)
	}
	slots := s.slots
	s.mu.Unlock()
	var fail acceptFailures
	for {
		// a free slot first: with MaxClients connections no new pipe
		// instance is created, further clients get ERROR_PIPE_BUSY
		select {
		case slots <- struct{}{}:
		case <-s.stop:
			return nil
		}
		release := func() { <-slots }
		rw, err := l.Accept()
		if err != nil {
			release()
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed || errors.Is(err, ErrListenerClosed) {
				return nil
			}
			select {
			case <-s.after(fail.next(s, err)):
				continue
			case <-s.stop:
				return nil
			}
		}
		fail.ok(s)
		c := &conn{rw: rw, out: make(chan model.IPCMessage, outQueue), done: make(chan struct{}), release: release}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			s.drop(c)
			return nil
		}
		s.conns[c] = struct{}{}
		s.wg.Add(2)
		s.mu.Unlock()
		go s.writeLoop(c)
		go s.readLoop(c)
	}
}

// acceptFailures is the backoff / log anti-spam state of the accept loop.
type acceptFailures struct {
	n          int // consecutive failures
	delay      time.Duration
	lastMsg    string
	lastLog    time.Time
	suppressed int // repeats not logged since the last log line
	skipped    int // repeats not logged in the whole failure series
}

// next records a failure, logs it (rate-limited) and returns the pause
// before the next attempt.
func (f *acceptFailures) next(s *Server, err error) time.Duration {
	min, max, every := s.BackoffMin, s.BackoffMax, s.LogEvery
	if min <= 0 {
		min = AcceptBackoffMin
	}
	if max <= 0 {
		max = AcceptBackoffMax
	}
	if max < min {
		max = min
	}
	if every <= 0 {
		every = AcceptLogEvery
	}
	f.n++
	if f.delay == 0 {
		f.delay = min
	} else if f.delay *= 2; f.delay > max {
		f.delay = max
	}
	now := s.now()
	msg := err.Error()
	if msg != f.lastMsg || now.Sub(f.lastLog) >= every {
		if f.suppressed > 0 {
			s.Log.Printf("ipc: accept: %v (failure #%d, %d repeat(s) not logged, retry in %s)", err, f.n, f.suppressed, f.delay)
		} else {
			s.Log.Printf("ipc: accept: %v (failure #%d, retry in %s)", err, f.n, f.delay)
		}
		f.lastMsg, f.lastLog, f.suppressed = msg, now, 0
	} else {
		f.suppressed++
		f.skipped++
	}
	return f.delay
}

// ok resets the state after a successful Accept.
func (f *acceptFailures) ok(s *Server) {
	if f.n > 0 {
		s.Log.Printf("ipc: accept works again after %d failure(s), %d of them not logged", f.n, f.skipped)
	}
	*f = acceptFailures{}
}

func (s *Server) drop(c *conn) {
	c.close()
	s.mu.Lock()
	delete(s.conns, c)
	s.mu.Unlock()
	if c.release != nil {
		c.relOnce.Do(c.release)
	}
}

var (
	errIdleTimeout = errors.New("ipc: idle timeout")
	errReadTimeout = errors.New("ipc: request read timeout")
)

// readRequest reads one request under two watchdogs that close the
// connection (which unblocks the pending read on every transport): the
// idle timeout until the first byte, then the read timeout for the rest
// of the frame; the frame is limited to MaxRequest bytes (CHK-MON-11).
func (s *Server) readRequest(c *conn, req *model.IPCRequest) error {
	var fired int32
	watch := func(d time.Duration) *time.Timer {
		if d <= 0 {
			d = 1<<63 - 1
		}
		return time.AfterFunc(d, func() { atomic.StoreInt32(&fired, 1); c.close() })
	}
	wd := watch(s.IdleTimeout)
	var first [1]byte
	_, err := io.ReadFull(c.rw, first[:])
	if !wd.Stop() || atomic.LoadInt32(&fired) == 1 {
		return errIdleTimeout
	}
	if err != nil {
		return err
	}
	wd = watch(s.ReadTimeout)
	max := s.MaxRequest
	if max <= 0 {
		max = model.IPCMaxRequest
	}
	var hdr [4]byte
	hdr[0] = first[0]
	_, err = io.ReadFull(c.rw, hdr[1:])
	if err == nil {
		n := int64(binary.LittleEndian.Uint32(hdr[:]))
		large := n > int64(max)
		// phase 10 (CHK-CFG-06): a frame above the limit is read only for
		// an elevated administrator of a ConfigTransfer handler, up to
		// IPCMaxImportRequest, and must be import_config; else the client
		// is dropped (ErrTooLarge) as before
		if large && (n > model.IPCMaxImportRequest || !s.largeAllowed(c)) {
			err = ErrTooLarge
		} else {
			err = ReadFrame(io.MultiReader(bytes.NewReader(hdr[:]), c.rw), req, model.IPCMaxImportRequest)
			if err == nil && large && req.Cmd != model.CmdImportConfig {
				err = ErrTooLarge
			}
		}
	}
	if !wd.Stop() || atomic.LoadInt32(&fired) == 1 {
		return errReadTimeout
	}
	if errors.Is(err, io.EOF) {
		err = io.ErrUnexpectedEOF
	}
	return err
}

// largeAllowed: the client may send an import_config request above
// IPCMaxRequest (handler with ConfigTransfer, elevated administrator,
// checked now).
func (s *Server) largeAllowed(c *conn) bool {
	if _, ok := s.H.(ConfigTransfer); !ok {
		return false
	}
	cl, err := s.identify(c)
	return err == nil && cl.Admin
}

func (s *Server) writeLoop(c *conn) {
	defer s.wg.Done()
	defer s.drop(c)
	for {
		select {
		case <-c.done:
			return
		case m := <-c.out:
			if err := WriteMessage(c.rw, m); err != nil {
				return
			}
		}
	}
}

func (s *Server) readLoop(c *conn) {
	defer s.wg.Done()
	defer s.drop(c)
	c.send(model.IPCMessage{Type: model.MsgHello, Version: model.IPCVersion, Capabilities: s.Capabilities(), Heartbeat: s.heartbeat()})
	for {
		var req model.IPCRequest
		if err := s.readRequest(c, &req); err != nil {
			if errors.Is(err, errIdleTimeout) || errors.Is(err, errReadTimeout) || errors.Is(err, ErrTooLarge) {
				s.Log.Printf("ipc: client dropped: %v", err)
			} else if !errors.Is(err, io.EOF) {
				select {
				case <-c.done:
				default:
					s.Log.Printf("ipc: client read: %v", err)
				}
			}
			return
		}
		if t, ok := s.H.(Tester); ok && req.Cmd == model.CmdTestProfile {
			s.startTest(c, t, req)
			continue
		}
		if req.Cmd == model.CmdHistory && s.History != nil {
			if !s.startHistory(c, req) {
				s.Log.Printf("ipc: client too slow, disconnected")
				return
			}
			continue
		}
		if w, ok := s.H.(SQLWizard); ok && model.SQLCommand(req.Cmd) {
			if !s.startSQL(c, w, req) {
				s.Log.Printf("ipc: client too slow, disconnected")
				return
			}
			continue
		}
		if d, ok := s.H.(Discoverer); ok && interactiveCommand(req.Cmd) {
			if !s.startInteractive(c, d, req) {
				s.Log.Printf("ipc: client too slow, disconnected")
				return
			}
			continue
		}
		var reply model.IPCMessage
		switch {
		case interactiveCommand(req.Cmd), model.SQLCommand(req.Cmd): // the handler has no Discoverer / SQLWizard
			reply = errorReply(req.ID, errors.New("unknown command: "+req.Cmd), model.ErrCodeUnknownCommand)
		case req.Cmd == model.CmdCancel:
			reply = s.cancelOp(c, req)
		case req.Cmd == model.CmdCheckNow:
			reply = s.checkNow(c, req)
		case req.Cmd == model.CmdEventsSince:
			reply = s.eventsSince(req)
		case req.Cmd == model.CmdClientHello:
			if !c.send(s.clientHello(c, req)) {
				return
			}
			continue
		case s.Summary != nil && req.Cmd == model.CmdSummary:
			if !s.startSummary(c, req) {
				s.Log.Printf("ipc: client too slow, disconnected")
				return
			}
			continue
		case s.AlarmTest != nil && req.Cmd == model.CmdAlarmTest:
			if !s.startAlarmTest(c, req) {
				s.Log.Printf("ipc: client too slow, disconnected")
				return
			}
			continue
		case s.Viber != nil && viberCommand(req.Cmd):
			if !s.startViber(c, req) {
				s.Log.Printf("ipc: client too slow, disconnected")
				return
			}
			continue
		case s.Email != nil && emailCommand(req.Cmd):
			if !s.startEmail(c, req) {
				s.Log.Printf("ipc: client too slow, disconnected")
				return
			}
			continue
		case model.AdminCommand(req.Cmd):
			reply = s.handleAdmin(c, req)
		default:
			reply = s.handle(req)
			if req.Cmd == model.CmdGetStatus || req.Cmd == model.CmdPing {
				reply.ClientElevated = c.elevated(s)
			}
		}
		if !c.send(reply) {
			s.Log.Printf("ipc: client too slow, disconnected")
			return
		}
	}
}

// startTest runs test_profile on its own goroutine; the reply is matched
// by ID (the client may send ping meanwhile). The test ends at its
// timeout, when the client disconnects or when the server closes.
func (s *Server) startTest(c *conn, t Tester, req model.IPCRequest) {
	s.startAsync(c, req, func(ctx context.Context) (model.IPCMessage, error) {
		res, err := t.TestProfile(ctx, req)
		return model.IPCMessage{Type: model.MsgTestResult, Test: &res}, err
	})
}

// adminOnly checks the client of an admin-only command on every request
// (no cache); false: refused (reply already queued or not, sent=ok).
func (s *Server) adminOnly(c *conn, req model.IPCRequest) (allowed, sent bool) {
	cl, err := s.identify(c)
	if err != nil || !cl.Admin {
		why := "not an elevated administrator"
		if err != nil {
			why = err.Error()
		}
		s.Log.Printf("ipc: %s refused for %q: %s", req.Cmd, cl.Account, why)
		return false, c.send(errorReply(req.ID, ErrAccessDenied, ""))
	}
	return true, true
}

// startSQL runs a wizard command (phase 9, CHK-IPC-10) for an elevated
// administrator: sql_diagnose / sql_grant_script synchronously,
// sql_browse / sql_databases like discover (own goroutine, cancel {ref},
// reply cut to model.IPCMaxInteractiveReply). false: the client is too
// slow.
func (s *Server) startSQL(c *conn, w SQLWizard, req model.IPCRequest) bool {
	if ok, sent := s.adminOnly(c, req); !ok {
		return sent
	}
	switch req.Cmd {
	case model.CmdSQLDiagnose:
		return c.send(sqlDiagnose(req))
	case model.CmdSQLGrantScript:
		gr := model.SQLGrantRequest{Principal: req.Principal, Windows: req.Windows, Auth: req.Auth, Database: req.Database, Tables: req.Tables}
		sc, err := model.SQLGrantScriptFor(gr)
		if err != nil {
			return c.send(errorReply(req.ID, fieldRequestError(err), ""))
		}
		return c.send(model.IPCMessage{ID: req.ID, Type: model.MsgSQLGrantScript, SQLGrantScript: &sc})
	}
	s.startAsync(c, req, func(ctx context.Context) (model.IPCMessage, error) {
		m := model.IPCMessage{ID: req.ID}
		switch req.Cmd {
		case model.CmdSQLTestSteps:
			w2, ok := w.(SQLWizardV2)
			if !ok {
				return m, &model.IPCError{Code: model.ErrCodeUnknownCommand, Message: "ipc: sql_test_steps is not supported"}
			}
			res, err := w2.SQLTestSteps(ctx, req, func(st model.SQLCheckStep) {
				c.send(model.IPCMessage{Type: model.MsgSQLTestStepsProgress, Ref: req.ID, TestStep: &st}) // dropped when the client is slow
			})
			if err != nil {
				return m, err
			}
			m.Type, m.SQLTestSteps = model.MsgSQLTestSteps, &res
		case model.CmdSQLBrowse:
			var res model.SQLBrowseResult
			var err error
			if w2, ok := w.(SQLWizardV2); ok {
				res, err = w2.SQLBrowseProgress(ctx, req, browseProgress(c, req.ID, time.Now))
			} else {
				res, err = w.SQLBrowse(ctx, req)
			}
			if err != nil {
				return m, err
			}
			m.Type, m.SQLBrowse = model.MsgSQLBrowse, &res
			if res.Truncated {
				m.Truncated, m.TruncatedReason = true, model.TruncatedLimit
			}
		default:
			res, err := w.SQLDatabases(ctx, req)
			if err != nil {
				return m, err
			}
			m.Type, m.SQLDatabases = model.MsgSQLDatabases, &res
			if res.Truncated {
				m.Truncated, m.TruncatedReason = true, model.TruncatedLimit
			}
		}
		fitInteractiveReply(&m, model.IPCMaxInteractiveReply)
		return m, nil
	})
	return true
}

// sqlDiagnose answers sql_diagnose: Code (+ TLSKey) or Category (+
// ErrorKey); neither is invalid_request.
func sqlDiagnose(req model.IPCRequest) model.IPCMessage {
	var d model.SQLDiagnosis
	switch {
	case req.Code != "":
		d = model.SQLDiagnoseCode(req.Code, req.TLSKey)
	case req.Category != "" || req.ErrorKey != "":
		d = model.SQLDiagnose(req.Category, req.ErrorKey)
	default:
		return errorReply(req.ID, &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgMonitorBadRequest,
			Message: "sql_diagnose: category or code is required", Args: []string{"category", model.ValRequired}}, "")
	}
	return model.IPCMessage{ID: req.ID, Type: model.MsgSQLDiagnosis, SQLDiagnosis: &d}
}

// fieldRequestError: invalid_request, errorKey monitor.bad_request,
// errorArgs [field, validation key, args...].
func fieldRequestError(err error) error {
	if fe := model.AsFieldError(err); fe != nil {
		return &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgMonitorBadRequest, Message: err.Error(),
			Args: append([]string{fe.Field, fe.Key}, fe.Args...)}
	}
	return &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgMonitorBadRequest, Message: err.Error()}
}

// startInteractive checks the client (admin, on every request, no cache;
// CHK-IPC-06) and runs a Discoverer command on its own goroutine like
// test_profile. The reply is cut to model.IPCMaxInteractiveReply
// (fitInteractiveReply). false: the access_denied reply could not be
// queued (client too slow).
func (s *Server) startInteractive(c *conn, d Discoverer, req model.IPCRequest) bool {
	cl, err := s.identify(c)
	if err != nil || !cl.Admin {
		why := "not an elevated administrator"
		if err != nil {
			why = err.Error()
		}
		s.Log.Printf("ipc: %s refused for %q: %s", req.Cmd, cl.Account, why)
		return c.send(errorReply(req.ID, ErrAccessDenied, ""))
	}
	s.startAsync(c, req, func(ctx context.Context) (model.IPCMessage, error) {
		var m model.IPCMessage
		var err error
		switch req.Cmd {
		case model.CmdDiscover, model.CmdDiscoverByValue:
			f := d.Discover
			if req.Cmd == model.CmdDiscoverByValue {
				f = d.DiscoverByValue
			}
			m.Type = model.MsgDiscovery
			m.Candidates, m.TruncatedReason, err = f(ctx, req)
		case model.CmdListNodePoints:
			m.Type = model.MsgNodePoints
			m.NodePoints, m.TruncatedReason, err = d.ListNodePoints(ctx, req)
		case model.CmdCheckMapping:
			var res model.MappingCheck
			res, err = d.CheckMapping(ctx, req)
			m.Type, m.MappingCheck = model.MsgMappingCheck, &res
		}
		if err == nil {
			m.ID = req.ID
			m.Truncated = m.TruncatedReason != ""

			if m.Type == model.MsgNodePoints && m.TruncatedReason == model.TruncatedLimit {
				m.Limit = req.Limit // the limit the list was cut to (EC Tray)
			}
			fitInteractiveReply(&m, model.IPCMaxInteractiveReply)
			if m.Type == model.MsgDiscovery {
				fitAutoPick(&m, model.IPCMaxInteractiveReply)
			}
		}
		return m, err
	})
	return true
}

// fitInteractiveReply keeps the serialized reply within max bytes
// (Вика, 06.10): the tail of Candidates / NodePoints /
// MappingCheck.SampleRows is dropped (the longest prefix that fits) and
// Truncated / MappingCheck.Truncated is set with the reason
// model.TruncatedSize (an earlier budget reason is kept: budget > size >
// limit).
func fitInteractiveReply(m *model.IPCMessage, max int) {
	size := func() int {
		b, err := json.Marshal(m)
		if err != nil {
			return 0
		}
		return len(b)
	}
	if size() <= max {
		return
	}
	// cut sets the longest prefix (n items do not fit) that fits
	cut := func(n int, set func(k int)) {
		lo, hi := 0, n-1
		for lo < hi {
			mid := (lo + hi + 1) / 2
			set(mid)
			if size() <= max {
				lo = mid
			} else {
				hi = mid - 1
			}
		}
		set(lo)
	}
	sizeReason := func(r string) string {
		if r == model.TruncatedBudget {
			return r
		}
		return model.TruncatedSize
	}
	switch {
	case m.SQLBrowse != nil && len(m.SQLBrowse.Instances) > 0:
		r := *m.SQLBrowse
		m.SQLBrowse = &r
		all := r.Instances
		r.Truncated, m.Truncated, m.TruncatedReason = true, true, model.TruncatedSize
		cut(len(all), func(k int) { r.Instances = all[:k] })
	case m.SQLDatabases != nil && len(m.SQLDatabases.Databases) > 0:
		r := *m.SQLDatabases
		m.SQLDatabases = &r
		all := r.Databases
		r.Truncated, m.Truncated, m.TruncatedReason = true, true, model.TruncatedSize
		cut(len(all), func(k int) { r.Databases = all[:k] })
	case len(m.Candidates) > 0:
		all := m.Candidates
		m.Truncated, m.TruncatedReason = true, sizeReason(m.TruncatedReason)
		cut(len(all), func(k int) { m.Candidates = all[:k] })
	case len(m.NodePoints) > 0:
		all := m.NodePoints
		m.Truncated, m.TruncatedReason = true, sizeReason(m.TruncatedReason)
		m.Limit = 0 // only with the reason "limit"
		cut(len(all), func(k int) { m.NodePoints = all[:k] })
	case m.MappingCheck != nil:
		mc := *m.MappingCheck
		m.MappingCheck = &mc
		if len(mc.SampleRows) > 0 {
			all := mc.SampleRows
			mc.Truncated, mc.TruncatedReason = true, sizeReason(mc.TruncatedReason)
			cut(len(all), func(k int) { mc.SampleRows = all[:k] })
		}
		if size() > max { // a huge error text (defense in depth)
			if r := []rune(mc.Error); len(r) > 1024 {
				mc.Error = string(r[:1024])
			}
		}
	}
}

// fitAutoPick adds AutoPick (model.SQLAutoPickFor over the candidates of
// the reply, phase 9 CHK-IPC-10) to a discovery reply; if that no longer
// fits, the last candidates are dropped (reason size) and AutoPick is
// recomputed.
func fitAutoPick(m *model.IPCMessage, max int) {
	for {
		ap := model.SQLAutoPickFor(m.Candidates)
		m.AutoPick = &ap
		b, err := json.Marshal(m)
		if err != nil || len(b) <= max || len(m.Candidates) == 0 {
			return
		}
		m.Candidates = m.Candidates[:len(m.Candidates)-1]
		m.Truncated = true
		if m.TruncatedReason != model.TruncatedBudget {
			m.TruncatedReason = model.TruncatedSize
		}
	}
}

// startAsync runs one asynchronous request of c (test_profile, the
// Discoverer commands) on its own goroutine, registered for cancel {ref}
// (G6, CHK-IPC-07). ctx ends when the client disconnects, cancels the
// request or the server closes. The reply is matched by ID.
func (s *Server) startAsync(c *conn, req model.IPCRequest, run func(ctx context.Context) (model.IPCMessage, error)) bool {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return false
	}
	sctx := s.ctx
	ctx, cancel := context.WithCancel(sctx)
	c.opsMu.Lock()
	if c.ops == nil {
		c.ops = map[uint64]*asyncOp{}
	}
	if _, dup := c.ops[req.ID]; dup {
		c.opsMu.Unlock()
		s.mu.Unlock()
		cancel()
		c.send(errorReply(req.ID, &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgMonitorBadRequest,
			Message: "ipc: a request with this id is still running", Args: []string{"id"}}, ""))
		return false
	}
	op := &asyncOp{cancel: cancel}
	c.ops[req.ID] = op
	c.opsMu.Unlock()
	s.wg.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.wg.Done()
		defer cancel()
		go func() {
			select {
			case <-c.done:
				cancel()
			case <-ctx.Done():
			}
		}()
		res, err := s.runRecovered(ctx, req, run)
		c.opsMu.Lock()
		canceled := op.canceled
		delete(c.ops, req.ID)
		c.opsMu.Unlock()
		if !canceled && sctx.Err() != nil {
			// the server is closing (Close cancels before it disconnects
			// the clients): no answer, the connection is about to close;
			// a shutdown must not reach the tray as invalid_request
			return
		}
		var m model.IPCMessage
		if canceled {
			// the client canceled it: whatever the request returned, the
			// answer is canceled (cancel already replied ok)
			m = errorReply(req.ID, errCanceled, "")
		} else if err != nil {
			m = errorReply(req.ID, err, "")
		} else {
			m = res
			m.ID = req.ID
		}
		if !c.send(m) {
			select {
			case <-c.done:
			default:
				s.Log.Printf("ipc: client too slow, disconnected")
				s.drop(c)
			}
		}
	}()
	return true
}

// errInternal answers a request whose handler panicked (CHK-IPC-08): no
// panic text, no stack.
var errInternal = &model.IPCError{Code: model.ErrCodeInternal, Key: model.MsgMonitorInternal, Message: "ipc: internal service error"}

// runRecovered runs the handler of an asynchronous request; a panic is
// recovered (the service keeps running, the request's record for cancel
// {ref} is removed by the caller as after any return) and logged with
// the stack — the password and the manual SQL of the request are scrubbed
// from the panic text, the request itself is not logged — and the reply
// is errInternal.
func (s *Server) runRecovered(ctx context.Context, req model.IPCRequest, run func(ctx context.Context) (model.IPCMessage, error)) (m model.IPCMessage, err error) {
	defer func() {
		if r := recover(); r != nil {
			s.Log.Printf("ipc: %s id %d: internal error (recovered panic): %s\n%s", req.Cmd, req.ID, scrubRequest(fmt.Sprint(r), req), debug.Stack())
			m, err = model.IPCMessage{}, errInternal
		}
	}()
	return run(ctx)
}

// scrubRequest removes the password and manual SQL texts of req from msg.
func scrubRequest(msg string, req model.IPCRequest) string {
	secrets := []string{req.Password, req.ViberToken, req.SmtpPassword}
	if req.Mapping != nil {
		secrets = append(secrets, req.Mapping.ManualSQL)
	}
	if req.Profile != nil {
		secrets = append(secrets, req.Profile.Mapping.ManualSQL)
		for _, mp := range req.Profile.Mappings {
			secrets = append(secrets, mp.ManualSQL)
		}
	}
	for _, sec := range secrets {
		if strings.TrimSpace(sec) != "" {
			msg = strings.ReplaceAll(msg, sec, "***")
		}
	}
	return msg
}

var errCanceled = &model.IPCError{Code: model.ErrCodeCanceled, Key: model.MsgMonitorCanceled, Message: "ipc: request canceled"}

// cancelOp handles cancel {ref}: only a running asynchronous request of
// the same connection (G6, CHK-IPC-07). The connection stays open.
func (s *Server) cancelOp(c *conn, req model.IPCRequest) model.IPCMessage {
	c.opsMu.Lock()
	op := c.ops[req.Ref]
	if op != nil && !op.canceled {
		op.canceled = true
		op.cancel()
	} else {
		op = nil
	}
	c.opsMu.Unlock()
	if op == nil {
		return errorReply(req.ID, &model.IPCError{Code: model.ErrCodeNotFound, Key: model.MsgMonitorUnknownRequest,
			Message: "ipc: no running request with this id"}, "")
	}
	return model.IPCMessage{ID: req.ID, Type: model.MsgOK, Result: model.ResultCanceled}
}

// handleAdmin runs an admin-only command after checking the client on
// every call (no cache): ImpersonateNamedPipeClient + CheckTokenMembership
// (BUILTIN\Administrators) on Windows (CHK-IPC-06).
func (s *Server) handleAdmin(c *conn, req model.IPCRequest) model.IPCMessage {
	cm, ok := s.H.(ConfigManager)
	ct, okT := s.H.(ConfigTransfer)
	cloner, okC := s.H.(ProfileCloner)
	if req.Cmd == model.CmdExportConfig || req.Cmd == model.CmdImportConfig {
		ok = okT
	}
	if req.Cmd == model.CmdCloneProfile {
		ok = okC
	}
	if !ok {
		return errorReply(req.ID, errors.New("unknown command: "+req.Cmd), model.ErrCodeUnknownCommand)
	}
	cl, err := s.identify(c)
	if err != nil || !cl.Admin {
		why := "not an elevated administrator"
		if err != nil {
			why = err.Error()
		}
		s.Log.Printf("ipc: %s refused for %q: %s", req.Cmd, cl.Account, why)
		return errorReply(req.ID, ErrAccessDenied, "")
	}
	switch req.Cmd {
	case model.CmdExportConfig: // phase 10 (CHK-CFG-06) + phase 12 (CHK-CFG-07)
		var exp model.ConfigExport
		var err error
		if ce, ok := ct.(ConfigEmailRecipientsTransfer); ok {
			exp, err = ce.ExportConfigBoth(req.IncludeViberRecipients, req.IncludeEmailRecipients)
		} else if cv, ok := ct.(ConfigViberRecipientsTransfer); ok {
			exp, err = cv.ExportConfigOpts(req.IncludeViberRecipients)
		} else {
			exp, err = ct.ExportConfig()
		}
		if err != nil {
			return errorReply(req.ID, err, model.ErrCodeSaveFailed)
		}
		return model.IPCMessage{ID: req.ID, Type: model.MsgConfigExport, Export: &exp}
	case model.CmdImportConfig:
		var res model.ImportResult
		var err error
		if ce, ok := ct.(ConfigEmailRecipientsTransfer); ok {
			res, err = ce.ImportConfigBoth(req.Config, req.Mode, req.ExpectedRevision, req.DryRun, req.IncludeViberRecipients, req.IncludeEmailRecipients, cl.Account)
		} else if cv, ok := ct.(ConfigViberRecipientsTransfer); ok {
			res, err = cv.ImportConfigOpts(req.Config, req.Mode, req.ExpectedRevision, req.DryRun, req.IncludeViberRecipients, cl.Account)
		} else {
			res, err = ct.ImportConfig(req.Config, req.Mode, req.ExpectedRevision, req.DryRun, cl.Account)
		}
		if err != nil {
			return errorReply(req.ID, err, model.ErrCodeSaveFailed)
		}
		result := model.ResultImported
		if res.DryRun {
			result = model.ResultImportChecked
		}
		return model.IPCMessage{ID: req.ID, Type: model.MsgOK, Result: result, Reloaded: !res.DryRun, Revision: res.Revision, Import: &res}
	case model.CmdGetConfig:
		cfg, rev, err := cm.GetConfig()
		if err != nil {
			return errorReply(req.ID, err, model.ErrCodeSaveFailed)
		}
		return model.IPCMessage{ID: req.ID, Type: model.MsgConfig, Config: &cfg, Revision: rev}
	case model.CmdSaveProfile:
		if req.Profile == nil {
			return errorReply(req.ID, &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgMonitorProfileInvalid,
				Message: "ipc: save_profile without profile", Args: []string{"profile", model.ValRequired}}, "")
		}
		res, err := cm.SaveProfile(*req.Profile, req.Password, req.ClearPassword, req.ExpectedRevision, cl.Account)
		if err != nil {
			return errorReply(req.ID, err, model.ErrCodeSaveFailed)
		}
		p := res.Profile
		return model.IPCMessage{ID: req.ID, Type: model.MsgOK, Result: model.ResultSaved, ProfileID: p.ID, Profile: &p,
			Reloaded: true, PollScheduled: res.PollScheduled, Revision: res.Revision}
	case model.CmdDeleteProfile:
		rev, err := cm.DeleteProfile(req.ProfileID, req.ExpectedRevision, cl.Account)
		if err != nil {
			return errorReply(req.ID, err, model.ErrCodeSaveFailed)
		}
		return model.IPCMessage{ID: req.ID, Type: model.MsgOK, Result: model.ResultDeleted, ProfileID: req.ProfileID,
			Reloaded: true, Revision: rev}
	case model.CmdCloneProfile: // phase 12 (CHK-CFG-08)
		res, needPW, err := cloner.CloneProfile(req.ProfileID, req.NewID, req.NewName, req.ExpectedRevision, cl.Account)
		if err != nil {
			return errorReply(req.ID, err, model.ErrCodeSaveFailed)
		}
		p := res.Profile
		return model.IPCMessage{ID: req.ID, Type: model.MsgOK, Result: model.ResultCloned, ProfileID: p.ID, Profile: &p,
			Reloaded: true, PollScheduled: res.PollScheduled, Revision: res.Revision, NeedPassword: needPW}
	case model.CmdSaveSettings:
		if req.Settings == nil {
			return errorReply(req.ID, &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgMonitorSettingsInvalid,
				Message: "ipc: save_settings without settings", Args: []string{"settings", model.ValRequired}}, "")
		}
		var rev string
		if es, ok := s.H.(EmailSettingsSaver); ok {
			rev, err = es.SaveSettingsEmail(*req.Settings, req.ViberToken, req.ClearViberToken, req.SmtpPassword, req.ClearSmtpPassword, req.ExpectedRevision, cl.Account)
		} else if vs, ok := s.H.(ViberSettingsSaver); ok {
			if req.SmtpPassword != "" || req.ClearSmtpPassword {
				return errorReply(req.ID, errors.New("email: SMTP password not supported"), model.ErrCodeUnknownCommand)
			}
			rev, err = vs.SaveSettingsViber(*req.Settings, req.ViberToken, req.ClearViberToken, req.ExpectedRevision, cl.Account)
		} else if req.ViberToken != "" || req.ClearViberToken || req.SmtpPassword != "" || req.ClearSmtpPassword {
			return errorReply(req.ID, errors.New("viber token: not supported"), model.ErrCodeUnknownCommand)
		} else {
			rev, err = cm.SaveSettings(*req.Settings, req.ExpectedRevision, cl.Account)
		}
		if err != nil {
			return errorReply(req.ID, err, model.ErrCodeSaveFailed)
		}
		return model.IPCMessage{ID: req.ID, Type: model.MsgOK, Result: model.ResultSaved, Reloaded: true, Revision: rev}
	}
	return errorReply(req.ID, errors.New("unknown command: "+req.Cmd), model.ErrCodeUnknownCommand)
}

// handle executes one command.
func (s *Server) handle(req model.IPCRequest) model.IPCMessage {
	switch req.Cmd {
	case model.CmdPing:
		return model.IPCMessage{ID: req.ID, Type: model.MsgPong, Version: model.IPCVersion, Heartbeat: s.heartbeat()}
	case model.CmdGetStatus:
		snap := s.H.Snapshot()
		reply := model.IPCMessage{ID: req.ID, Type: model.MsgStatus, Status: &snap}
		if s.TrayAutostartError != nil {
			reply.TrayAutostartError = s.TrayAutostartError()
		}
		return reply
	case model.CmdReloadConfig:
		return model.IPCMessage{ID: req.ID, Type: model.MsgOK, Result: result(s.H.ReloadConfig())}
	case model.CmdPollNow:
		coalesced, err := s.H.PollNow(req.ProfileID)
		if err != nil {
			return errorReply(req.ID, err, "")
		}
		return model.IPCMessage{ID: req.ID, Type: model.MsgOK, Result: result(coalesced)}
	case model.CmdAckAlert:
		if a, ok := s.H.(Acker); ok {
			var n int
			var err error
			if ea, ok := s.H.(EventAcker); ok && req.EventID != 0 {
				n, err = ea.AckEvent(req.EventID)
			} else {
				n, err = a.AckAlert(req.ProfileID, req.PointKey)
			}
			if err != nil {
				return errorReply(req.ID, err, "")
			}
			return model.IPCMessage{ID: req.ID, Type: model.MsgOK, Result: model.ResultAcknowledged, Acked: n}
		}
	case model.CmdMuteUntilRecovered: // phase 12 (CHK-MON-20 / CHK-IPC-15)
		if mu, ok := s.H.(Muter); ok {
			if err := mu.MuteUntilRecovered(req.ProfileID); err != nil {
				return errorReply(req.ID, err, "")
			}
			return model.IPCMessage{ID: req.ID, Type: model.MsgOK, Result: model.ResultMuted}
		}
	case model.CmdUnmuteProfile:
		if mu, ok := s.H.(Muter); ok {
			if err := mu.UnmuteProfile(req.ProfileID); err != nil {
				return errorReply(req.ID, err, "")
			}
			return model.IPCMessage{ID: req.ID, Type: model.MsgOK, Result: model.ResultUnmuted}
		}
	}
	return errorReply(req.ID, errors.New("unknown command: "+req.Cmd), model.ErrCodeUnknownCommand)
}

func result(coalesced bool) string {
	if coalesced {
		return model.ResultCoalesced
	}
	return model.ResultAccepted
}

// broadcast queues m for every client; slow clients are disconnected.
func (s *Server) broadcast(m model.IPCMessage) { s.broadcastTo(m, "") }

// broadcastTo queues m for every client, or only those that declared
// clientCap in client_hello (clientCap != "").
func (s *Server) broadcastTo(m model.IPCMessage, clientCap string) {
	s.mu.Lock()
	cs := make([]*conn, 0, len(s.conns))
	for c := range s.conns {
		if clientCap != "" && !c.hasClientCap(clientCap) {
			continue
		}
		cs = append(cs, c)
	}
	s.mu.Unlock()
	for _, c := range cs {
		if !c.send(m) {
			s.Log.Printf("ipc: client queue full, disconnected")
			s.drop(c)
		}
	}
}

// PublishStatus pushes a snapshot to all clients (monitor OnSnapshot).
func (s *Server) PublishStatus(snap model.StatusSnapshot) {
	s.broadcast(model.IPCMessage{Type: model.MsgStatus, Status: &snap})
}

// Alert pushes an alert event to all clients (alert.Sink).
func (s *Server) Alert(e model.AlertEvent) {
	s.broadcast(model.IPCMessage{Type: model.MsgAlert, Alert: &e})
}

// AlertRepeat pushes an alert_repeat event to all clients (phase 10,
// CHK-MON-17).
func (s *Server) AlertRepeat(e model.AlertEvent) {
	s.broadcast(model.IPCMessage{Type: model.MsgAlertRepeat, Alert: &e})
}

// Clients returns the number of connected clients.
func (s *Server) Clients() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.conns)
}

// Close stops accepting, disconnects all clients and waits for their
// goroutines.
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.initLocked()
	s.closed = true
	close(s.stop)
	s.cancel()
	hook := s.closeHook
	l := s.l
	cs := make([]*conn, 0, len(s.conns))
	for c := range s.conns {
		cs = append(cs, c)
	}
	s.mu.Unlock()
	if hook != nil {
		hook()
	}
	var err error
	if l != nil {
		err = l.Close()
	}
	for _, c := range cs {
		c.close()
	}
	s.wg.Wait()
	return err
}

// startHistory runs one history request (phase 9, CHK-SVC-03) like
// test_profile: asynchronous, canceled by cancel {ref} or a disconnect.
// At most model.HistoryMaxPerConn per connection and
// model.HistoryMaxGlobal per service run at once; more -> busy
// (history.busy). false: the reply could not be queued.
func (s *Server) startHistory(c *conn, req model.IPCRequest) bool {
	s.mu.Lock()
	busy := s.histN >= model.HistoryMaxGlobal || c.histN >= model.HistoryMaxPerConn
	if !busy {
		s.histN++
		c.histN++
	}
	s.mu.Unlock()
	if busy {
		return c.send(errorReply(req.ID, &model.IPCError{Code: model.ErrCodeBusy, Key: model.MsgHistoryBusy,
			Message: "ipc: a history request is already running"}, ""))
	}
	release := func() {
		s.mu.Lock()
		s.histN--
		c.histN--
		s.mu.Unlock()
	}
	src := s.History
	if !s.startAsync(c, req, func(ctx context.Context) (model.IPCMessage, error) {
		defer release()
		page, err := src.History(ctx, req)
		if err != nil {
			return model.IPCMessage{}, err
		}
		if page.Records == nil {
			page.Records = []model.HistoryRecord{}
		}
		return model.IPCMessage{Type: model.MsgHistory, History: &page}, nil
	}) {
		release()
	}
	return true
}

// checkNow handles check_now (phase 9, CHK-SVC-04): at most one request
// per model.CheckConnInterval per connection (rate_limited +
// retryAfterMs), then the handler's limits.
//
// The per-connection limit only damps a chatty client; several pipes can
// bypass it, which is fine: the monitor's global limits (all profiles once
// per model.CheckAllInterval, a profile once per
// model.CheckProfileInterval, coalescing with queued / running polls)
// bound the SQL load regardless of the number of connections, and the
// pipe server caps the connections.
func (s *Server) checkNow(c *conn, req model.IPCRequest) model.IPCMessage {
	ch, ok := s.H.(Checker)
	if !ok {
		return errorReply(req.ID, errors.New("unknown command: "+req.Cmd), model.ErrCodeUnknownCommand)
	}
	now := time.Now()
	if !c.lastCheck.IsZero() {
		if el := now.Sub(c.lastCheck); el >= 0 && el < model.CheckConnInterval {
			wait := model.CheckConnInterval - el
			ms := int64((wait + time.Millisecond - 1) / time.Millisecond)
			return errorReply(req.ID, &model.IPCError{Code: model.ErrCodeRateLimited, Key: model.MsgCheckRateLimited,
				Message: "ipc: check_now too often on this connection", RetryAfterMs: ms}, "")
		}
	}
	c.lastCheck = now
	res, err := ch.CheckNow(req.ProfileID)
	if err != nil {
		return errorReply(req.ID, err, "")
	}
	seq := res.CheckSeq
	r := model.ResultAccepted
	if res.Coalesced {
		r = model.ResultCheckCoalesced
	}
	return model.IPCMessage{ID: req.ID, Type: model.MsgOK, Result: r, CheckSeq: &seq}
}

// eventsSince handles events_since (phase 9, CHK-SVC-05); the reply is
// kept within model.IPCMaxInteractiveReply (tail dropped, truncated
// "size", lastId = the last event sent).
func (s *Server) eventsSince(req model.IPCRequest) model.IPCMessage {
	ef, ok := s.H.(EventFeed)
	if !ok {
		return errorReply(req.ID, errors.New("unknown command: "+req.Cmd), model.ErrCodeUnknownCommand)
	}
	page := ef.EventsSince(req.RunID, req.SinceID, req.Limit)
	last := page.LastID
	m := model.IPCMessage{ID: req.ID, Type: model.MsgEvents, Events: page.Events, RunID: page.RunID, LastID: &last,
		ResetRequired: page.ResetRequired}
	if page.Truncated {
		m.Truncated, m.TruncatedReason = true, model.TruncatedLimit
	}
	for len(m.Events) > 0 {
		b, err := json.Marshal(m)
		if err == nil && len(b) <= model.IPCMaxInteractiveReply {
			break
		}
		m.Events = m.Events[:len(m.Events)-1]
		m.Truncated, m.TruncatedReason = true, model.TruncatedSize
		if n := len(m.Events); n > 0 {
			l := m.Events[n-1].ID
			m.LastID = &l
		} else {
			l := req.SinceID
			m.LastID = &l
		}
	}
	return m
}

// browseProgress pushes sql_browse_progress {ref, phase, done, total,
// found} (CHK-IPC-12): at most one per model.SQLBrowseProgressEvery, but
// always the first event, a phase change and the "done" phase. A push
// the slow client cannot take is dropped (never a disconnect).
func browseProgress(c *conn, ref uint64, now func() time.Time) func(model.SQLBrowseProgress) {
	var mu sync.Mutex
	var last time.Time
	var phase string
	sent := false
	return func(p model.SQLBrowseProgress) {
		mu.Lock()
		t := now()
		ok := !sent || p.Phase != phase || p.Phase == "done" || t.Sub(last) >= model.SQLBrowseProgressEvery
		if ok {
			sent, last, phase = true, t, p.Phase
		}
		mu.Unlock()
		if ok {
			c.send(model.IPCMessage{Type: model.MsgSQLBrowseProgress, Ref: ref, BrowseProgress: &p})
		}
	}
}

// clientHello stores the client's capabilities (phase 11, CHK-IPC-11):
// at most model.ClientCapsMax items of at most model.ClientCapMaxLen
// chars (else invalid_request + monitor.bad_request), unknown ones
// ignored; a repeated client_hello replaces the set. The reply echoes the
// accepted ones.
func (s *Server) clientHello(c *conn, req model.IPCRequest) model.IPCMessage {
	if len(req.Capabilities) > model.ClientCapsMax {
		return errorReply(req.ID, &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgMonitorBadRequest,
			Message: "client_hello: too many capabilities", Args: []string{"capabilities", model.ValTooMany, strconv.Itoa(model.ClientCapsMax)}}, "")
	}
	caps := map[string]bool{}
	acc := []string{}
	for _, k := range req.Capabilities {
		if len(k) > model.ClientCapMaxLen {
			return errorReply(req.ID, &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgMonitorBadRequest,
				Message: "client_hello: capability too long", Args: []string{"capabilities", model.ValRange, "1", strconv.Itoa(model.ClientCapMaxLen)}}, "")
		}
		if model.KnownClientCaps[k] && !caps[k] {
			caps[k] = true
			acc = append(acc, k)
		}
	}
	c.capMu.Lock()
	c.clientCaps = caps
	c.capMu.Unlock()
	return model.IPCMessage{ID: req.ID, Type: model.MsgClientHello, Capabilities: acc}
}

func (c *conn) hasClientCap(k string) bool {
	c.capMu.Lock()
	defer c.capMu.Unlock()
	return c.clientCaps[k]
}
