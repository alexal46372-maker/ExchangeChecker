package checks_test

import (
	"bytes"
	"go/format"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestCHK_CI_02_GofmtStep — CHK-CI-02: the linux job of ci.yml has a gofmt
// step (named with CHK-CI-02) that runs `gofmt -l .` from the installed
// go1.20.14, prints `gofmt -d .` and fails when the list is not empty.
func TestCHK_CI_02_GofmtStep(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	body := workflowCode(b)
	lin := strings.Index(body, "\n  linux:\n")
	if lin < 0 {
		t.Fatal("ci.yml: linux job not found")
	}
	job := body[lin+1:]
	i := strings.Index(job, "- name: gofmt (CHK-CI-02)")
	if i < 0 {
		t.Fatal("ci.yml: step `gofmt (CHK-CI-02)` is missing in the linux job")
	}
	step := job[i:]
	if j := strings.Index(step[1:], "\n      - "); j >= 0 {
		step = step[:j+1]
	}
	for _, want := range []string{"gofmt -l .", "gofmt -d .", "exit 1"} {
		if !strings.Contains(step, want) {
			t.Errorf("ci.yml: gofmt step lacks %q:\n%s", want, step)
		}
	}
	// it must run after the Go version check and before go vet, and must not
	// be conditional or tolerate failure
	if !regexp.MustCompile(`(?s)go1\.20\.14 linux/amd64.*gofmt \(CHK-CI-02\).*go vet \(linux`).MatchString(job) {
		t.Error("ci.yml: gofmt step must sit between the go version check and go vet")
	}
	if regexp.MustCompile(`(?m)^\s*(if|continue-on-error):`).MatchString(step) {
		t.Errorf("ci.yml: gofmt step must be unconditional and must not continue on error:\n%s", step)
	}
}

// TestCHK_CI_02_RepoFormatted — the same rule as the CI step, enforced in
// go test: every .go file of the repository is already gofmt-clean.
func TestCHK_CI_02_RepoFormatted(t *testing.T) {
	root := filepath.Join("..")
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		// a Windows checkout may convert line endings (core.autocrlf)
		src = bytes.ReplaceAll(src, []byte("\r\n"), []byte("\n"))
		out, err := format.Source(src)
		if err != nil {
			t.Errorf("%s: %v", p, err)
			return nil
		}
		if !bytes.Equal(src, out) {
			t.Errorf("%s is not gofmt-formatted", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
