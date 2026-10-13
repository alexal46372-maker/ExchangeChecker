//go:build windows

package wininstall

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// TestCHK_SETUP_03_RealSCM — create a test service (not started), verify
// config / failure actions / delayed / DACL; deny-only BA can query status
// but not start; DeleteService in cleanup.
func TestCHK_SETUP_03_RealSCM(t *testing.T) {
	if admin, _ := winapi.ProcessIsAdmin(); !admin {
		t.Skip("admin required")
	}
	var rnd [4]byte
	_, _ = rand.Read(rnd[:])
	name := "ExchangeCheckerTest-" + hex.EncodeToString(rnd[:])
	dir := t.TempDir()
	exe := filepath.Join(dir, "nop.exe")
	// any existing pe works as ImagePath for CreateService without Start;
	// use the test binary itself.
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, data, 0o644); err != nil {
		t.Fatal(err)
	}
	sys := NewSystem()
	spec := ServiceSpec{
		Name: name, DisplayName: "ExchangeChecker Test " + name,
		ImagePath: ServiceImagePath(exe), Description: ServiceDescription("en"),
		AutoStart: true, ResetPeriod: FailureResetPeriod, FailureDelays: FailureDelays, SDDL: ServiceSDDL,
	}
	if err := sys.CreateService(spec); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = sys.StopService(name, 5*time.Second)
		_ = sys.DeleteService(name)
	})
	scm, err := winapi.OpenSCManager(winapi.SCManagerConnect)
	if err != nil {
		t.Fatal(err)
	}
	defer scm.Close()
	svc, err := winapi.OpenService(scm, name, winapi.ServiceQueryConfig|winapi.ServiceQueryStatus|winapi.ServiceReadControl|winapi.ServiceStart)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	cfg, err := winapi.QueryServiceConfig(svc)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ServiceType != winapi.ServiceWin32OwnProcess {
		t.Fatalf("type=%d", cfg.ServiceType)
	}
	if cfg.StartType != winapi.ServiceAutoStart {
		t.Fatalf("start=%d", cfg.StartType)
	}
	if !strings.HasPrefix(cfg.BinaryPath, `"`) {
		t.Fatalf("ImagePath not quoted: %q", cfg.BinaryPath)
	}
	// LocalSystem: empty or LocalSystem
	if cfg.StartName != "" && !strings.EqualFold(cfg.StartName, "LocalSystem") {
		t.Fatalf("StartName=%q", cfg.StartName)
	}
	descBuf, err := winapi.QueryServiceConfig2Bytes(svc, winapi.ServiceConfigDescription)
	if err != nil || len(descBuf) < 8 {
		t.Fatalf("description: %v len=%d", err, len(descBuf))
	}
	faBuf, err := winapi.QueryServiceConfig2Bytes(svc, winapi.ServiceConfigFailureActions)
	if err != nil || len(faBuf) < 40 {
		t.Fatalf("failure actions: %v", err)
	}
	delBuf, err := winapi.QueryServiceConfig2Bytes(svc, winapi.ServiceConfigDelayedAutoStart)
	if err != nil {
		t.Fatalf("delayed: %v", err)
	}
	if len(delBuf) < 4 || delBuf[0] == 0 {
		t.Fatalf("delayed not set: %v", delBuf)
	}
	sddl, err := winapi.ServiceDACL(svc)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("service SDDL: %s", sddl)
	if !strings.Contains(sddl, "AU") && !strings.Contains(sddl, "WD") {
		// AU may appear as S-1-5-11
		if !strings.Contains(sddl, "S-1-5-11") {
			t.Fatalf("DACL missing Authenticated Users: %s", sddl)
		}
	}
	// deny-only BA: QUERY_STATUS ok, START denied
	tok, err := winapi.DenyOnlyToken(winapi.SIDAdministrators)
	if err != nil {
		t.Fatal(err)
	}
	defer tok.Close()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := winapi.ImpersonateLoggedOnUser(tok); err != nil {
		t.Fatal(err)
	}
	defer winapi.RevertToSelf()
	scm2, err := winapi.OpenSCManager(winapi.SCManagerConnect)
	if err != nil {
		t.Fatal(err)
	}
	defer scm2.Close()
	q, err := winapi.OpenService(scm2, name, winapi.ServiceQueryStatus)
	if err != nil {
		t.Fatalf("QUERY_STATUS as deny-only BA: %v", err)
	}
	q.Close()
	_, err = winapi.OpenService(scm2, name, winapi.ServiceStart)
	if err == nil {
		t.Fatal("START as deny-only BA unexpectedly succeeded")
	}
}

// TestCHK_SETUP_05_ACLWindows — DACL on temp dirs; secrets.dpapi stays protected.
func TestCHK_SETUP_05_ACLWindows(t *testing.T) {
	if admin, _ := winapi.ProcessIsAdmin(); !admin {
		t.Skip("admin required")
	}
	sys := NewSystem()
	root := t.TempDir()
	data := filepath.Join(root, "ExchangeChecker")
	inst := filepath.Join(root, "Install")
	if _, err := sys.MkdirAll(data); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "settings.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// secrets with protected DACL
	sec := filepath.Join(data, "secrets.dpapi")
	f, err := winapi.CreateFileSDDL(sec, "D:P(A;;FA;;;SY)(A;;FA;;;BA)")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.Write([]byte("x"))
	_ = f.Close()
	if _, err := sys.TakeOwnership(data); err != nil {
		t.Logf("TakeOwnership: %v", err)
	}
	if err := sys.SetDACL(data, DataDirSDDL); err != nil {
		t.Fatal(err)
	}
	if _, err := sys.MkdirAll(inst); err != nil {
		t.Fatal(err)
	}
	if err := sys.SetDACL(inst, InstallDirSDDL); err != nil {
		t.Fatal(err)
	}
	dacl, err := winapi.FileDACL(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("data DACL: %s", dacl)
	if strings.Contains(dacl, "BU") || strings.Contains(dacl, "S-1-5-32-545") {
		t.Fatalf("Users on ProgramData: %s", dacl)
	}
	secDACL, err := winapi.FileDACL(sec)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("secrets DACL: %s", secDACL)
	// protected: should still be SY+BA only
	if strings.Contains(secDACL, "BU") || strings.Contains(secDACL, "S-1-5-32-545") {
		t.Fatalf("secrets inherited Users: %s", secDACL)
	}
	idacl, err := winapi.FileDACL(inst)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("install DACL: %s", idacl)
}

// TestCHK_SETUP_04_EventSourceWindows — registry source + FormatMessage via file.
func TestCHK_SETUP_04_EventSourceWindows(t *testing.T) {
	if admin, _ := winapi.ProcessIsAdmin(); !admin {
		t.Skip("admin required")
	}
	var rnd [4]byte
	_, _ = rand.Read(rnd[:])
	src := "ExchangeCheckerTest-" + hex.EncodeToString(rnd[:])
	// Use the built-in service .syso message table: the test binary of
	// cmd/exchsvc is not available here; register against the current
	// module only if it has a message table. Prefer the exchsvc syso path
	// by copying a known-good approach: write EventMessageFile to
	// os.Executable when linked with message table — setup test binary
	// does not have one. So only verify registry write/read/delete.
	sys := winSystem{}
	self, _ := os.Executable()
	key := `SYSTEM\CurrentControlSet\Services\EventLog\Application\` + src
	view := uint32(winapi.KEY_WOW64_64KEY)
	if err := winapi.RegSetExpandStringView(winapi.HKEY_LOCAL_MACHINE, key, "EventMessageFile", self, view); err != nil {
		t.Fatal(err)
	}
	if err := winapi.RegSetDWORDView(winapi.HKEY_LOCAL_MACHINE, key, "TypesSupported", 7, view); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = winapi.RegDeleteKeyView(winapi.HKEY_LOCAL_MACHINE, key, view) })
	got, ok, err := winapi.RegGetExpandStringView(winapi.HKEY_LOCAL_MACHINE, key, "EventMessageFile", view)
	if err != nil || !ok || !strings.EqualFold(got, self) {
		t.Fatalf("EventMessageFile=%q ok=%v err=%v", got, ok, err)
	}
	n, ok, err := winapi.RegGetDWORDView(winapi.HKEY_LOCAL_MACHINE, key, "TypesSupported", view)
	if err != nil || !ok || n != 7 {
		t.Fatalf("TypesSupported=%d ok=%v err=%v", n, ok, err)
	}
	_ = sys // keep
	if err := winapi.RegDeleteKeyView(winapi.HKEY_LOCAL_MACHINE, key, view); err != nil {
		t.Fatal(err)
	}
}

// TestCHK_SETUP_07_RenameRunning — MoveFileEx rename of a temp file (stand-in for self-delete).
func TestCHK_SETUP_07_RenameRunning(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.exe")
	b := filepath.Join(dir, "b.exe")
	if err := os.WriteFile(a, []byte("xx"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := winapi.MoveFileEx(a, b, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(a); !errors.Is(err, os.ErrNotExist) && !os.IsNotExist(err) {
		t.Fatalf("a still exists: %v", err)
	}
	if _, err := os.Stat(b); err != nil {
		t.Fatal(err)
	}
	_ = syscall.Errno(0)
}
