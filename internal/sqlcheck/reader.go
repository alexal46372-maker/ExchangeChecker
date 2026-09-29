package sqlcheck

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// DiscoveryCandidate is the IPC-ready candidate type (defined in model so
// the tray does not import the SQL driver).
type DiscoveryCandidate = model.DiscoveryCandidate

// ExchangeReader reads exchange times of one profile (PLAN.md §5.3).
type ExchangeReader interface {
	// Test checks that the profile can log in (CHK-SQL-01).
	Test(ctx context.Context) error
	// Discover proposes mappings, best first (CHK-SQL-02).
	Discover(ctx context.Context) ([]DiscoveryCandidate, error)
	// DiscoverByValue proposes mappings from a known point name and an
	// optional approximate last-exchange time (CHK-SQL-02).
	DiscoverByValue(ctx context.Context, q model.ValueQuery) ([]DiscoveryCandidate, error)
	// ListPoints reads the points by the saved mapping (CHK-SQL-03/04).
	ListPoints(ctx context.Context) ([]model.ExchangePoint, error)
}

// PlanPointLister is implemented by the readers that can list the
// exchange-plan points for the tray's point picker (CHK-SQL-02).
type PlanPointLister interface {
	NodePoints(ctx context.Context) ([]model.NodePoint, error)
}

// ReadCloser is an ExchangeReader owning a connection pool.
type ReadCloser interface {
	ExchangeReader
	Close() error
}

// DefaultMaxPoints caps the rows read by one ListPoints call.
const DefaultMaxPoints = 10000

// Options tune the readers. Zero values = defaults.
type Options struct {
	// Settings give the warning/error thresholds used to evaluate points
	// (zero value = model.DefaultSettings()).
	Settings  model.Settings
	Discover  DiscoverOptions
	MaxPoints int
	// Now is the clock (tests); nil = time.Now.
	Now func() time.Time
	// Counters keeps the counters-mode state between polls (nil = a new
	// in-memory store; the monitor passes a persistent one in phase 3).
	Counters model.CounterStateStore
}

func (o Options) withDefaults() Options {
	if o.Settings.ErrorMinutes == 0 && o.Settings.WarnMinutes == 0 {
		o.Settings = model.DefaultSettings()
	}
	if o.MaxPoints <= 0 {
		o.MaxPoints = DefaultMaxPoints
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Counters == nil {
		o.Counters = model.NewMemCounterStore()
	}
	return o
}

// StubReader returns canned data (tests, IPC/tray development).
type StubReader struct {
	TestErr     error
	Candidates  []DiscoveryCandidate
	DiscoverErr error
	Points      []model.ExchangePoint
	ListErr     error
	Nodes       []model.NodePoint
}

// NodePoints implements PlanPointLister.
func (s *StubReader) NodePoints(context.Context) ([]model.NodePoint, error) {
	return append([]model.NodePoint(nil), s.Nodes...), nil
}

// Test implements ExchangeReader.
func (s *StubReader) Test(context.Context) error { return s.TestErr }

// Discover implements ExchangeReader.
func (s *StubReader) Discover(context.Context) ([]DiscoveryCandidate, error) {
	if s.DiscoverErr != nil {
		return nil, s.DiscoverErr
	}
	return append([]DiscoveryCandidate(nil), s.Candidates...), nil
}

// DiscoverByValue implements ExchangeReader.
func (s *StubReader) DiscoverByValue(context.Context, model.ValueQuery) ([]DiscoveryCandidate, error) {
	return s.Discover(context.Background())
}

// ListPoints implements ExchangeReader.
func (s *StubReader) ListPoints(context.Context) ([]model.ExchangePoint, error) {
	if s.ListErr != nil {
		return nil, s.ListErr
	}
	return append([]model.ExchangePoint(nil), s.Points...), nil
}

// Close implements ReadCloser.
func (s *StubReader) Close() error { return nil }

// base holds what all store-backed readers share: Test and Discover.
type base struct {
	st      Store
	profile model.Profile
	opt     Options
}

// Test implements ExchangeReader.
func (b *base) Test(ctx context.Context) error { return refineTLS(b.st.Ping(ctx), b.profile) }

// Discover implements ExchangeReader.
func (b *base) Discover(ctx context.Context) ([]DiscoveryCandidate, error) {
	return Discover(ctx, b.st, b.opt.Discover, b.opt.Now())
}

// DiscoverByValue implements ExchangeReader.
func (b *base) DiscoverByValue(ctx context.Context, q model.ValueQuery) ([]DiscoveryCandidate, error) {
	return DiscoverByValue(ctx, b.st, q, b.opt.Discover, b.opt.Now())
}

// ValidateMapping checks m against this database's catalog (manual mapping).
func (b *base) ValidateMapping(ctx context.Context, m model.Mapping) error {
	return ValidateMapping(ctx, b.st, m)
}

// PreviewMapping validates m and reads up to limit converted rows through it.
func (b *base) PreviewMapping(ctx context.Context, m model.Mapping, limit int) ([]model.SampleRow, error) {
	return PreviewMapping(ctx, b.st, m, limit)
}

// Close implements ReadCloser.
func (b *base) Close() error { return b.st.Close() }

// NodePoints lists the exchange-plan points (_NodeNNN rows) for the
// tray's point picker (time-based DiscoverByValue).
func (b *base) NodePoints(ctx context.Context) ([]model.NodePoint, error) {
	return ListNodePoints(ctx, b.st, b.opt.Discover, 0)
}

// tagPlan tags points with the mapping's exchange-plan table and label.
func tagPlan(pts []model.ExchangePoint, m model.Mapping) {
	for i := range pts {
		pts[i].PlanTable, pts[i].PlanNo, pts[i].PlanLabel = m.PlanTable(), m.PlanNo(), m.PlanLabel
	}
}

// listMappings reads the primary mapping (by manual SQL when primaryManual,
// else by its table / counters) and every additional mapping (manual SQL
// wins, as in ReaderFor). Counters of all counters-mode mappings are
// applied to the profile's state in one step; keys of additional mappings
// are prefixed with their table so points of different plans never mix.
func (b *base) listMappings(ctx context.Context, primaryManual bool) ([]model.ExchangePoint, error) {
	var out []model.ExchangePoint
	var obs []model.CounterObservation
	owner := map[string]model.Mapping{}
	now := b.opt.Now()
	for i, m := range b.profile.AllMappings() {
		manual := usesManualSQL(m)
		if i == 0 {
			manual = primaryManual
		}
		switch {
		case manual:
			raw, err := b.st.ManualPoints(ctx, m.ManualSQL, b.opt.MaxPoints)
			if err != nil {
				return nil, err
			}
			pts := toPoints(raw, m.YearOffset, b.profile.ID, b.opt.Settings, now)
			tagPlan(pts, m)
			out = append(out, pts...)
		case m.Mode == model.MappingCounters:
			raw, err := b.st.Counters(ctx, m, b.opt.MaxPoints)
			if err != nil {
				return nil, err
			}
			for _, o := range CounterObservations(raw) {
				if i > 0 {
					o.Key = m.Table + "/" + o.Key
				}
				if _, dup := owner[o.Key]; !dup {
					owner[o.Key] = m
				}
				obs = append(obs, o)
			}
		default:
			raw, err := b.st.Points(ctx, m, b.opt.MaxPoints)
			if err != nil {
				return nil, err
			}
			pts := toPoints(raw, m.YearOffset, b.profile.ID, b.opt.Settings, now)
			tagPlan(pts, m)
			out = append(out, pts...)
		}
	}
	if len(obs) == 0 {
		return out, nil
	}
	times, pts, err := b.applyCounters(obs, now)
	if err != nil {
		return nil, err
	}
	for i := range pts {
		tagPlan(pts[i:i+1], owner[times[i].Key])
	}
	return append(out, pts...), nil
}

// DiscoveringReader discovers candidates and polls by the saved table
// mapping (single or linked mode).
type DiscoveringReader struct{ base }

// NewDiscoveringReader returns a reader over st for profile p.
func NewDiscoveringReader(st Store, p model.Profile, opt Options) *DiscoveringReader {
	return &DiscoveringReader{base{st: st, profile: p, opt: opt.withDefaults()}}
}

// ListPoints implements ExchangeReader: only the saved mapping is used,
// nothing is guessed during polling (PLAN.md §5.3 p.7). Mode "counters"
// reads the exchange-plan counters and returns their change times.
//
// Points of the profile's additional mappings (Profile.Mappings, e.g. a
// second exchange-plan table) are appended; every point is tagged with its
// mapping's plan table / label.
func (r *DiscoveringReader) ListPoints(ctx context.Context) ([]model.ExchangePoint, error) {
	if strings.TrimSpace(r.profile.Mapping.Table) == "" {
		return nil, &Error{Kind: KindMapping, Op: "list points", Err: ErrNotConfigured}
	}
	return r.listMappings(ctx, false)
}

// QueryReader polls by the profile's manual SQL (CHK-SQL-04).
type QueryReader struct{ base }

// NewQueryReader returns a manual-SQL reader over st for profile p.
func NewQueryReader(st Store, p model.Profile, opt Options) *QueryReader {
	return &QueryReader{base{st: st, profile: p, opt: opt.withDefaults()}}
}

// ListPoints implements ExchangeReader.
func (r *QueryReader) ListPoints(ctx context.Context) ([]model.ExchangePoint, error) {
	if strings.TrimSpace(r.profile.Mapping.ManualSQL) == "" {
		return nil, &Error{Kind: KindMapping, Op: "list points", Err: ErrNotConfigured}
	}
	return r.listMappings(ctx, true)
}

// ReaderFor picks the reader for a profile: QueryReader when a manual SQL
// is set (explicit fallback wins), otherwise DiscoveringReader.
func ReaderFor(st Store, p model.Profile, opt Options) ReadCloser {
	if usesManualSQL(p.Mapping) {
		return NewQueryReader(st, p, opt)
	}
	return NewDiscoveringReader(st, p, opt)
}

// NewReader opens a connection pool for p (lazily, no network I/O) and
// returns the matching reader. The caller must Close it.
func NewReader(p model.Profile, password string, opt Options) (ReadCloser, error) {
	st, err := Open(p, password)
	if err != nil {
		return nil, err
	}
	return ReaderFor(st, p, opt), nil
}

// Status polls r once and builds the phase-1 ProfileStatus. Any error
// (connection, timeout, query, mapping) goes to ProfileStatus.Error, which
// makes the profile LevelError (phase-1 rule); points are left empty.
func Status(ctx context.Context, r ExchangeReader, p model.Profile, now time.Time) model.ProfileStatus {
	ps := model.ProfileStatus{ProfileID: p.ID, Name: p.DisplayName(), CheckedAt: now.In(model.DBZone)}
	points, err := r.ListPoints(ctx)
	if err != nil {
		err = refineTLS(err, p)
		ps.Error, ps.ErrorKey = err.Error(), MessageKeyOf(err)
		if ps.Error == "" {
			ps.Error = errors.New("unknown error").Error()
		}
		return ps
	}
	if points == nil {
		points = []model.ExchangePoint{}
	}
	ps.Points = points
	return ps
}
