package main

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/wininstall"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winres"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

// TestCHK_DELIV_01_Version — tag vX.Y.Z → X.Y.Z; other builds 0.0.0-ci.N.
func TestCHK_DELIV_01_Version(t *testing.T) {
	cases := []struct {
		env  map[string]string
		want string
		ok   bool
	}{
		{map[string]string{"GITHUB_REF_TYPE": "tag", "GITHUB_REF_NAME": "v1.2.3", "GITHUB_RUN_NUMBER": "9"}, "1.2.3", true},
		{map[string]string{"GITHUB_REF_TYPE": "branch", "GITHUB_REF_NAME": "v1.2.3", "GITHUB_RUN_NUMBER": "9"}, "0.0.0-ci.9", true},
		{map[string]string{"GITHUB_REF_TYPE": "branch", "GITHUB_EVENT_NAME": "workflow_dispatch", "GITHUB_RUN_NUMBER": "4"}, "0.0.0-rel.4", true},
		{map[string]string{"GITHUB_REF_TYPE": "tag", "GITHUB_REF_NAME": "v0.7.0", "GITHUB_EVENT_NAME": "push", "GITHUB_RUN_NUMBER": "4"}, "0.7.0", true},
		{map[string]string{"GITHUB_REF_TYPE": "branch", "GITHUB_EVENT_NAME": "push", "GITHUB_RUN_NUMBER": "4"}, "0.0.0-ci.4", true},
		{map[string]string{"GITHUB_REF_TYPE": "tag", "GITHUB_REF_NAME": "v1.2"}, "", false},
		{map[string]string{"GITHUB_REF_TYPE": "tag", "GITHUB_REF_NAME": "v01.2.3"}, "", false},
		{map[string]string{"GITHUB_REF_TYPE": "tag", "GITHUB_REF_NAME": "release"}, "", false},
		{map[string]string{}, "", false},
	}
	for _, c := range cases {
		v, err := Version(env(c.env))
		if (err == nil) != c.ok || v != c.want {
			t.Errorf("%v: %q %v", c.env, v, err)
		}
		if err == nil {
			if _, _, perr := winres.ParseVersion(v); perr != nil {
				t.Errorf("%q not accepted by winres.ParseVersion: %v", v, perr)
			}
		}
	}
}

// TestCHK_DELIV_01_ZipEntryName — entry is zip basename with .exe (phase 10).
func TestCHK_DELIV_01_ZipEntryName(t *testing.T) {
	cases := []struct {
		in, want string
		ok       bool
	}{
		{"ExchangeChecker-1.2.3.zip", "ExchangeChecker-1.2.3.exe", true},
		{"/tmp/ExchangeChecker-0.0.0-ci.9.zip", "ExchangeChecker-0.0.0-ci.9.exe", true},
		{"ExchangeChecker-0.0.0-rel.4.zip", "ExchangeChecker-0.0.0-rel.4.exe", true},
		{"out.exe", "", false},
		{"noid", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, err := ZipEntryName(c.in)
		if (err == nil) != c.ok || got != c.want {
			t.Errorf("ZipEntryName(%q): %q %v, want %q ok=%v", c.in, got, err, c.want, c.ok)
		}
	}
}

// TestCHK_DELIV_01_Deterministic — gz and zip are byte-identical for equal
// input; gz round-trips through wininstall.GunzipPayload (stdlib gzip); the
// zip has exactly one entry named after the zip (phase 10).
func TestCHK_DELIV_01_Deterministic(t *testing.T) {
	data := bytes.Repeat([]byte("MZ exchange checker payload "), 5000)
	g1, _ := Gzip(data)
	g2, _ := Gzip(data)
	if !bytes.Equal(g1, g2) || len(g1) >= len(data) {
		t.Fatalf("gzip not deterministic/compressing: %d %d", len(g1), len(data))
	}
	out, err := wininstall.GunzipPayload(map[string][]byte{"a.exe": g1})
	if err != nil || !bytes.Equal(out["a.exe"], data) {
		t.Fatalf("round trip: %v", err)
	}
	if _, err := wininstall.GunzipPayload(map[string][]byte{"a.exe": g1[:len(g1)/2]}); err == nil {
		t.Fatal("truncated gzip accepted")
	}
	entry := "ExchangeChecker-1.2.3.exe"
	z1, _ := Zip(data, entry)
	z2, _ := Zip(data, entry)
	if !bytes.Equal(z1, z2) {
		t.Fatal("zip not deterministic")
	}
	zr, err := zip.NewReader(bytes.NewReader(z1), int64(len(z1)))
	if err != nil || len(zr.File) != 1 || zr.File[0].Name != entry {
		t.Fatalf("zip entries: %v", err)
	}
	rc, _ := zr.File[0].Open()
	got, _ := io.ReadAll(rc)
	if !bytes.Equal(got, data) {
		t.Fatal("zip content")
	}
}

// TestCHK_DELIV_01_ZipAnyInputName — mkzip zip accepts any input exe name;
// the zip entry follows -o, not the input basename (phase 10).
func TestCHK_DELIV_01_ZipAnyInputName(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "renamed-setup.exe")
	if err := os.WriteFile(in, []byte("MZ fake setup"), 0o644); err != nil {
		t.Fatal(err)
	}
	zpath := filepath.Join(dir, "ExchangeChecker-9.9.9.zip")
	if err := run([]string{"zip", "-o", zpath, in}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(zpath)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	want := "ExchangeChecker-9.9.9.exe"
	if len(zr.File) != 1 || zr.File[0].Name != want {
		t.Fatalf("entry %v, want [%s]", zr.File, want)
	}
}
