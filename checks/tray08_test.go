package checks

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
)

// TestCHK_TRAY_08_NoCyrillicLiterals — i18n audit (phase 6e): the tray UI
// packages carry no Russian text in string literals of non-test code; every
// RU/EN text comes from internal/i18n/catalog.go.
func TestCHK_TRAY_08_NoCyrillicLiterals(t *testing.T) {
	root := ".."
	dirs := []string{"internal/wintray", "internal/trayui", "internal/trayalert", "internal/traysettings", "internal/trayipc", "cmd/exchtray"}
	n := 0
	for _, d := range dirs {
		files, err := filepath.Glob(filepath.Join(root, d, "*.go"))
		if err != nil || len(files) == 0 {
			t.Fatalf("%s: no files (%v)", d, err)
		}
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			fs := token.NewFileSet()
			file, err := parser.ParseFile(fs, f, src, parser.SkipObjectResolution)
			if err != nil {
				t.Fatal(err)
			}
			n++
			ast.Inspect(file, func(node ast.Node) bool {
				lit, ok := node.(*ast.BasicLit)
				if !ok || (lit.Kind != token.STRING && lit.Kind != token.CHAR) {
					return true
				}
				for _, r := range lit.Value {
					if unicode.Is(unicode.Cyrillic, r) {
						t.Errorf("CHK-TRAY-08: %s: Cyrillic literal %s (move it to internal/i18n/catalog.go)", fs.Position(lit.Pos()), lit.Value)
						break
					}
				}
				return true
			})
		}
	}
	if n < 20 {
		t.Errorf("only %d files audited", n)
	}
}
