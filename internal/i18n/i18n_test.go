package i18n

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_TRAY_08_CatalogComplete — every catalog key has a non-empty
// (Russian) text.
func TestCHK_TRAY_08_CatalogComplete(t *testing.T) {
	for k, v := range catalog {
		if strings.TrimSpace(v) == "" {
			t.Errorf("%s: empty text", k)
		}
	}
}

// TestCHK_TRAY_08_AllKeyConstsInCatalog — every Key* constant of the
// package is in the catalog (no raw keys in the UI).
func TestCHK_TRAY_08_AllKeyConstsInCatalog(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "catalog.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, d := range f.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.CONST {
			continue
		}
		for _, s := range g.Specs {
			vs := s.(*ast.ValueSpec)
			for i, name := range vs.Names {
				if !strings.HasPrefix(name.Name, "Key") {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok {
					continue
				}
				key, _ := strconv.Unquote(lit.Value)
				n++
				if !Has(key) {
					t.Errorf("%s = %q is not in the catalog", name.Name, key)
				}
			}
		}
	}
	if n < 10 {
		t.Fatalf("only %d Key* constants found", n)
	}
}

// TestCHK_TRAY_08_ServiceKeys — every monitor.* key of the service renders
// in both languages; unknown keys never give an empty text.
func TestCHK_TRAY_08_ServiceKeys(t *testing.T) {
	for _, k := range model.MonitorMessageKeys() {
		ru := Message(k)
		if ru == "" {
			t.Errorf("%s: ru %q", k, ru)
		}
	}
	if got := Message("sql.no_such_key"); !strings.Contains(got, "sql.no_such_key") {
		t.Errorf("unknown key text %q must name the key", got)
	}
	// sql.* texts come from model.SQLMessage (G1, phase 6d: failed / test errors)
	for _, k := range model.SQLMessageKeys() {
		if ru := Message(k); ru != model.SQLMessage(k) || ru == "" {
			t.Errorf("%s: %q", k, ru)
		}
	}
	if Message("") != "" {
		t.Error("empty key -> empty text")
	}
	if T("no.such.key") != "no.such.key" {
		t.Error("unknown catalog key is returned unchanged")
	}
}
