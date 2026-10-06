package sqlcheck

import (
	"context"
	"errors"
	"net"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// customMapping is the manual table mapping of ec_1c_custom.
func customMapping() model.Mapping {
	return model.Mapping{Mode: model.MappingManual, Table: "_Reference77", PointNameColumn: "_Description", PointKeyColumn: "_IDRRef",
		TimesTable: "_InfoRg88", TimesLinkColumn: "_Fld89RRef", LastSendColumn: "_Fld90", LastRecvColumn: "_Fld91", YearOffset: 2000}
}

// slowManualSQL is a read-only, SQL 2008 compatible SELECT that runs for
// minutes (the server answers ATTENTION promptly).
const slowManualSQL = "SELECT MAX(a.name), CAST(NULL AS datetime), CAST(NULL AS datetime) " +
	"FROM sys.all_objects AS a CROSS JOIN sys.all_objects AS b CROSS JOIN sys.all_objects AS c CROSS JOIN sys.all_objects AS d"

// TestCHK_SQL_02_IntegrationInteractiveCancel (phase 6 G1/G4): the
// interactive operations of EC Service (discover, discover by value, node
// points, check_mapping, sample / manual SQL preview) on a server that
// goes silent after login return promptly on the caller's cancel with
// KindCanceled (ConnCategory other), on the profile budget with
// KindTimeout, close the socket and leave no goroutine behind.
func TestCHK_SQL_02_IntegrationInteractiveCancel(t *testing.T) {
	p, pw := itProfile(t, "ec_1c_custom")
	target := net.JoinHostPort(p.Server, strconv.Itoa(p.Port))
	ops := map[string]func(ctx context.Context, r *InteractiveReader) error{
		"discover": func(ctx context.Context, r *InteractiveReader) error { _, err := r.Discover(ctx); return err },
		"discover by value": func(ctx context.Context, r *InteractiveReader) error {
			_, err := r.DiscoverByValue(ctx, model.ValueQuery{PointName: "Магазин 1"})
			return err
		},
		"node points":   func(ctx context.Context, r *InteractiveReader) error { _, err := r.NodePoints(ctx); return err },
		"check mapping": func(ctx context.Context, r *InteractiveReader) error { return r.ValidateMapping(ctx, customMapping()) },
		"sample": func(ctx context.Context, r *InteractiveReader) error {
			_, err := r.PreviewMapping(ctx, customMapping(), 5)
			return err
		},
		"manual preview": func(ctx context.Context, r *InteractiveReader) error {
			_, err := r.PreviewMapping(ctx, model.Mapping{Mode: model.MappingManual, ManualSQL: "SELECT N'ec_g1g4_marker', NULL, NULL"}, 5)
			return err
		},
		"list points": func(ctx context.Context, r *InteractiveReader) error { _, err := r.ListPoints(ctx); return err },
	}
	// control through a transparent proxy: every operation works
	px := newSilentProxy(t, target, "")
	q := silentProfile(t, px, p)
	q.Mapping = customMapping()
	r, err := NewInteractiveReader(q, pw, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for name, f := range ops {
		if err := f(itCtx(t), r); err != nil && name != "node points" {
			t.Errorf("control %s: %v", name, err)
		}
	}
	r.Close()

	base := runtime.NumGoroutine()
	type tc struct {
		name, op, marker string
		querySec         int
		cancel           time.Duration
		max              time.Duration
		kind             ErrorKind
		category         string
	}
	var cases []tc
	for name := range ops {
		marker := "INFORMATION_SCHEMA" // the catalog query
		switch name {
		case "manual preview":
			marker = "ec_g1g4_marker"
		case "sample", "list points":
			marker = "_InfoRg88" // the points query of the mapping
		}
		cases = append(cases, tc{name + " / cancel", name, marker, 30, 700 * time.Millisecond, 700*time.Millisecond + 1500*time.Millisecond, KindCanceled, ConnCategoryOther})
	}
	cases = append(cases,
		tc{"discover / silent after catalog", "discover", "SELECT TOP (@p1)", 30, 700 * time.Millisecond, 700*time.Millisecond + 1500*time.Millisecond, KindCanceled, ConnCategoryOther},
		tc{"discover / timeout", "discover", "INFORMATION_SCHEMA", 1, 0, 2*time.Second + 1500*time.Millisecond, KindTimeout, ConnCategoryTimeout},
		tc{"manual preview / timeout", "manual preview", "ec_g1g4_marker", 1, 0, 3*time.Second + 1500*time.Millisecond, KindTimeout, ConnCategoryTimeout},
	)
	for _, c := range cases {
		px := newSilentProxy(t, target, c.marker)
		q := silentProfile(t, px, p)
		q.Mapping = customMapping()
		q.ConnectTimeoutSec, q.QueryTimeoutSec = 1, c.querySec
		r, err := NewInteractiveReader(q, pw, Options{})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		if c.cancel > 0 {
			time.AfterFunc(c.cancel, cancel)
		}
		start := time.Now()
		err = ops[c.op](ctx, r)
		d := time.Since(start)
		cancel()
		if err == nil || d > c.max || KindOf(err) != c.kind || ConnCategory(err) != c.category {
			t.Errorf("%s: %v after %v (max %v) -> kind %q, category %q", c.name, err, d, c.max, KindOf(err), ConnCategory(err))
		}
		if c.kind == KindCanceled && !errors.Is(err, context.Canceled) {
			t.Errorf("%s: errors.Is(Canceled) = false: %v", c.name, err)
		}
		px.mu.Lock()
		silenced := px.silenced
		px.mu.Unlock()
		if silenced != 1 {
			t.Errorf("%s: the proxy went silent %d times", c.name, silenced)
		}
		px.waitClosed(t, c.name)
		r.Close()
		px.ln.Close()
		px.closeAll()
		px.wg.Wait()
		t.Logf("%s: returned after %v: %v", c.name, d.Round(10*time.Millisecond), err)
	}
	settleGoroutines(t, base)
}

// TestCHK_SQL_02_IntegrationInteractiveCancelResponsive: on a responsive
// server a long manual SQL preview is canceled promptly (KindCanceled);
// the guard is not sticky: the next request on the same reader opens a
// new session and works.
func TestCHK_SQL_02_IntegrationInteractiveCancelResponsive(t *testing.T) {
	p, pw := itProfile(t, "ec_1c_custom")
	p.Mapping = customMapping()
	base := runtime.NumGoroutine()
	r, err := NewInteractiveReader(p, pw, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateManualSQL(slowManualSQL); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(700*time.Millisecond, cancel)
		start := time.Now()
		_, err = r.PreviewMapping(ctx, model.Mapping{Mode: model.MappingManual, ManualSQL: slowManualSQL}, 5)
		d := time.Since(start)
		cancel()
		if KindOf(err) != KindCanceled || d > 2200*time.Millisecond {
			t.Errorf("slow manual SQL %d: %v after %v", i, err, d)
		}
		rows, err := r.PreviewMapping(itCtx(t), customMapping(), 2)
		if err != nil || len(rows) != 2 {
			t.Errorf("next request %d: %v %+v", i, err, rows)
		}
	}
	if err := r.ValidateMapping(itCtx(t), customMapping()); err != nil {
		t.Errorf("validate after cancel: %v", err)
	}
	r.Close()
	settleGoroutines(t, base)
}
