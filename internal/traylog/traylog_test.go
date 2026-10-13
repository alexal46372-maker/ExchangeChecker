package traylog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCHK_TRAY_28_Rotation — the file stays under the limit, the full one
// becomes .1 (one old file only), credentials are masked.
func TestCHK_TRAY_28_Rotation(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sub", FileName)
	l := &Logger{Path: p, Max: 200, Now: func() time.Time { return time.Date(2026, 10, 13, 21, 0, 0, 0, time.FixedZone("", 3*3600)) }}
	line := strings.Repeat("x", 40)
	for i := 0; i < 20; i++ {
		if err := l.Log(line); err != nil {
			t.Fatal(err)
		}
	}
	fi, err := os.Stat(p)
	if err != nil || fi.Size() > 200 {
		t.Fatalf("size %v %v", fi, err)
	}
	if fi, err := os.Stat(p + ".1"); err != nil || fi.Size() > 200 || fi.Size() == 0 {
		t.Fatalf(".1 %v %v", fi, err)
	}
	if _, err := os.Stat(p + ".2"); err == nil {
		t.Fatal("only one old file")
	}
	b, _ := os.ReadFile(p)
	if !strings.HasPrefix(string(b), "2026-10-13 21:00:00.000 +03:00 xxxx") || !strings.HasSuffix(string(b), "\r\n") {
		t.Errorf("line %q", b)
	}
	os.Remove(p)
	os.Remove(p + ".1")
	l.Max = 0
	l.Log("connect Password=secret; User ID=sa pwd: 'q w' login=bob\nnext")
	b, _ = os.ReadFile(p)
	s := string(b)
	for _, bad := range []string{"secret", "=sa", "bob", "q w", "\nnext"} {
		if strings.Contains(s, bad) {
			t.Errorf("leaked %q in %q", bad, s)
		}
	}
	if !strings.Contains(s, "Password=***") || !strings.Contains(s, "login=***") {
		t.Errorf("scrub %q", s)
	}
	var nl *Logger
	if nl.Log("x") != nil || (&Logger{}).Log("x") != nil {
		t.Error("nil / empty logger must be a no-op")
	}
	os.Setenv("LOCALAPPDATA", dir)
	if DefaultPath() != filepath.Join(dir, Dir, FileName) {
		t.Error(DefaultPath())
	}
}
