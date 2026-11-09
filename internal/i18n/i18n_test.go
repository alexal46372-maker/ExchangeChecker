package i18n

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"

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

// TestCHK_TRAY_86_NoLatinOnlyTexts — phase 13, Russian only: no catalog
// text is purely Latin (a leftover English text). A text with a Latin
// letter must have a Cyrillic one too, unless it is one of the names
// below (product, protocol and format names, sample values), each with
// exactly this text.
func TestCHK_TRAY_86_NoLatinOnlyTexts(t *testing.T) {
	names := map[string]string{
		"about.ok":            "OK",
		"app.name":            "Exchange Checker",
		"cue.disc_schema":     "dbo",
		"cue.em_from":         "monitor@example.com",
		"cue.em_host":         "smtp.example.com",
		"set.em_tls_mode":     "TLS:",
		"set.em_tls_starttls": "STARTTLS",
		"set.nav_telegram":    "Telegram",
		"set.preset_2008":     "SQL Server 2008 / 2008 R2",
		"st.filter_csv":       "CSV (;)",
		"wiz.check_browser":   "SQL Server Browser",
		"wiz.check_dns":       "DNS",
		"wiz.check_prelogin":  "PRELOGIN",
		"wiz.check_tcp":       "TCP",
	}
	for k, v := range catalog {
		if latinOnly(v) && names[k] != v {
			t.Errorf("%s = %q: purely Latin text (English left over?)", k, v)
		}
	}
	for k, v := range names {
		if catalog[k] != v {
			t.Errorf("exception %s = %q is stale: catalog has %q", k, v, catalog[k])
		}
	}
	// the check itself: English is caught, Russian and bare formats pass
	for v, want := range map[string]bool{"Strict (TDS 8.0)": true, "Settings": true, "%s: Cancel": true,
		"Строгое (TDS 8.0)": false, "%s: %s — %s": false, "[%d%%]": false, " (+%d)": false} {
		if latinOnly(v) != want {
			t.Errorf("latinOnly(%q) != %v", v, want)
		}
	}
}

// fmtVerb is a fmt verb of a catalog text (%s, %d, %%, %-5.1f…).
var fmtVerb = regexp.MustCompile(`%[-+# 0-9.]*[a-zA-Z%]`)

// latinOnly: the text (fmt verbs aside) has a Latin letter and no
// Cyrillic one.
func latinOnly(v string) bool {
	latin, cyr := false, false
	for _, r := range fmtVerb.ReplaceAllString(v, "") {
		latin = latin || unicode.In(r, unicode.Latin)
		cyr = cyr || unicode.In(r, unicode.Cyrillic)
	}
	return latin && !cyr
}
