package config

import (
	"os"
	"strings"
	"testing"
)

func TestCHK_CFG_09_EmailPasswordDPAPI(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	s.Protector = fakeProtector{}
	if s.HasEmailPassword() {
		t.Fatal("has before save")
	}
	if pw, err := s.LoadEmailPassword(); err != nil || pw != "" {
		t.Fatalf("load empty: %q %v", pw, err)
	}
	if err := s.SaveEmailPassword("SECRET-smtp-1"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(s.Path(EmailPasswordFile))
	if len(raw) == 0 || strings.Contains(string(raw), "SECRET") || !s.HasEmailPassword() {
		t.Fatalf("blob %q has=%v", raw, s.HasEmailPassword())
	}
	if pw, err := s.LoadEmailPassword(); err != nil || pw != "SECRET-smtp-1" {
		t.Fatalf("load %q %v", pw, err)
	}
	if err := s.SaveEmailPassword(""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.Path(EmailPasswordFile)); !os.IsNotExist(err) || s.HasEmailPassword() {
		t.Fatalf("remove: %v has=%v", err, s.HasEmailPassword())
	}
}
