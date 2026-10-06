package sqlcheck

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Phase 6, G1/G4 (EC Service): texts / warnings / templates in model,
// typed validation errors, sample cell limit, interactive query guard.

// TestCHK_SQL_04_MessagesInModel: sqlcheck delegates its texts, warnings
// and templates to package model (IPC without the driver).
func TestCHK_SQL_04_MessagesInModel(t *testing.T) {
	if !reflect.DeepEqual(MessageKeys(), model.SQLMessageKeys()) || len(MessageKeys()) != 5 {
		t.Errorf("keys %v", MessageKeys())
	}
	for _, k := range append(model.SQLMessageKeys(), model.SQLValidationKeys()...) {
		for _, lang := range []string{model.LangRU, model.LangEN} {
			if model.SQLMessage(k, lang) == "" || Message(k, lang) != model.SQLMessage(k, lang) {
				t.Errorf("%s/%s: %q", k, lang, model.SQLMessage(k, lang))
			}
		}
		if model.SQLMessage(k, model.LangRU) == model.SQLMessage(k, model.LangEN) {
			t.Errorf("%s: ru == en", k)
		}
		if m := model.SQLMessages()[k]; m.RU == "" || m.EN == "" {
			t.Errorf("SQLMessages[%s] = %+v", k, m)
		}
	}
	if MsgTLSEnableTLS10 != "sql.tls_enable_tls10" || MsgWarnPlaintextLogin != model.MsgSQLWarnPlaintextLogin {
		t.Error("key values changed")
	}
	// the returned map is a copy
	m := model.SQLMessages()
	m[MsgTLSCertificate] = model.LocalizedText{}
	if model.SQLMessage(MsgTLSCertificate, model.LangEN) == "" {
		t.Error("SQLMessages must return a copy")
	}
	// validation.sql_* render through model.FormatMessage (tray editor)
	got := model.FormatMessage(model.ValSQLManualForbidden, []string{"manualSql", model.ValSQLManualForbidden, "DELETE"}, model.LangEN)
	if got != `Field "manualSql": the query must be read-only; forbidden keyword DELETE.` {
		t.Errorf("format: %q", got)
	}
	// warnings
	p := connProfile()
	p.AuthMode, p.Encrypt = model.AuthSQL, model.EncryptDisable
	if !reflect.DeepEqual(Warnings(p), model.SQLWarnings(p)) || len(Warnings(p)) != 1 {
		t.Errorf("warnings %v", Warnings(p))
	}
	p.AuthMode = model.AuthWindows
	if len(model.SQLWarnings(p)) != 0 {
		t.Error("windows auth: no warning")
	}
	// templates: same SQL, old shape kept, 2008 T-SQL, valid manual SQL
	tpls := model.SQLManualTemplates()
	if len(tpls) != len(ManualSQLTemplates) || len(tpls) < 4 {
		t.Fatalf("templates %d / %d", len(tpls), len(ManualSQLTemplates))
	}
	for i, tpl := range tpls {
		old := ManualSQLTemplates[i]
		if old.Name != tpl.ID || old.SQL != tpl.SQL || old.Description != tpl.Description.EN {
			t.Errorf("template %d: %+v vs %+v", i, old, tpl)
		}
		if tpl.Title.RU == "" || tpl.Title.EN == "" || tpl.Description.RU == "" {
			t.Errorf("template %s: texts %+v", tpl.ID, tpl)
		}
		if err := ValidateManualSQL(tpl.SQL); err != nil {
			t.Errorf("template %s: %v", tpl.ID, err)
		}
	}
	tpls[0].SQL = "x"
	if model.SQLManualTemplates()[0].SQL == "x" {
		t.Error("SQLManualTemplates must return a copy")
	}
	b, _ := json.Marshal(model.SQLManualTemplates()[0])
	for _, want := range []string{`"id":"1c_node_linked"`, `"title":{"ru":`, `"en":`, `"description":`, `"sql":"SELECT`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("template JSON %s lacks %s", b, want)
		}
	}
}

// TestCHK_SQL_04_TypedManualErrors: ValidateManualSQL errors carry a
// model.FieldError (field, validation.sql_* key, args), keep the old text
// and stay errors.Is-compatible with the sentinels.
func TestCHK_SQL_04_TypedManualErrors(t *testing.T) {
	for _, c := range []struct {
		q        string
		sentinel error
		key      string
		args     []string
		text     string
	}{
		{"   ", ErrManualEmpty, model.ValSQLManualEmpty, nil, "manual SQL is empty"},
		{"-- only a comment", ErrManualEmpty, model.ValSQLManualEmpty, nil, "manual SQL is empty"},
		{"UPDATE t SET a = 1", ErrManualNotSelect, model.ValSQLManualNotSelect, nil, "must be a single SELECT"},
		{"SELECT a, b, c FROM t; SELECT 1", ErrManualMulti, model.ValSQLManualMulti, nil, "single statement"},
		{"SELECT a, b, c FROM t SELECT 1, 2, 3", ErrManualMulti, model.ValSQLManualMulti, nil, "single statement"},
		{"SELECT 'abc, b, c FROM t", ErrManualLiteral, model.ValSQLManualLiteral, nil, "unterminated"},
		{"SELECT a, b, c FROM t DELETE FROM t", ErrManualForbidden, model.ValSQLManualForbidden, []string{"DELETE"},
			"forbidden keyword DELETE (only one read-only SELECT is allowed)"},
		{"SELECT a, b, c FROM t)", ErrManualNotSelect, model.ValSQLManualNotSelect, nil, "single SELECT"},
		{"SELECT IIF(a = 1, 'x', 'y'), b, c FROM t", ErrManualCompat2008, model.ValSQLManualCompat2008, []string{"IIF"},
			"compatibility level 100): IIF (use TOP"},
	} {
		err := ValidateManualSQL(c.q)
		fe := FieldErrorOf(err)
		if !errors.Is(err, c.sentinel) || fe == nil || fe.Field != "manualSql" || fe.Key != c.key ||
			!reflect.DeepEqual(fe.Args, c.args) || !strings.Contains(err.Error(), c.text) || err.Error() != fe.Msg {
			t.Errorf("%q: %v -> %+v", c.q, err, fe)
			continue
		}
		if got := fe.IPCArgs(); got[0] != "manualSql" || got[1] != c.key {
			t.Errorf("%q: IPCArgs %v", c.q, got)
		}
		if model.FormatMessage(c.key, fe.IPCArgs(), model.LangRU) == "" {
			t.Errorf("%q: no text", c.q)
		}
		// through CheckMapping: *Error KindMapping, still typed (an empty
		// manual SQL is "not configured" there: model.Mapping.Validate)
		if c.sentinel == ErrManualEmpty {
			continue
		}
		merr := CheckMapping(nil, model.Mapping{Mode: model.MappingManual, ManualSQL: c.q})
		if KindOf(merr) != KindMapping || !errors.Is(merr, c.sentinel) || FieldErrorOf(merr) == nil ||
			FieldErrorOf(merr).Key != c.key || len(model.FieldErrors(merr)) != 1 {
			t.Errorf("%q: CheckMapping %v", c.q, merr)
		}
	}
	if err := ValidateManualSQL("SELECT a, b, c FROM t"); err != nil {
		t.Errorf("valid: %v", err)
	}
	// errors.Join of two typed errors: both are found
	j := errors.Join(ValidateManualSQL(""), ValidateManualSQL("SELECT a, b, c FROM t DELETE x"))
	if fes := model.FieldErrors(j); len(fes) != 2 || fes[0].Key != model.ValSQLManualEmpty || fes[1].Key != model.ValSQLManualForbidden {
		t.Errorf("FieldErrors(join) = %+v", fes)
	}
	if model.FieldErrors(errors.New("plain")) != nil || model.FieldErrors(nil) != nil {
		t.Error("FieldErrors of a plain error")
	}
}

// TestCHK_SQL_04_TypedMappingErrors: CheckMapping problems name the
// mapping field and pass table / column / types as args.
func TestCHK_SQL_04_TypedMappingErrors(t *testing.T) {
	var cols []Column
	for _, tb := range fx1CNode(0) {
		cols = append(cols, tb.cols...)
	}
	dup := fxT("x", "_InfoRg456", "_Fld457RRef:binary(16)")
	dup2 := fxT("y", "_InfoRg456", "_Fld457RRef:binary(16)")
	amb := append(append([]Column(nil), dup.cols...), dup2.cols...)
	good := model.Mapping{Mode: model.MappingManual, Table: "_Node123", PointNameColumn: "_Description", PointKeyColumn: "_IDRRef",
		TimesTable: "_InfoRg456", TimesLinkColumn: "_Fld457RRef", LastSendColumn: "_Fld458", LastRecvColumn: "_Fld459"}
	if err := CheckMapping(cols, good); err != nil {
		t.Fatalf("good: %v", err)
	}
	for _, c := range []struct {
		name     string
		cols     []Column
		edit     func(*model.Mapping)
		sentinel error
		field    string
		key      string
		args     []string
	}{
		{"no table", cols, func(m *model.Mapping) { m.Table = "_Node999" }, ErrTableNotFound, "table", model.ValSQLTableNotFound, []string{"_Node999"}},
		{"no times table", cols, func(m *model.Mapping) { m.TimesTable, m.TimesSchema = "_InfoRg1", "dbo" }, ErrTableNotFound, "timesTable",
			model.ValSQLTableNotFound, []string{"dbo._InfoRg1"}},
		{"ambiguous", amb, func(m *model.Mapping) {
			*m = model.Mapping{Mode: model.MappingManual, Table: "_InfoRg456", PointNameColumn: "_Fld457RRef", LastSendColumn: "x"}
		}, ErrTableAmbiguous, "schema", model.ValSQLTableAmbiguous, []string{"_InfoRg456"}},
		{"no column", cols, func(m *model.Mapping) { m.LastRecvColumn = "_Fld999" }, ErrColumnNotFound, "lastRecvColumn",
			model.ValSQLColumnNotFound, []string{"dbo._InfoRg456", "_Fld999"}},
		{"send not date", cols, func(m *model.Mapping) { m.LastSendColumn = "_Fld460" }, ErrColumnType, "lastSendColumn",
			model.ValSQLColumnType, []string{"dbo._InfoRg456", "_Fld460", "numeric", model.SQLTypesDateTime}},
		{"name binary", cols, func(m *model.Mapping) { m.PointNameColumn = "_Marked" }, ErrColumnType, "pointNameColumn",
			model.ValSQLColumnType, []string{"dbo._Node123", "_Marked", "binary", model.SQLTypesPointColumn}},
		{"link type", cols, func(m *model.Mapping) { m.PointKeyColumn = "_Code" }, ErrLinkTypeMismatch, "timesLinkColumn",
			model.ValSQLLinkTypeMismatch, []string{"dbo._InfoRg456._Fld457RRef", "binary(16)", "dbo._Node123._Code", "nvarchar(9)"}},
		{"composite type", cols, func(m *model.Mapping) {
			m.TimesLinkTypeColumn, m.TimesLinkRTRefColumn, m.TimesLinkRTRef = "_Fld460", "_Fld460", 123
		}, ErrColumnType, "timesLinkTypeColumn", model.ValSQLColumnType, []string{"dbo._InfoRg456", "_Fld460", "numeric(0)", model.SQLTypesBinary1}},
		{"long ident", cols, func(m *model.Mapping) { m.LastSendColumn = strings.Repeat("я", MaxIdentLen+1) }, nil, "lastSendColumn",
			model.ValSQLIdentifier, []string{"too_long", "128"}},
		{"counter type", cols, func(m *model.Mapping) {
			*m = model.Mapping{Mode: model.MappingCounters, Table: "_Node123", PointNameColumn: "_Description", SentNoColumn: "_Code"}
		}, ErrColumnType, "sentNoColumn", model.ValSQLColumnType, []string{"dbo._Node123", "_Code", "nvarchar", model.SQLTypesNumber}},
		{"not configured", cols, func(m *model.Mapping) { *m = model.Mapping{Mode: model.MappingManual} }, nil, "manualSql",
			model.ValRequired, nil},
	} {
		m := good
		c.edit(&m)
		err := CheckMapping(c.cols, m)
		fe := FieldErrorOf(err)
		if err == nil || KindOf(err) != KindMapping || fe == nil || fe.Field != c.field || fe.Key != c.key || !reflect.DeepEqual(fe.Args, c.args) {
			t.Errorf("%s: %v -> %+v", c.name, err, fe)
			continue
		}
		if c.sentinel != nil && !errors.Is(err, c.sentinel) {
			t.Errorf("%s: errors.Is(%v, %v) = false", c.name, err, c.sentinel)
		}
		if txt := model.FormatMessage(fe.Key, fe.IPCArgs(), model.LangEN); txt == "" || strings.Contains(txt, "{") {
			t.Errorf("%s: text %q", c.name, txt)
		}
	}
	// Mapping.Validate errors come through unchanged (model keys)
	bad := good
	bad.YearOffset = 7
	if fe := FieldErrorOf(CheckMapping(cols, bad)); fe == nil || fe.Key != model.ValEnum || fe.Field != "yearOffset" {
		t.Errorf("Validate error: %+v", fe)
	}
}

// TestCHK_SQL_02_SampleCellTruncate: sample text cells are cut to 256
// characters (runes) on a UTF-8 boundary with a flag; previews are capped
// at model.MaxSampleRows rows; Discover / DiscoverByValue rows too.
func TestCHK_SQL_02_SampleCellTruncate(t *testing.T) {
	for _, c := range []struct {
		name string
		in   string
		cut  bool
	}{
		{"short", "Магазин 1", false},
		{"256 ascii", strings.Repeat("a", 256), false},
		{"257 ascii", strings.Repeat("a", 257), true},
		{"256 cyrillic (512 bytes)", strings.Repeat("ж", 256), false},
		{"300 cyrillic", strings.Repeat("ж", 300), true},
		{"emoji", strings.Repeat("😀", 255) + "ab", true},
		{"invalid utf8", strings.Repeat("\xff", 300), true},
		{"mixed", "x" + strings.Repeat("€", 400), true},
	} {
		out, cut := model.TruncateSampleCell(c.in)
		n := utf8.RuneCountInString(out)
		if cut != c.cut || n > model.MaxSampleCellRunes || (cut && n != model.MaxSampleCellRunes) || !strings.HasPrefix(c.in, out) {
			t.Errorf("%s: cut %v, %d runes", c.name, cut, n)
		}
		if utf8.ValidString(c.in) && !utf8.ValidString(out) {
			t.Errorf("%s: split a character", c.name)
		}
	}
	long := strings.Repeat("Точка обмена ", 40) // 520 runes
	st := newFake()
	var raw []RawPoint
	for i := 0; i < 150; i++ {
		raw = append(raw, RawPoint{Name: long})
	}
	st.manual = map[string]fakeManual{"SELECT a, b, c FROM v": {rows: raw}}
	rows, err := PreviewMapping(context.Background(), st, model.Mapping{Mode: model.MappingManual, ManualSQL: "SELECT a, b, c FROM v"}, 1000)
	if err != nil || len(rows) != model.MaxSampleRows {
		t.Fatalf("preview: %d rows, %v", len(rows), err)
	}
	if !rows[0].PointTruncated || utf8.RuneCountInString(rows[0].Point) != model.MaxSampleCellRunes {
		t.Errorf("row %+v", rows[0])
	}
	b, _ := json.Marshal(rows[0])
	if !strings.Contains(string(b), `"pointTruncated":true`) {
		t.Errorf("JSON %s", b)
	}
	b, _ = json.Marshal(model.SampleRow{Point: "x"})
	if strings.Contains(string(b), "pointTruncated") {
		t.Errorf("JSON of a short row %s", b)
	}
	// Discover: a point name longer than 256 characters in an exchange plan
	tables := fx1CNode(0)
	tables[0].rows[2][4] = strings.Repeat("М", 300)
	cands, err := Discover(context.Background(), newFake(tables...), fixOpt().Discover, fixNow)
	if err != nil {
		t.Fatal(err)
	}
	seen := false
	for _, c := range cands {
		for _, r := range c.SampleRows {
			if utf8.RuneCountInString(r.Point) > model.MaxSampleCellRunes {
				t.Errorf("%s: %d runes", c.Table, utf8.RuneCountInString(r.Point))
			}
			seen = seen || r.PointTruncated
		}
	}
	if !seen {
		t.Errorf("no truncated sample row in %d candidates", len(cands))
	}
	cs := []model.DiscoveryCandidate{{Matched: &model.SampleRow{Point: long}, SampleRows: []model.SampleRow{{Point: long}, {Point: "x"}}}}
	model.TruncateCandidates(cs)
	if !cs[0].Matched.PointTruncated || !cs[0].SampleRows[0].PointTruncated || cs[0].SampleRows[1].PointTruncated {
		t.Errorf("TruncateCandidates %+v", cs[0])
	}
}

// TestCHK_SQL_02_DiscoverCanceledKind: a canceled Discover /
// DiscoverByValue / NodePoints / ValidateMapping / PreviewMapping returns
// an *Error of KindCanceled (deadline: KindTimeout), not a bare context
// error.
func TestCHK_SQL_02_DiscoverCanceledKind(t *testing.T) {
	st := newFake(fx1CNode(0)...)
	st.delay = 200 * time.Millisecond
	r := NewDiscoveringReader(st, connProfile(), fixOpt())
	ops := map[string]func(context.Context) error{
		"discover": func(ctx context.Context) error { _, err := r.Discover(ctx); return err },
		"by value": func(ctx context.Context) error {
			_, err := r.DiscoverByValue(ctx, model.ValueQuery{PointName: "Магазин"})
			return err
		},
		"node points":   func(ctx context.Context) error { _, err := r.NodePoints(ctx); return err },
		"check mapping": func(ctx context.Context) error { return r.ValidateMapping(ctx, linkedProfile(0).Mapping) },
		"preview mapping": func(ctx context.Context) error {
			_, err := r.PreviewMapping(ctx, linkedProfile(0).Mapping, 5)
			return err
		},
	}
	for name, f := range ops {
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(50*time.Millisecond, cancel)
		err := f(ctx)
		cancel()
		if KindOf(err) != KindCanceled || !errors.Is(err, context.Canceled) || ConnCategory(err) != ConnCategoryOther {
			t.Errorf("%s: %v kind %q", name, err, KindOf(err))
		}
		ctx, cancel = context.WithTimeout(context.Background(), 50*time.Millisecond)
		err = f(ctx)
		cancel()
		if KindOf(err) != KindTimeout || ConnCategory(err) != ConnCategoryTimeout {
			t.Errorf("%s deadline: %v kind %q", name, err, KindOf(err))
		}
	}
}

// pipeConns returns n connected TCP socket pairs (client side adopted by
// the guard, server side to observe the close).
func pipeConns(t *testing.T, n int) (cli, srv []net.Conn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	for i := 0; i < n; i++ {
		c, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		s, err := ln.Accept()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Close(); s.Close() })
		cli, srv = append(cli, c), append(srv, s)
	}
	return cli, srv
}

func closedByPeer(c net.Conn) bool {
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err := c.Read(make([]byte, 1))
	var ne net.Error
	return err != nil && !(errors.As(err, &ne) && ne.Timeout())
}

// TestCHK_SQL_02_QueryGuard: the interactive guard cuts only a running
// operation whose context ended, closes the sockets, reports the cause
// and is not sticky (a new session is adopted and guarded again).
func TestCHK_SQL_02_QueryGuard(t *testing.T) {
	g := newQueryGuard()
	cli, srv := pipeConns(t, 3)
	if !g.adopt(cli[:1]) || g.causeOr(nil) != nil {
		t.Fatal("adopt")
	}
	// finished operation: a later cancel does nothing
	ctx, cancel := context.WithCancel(context.Background())
	end := g.op(ctx)
	if cause := end(); cause != nil {
		t.Errorf("cause %v", cause)
	}
	cancel()
	time.Sleep(50 * time.Millisecond)
	if g.killCount() != 0 {
		t.Error("killed after the operation ended")
	}
	// running operation: cancel -> sockets closed, cause = Canceled
	ctx, cancel = context.WithCancel(context.Background())
	end = g.op(ctx)
	cancel()
	deadline := time.Now().Add(2 * time.Second)
	for g.killCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if cause := end(); !errors.Is(cause, context.Canceled) {
		t.Errorf("cause %v", cause)
	}
	if !closedByPeer(srv[0]) {
		t.Error("socket 0 not closed")
	}
	// not sticky: a new session is adopted and cut by a deadline
	if !g.adopt(cli[1:2]) {
		t.Fatal("adopt after kill")
	}
	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Millisecond)
	end = g.op(ctx)
	time.Sleep(200 * time.Millisecond)
	if cause := end(); !errors.Is(cause, context.DeadlineExceeded) {
		t.Errorf("deadline cause %v", cause)
	}
	cancel()
	if !closedByPeer(srv[1]) || g.killCount() != 2 {
		t.Errorf("socket 1 / kills %d", g.killCount())
	}
	// concurrent operations (race detector): only ended ones report a cause
	g.adopt(cli[2:])
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			end := g.op(ctx)
			if i%2 == 0 {
				cancel()
				time.Sleep(10 * time.Millisecond)
			}
			cause := end()
			if i%2 == 1 && cause != nil {
				t.Errorf("op %d: cause %v", i, cause)
			}
		}(i)
	}
	wg.Wait()
	// guarded(): an error of a cut operation becomes KindCanceled
	s := &interactiveStore{SQLStore: NewSQLStore(nil, time.Second, time.Second), g: newQueryGuard()}
	ctx, cancel = context.WithCancel(context.Background())
	_, err := guarded(s, ctx, "catalog", time.Second, func(ctx context.Context) (int, error) {
		cancel()
		<-ctx.Done()
		time.Sleep(20 * time.Millisecond)
		return 0, classify("catalog", errors.New("read tcp: use of closed network connection"), false)
	})
	if KindOf(err) != KindCanceled || ConnCategory(err) != ConnCategoryOther {
		t.Errorf("guarded cancel: %v", err)
	}
	_, err = guarded(s, context.Background(), "catalog", 20*time.Millisecond, func(ctx context.Context) (int, error) {
		<-ctx.Done()
		time.Sleep(20 * time.Millisecond)
		return 0, classify("catalog", errors.New("read tcp: use of closed network connection"), false)
	})
	if KindOf(err) != KindTimeout || ConnCategory(err) != ConnCategoryTimeout {
		t.Errorf("guarded budget: %v", err)
	}
	if v, err := guarded(s, context.Background(), "x", time.Second, func(context.Context) (int, error) { return 7, nil }); v != 7 || err != nil {
		t.Errorf("guarded ok: %v %v", v, err)
	}
}
