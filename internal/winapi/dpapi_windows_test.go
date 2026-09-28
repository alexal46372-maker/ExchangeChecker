//go:build windows

package winapi

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestCHK_CFG_02_DPAPIRoundtripLocalMachine — real CryptProtectData /
// CryptUnprotectData with the machine key (runs in the CI windows job).
func TestCHK_CFG_02_DPAPIRoundtripLocalMachine(t *testing.T) {
	plain := []byte("пароль-Secret-123!")
	ent := []byte("test-entropy")
	blob, err := ProtectData(plain, ent, CryptProtectLocalMachine|CryptProtectUIForbidden)
	if err != nil {
		t.Fatalf("ProtectData: %v", err)
	}
	if len(blob) == 0 || bytes.Contains(blob, plain) {
		t.Fatalf("blob empty or contains plaintext (len %d)", len(blob))
	}
	got, err := UnprotectData(blob, ent, CryptProtectUIForbidden)
	if err != nil {
		t.Fatalf("UnprotectData: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("roundtrip mismatch: %q", got)
	}
	if _, err := UnprotectData(blob, []byte("other-entropy"), CryptProtectUIForbidden); err == nil {
		t.Error("wrong entropy must fail")
	}
	tampered := append([]byte(nil), blob...)
	tampered[len(tampered)-1] ^= 0xff
	if _, err := UnprotectData(tampered, ent, CryptProtectUIForbidden); err == nil {
		t.Error("tampered blob must fail")
	}
	if _, err := UnprotectData(nil, ent, CryptProtectUIForbidden); err == nil {
		t.Error("empty blob must fail")
	}
}

func TestCHK_CFG_02_DPAPINoEntropy(t *testing.T) {
	blob, err := ProtectData([]byte("x"), nil, CryptProtectLocalMachine|CryptProtectUIForbidden)
	if err != nil {
		t.Fatalf("ProtectData: %v", err)
	}
	got, err := UnprotectData(blob, nil, CryptProtectUIForbidden)
	if err != nil || string(got) != "x" {
		t.Fatalf("UnprotectData = %q, %v", got, err)
	}
}

func TestCHK_CFG_02_MoveFileReplace(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "new.tmp")
	dst := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(dst, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := MoveFileReplace(src, dst); err != nil {
		t.Fatalf("MoveFileReplace: %v", err)
	}
	b, err := os.ReadFile(dst)
	if err != nil || string(b) != "new" {
		t.Fatalf("dst = %q, %v", b, err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("src still exists: %v", err)
	}
	if err := MoveFileReplace(filepath.Join(dir, "missing"), dst); err == nil {
		t.Error("missing source must fail")
	}
}
