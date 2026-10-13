//go:build windows

package winapi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// LongPath of a not-yet-existing folder expands its nearest existing parent
// (7c review carry-over: /DIR may name a new folder under an 8.3 path).
func TestLongPathNonExistent(t *testing.T) {
	tmp := os.TempDir()
	p := filepath.Join(tmp, "ec-longpath-missing", "sub")
	got := LongPath(p)
	if !strings.HasSuffix(strings.ToLower(got), `\ec-longpath-missing\sub`) {
		t.Fatalf("tail lost: %q", got)
	}
	if strings.Contains(tmp, "~") && strings.Contains(got, "~") {
		t.Fatalf("8.3 parent not expanded: %q -> %q", p, got)
	}
	t.Logf("%s -> %s", p, got)
}
