package alert

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_SVC_02_EventIDs — EventIDs() is ascending, unique, bare, a fresh
// copy, and covers every EventID* the service writes (PLAN-phase7 §3.3 S1).
func TestCHK_SVC_02_EventIDs(t *testing.T) {
	ids := EventIDs()
	want := []uint32{100, 101, 102, 103, 104, 105, 110}
	if len(ids) != len(want) {
		t.Fatalf("EventIDs() = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("EventIDs() = %v, want %v", ids, want)
		}
	}
	for i, id := range ids {
		if i > 0 && ids[i-1] >= id {
			t.Errorf("not ascending/unique at %d: %v", i, ids)
		}
		if id&0xFFFF0000 != 0 || id == 0 {
			t.Errorf("event ID %#x is not a bare ID", id)
		}
	}
	ids[0] = 9999
	_ = append(ids[:0], 1) // overwrite via append must not leak either
	if again := EventIDs(); again[0] != 100 || len(again) != len(want) {
		t.Errorf("mutation leaked: %v", again)
	}
	set := map[uint32]bool{}
	for _, id := range EventIDs() {
		set[id] = true
	}

	// Every EventID* constant declared in package alert (all non-test
	// .go files) must be listed.
	fs := token.NewFileSet()
	srcs, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var pkgFiles []*ast.File
	for _, p := range srcs {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fs, p, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		pkgFiles = append(pkgFiles, f)
	}
	if len(pkgFiles) < 2 {
		t.Fatalf("package alert sources not found: %v", srcs)
	}
	vals := map[string]uint32{
		"EventIDService": EventIDService, "EventIDRecover": EventIDRecover,
		"EventIDWarning": EventIDWarning, "EventIDError": EventIDError,
		"EventIDAck": EventIDAck, "EventIDConfig": EventIDConfig,
		"EventIDInternal": EventIDInternal,
	}
	declared := 0
	for _, f := range pkgFiles {
		ast.Inspect(f, func(n ast.Node) bool {
			if vs, ok := n.(*ast.ValueSpec); ok {
				for _, nm := range vs.Names {
					if strings.HasPrefix(nm.Name, "EventID") {
						declared++
						v, ok := vals[nm.Name]
						if !ok {
							t.Errorf("%s: new event ID constant, add it to EventIDs() and this test", nm.Name)
						} else if !set[v] {
							t.Errorf("%s=%d missing from EventIDs()", nm.Name, v)
						}
					}
				}
			}
			return true
		})
	}
	if declared != len(vals) {
		t.Errorf("declared %d EventID constants, want %d", declared, len(vals))
	}

	// Every alert.EventID* used by the service (cmd/exchsvc) is known.
	files, _ := filepath.Glob(filepath.Join("..", "..", "cmd", "exchsvc", "*.go"))
	if len(files) == 0 {
		t.Fatal("cmd/exchsvc sources not found")
	}
	used := 0
	for _, p := range files {
		if strings.HasSuffix(p, "_test.go") {
			continue // only what the service itself writes
		}
		f, err := parser.ParseFile(fs, p, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if se, ok := n.(*ast.SelectorExpr); ok && strings.HasPrefix(se.Sel.Name, "EventID") && se.Sel.Name != "EventIDs" {
				if x, ok := se.X.(*ast.Ident); ok && x.Name == "alert" {
					used++
					if v, ok := vals[se.Sel.Name]; !ok || !set[v] {
						t.Errorf("%s uses alert.%s not in EventIDs()", p, se.Sel.Name)
					}
				}
			}
			return true
		})
	}
	if used == 0 {
		t.Error("no alert.EventID* usage found in cmd/exchsvc")
	}

	// IDs written by EventLogSink via EventTypeOf.
	for _, e := range []model.AlertEvent{{From: model.LevelOK, To: model.LevelWarning}, {From: model.LevelOK, To: model.LevelError}, {From: model.LevelError, To: model.LevelOK}} {
		if _, id := EventTypeOf(e); !set[id] {
			t.Errorf("EventTypeOf(%+v) = %d not in EventIDs()", e, id)
		}
	}
}

type eventIDFakeReporter struct {
	typ []uint16
	id  []uint32
	msg []string
}

func (f *eventIDFakeReporter) Report(t uint16, id uint32, m string) error {
	f.typ, f.id, f.msg = append(f.typ, t), append(f.id, id), append(f.msg, m)
	return nil
}

// TestCHK_SVC_02_EventLogSinkContract — the sink passes a bare listed ID
// and one message string per event to the Reporter (S4, Go side).
func TestCHK_SVC_02_EventLogSinkContract(t *testing.T) {
	r := &eventIDFakeReporter{}
	s := &EventLogSink{R: r, Lang: "en"}
	s.Alert(model.AlertEvent{From: model.LevelOK, To: model.LevelError})
	s.Alert(model.AlertEvent{From: model.LevelError, To: model.LevelOK})
	if len(r.id) != 2 || r.id[0] != EventIDError || r.id[1] != EventIDRecover {
		t.Fatalf("ids = %v", r.id)
	}
	for i, m := range r.msg {
		if m == "" || r.id[i]&0xFFFF0000 != 0 {
			t.Errorf("entry %d: id %#x msg %q", i, r.id[i], m)
		}
	}
}
