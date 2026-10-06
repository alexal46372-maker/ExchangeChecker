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

// TestCHK_TRAY_08_CatalogComplete — every catalog key has non-empty RU and
// EN texts with the same format verbs.
func TestCHK_TRAY_08_CatalogComplete(t *testing.T) {
	for k, v := range catalog {
		if strings.TrimSpace(v[0]) == "" || strings.TrimSpace(v[1]) == "" {
			t.Errorf("%s: empty translation %q", k, v)
		}
		if verbs(v[0]) != verbs(v[1]) {
			t.Errorf("%s: format verbs differ: %q vs %q", k, v[0], v[1])
		}
	}
}

func verbs(s string) string {
	var b strings.Builder
	for i := 0; i+1 < len(s); i++ {
		if s[i] == '%' {
			b.WriteByte(s[i+1])
			i++
		}
	}
	return b.String()
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

// TestCHK_TRAY_08_DefaultLanguage — explicit override wins, otherwise the
// OS UI language, anything but RU/EN -> RU.
func TestCHK_TRAY_08_DefaultLanguage(t *testing.T) {
	os := func(l string) func() string { return func() string { return l } }
	cases := []struct {
		override, os, want string
	}{
		{"", "ru", RU}, {"", "en", EN}, {"", "de", RU}, {"", "", RU},
		{"en", "ru", EN}, {"ru", "en", RU}, {"EN", "ru", EN}, {"fr", "en", EN},
	}
	for _, c := range cases {
		if got := Resolve(c.override, os(c.os)); got != c.want {
			t.Errorf("Resolve(%q, os=%q) = %q, want %q", c.override, c.os, got, c.want)
		}
	}
	if Resolve("", nil) != RU {
		t.Error("no OS language -> RU")
	}
	if d := Default(); d != RU && d != EN {
		t.Errorf("Default() = %q", d)
	}
}

// TestCHK_TRAY_08_ServiceKeys — every monitor.* key of the service renders
// in both languages; unknown keys never give an empty text.
func TestCHK_TRAY_08_ServiceKeys(t *testing.T) {
	for _, k := range model.MonitorMessageKeys() {
		ru, en := Message(k, RU), Message(k, EN)
		if ru == "" || en == "" || ru == en {
			t.Errorf("%s: ru %q en %q", k, ru, en)
		}
	}
	if got := Message("sql.no_such_key", RU); !strings.Contains(got, "sql.no_such_key") {
		t.Errorf("unknown key text %q must name the key", got)
	}
	// sql.* texts come from model.SQLMessage (G1, phase 6d: failed / test errors)
	for _, k := range model.SQLMessageKeys() {
		if ru, en := Message(k, RU), Message(k, EN); ru != model.SQLMessage(k, RU) || en != model.SQLMessage(k, EN) || ru == "" {
			t.Errorf("%s: %q / %q", k, ru, en)
		}
	}
	if Message("", RU) != "" {
		t.Error("empty key -> empty text")
	}
	if T("no.such.key", EN) != "no.such.key" {
		t.Error("unknown catalog key is returned unchanged")
	}
}
