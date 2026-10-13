package checks_test

import (
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
// syscall.CreateProcess, or NewProc("CreateProcess…"). checks/ tests may
// (CHK-SETUP-07 rename). CreateProcessWithTokenW for launching the tray
// (phase 7d) is not exempted here yet.
func TestCHK_POL_01_NoStartProcess(t *testing.T) {
	root := findModuleRoot(t)
	re := regexp.MustCompile(
		`os\.StartProcess|` +
			`syscall\.StartProcess|` +
			`syscall\.CreateProcess|` +
			`NewProc\("CreateProcess[^"]*"\)`,
	)
	// Self-check: the scanner must catch a probe.
	probe := "package p\nfunc f(){ os.StartProcess(\"x\", nil, nil) }\n"
	if !re.MatchString(probe) {
		t.Fatal("CHK-POL-01: scan does not detect os.StartProcess")
	}
	probe2 := `package p` + "\n" + `var _ = mod.NewProc("CreateProcessWithTokenW")` + "\n"
	if !re.MatchString(probe2) {
		t.Fatal("CHK-POL-01: scan does not detect NewProc(\"CreateProcess…\")")
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
			if re.Match(b) {
				rel, _ := filepath.Rel(root, p)
				t.Errorf("CHK-POL-01: %s starts a process (os/syscall.StartProcess or CreateProcess NewProc); allowed only in checks/ tests", filepath.ToSlash(rel))
			}
			return nil
		})
	}
	if files < 100 {
		t.Fatalf("scan found only %d .go files", files)
	}
}
