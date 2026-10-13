package main

import (
	"archive/zip"
	"bytes"
	"io"
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

// TestCHK_DELIV_01_Deterministic — gz and zip are byte-identical for equal
// input; gz round-trips through wininstall.GunzipPayload (stdlib gzip); the
// zip has exactly one entry.
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
	z1, _ := Zip(data)
	z2, _ := Zip(data)
	if !bytes.Equal(z1, z2) {
		t.Fatal("zip not deterministic")
	}
	zr, err := zip.NewReader(bytes.NewReader(z1), int64(len(z1)))
	if err != nil || len(zr.File) != 1 || zr.File[0].Name != SetupEntry {
		t.Fatalf("zip entries: %v", err)
	}
	rc, _ := zr.File[0].Open()
	got, _ := io.ReadAll(rc)
	if !bytes.Equal(got, data) {
		t.Fatal("zip content")
	}
}
