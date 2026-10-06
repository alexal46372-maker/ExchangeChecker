package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// fakeRun is an in-memory RunRegistry with injectable errors.
type fakeRun struct {
	mu                  sync.Mutex
	vals                map[string]string
	getErr, setErr, del error
	sets, dels          int
}

func (f *fakeRun) Get(name string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return "", false, f.getErr
	}
	v, ok := f.vals[name]
	return v, ok, nil
}

func (f *fakeRun) Set(name, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sets++
	if f.setErr != nil {
		return f.setErr
	}
	if f.vals == nil {
		f.vals = map[string]string{}
	}
	f.vals[name] = value
	return nil
}

func (f *fakeRun) Delete(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dels++
	if f.del != nil {
		return f.del
	}
	delete(f.vals, name)
	return nil
}

// TestCHK_CFG_04_TrayRunCommand — the Run value is the full path of
// ExchangeCheckerTray.exe in the folder of the service exe, in quotes.
func TestCHK_CFG_04_TrayRunCommand(t *testing.T) {
	for exe, want := range map[string]string{
		`C:\Program Files\ExchangeChecker\ExchangeCheckerSvc.exe`: `"C:\Program Files\ExchangeChecker\ExchangeCheckerTray.exe"`,
		`D:\EC\exchsvc.exe`: `"D:\EC\ExchangeCheckerTray.exe"`,
		`/opt/ec/exchsvc`:   `"/opt/ec/ExchangeCheckerTray.exe"`,
	} {
		got, err := TrayRunCommand(exe)
		if err != nil || got != want {
			t.Errorf("TrayRunCommand(%q) = %q, %v; want %q", exe, got, err, want)
		}
	}
	for _, exe := range []string{"", "exchsvc.exe", `\exchsvc.exe`} {
		if got, err := TrayRunCommand(exe); err == nil {
			t.Errorf("TrayRunCommand(%q) = %q, want an error", exe, got)
		}
	}
	if TrayRunValue != "ExchangeCheckerTray" || RunKeyPath != `Software\Microsoft\Windows\CurrentVersion\Run` {
		t.Errorf("Run key / value name changed: %s %s", RunKeyPath, TrayRunValue)
	}
}

// TestCHK_CFG_04_ApplyTrayAutostart — on: written when missing or
// different, left alone when equal; off: removed, missing is fine;
// registry errors are returned (the caller only warns).
func TestCHK_CFG_04_ApplyTrayAutostart(t *testing.T) {
	const cmd = `"C:\EC\ExchangeCheckerTray.exe"`
	f := &fakeRun{}
	if act, err := ApplyTrayAutostart(f, true, cmd); err != nil || act != AutostartWritten || f.vals[TrayRunValue] != cmd {
		t.Fatalf("on, missing: %v %v %v", act, err, f.vals)
	}
	if act, err := ApplyTrayAutostart(f, true, cmd); err != nil || act != AutostartUnchanged || f.sets != 1 {
		t.Fatalf("on, equal: %v %v sets=%d", act, err, f.sets)
	}
	f.vals[TrayRunValue] = `C:\Old\exchtray.exe`
	if act, err := ApplyTrayAutostart(f, true, cmd); err != nil || act != AutostartWritten || f.vals[TrayRunValue] != cmd {
		t.Fatalf("on, different: %v %v %v", act, err, f.vals)
	}
	f.vals["Other"] = "keep"
	if act, err := ApplyTrayAutostart(f, false, cmd); err != nil || act != AutostartDeleted {
		t.Fatalf("off, present: %v %v", act, err)
	}
	if _, ok := f.vals[TrayRunValue]; ok || f.vals["Other"] != "keep" {
		t.Fatalf("off: %v", f.vals)
	}
	if act, err := ApplyTrayAutostart(f, false, ""); err != nil || act != AutostartUnchanged || f.dels != 1 {
		t.Fatalf("off, missing: %v %v dels=%d", act, err, f.dels)
	}
	// errors
	boom := errors.New("access denied")
	f.setErr = boom
	if _, err := ApplyTrayAutostart(f, true, cmd); !errors.Is(err, boom) || !strings.Contains(err.Error(), TrayRunValue) {
		t.Errorf("set error: %v", err)
	}
	f.setErr, f.vals[TrayRunValue] = nil, cmd
	f.del = boom
	if _, err := ApplyTrayAutostart(f, false, cmd); !errors.Is(err, boom) {
		t.Errorf("delete error: %v", err)
	}
	// a read error: on -> still tries to write; off -> still tries to delete
	f.del, f.getErr = nil, boom
	sets := f.sets
	if act, err := ApplyTrayAutostart(f, true, cmd); err != nil || act != AutostartWritten || f.sets != sets+1 {
		t.Errorf("on, read error: %v %v", act, err)
	}
	if act, err := ApplyTrayAutostart(f, false, cmd); err != nil || act != AutostartDeleted {
		t.Errorf("off, read error: %v %v", act, err)
	}
	if _, err := ApplyTrayAutostart(nil, true, cmd); err == nil {
		t.Error("nil registry: no error")
	}
	if _, err := ApplyTrayAutostart(&fakeRun{}, true, ""); err == nil {
		t.Error("empty command: no error")
	}
}

// TestCHK_CFG_04_AutostartErrorCode — errors of the Run value map to the
// get_status.trayAutostartError codes.
func TestCHK_CFG_04_AutostartErrorCode(t *testing.T) {
	_, pathErr := TrayRunCommand("exchsvc.exe")
	for err, want := range map[error]string{
		nil:                                   "",
		pathErr:                               model.AutostartErrTrayPath,
		fmt.Errorf("x: %w", os.ErrPermission): model.AutostartErrAccessDenied,
		errors.New("other"):                   model.AutostartErrRegistry,
	} {
		if got := AutostartErrorCode(err); got != want {
			t.Errorf("AutostartErrorCode(%v) = %q, want %q", err, got, want)
		}
	}
	f := &fakeRun{setErr: fmt.Errorf("RegSetValueExW: %w", os.ErrPermission)}
	if _, err := ApplyTrayAutostart(f, true, `"C:\x\ExchangeCheckerTray.exe"`); AutostartErrorCode(err) != model.AutostartErrAccessDenied {
		t.Errorf("wrapped set error: %v", err)
	}
}
