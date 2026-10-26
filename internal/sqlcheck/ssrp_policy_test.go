package sqlcheck

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// sqlcheckWinAPI is the WinAPI allowlist of internal/sqlcheck (SSRP ANSI
// decoding; phase 11: InstalledInstances REG_MULTI_SZ; NT 3.1+).
var sqlcheckWinAPI = []string{"CloseServiceHandle", "MultiByteToWideChar", "OpenSCManagerW", "OpenServiceW", "QueryServiceConfigW", "RegCloseKey", "RegOpenKeyExW", "RegQueryValueExW"}

// CHK-SQL-06: the package loads only allowlisted WinAPI functions and no
// os/exec.
func TestCHK_SQL_06_WinAPIAllowlist(t *testing.T) {
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`NewProc\("([A-Za-z0-9_]+)"\)`)
	var got []string
	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".go") || strings.HasSuffix(f.Name(), "_test.go") {
			continue
		}
		b, err := os.ReadFile(f.Name())
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), `"os/exec"`) {
			t.Errorf("%s imports os/exec", f.Name())
		}
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			got = append(got, m[1])
		}
	}
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(sqlcheckWinAPI, ",") {
		t.Errorf("WinAPI %v, allowlist %v", got, sqlcheckWinAPI)
	}
}
