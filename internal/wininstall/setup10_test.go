package wininstall

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

// TestCHK_SETUP_10_CatalogComplete — RU/EN have the same setup.* keys,
// no empty strings and the same format verbs.
func TestCHK_SETUP_10_CatalogComplete(t *testing.T) {
	ru, en := CatalogKeys("ru"), CatalogKeys("en")
	if len(ru) < 25 {
		t.Fatalf("catalog too small: %d", len(ru))
	}
	for _, pair := range [][2]map[string]string{{ru, en}, {en, ru}} {
		for k := range pair[0] {
			if _, ok := pair[1][k]; !ok {
				t.Errorf("key %s missing in one language", k)
			}
		}
	}
	verbs := func(s string) string {
		s = strings.ReplaceAll(s, "%%", "")
		var v []string
		for i := 0; i < len(s)-1; i++ {
			if s[i] == '%' {
				v = append(v, s[i:i+2])
			}
		}
		return strings.Join(v, ",")
	}
	for k, r := range ru {
		if !strings.HasPrefix(k, "setup.") {
			t.Errorf("key %s: want setup.* prefix", k)
		}
		if strings.TrimSpace(r) == "" || strings.TrimSpace(en[k]) == "" {
			t.Errorf("key %s: empty", k)
		}
		if verbs(r) != verbs(en[k]) {
			t.Errorf("key %s: format verbs differ ru=%q en=%q", k, verbs(r), verbs(en[k]))
		}
	}
	if T("de", "setup.btn.next") != ru["setup.btn.next"] {
		t.Error("fallback language must be ru")
	}
}

// TestCHK_SETUP_10_CatalogUsed — every T(…, "setup.x") key in the package
// and in cmd/exchsetup exists.
func TestCHK_SETUP_10_CatalogUsed(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..")
	var used []string
	for _, d := range []string{"internal/wininstall", "cmd/exchsetup"} {
		ents, _ := os.ReadDir(filepath.Join(root, d))
		for _, e := range ents {
			if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") || e.Name() == "strings.go" {
				continue
			}
			b, _ := os.ReadFile(filepath.Join(root, d, e.Name()))
			s := string(b)
			for {
				i := strings.Index(s, `"setup.`)
				if i < 0 {
					break
				}
				j := strings.Index(s[i+1:], `"`)
				used = append(used, s[i+1:i+1+j])
				s = s[i+1+j:]
			}
		}
	}
	sort.Strings(used)
	if len(used) < 15 {
		t.Fatalf("found only %d used keys", len(used))
	}
	for _, k := range used {
		if _, ok := CatalogKeys("ru")[k]; !ok {
			t.Errorf("key %s used but not in the catalog", k)
		}
	}
}

// TestCHK_SETUP_10_WizardModel — page order, Back/Cancel, folder
// validation, defaults (§D.1, §D.8), upgrade keeps folder and start state.
func TestCHK_SETUP_10_WizardModel(t *testing.T) {
	s := NewWizardState("en", "1.1.0", ModeFresh, "", `C:\Program Files\ExchangeChecker`, false, `C:\ProgramData\ExchangeChecker`, false)
	if !s.Shortcut || !s.Service || !s.LaunchTray {
		t.Fatalf("defaults: %+v", s)
	}
	if s.Page != PageWelcome || s.CanBack() || !s.CanCancel() {
		t.Fatal("welcome state")
	}
	_ = s.Next()
	if s.Page != PageFolder || !s.CanBack() {
		t.Fatal("folder page")
	}
	for _, bad := range []string{``, `relative\dir`, `\\server\share\x`, `C:\`, `C:\ProgramData\ExchangeChecker\x`} {
		s.Dir = bad
		if err := s.Next(); !errors.Is(err, ErrBadDir) || s.Page != PageFolder {
			t.Fatalf("dir %q: err=%v page=%d", bad, err, s.Page)
		}
	}
	s.Dir = `D:\Apps\EC`
	s.CheckDrive = func(string) error { return errors.New("not fixed") }
	if err := s.Next(); err == nil || s.Page != PageFolder {
		t.Fatal("drive check ignored")
	}
	s.CheckDrive = nil
	if err := s.Next(); err != nil || s.Page != PageOptions {
		t.Fatalf("valid dir: %v", err)
	}
	s.Back()
	if s.Page != PageFolder {
		t.Fatal("back")
	}
	_ = s.Next()
	s.Shortcut, s.Service = false, false
	_ = s.Next()
	if s.Page != PageProgress || s.CanCancel() || s.CanBack() {
		t.Fatal("progress: no back/cancel")
	}
	if err := s.Next(); err == nil {
		t.Fatal("finish before result")
	}
	o := s.Options(Options{Silent: true})
	if o.Silent || o.Dir != `D:\Apps\EC` || !o.NoShortcut || !o.NoStart || o.Lang != "en" {
		t.Fatalf("options: %+v", o)
	}
	s.Result = &Result{Code: ExitOK}
	_ = s.Next()
	if s.Page != PageFinish || !s.ShowTrayCheckbox() || !strings.Contains(s.FinishText("x.log"), "1.1.0") {
		t.Fatal("finish ok")
	}
	s.Result = &Result{Code: ExitError, Err: errors.New("boom")}
	if s.ShowTrayCheckbox() || !strings.Contains(s.FinishText(`C:\t\setup.log`), `C:\t\setup.log`) {
		t.Fatal("finish error must show the log path, no tray checkbox")
	}

	u := NewWizardState("ru", "1.1.0", ModeUpgrade, "1.0.0", `C:\EC`, true, `C:\ProgramData\ExchangeChecker`, false)
	if u.Service {
		t.Fatal("upgrade: service checkbox = was running (false)")
	}
	if !strings.Contains(u.WelcomeText(), "1.0.0") || !strings.Contains(u.WelcomeText(), "1.1.0") {
		t.Fatal("upgrade welcome text")
	}
	_ = u.Next()
	u.Dir = "garbage" // locked: not validated, not passed
	if err := u.Next(); err != nil {
		t.Fatal(err)
	}
	if u.Options(Options{}).Dir != "" {
		t.Fatal("locked dir must not become /DIR")
	}
}

// TestCHK_SETUP_10_SilentNoWindows — in cmd/exchsetup the wizard, the
// uninstall confirmation, message boxes and the tray launch are reachable
// only when !opts.Silent.
func TestCHK_SETUP_10_SilentNoWindows(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	b, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "cmd", "exchsetup", "main_windows.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	silent := strings.Index(src, "if opts.Silent {\n\t\t// silent: no window")
	wiz := strings.Index(src, "return runWizard(")
	if silent < 0 || wiz < 0 || wiz < silent {
		t.Fatal("wizard must be reached only after the silent return")
	}
	if !strings.Contains(src, "if !opts.Silent {\n\t\t\tok, p, err := wininstall.ConfirmUninstall(") {
		t.Fatal("uninstall confirmation must be guarded by !opts.Silent")
	}
	if strings.Count(src, "LaunchAsShellUser(") != 1 || strings.Index(src, "LaunchAsShellUser(") < strings.Index(src, "func runWizard(") {
		t.Fatal("tray launch only inside runWizard (never silent)")
	}
	for i := 0; ; {
		j := strings.Index(src[i:], "winapi.MessageBox(")
		if j < 0 {
			break
		}
		i += j + 1
		before := src[:i]
		if !(strings.LastIndex(before, "if !opts.Silent") > strings.LastIndex(before, "\nfunc ") ||
			strings.LastIndex(before, "func runWizard(") > strings.LastIndex(before[:len(before)-1], "\nfunc run(")) {
			t.Fatalf("MessageBox at offset %d not guarded by !opts.Silent", i)
		}
	}
}

// fresh install creates the shortcut (and rolls it back), /NOSHORTCUT
// does not, uninstall deletes it.
func TestCHK_SETUP_08_PlanShortcut(t *testing.T) {
	f := newFake()
	res := Install(f, InstallConfig{
		Opts: Options{Silent: true}, DefaultDir: `C:\EC`, DataDir: `C:\ProgramData\ExchangeChecker`,
		Files: sampleFiles(), Version: "1.1.0", Now: time.Now(),
	}, &memLog{})
	if res.Code != ExitOK || f.Shortcut != `C:\EC\ExchangeCheckerTray.exe` {
		t.Fatalf("code=%d shortcut=%q", res.Code, f.Shortcut)
	}
	if indexOf(f.Steps, "CreateStartShortcut|") > indexOf(f.Steps, "WriteUninstallKey|") {
		t.Fatal("shortcut must precede the Uninstall key")
	}
	Uninstall(f, UninstallConfig{DataDir: `C:\ProgramData\ExchangeChecker`}, &memLog{})
	if f.Shortcut != "" {
		t.Fatal("uninstall must delete the shortcut")
	}

	g := newFake()
	Install(g, InstallConfig{
		Opts: Options{Silent: true, NoShortcut: true}, DefaultDir: `C:\EC`, DataDir: `C:\ProgramData\ExchangeChecker`,
		Files: sampleFiles(), Version: "1.1.0", Now: time.Now(),
	}, &memLog{})
	if indexOf(g.Steps, "CreateStartShortcut|") >= 0 {
		t.Fatal("/NOSHORTCUT created a shortcut")
	}

	h := newFake()
	h.FailAt = "WriteUninstallKey|"
	Install(h, InstallConfig{
		Opts: Options{Silent: true}, DefaultDir: `C:\EC`, DataDir: `C:\ProgramData\ExchangeChecker`,
		Files: sampleFiles(), Version: "1.1.0", Now: time.Now(),
	}, &memLog{})
	if h.Shortcut != "" {
		t.Fatal("fresh rollback must delete the shortcut")
	}
}
