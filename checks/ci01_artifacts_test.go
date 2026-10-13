package checks_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// workflowCode returns the workflow text without comment lines / trailing
// comments (good enough for our workflows: no '#' inside quoted values).
func workflowCode(b []byte) string {
	var out []string
	for _, l := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
		if i := strings.Index(l, "#"); i >= 0 {
			l = l[:i]
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

// TestCHK_CI_01_ArtifactsOnlyRelease — CHK-CI-01 forbids uploading build
// artifacts; the only exception (POLICY.md) is .github/workflows/release.yml:
// exactly one actions/upload-artifact step (the delivery zip),
// retention-days: 7, permissions contents: read, triggers only
// workflow_dispatch and push tags v*. No other workflow (ci.yml included)
// uploads anything.
func TestCHK_CI_01_ArtifactsOnlyRelease(t *testing.T) {
	dir := filepath.Join("..", ".github", "workflows")
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	uploadStep := regexp.MustCompile(`(?m)^\s*(-\s*)?uses:\s*["']?actions/upload-artifact@`)
	anyUpload := regexp.MustCompile(`(?i)upload-artifact|upload-release-asset|gh\s+release|action-gh-release`)
	seen := map[string]bool{}
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() || !(strings.HasSuffix(n, ".yml") || strings.HasSuffix(n, ".yaml")) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			t.Fatal(err)
		}
		body := workflowCode(b)
		seen[n] = true
		if n != "release.yml" {
			if anyUpload.MatchString(body) {
				t.Errorf("%s uploads artifacts; only release.yml may (CHK-CI-01 exception)", n)
			}
			continue
		}
		if c, all := len(uploadStep.FindAllString(body, -1)), len(anyUpload.FindAllString(body, -1)); c != 1 || all != 1 {
			t.Errorf("release.yml: want exactly one actions/upload-artifact step, got %d (%d upload mentions)", c, all)
		}
		if !regexp.MustCompile(`(?m)^\s*retention-days:\s*7\s*$`).MatchString(body) {
			t.Error("release.yml: retention-days: 7 missing")
		}
		perms := regexp.MustCompile(`(?m)^permissions:\n((?:[ \t]+.*\n)+)`).FindStringSubmatch(body)
		if perms == nil || strings.TrimSpace(perms[1]) != "contents: read" {
			t.Errorf("release.yml: permissions must be exactly contents: read, got %q", perms)
		}
		on := regexp.MustCompile(`(?m)^on:\n((?:[ \t]+.*\n|\n)+)`).FindStringSubmatch(body)
		if on == nil || strings.Contains(on[1], "pull_request") || strings.Contains(on[1], "branches") ||
			strings.Contains(on[1], "schedule") || !strings.Contains(on[1], "workflow_dispatch") ||
			!strings.Contains(on[1], `tags: ["v*"]`) {
			t.Errorf("release.yml: triggers must be only workflow_dispatch and push tags v*, got %q", on)
		}
	}
	if !seen["ci.yml"] || !seen["release.yml"] {
		t.Fatalf("workflows not found: %v", seen)
	}
}
