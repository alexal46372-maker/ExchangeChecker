//go:build windows

package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/version"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
	"github.com/alexal46372-maker/ExchangeChecker/internal/wininstall"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	opts, err := wininstall.ParseArgs(args)
	if err != nil {
		logEarly(opts.Log, "usage: %v", err)
		return wininstall.ExitError
	}
	logf, logCloser, err := openLog(opts.Log)
	if err != nil {
		fmt.Fprintf(os.Stderr, "log: %v\n", err)
		return wininstall.ExitError
	}
	defer logCloser()
	logf("Exchange Checker setup %s starting", version.Version)
	lang := wininstall.NormLang(opts.Lang)
	if opts.Lang == "" {
		lang = wininstall.NormLang(winapi.UserUILanguage())
	}
	// interactive only: a message box for fatal start-up errors (never in /S)
	fatal := func(key string) int {
		logf("ERROR: %s", wininstall.T("en", key))
		if !opts.Silent {
			winapi.MessageBox(0, wininstall.T(lang, key), "Exchange Checker", 0x10 /*MB_ICONERROR*/)
		}
		return wininstall.ExitError
	}

	if admin, err := winapi.ProcessIsAdmin(); err != nil {
		logf("WARNING: admin check: %v", err)
	} else if !admin {
		return fatal("setup.err.admin")
	}

	mu, already, err := winapi.CreateMutex(wininstall.SetupMutexName)
	if err != nil {
		logf("ERROR: mutex: %v", err)
		return wininstall.ExitError
	}
	defer syscall.CloseHandle(mu)
	if already {
		return fatal("setup.err.running")
	}

	sys := wininstall.NewSystem()

	if opts.Uninstall {
		purge := opts.PurgeData
		if !opts.Silent {
			ok, p, err := wininstall.ConfirmUninstall(lang)
			if err != nil {
				logf("ERROR: confirmation window: %v", err)
				return wininstall.ExitError
			}
			if !ok {
				logf("cancelled by the user; exit %d", wininstall.ExitCancelled)
				return wininstall.ExitCancelled
			}
			purge = purge || p
		}
		self, _ := os.Executable()
		res := wininstall.Uninstall(sys, wininstall.UninstallConfig{
			DataDir: config.DefaultDir(), SelfExe: self, PurgeData: purge,
		}, printfLogger(logf))
		if res.Err != nil {
			logf("ERROR: %v", res.Err)
		}
		logf("exit %d", res.Code)
		if !opts.Silent {
			msg := wininstall.T(lang, "setup.uninstall.done")
			switch {
			case res.Err != nil:
				msg = wininstall.T(lang, "setup.uninstall.error", res.Err.Error(), logPath)
			case res.Code == wininstall.ExitRebootRequired:
				msg = wininstall.T(lang, "setup.uninstall.reboot")
			}
			winapi.MessageBox(0, msg, wininstall.T(lang, "setup.uninstall.title"), 0x40 /*MB_ICONINFORMATION*/)
		}
		return res.Code
	}

	defaultDir := filepath.Join(os.Getenv("ProgramFiles"), "ExchangeChecker")
	if pf, err := winapi.SHGetFolderPath(winapi.CSIDL_PROGRAM_FILES); err == nil && pf != "" {
		defaultDir = filepath.Join(pf, "ExchangeChecker")
	}
	install := func(o wininstall.Options, lg wininstall.Logger) wininstall.Result {
		res := wininstall.Install(sys, wininstall.InstallConfig{
			Opts:       o,
			DefaultDir: defaultDir,
			DataDir:    config.DefaultDir(),
			Files:      payloadFiles(),
			Version:    version.Version,
			Lang:       lang,
			Now:        time.Now(),
		}, lg)
		if res.Err != nil {
			logf("ERROR: %v", res.Err)
		}
		logf("exit %d", res.Code)
		return res
	}
	if opts.Silent {
		// silent: no window, no message box, no tray launch (§1.3, §D.8)
		return install(opts, printfLogger(logf)).Code
	}
	return runWizard(sys, opts, lang, defaultDir, install, logf)
}

func runWizard(sys wininstall.System, opts wininstall.Options, lang, defaultDir string,
	install func(wininstall.Options, wininstall.Logger) wininstall.Result, logf func(string, ...interface{})) int {
	inst, svc, mode, err := wininstall.Detect(sys, version.Version)
	if err != nil {
		logf("WARNING: detect: %v", err)
	}
	dir, locked := defaultDir, false
	if opts.Dir != "" {
		dir = opts.Dir
	}
	if inst.Present && inst.Dir != "" {
		dir, locked = inst.Dir, true
	}
	st := wininstall.NewWizardState(lang, version.Version, mode, inst.Version, dir, locked, config.DefaultDir(), svc.Running)
	st.CheckDrive = sys.CheckFixedDrive
	st.AllowDowngrade = opts.AllowDowngrade
	out, err := wininstall.RunWizard(wininstall.WizardConfig{
		State: st, Base: opts, LogPath: logPath,
		Run: func(o wininstall.Options, progress func(string)) wininstall.Result {
			return install(o, printfLogger(func(format string, a ...interface{}) {
				logf(format, a...)
				progress(fmt.Sprintf(format, a...))
			}))
		},
	})
	if err != nil {
		logf("ERROR: wizard: %v", err)
		return wininstall.ExitError
	}
	if out.Cancelled {
		logf("cancelled by the user; exit %d", wininstall.ExitCancelled)
		return wininstall.ExitCancelled
	}
	if out.LaunchTray {
		trayDir := st.Dir
		if out.Result.Dir != "" {
			trayDir = out.Result.Dir
		}
		tray := filepath.Join(trayDir, config.TrayExeName)
		if err := winapi.LaunchAsShellUser(tray, trayDir); err != nil {
			logf("WARNING: tray launch: %v", err)
			winapi.MessageBox(0, wininstall.T(lang, "setup.finish.trayfail"), "Exchange Checker", 0x40)
		} else {
			logf("tray started (explorer.exe token): %s", tray)
		}
	}
	return out.Result.Code
}

type printfLogger func(string, ...interface{})

func (f printfLogger) Printf(format string, args ...interface{}) { f(format, args...) }

// logPath is the setup log in use (shown on the Finish page).
var logPath string

func openLog(path string) (func(string, ...interface{}), func(), error) {
	if path == "" {
		path = filepath.Join(os.TempDir(), fmt.Sprintf("ExchangeCheckerSetup-%s.log", time.Now().Format("20060102-150405")))
	}
	logPath = path
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, nil, err
	}
	logf := func(format string, args ...interface{}) {
		line := fmt.Sprintf("%s %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(format, args...))
		_, _ = io.WriteString(f, line)
	}
	return logf, func() { _ = f.Close() }, nil
}

func logEarly(path, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintln(os.Stderr, msg)
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintln(f, msg)
	_ = f.Close()
}
