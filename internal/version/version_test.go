package version

import "testing"

// TestCHK_VER_01_VersionIsVar — Version is a package variable the linker
// can override with -X (a const would ignore -X silently); the -X build
// itself is checked in checks/ver01_test.go.
func TestCHK_VER_01_VersionIsVar(t *testing.T) {
	p := &Version // does not compile for a const
	if *p != "0.0.0-dev" {
		t.Fatalf("default Version = %q, want 0.0.0-dev", *p)
	}
	if ProductName != "Exchange Checker" {
		t.Fatalf("ProductName = %q", ProductName)
	}
}
