package sqlcheck

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Interactive one-shot operations (phase 6, G2/G3 prerequisites; G1/G4
// task, CHK-SQL-02/03/04): discover, discover by value, node points,
// check_mapping (ValidateMapping) and sample / manual SQL preview
// (PreviewMapping) are started by a tray request and canceled by the
// service through the context (cancel {ref}).
//
// go-mssqldb v1.8.2 answers the end of a query context after login with
// ATTENTION and then waits for the confirmation with a blocking read
// without deadline (see sessionHold). On a responsive server that is fine:
// the server confirms and the query returns at once with the context
// error. On a server that went silent after login the call would hang
// until TCP keepalive gives up. Interactive stores therefore run every
// Store call as an operation of a queryGuard: when the operation's context
// ends (the caller's cancel / deadline, the Discovery budget, the
// per-query timeout of Discovery or the store's query + connect timeout)
// while the call runs, the session socket is expired and closed, so the
// blocked read returns and the call reports KindCanceled / KindTimeout.
// Unlike sessionHold the guard is not sticky: the next call dials a new
// session (Discovery skips the table whose query timed out and goes on).
// Long-lived poll stores (Open / NewReader) are not guarded: a slow but
// healthy poll query is never cut; the monitor's hung-reader handling
// (CHK-MON-13) covers them.

// queryGuard keeps the sockets of an interactive store and kills them
// when an operation's context ends while the operation runs.
type queryGuard struct {
	mu     sync.Mutex
	conns  []net.Conn
	gen    int
	active map[int]bool
	causes map[int]error // kill cause of a running operation
	kills  int
}

func newQueryGuard() *queryGuard {
	return &queryGuard{active: map[int]bool{}, causes: map[int]error{}}
}

// adopt implements sessionKeeper: every new session is accepted.
func (g *queryGuard) adopt(c []net.Conn) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.conns = append(g.conns, c...)
	return true
}

// causeOr implements sessionKeeper (adopt never refuses).
func (g *queryGuard) causeOr(def error) error { return def }

// op starts an operation bounded by ctx; end returns the kill cause (nil
// when the operation was not cut).
func (g *queryGuard) op(ctx context.Context) (end func() error) {
	g.mu.Lock()
	g.gen++
	gen := g.gen
	g.active[gen] = true
	g.mu.Unlock()
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		select {
		case <-stop:
		case <-ctx.Done():
			g.kill(gen, ctx.Err())
		}
	}()
	return func() error {
		g.mu.Lock()
		delete(g.active, gen)
		g.mu.Unlock()
		close(stop)
		<-done
		g.mu.Lock()
		defer g.mu.Unlock()
		cause := g.causes[gen]
		delete(g.causes, gen)
		return cause
	}
}

func (g *queryGuard) kill(gen int, cause error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.active[gen] {
		return
	}
	g.causes[gen] = cause
	g.kills++
	for _, c := range g.conns {
		_ = c.SetDeadline(time.Unix(1, 0))
		_ = c.Close()
	}
	g.conns = nil
}

// killCount reports how many operations were cut (tests).
func (g *queryGuard) killCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.kills
}

// interactiveStore is a Store whose calls run under a queryGuard.
type interactiveStore struct {
	*SQLStore
	g *queryGuard
}

// OpenInteractive creates a lazily-connecting Store for one interactive
// request of profile p (discover, check_mapping, sample, manual SQL
// preview, node points). Every call is bounded by its context and by the
// profile's query + connect timeout (Ping: 2 x connect timeout): when the
// bound ends while the call runs, the session socket is closed, so the
// call returns promptly even if the server went silent after login, with
// an *Error of KindCanceled (caller cancel) or KindTimeout. Use one store
// per request (calls on one store are serialized on its single session;
// a cancel of one call closes the session of the store) and Close it.
func OpenInteractive(p model.Profile, password string) (Store, error) {
	return openInteractive(p, password)
}

func openInteractive(p model.Profile, password string) (*interactiveStore, error) {
	g := newQueryGuard()
	st, err := openWith(p, password, g)
	if err != nil {
		return nil, err
	}
	return &interactiveStore{SQLStore: st, g: g}, nil
}

// guarded runs f as one guarded operation with ctx bounded by budget.
func guarded[T any](s *interactiveStore, ctx context.Context, op string, budget time.Duration, f func(context.Context) (T, error)) (T, error) {
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	end := s.g.op(ctx)
	v, err := f(ctx)
	if cause := end(); cause != nil && err != nil {
		kind := KindTimeout
		if errors.Is(cause, context.Canceled) {
			kind = KindCanceled
		}
		var zero T
		return zero, &Error{Kind: kind, Op: op, Err: &hsError{err: err, cause: cause}}
	}
	return v, err
}

func (s *interactiveStore) qbudget() time.Duration { return s.queryTimeout + s.connectTimeout }

// Ping implements Store.
func (s *interactiveStore) Ping(ctx context.Context) error {
	_, err := guarded(s, ctx, "connect", 2*s.connectTimeout, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, s.SQLStore.Ping(ctx)
	})
	return err
}

// Columns implements Store.
func (s *interactiveStore) Columns(ctx context.Context) ([]Column, error) {
	return guarded(s, ctx, "catalog", s.qbudget(), s.SQLStore.Columns)
}

// RowCounts implements Store (two queries at most: sys.partitions, then
// sysindexes).
func (s *interactiveStore) RowCounts(ctx context.Context) (map[TableRef]int64, error) {
	return guarded(s, ctx, "row counts", 2*s.qbudget(), s.SQLStore.RowCounts)
}

// SampleColumns implements Store.
func (s *interactiveStore) SampleColumns(ctx context.Context, t TableRef, cols []string, limit int) ([][]interface{}, error) {
	return guarded(s, ctx, "sample", s.qbudget(), func(ctx context.Context) ([][]interface{}, error) {
		return s.SQLStore.SampleColumns(ctx, t, cols, limit)
	})
}

// LinkStats implements Store.
func (s *interactiveStore) LinkStats(ctx context.Context, q LinkQuery) (LinkStats, error) {
	return guarded(s, ctx, "link stats", s.qbudget(), func(ctx context.Context) (LinkStats, error) {
		return s.SQLStore.LinkStats(ctx, q)
	})
}

// SingleStats implements Store.
func (s *interactiveStore) SingleStats(ctx context.Context, q SingleQuery) (SingleStats, error) {
	return guarded(s, ctx, "single stats", s.qbudget(), func(ctx context.Context) (SingleStats, error) {
		return s.SQLStore.SingleStats(ctx, q)
	})
}

// Points implements Store.
func (s *interactiveStore) Points(ctx context.Context, m model.Mapping, limit int) ([]RawPoint, error) {
	return guarded(s, ctx, "points", s.qbudget(), func(ctx context.Context) ([]RawPoint, error) {
		return s.SQLStore.Points(ctx, m, limit)
	})
}

// SearchText implements Store.
func (s *interactiveStore) SearchText(ctx context.Context, t TableRef, selectCols, textCols []string, pattern string, limit int) ([][]interface{}, error) {
	return guarded(s, ctx, "search text", s.qbudget(), func(ctx context.Context) ([][]interface{}, error) {
		return s.SQLStore.SearchText(ctx, t, selectCols, textCols, pattern, limit)
	})
}

// SearchRef implements Store.
func (s *interactiveStore) SearchRef(ctx context.Context, t TableRef, selectCols, refCols []string, key []byte, limit int) ([][]interface{}, error) {
	return guarded(s, ctx, "search ref", s.qbudget(), func(ctx context.Context) ([][]interface{}, error) {
		return s.SQLStore.SearchRef(ctx, t, selectCols, refCols, key, limit)
	})
}

// SearchComposite implements Store.
func (s *interactiveStore) SearchComposite(ctx context.Context, t TableRef, selectCols []string, ref CompositeRef, key []byte, limit int) ([][]interface{}, error) {
	return guarded(s, ctx, "search composite", s.qbudget(), func(ctx context.Context) ([][]interface{}, error) {
		return s.SQLStore.SearchComposite(ctx, t, selectCols, ref, key, limit)
	})
}

// Counters implements Store.
func (s *interactiveStore) Counters(ctx context.Context, m model.Mapping, limit int) ([]RawCounter, error) {
	return guarded(s, ctx, "counters", s.qbudget(), func(ctx context.Context) ([]RawCounter, error) {
		return s.SQLStore.Counters(ctx, m, limit)
	})
}

// ManualPoints implements Store (query plus the READ COMMITTED restore of
// the session, both inside the operation).
func (s *interactiveStore) ManualPoints(ctx context.Context, query string, limit int) ([]RawPoint, error) {
	return guarded(s, ctx, "manual sql", s.qbudget()+s.queryTimeout, func(ctx context.Context) ([]RawPoint, error) {
		return s.SQLStore.ManualPoints(ctx, query, limit)
	})
}

// InteractiveReader serves the interactive requests of one profile on an
// interactive store (OpenInteractive): Test, Discover, DiscoverByValue,
// NodePoints, ValidateMapping, PreviewMapping and ListPoints. Errors of a
// canceled / expired context are *Error with KindCanceled / KindTimeout
// (ConnCategory "other" / "timeout"); validation problems carry a
// *model.FieldError (FieldErrorOf). Create one per request and Close it.
type InteractiveReader struct{ base }

// NewInteractiveReader opens an interactive store for p (lazily, no
// network I/O) and returns its reader.
func NewInteractiveReader(p model.Profile, password string, opt Options) (*InteractiveReader, error) {
	st, err := openInteractive(p, password)
	if err != nil {
		return nil, err
	}
	return &InteractiveReader{base{st: st, profile: p, opt: opt.withDefaults()}}, nil
}

// ListPoints implements ExchangeReader with the reader ReaderFor picks
// (manual SQL or table mapping).
func (r *InteractiveReader) ListPoints(ctx context.Context) ([]model.ExchangePoint, error) {
	pts, err := ReaderFor(r.st, r.profile, r.opt).ListPoints(ctx)
	return pts, ctxErr("list points", err)
}
