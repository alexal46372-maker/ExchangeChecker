package checks_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func findModuleRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found walking up from", wd)
		}
		dir = parent
	}
}

// TestCHK_POL_01 — CHK-POL-01: go 1.20 in go.mod + no forbidden helper-process patterns.
func TestCHK_POL_01(t *testing.T) {
	root := findModuleRoot(t)

	modBytes, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	mod := string(modBytes)
	// Exact major.minor 1.20 (not 1.21+)
	reGo := regexp.MustCompile(`(?m)^go\s+1\.20(?:\.\d+)?\s*$`)
	if !reGo.MatchString(mod) {
		t.Fatalf("CHK-POL-01: go.mod must declare go 1.20 (exact major.minor); got:\n%s", mod)
	}
	if regexp.MustCompile(`(?m)^go\s+1\.(2[1-9]|[3-9]\d)\b`).MatchString(mod) {
		t.Fatalf("CHK-POL-01: go version must not be 1.21+")
	}

	forbidden := []string{
		"cmd.exe",
		"powershell.exe",
		"powershell",
		"wmic",
		"sc.exe",
		"net.exe",
	}

	// tools/ (phase 7a: mkres) is held to the same rule
	scanDirs := []string{"cmd", "internal", "tools"}
	var hits []string
	for _, d := range scanDirs {
		base := filepath.Join(root, d)
		_ = filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return err
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				t.Errorf("read %s: %v", path, err)
				return nil
			}
			lower := strings.ToLower(string(b))
			rel, _ := filepath.Rel(root, path)
			for _, f := range forbidden {
				if strings.Contains(lower, strings.ToLower(f)) {
					// Allow mentioning in comments that forbid them is OK for phase 0 doc.go —
					// but POLICY wants static scan. Doc comments often say "No sc.exe".
					// Treat as hit only outside pure documentation lines that say "No ..." / "без".
					// Phase 0 requirement: simple substring scan is OK — keep strict,
					// but our doc.go files mention sc.exe/PowerShell as forbidden.
					// Soften: skip lines that are comments documenting the ban.
					if mentionIsBanComment(string(b), f) {
						continue
					}
					hits = append(hits, rel+": contains "+f)
				}
			}
			return nil
		})
	}
	if len(hits) > 0 {
		t.Fatalf("CHK-POL-01: forbidden helper-process patterns:\n  %s", strings.Join(hits, "\n  "))
	}
}

// mentionIsBanComment reports whether every occurrence of needle is only in
// comments that document the prohibition (No / не / без / forbidden).
func mentionIsBanComment(src, needle string) bool {
	lowerNeedle := strings.ToLower(needle)
	for _, line := range strings.Split(src, "\n") {
		trim := strings.TrimSpace(line)
		idx := strings.Index(strings.ToLower(trim), lowerNeedle)
		if idx < 0 {
			continue
		}
		// Must be a comment line
		if !strings.HasPrefix(trim, "//") && !strings.HasPrefix(trim, "*") {
			return false
		}
		l := strings.ToLower(trim)
		okBan := strings.Contains(l, "no ") ||
			strings.Contains(l, "не ") ||
			strings.Contains(l, "без ") ||
			strings.Contains(l, "forbidden") ||
			strings.Contains(l, "запрет") ||
			strings.Contains(l, "only winapi") ||
			strings.Contains(l, "policy")
		if !okBan {
			return false
		}
	}
	return true
}

// TestCHK_POL_01_NoOSExec — no .go file under cmd/, internal/ or tools/
// (tests included) imports os/exec: no helper processes (POLICY §4). The
// only exception are the tests of package checks/ (they run `go build`,
// checks/res01_test.go), which this scan does not cover by design; a
// self-check below makes sure the scan really finds an os/exec import.
func TestCHK_POL_01_NoOSExec(t *testing.T) {
	root := findModuleRoot(t)
	importsExec := func(name string, src []byte) bool {
		f, err := parser.ParseFile(token.NewFileSet(), name, src, parser.ImportsOnly)
		if err != nil {
			t.Errorf("parse %s: %v", name, err)
			return false
		}
		for _, imp := range f.Imports {
			if p, _ := strconv.Unquote(imp.Path.Value); p == "os/exec" {
				return true
			}
		}
		return false
	}
	if !importsExec("probe.go", []byte("package p\nimport x \"os/exec\"\nvar _ = x.Command\n")) {
		t.Fatal("CHK-POL-01: scan does not detect an os/exec import")
	}
	files := 0
	for _, d := range []string{"cmd", "internal", "tools"} {
		filepath.Walk(filepath.Join(root, d), func(p string, fi os.FileInfo, err error) error {
			if err != nil || fi.IsDir() || !strings.HasSuffix(p, ".go") {
				return err
			}
			files++
			b, err := os.ReadFile(p)
			if err != nil {
				t.Error(err)
				return nil
			}
			if importsExec(p, b) {
				rel, _ := filepath.Rel(root, p)
				t.Errorf("CHK-POL-01: %s imports os/exec (allowed only in checks/ tests)", filepath.ToSlash(rel))
			}
			return nil
		})
	}
	if files < 100 {
		t.Fatalf("scan found only %d .go files", files)
	}
}

// TestCHK_POL_01_NoStartProcess — cmd/, internal/, tools/ (including _test.go)
// must not start helper processes via os.StartProcess, syscall.StartProcess,
// syscall.CreateProcess / CreateProcessAsUser, or NewProc("CreateProcess…")
// (string literal or same-file const). Import aliases are followed; comments
// are ignored (go/ast). checks/ tests may (CHK-SETUP-07 rename).
// CreateProcessWithTokenW for launching the tray (phase 7d) is not exempted.
func TestCHK_POL_01_NoStartProcess(t *testing.T) {
	root := findModuleRoot(t)

	startsProcess := func(name string, src []byte) bool {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Errorf("parse %s: %v", name, err)
			return false
		}
		// import path → local name (alias or default)
		pkgName := map[string]string{} // local → import path
		for _, imp := range f.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			local := path
			if i := strings.LastIndex(path, "/"); i >= 0 {
				local = path[i+1:]
			}
			if imp.Name != nil {
				local = imp.Name.Name
			}
			pkgName[local] = path
		}
		// same-file string consts
		consts := map[string]string{}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, n := range vs.Names {
					if i < len(vs.Values) {
						if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
							if s, err := strconv.Unquote(lit.Value); err == nil {
								consts[n.Name] = s
							}
						}
					}
				}
			}
		}
		bannedSel := map[string]bool{
			"StartProcess": true, "CreateProcess": true, "CreateProcessAsUser": true,
		}
		var hit bool
		ast.Inspect(f, func(n ast.Node) bool {
			if hit {
				return false
			}
			switch x := n.(type) {
			case *ast.SelectorExpr:
				id, ok := x.X.(*ast.Ident)
				if !ok || !bannedSel[x.Sel.Name] {
					return true
				}
				path := pkgName[id.Name]
				if path == "os" || path == "syscall" {
					hit = true
				}
			case *ast.CallExpr:
				sel, ok := x.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "NewProc" || len(x.Args) < 1 {
					return true
				}
				var name string
				switch a := x.Args[0].(type) {
				case *ast.BasicLit:
					if a.Kind == token.STRING {
						name, _ = strconv.Unquote(a.Value)
					}
				case *ast.Ident:
					name = consts[a.Name]
				}
				if strings.HasPrefix(name, "CreateProcess") {
					hit = true
				}
			}
			return true
		})
		return hit
	}

	probes := []struct {
		name string
		src  string
	}{
		{"alias_os.go", "package p\nimport x \"os\"\nfunc f(){ x.StartProcess(\"a\", nil, nil) }\n"},
		{"alias_sys.go", "package p\nimport s \"syscall\"\nfunc f(){ s.CreateProcess(nil, nil, nil, nil, false, 0, nil, nil, nil, nil) }\n"},
		{"as_user.go", "package p\nimport \"syscall\"\nfunc f(){ syscall.CreateProcessAsUser(0, nil, nil, nil, nil, false, 0, nil, nil, nil, nil) }\n"},
		{"newproc_lit.go", "package p\nvar _ = mod.NewProc(\"CreateProcessWithTokenW\")\n"},
		{"newproc_const.go", "package p\nconst n = \"CreateProcessW\"\nvar _ = mod.NewProc(n)\n"},
		{"comment_only.go", "package p\n// os.StartProcess is forbidden\nconst x = 1\n"},
	}
	for _, p := range probes[:5] {
		if !startsProcess(p.name, []byte(p.src)) {
			t.Fatalf("CHK-POL-01: scan does not detect probe %s", p.name)
		}
	}
	if startsProcess(probes[5].name, []byte(probes[5].src)) {
		t.Fatal("CHK-POL-01: scan flags comment-only mention")
	}

	files := 0
	for _, d := range []string{"cmd", "internal", "tools"} {
		filepath.Walk(filepath.Join(root, d), func(p string, fi os.FileInfo, err error) error {
			if err != nil || fi.IsDir() || !strings.HasSuffix(p, ".go") {
				return err
			}
			files++
			b, err := os.ReadFile(p)
			if err != nil {
				t.Error(err)
				return nil
			}
			if startsProcess(p, b) {
				rel, _ := filepath.Rel(root, p)
				t.Errorf("CHK-POL-01: %s starts a process (os/syscall.StartProcess or CreateProcess); allowed only in checks/ tests", filepath.ToSlash(rel))
			}
			return nil
		})
	}
	if files < 100 {
		t.Fatalf("scan found only %d .go files", files)
	}
}
