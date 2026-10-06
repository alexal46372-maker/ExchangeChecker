package monitor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/sqlcheck"
)

// Connection test of IPC "test_profile" (phase 4, CHK-IPC-03).

// Defaults of the connection test.
const (
	// DefaultTestTimeout is the hard time budget of one test: the reply is
	// sent at the latest then, even if the driver ignores its context. It
	// is the ONE deadline of the test: the context handed to the driver
	// carries it, so the dialer sees context.DeadlineExceeded (phase 4b).
	DefaultTestTimeout = 15 * time.Second
	// DefaultTestConcurrent tests may run at the same time; further
	// requests are answered busy.
	DefaultTestConcurrent = 2
	// DefaultTestAbandoned caps the timed-out tests whose goroutine has
	// not returned yet; while that many are pending, requests are busy.
	DefaultTestAbandoned = 2
)

// TestInfo is the server information returned by a successful test
// (sqlcheck.ServerInfo).
type TestInfo struct {
	Version, ProductLevel, Edition string
	CompatLevel                    int
}

// TestFunc tests a connection with an explicit password (the service
// passes sqlcheck.TestConnectionInfo; tests pass fakes).
type TestFunc func(ctx context.Context, p model.Profile, password string) (TestInfo, error)

// SQLTest is the production TestFunc: sqlcheck.TestConnectionInfo
// (session + ping + one read-only 2008-compatible query for the server
// description; encrypt "disable" and the TLS 1.0 preset go the same way).
func SQLTest(ctx context.Context, p model.Profile, password string) (TestInfo, error) {
	i, err := sqlcheck.TestConnectionInfo(ctx, p, password)
	return TestInfo{Version: i.Version, ProductLevel: i.ProductLevel, Edition: i.Edition, CompatLevel: i.CompatLevel}, err
}

// Tester runs connection tests: at most Max at a time, each with a hard
// timeout on its own goroutine, so a test never blocks the poll loop or
// the IPC server (CHK-IPC-03).
type Tester struct {
	Test     TestFunc
	Category func(error) string
	Timeout  time.Duration
	Max      int // concurrent tests
	// MaxAbandoned timed-out tests may still be running in the driver.
	MaxAbandoned int
	Now          func() time.Time
	// WithTimeout derives the test context carrying the single deadline
	// (default context.WithTimeout; tests inject a fake-clock context).
	WithTimeout func(parent context.Context, d time.Duration) (context.Context, context.CancelFunc)

	mu        sync.Mutex
	running   int
	abandoned int
}

// ErrTestBusy: too many tests are running (IPC code busy).
var ErrTestBusy = &model.IPCError{Code: model.ErrCodeBusy, Key: model.MsgTestBusy, Message: "monitor: connection test busy"}

type testRun struct {
	finished, abandoned bool
}

func (t *Tester) defaults() {
	if t.Test == nil {
		t.Test = SQLTest
	}
	if t.Category == nil {
		t.Category = sqlcheck.ConnCategory
	}
	if t.Timeout <= 0 {
		t.Timeout = DefaultTestTimeout
	}
	if t.Max <= 0 {
		t.Max = DefaultTestConcurrent
	}
	if t.MaxAbandoned <= 0 {
		t.MaxAbandoned = DefaultTestAbandoned
	}
	if t.Now == nil {
		t.Now = time.Now
	}
	if t.WithTimeout == nil {
		t.WithTimeout = context.WithTimeout
	}
}

// Busy reports the running and abandoned tests (tests).
func (t *Tester) Busy() (running, abandoned int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.running, t.abandoned
}

// scrub removes the password from a message (defense in depth: sqlcheck
// never puts it into errors).
func scrub(msg, password string) string {
	if password == "" {
		return msg
	}
	return strings.ReplaceAll(msg, password, "***")
}

// Run tests p with password. It returns ErrTestBusy when Max tests (or
// MaxAbandoned timed-out ones) are running; otherwise the result (a
// timeout is a result with Category TestTimeout).
//
// There is exactly one deadline: the context of the test (phase 4b). When
// it expires the driver sees context.DeadlineExceeded and the reply is a
// timeout; when the caller's context ends first (client disconnected) the
// test context is canceled with it. Either way a test whose goroutine is
// still in the driver is counted as abandoned (at most MaxAbandoned, then
// busy) until that goroutine returns, which frees its slot.
func (t *Tester) Run(ctx context.Context, p model.Profile, password string) (model.TestProfileResult, error) {
	t.mu.Lock()
	t.defaults()
	if t.running >= t.Max || t.abandoned >= t.MaxAbandoned {
		t.mu.Unlock()
		return model.TestProfileResult{}, ErrTestBusy
	}
	t.running++
	test, category, timeout, now, withTimeout := t.Test, t.Category, t.Timeout, t.Now, t.WithTimeout
	t.mu.Unlock()

	start := now()
	res := model.TestProfileResult{ProfileID: p.ID, Encrypt: string(p.EffectiveEncrypt()), TLSMin: p.EffectiveTLSMinVersion()}
	cctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	type out struct {
		info TestInfo
		err  error
	}
	ch := make(chan out, 1)
	run := &testRun{}
	go func() {
		// as exploreRun: one non-blocking send, the abandoned count is
		// decremented exactly once whatever the outcome (also a panic)
		sent := false
		send := func(o out) {
			if sent {
				return
			}
			sent = true
			select {
			case ch <- o:
			default:
			}
		}
		defer func() {
			if r := recover(); r != nil {
				send(out{err: fmt.Errorf("internal error: %v", r)})
			}
			t.mu.Lock()
			run.finished = true
			if run.abandoned {
				run.abandoned = false
				t.abandoned--
			}
			t.mu.Unlock()
		}()
		info, err := test(cctx, p, password)
		send(out{info, err})
	}()
	end := func(abandon bool) {
		t.mu.Lock()
		t.running--
		if abandon && !run.finished {
			run.abandoned = true
			t.abandoned++
		}
		t.mu.Unlock()
	}
	select {
	case o := <-ch:
		end(false)
		res.DurationMs = now().Sub(start).Milliseconds()
		if o.err == nil {
			res.OK, res.Category, res.ErrorKey = true, model.TestOK, model.MsgTestOK
			res.ServerVersion, res.ProductLevel, res.Edition, res.CompatLevel = o.info.Version, o.info.ProductLevel, o.info.Edition, o.info.CompatLevel
			return res, nil
		}
		cat := category(o.err)
		if cat == "" || cat == model.TestOK {
			cat = model.TestOther
		}
		res.Category, res.ErrorKey = cat, model.TestCategoryKey(cat)
		var se *sqlcheck.Error
		if errors.As(o.err, &se) && KnownErrorKey(se.Key) {
			res.ErrorKey = se.Key // e.g. sql.tls_enable_tls10 (text: model.SQLMessage)
		}
		res.Error = scrub(o.err.Error(), password)
		return res, nil
	case <-cctx.Done():
		end(true)
	}
	res.DurationMs = now().Sub(start).Milliseconds()
	if !errors.Is(cctx.Err(), context.DeadlineExceeded) {
		// the caller went away (client disconnected / service stop)
		res.Category, res.ErrorKey = model.TestOther, model.TestCategoryKey(model.TestOther)
		res.Error = "connection test canceled"
		return res, nil
	}
	res.Category, res.ErrorKey = model.TestTimeout, model.MsgTestTimeout
	res.Error = fmt.Sprintf("connection test timed out after %s", timeout)
	return res, nil
}
