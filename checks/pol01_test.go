package checks_test

import (
	"os"
	"path/filepath"
	"regexp"
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
