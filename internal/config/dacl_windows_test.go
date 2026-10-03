//go:build windows

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// TestCHK_CFG_03_SecretsDACL — secrets.dpapi gets the protected DACL
// SY+BA (no inherited ACEs, no Users); an existing file with an inherited
// DACL is fixed by the next write; settings.json / profiles.json keep the
// inherited (Users-read) DACL. Вика's decision of 03.10.
func TestCHK_CFG_03_SecretsDACL(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "ExchangeChecker"))
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// pre-phase-6 file: plain create = inherited DACL
	if err := os.WriteFile(s.Path(SecretsFile), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := winapi.FileDACL(s.Path(SecretsFile))
	if err != nil {
		t.Fatal(err)
	}
	if secretsDACL(before) {
		t.Fatalf("precondition: plain file already has %s", before)
	}
	if err := s.SaveSecrets(Secrets{"p1": "S3cret!"}); err != nil {
		t.Fatalf("SaveSecrets: %v", err)
	}
	got, err := winapi.FileDACL(s.Path(SecretsFile))
	if err != nil {
		t.Fatal(err)
	}
	if !secretsDACL(got) {
		t.Fatalf("secrets.dpapi DACL = %s, want %s (was %s)", got, SecretsSDDL, before)
	}
	if sec, err := s.LoadSecrets(); err != nil || sec["p1"] != "S3cret!" {
		t.Fatalf("LoadSecrets after DACL: %v %v", sec, err)
	}
	// a second write keeps it
	if err := s.SetPassword("p2", "x"); err != nil {
		t.Fatal(err)
	}
	if got, _ := winapi.FileDACL(s.Path(SecretsFile)); !secretsDACL(got) {
		t.Fatalf("after second write: %s", got)
	}
	if err := s.SaveSettings(model.DefaultSettings()); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveProfiles(nil); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{SettingsFile, ProfilesFile} {
		d, err := winapi.FileDACL(s.Path(name))
		if err != nil {
			t.Fatal(err)
		}
		if flags, _ := splitDACL(d); strings.Contains(flags, "P") {
			t.Errorf("%s got a protected DACL %s (must stay inherited / Users-read)", name, d)
		}
		t.Logf("%s DACL: %s", name, d)
	}
	// no temp files left behind
	ents, _ := os.ReadDir(s.Dir)
	for _, e := range ents {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("temp file left: %s", e.Name())
		}
	}
}

// splitDACL splits "D:PAI(ace)(ace)" into the flags "PAI" and the ACEs.
func splitDACL(s string) (flags, aces string) {
	s = strings.TrimPrefix(s, "D:")
	i := strings.IndexByte(s, '(')
	if i < 0 {
		return s, ""
	}
	return s[:i], s[i:]
}

// secretsDACL: protected (P; AI may be added by NTFS) and exactly the ACEs
// of SecretsSDDL (explicit, no ID flag).
func secretsDACL(s string) bool {
	flags, aces := splitDACL(s)
	_, want := splitDACL(SecretsSDDL)
	return strings.Contains(flags, "P") && aces == want
}

// TestCHK_CFG_03_SecretsDACLAtStart — phase 6b: an existing secrets.dpapi
// with an inherited DACL that grants Users access (written before phase 6
// under %ProgramData%) gets exactly SY+BA, protected, from the start-up
// call (Store.SecureSecretsFile -> SetNamedSecurityInfoW +
// PROTECTED_DACL_SECURITY_INFORMATION); the content is untouched; a
// missing file is left missing.
func TestCHK_CFG_03_SecretsDACLAtStart(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "ExchangeChecker"))
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if present, err := s.SecureSecretsFile(); present || err != nil {
		t.Fatalf("missing file: present=%v err=%v", present, err)
	}
	if _, err := os.Stat(s.Path(SecretsFile)); !os.IsNotExist(err) {
		t.Fatalf("missing file was created: %v", err)
	}
	// directory like %ProgramData%\ExchangeChecker before phase 6: ACEs
	// inherited by files, including read access for Users (BU)
	dirSDDL := "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;FA;;;OW)(A;OICI;FR;;;BU)"
	if err := winapi.SetFileDACL(s.Dir, dirSDDL); err != nil {
		t.Fatalf("set directory DACL: %v", err)
	}
	if err := os.WriteFile(s.Path(SecretsFile), []byte("blob"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := winapi.FileDACL(s.Path(SecretsFile))
	if err != nil {
		t.Fatal(err)
	}
	if flags, aces := splitDACL(before); strings.Contains(flags, "P") || !strings.Contains(aces, ";ID;") || !strings.Contains(aces, ";;;BU)") {
		t.Fatalf("precondition: want an inherited DACL with Users access, got %s", before)
	}
	present, err := s.SecureSecretsFile()
	if !present || err != nil {
		t.Fatalf("SecureSecretsFile: present=%v err=%v", present, err)
	}
	got, err := winapi.FileDACL(s.Path(SecretsFile))
	if err != nil {
		t.Fatal(err)
	}
	if !secretsDACL(got) {
		t.Fatalf("secrets.dpapi DACL = %s, want exactly %s protected (was %s)", got, SecretsSDDL, before)
	}
	if b, err := os.ReadFile(s.Path(SecretsFile)); err != nil || string(b) != "blob" {
		t.Fatalf("content changed: %q %v", b, err)
	}
	// idempotent
	if _, err := s.SecureSecretsFile(); err != nil {
		t.Fatal(err)
	}
	if got, _ := winapi.FileDACL(s.Path(SecretsFile)); !secretsDACL(got) {
		t.Fatalf("second call: %s", got)
	}
	t.Logf("before %s -> after %s", before, got)
}
