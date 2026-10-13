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

	if admin, err := winapi.ProcessIsAdmin(); err != nil {
		logf("WARNING: admin check: %v", err)
	} else if !admin {
		logf("ERROR: administrator rights required")
		return wininstall.ExitError
	}

	mu, already, err := winapi.CreateMutex(wininstall.SetupMutexName)
	if err != nil {
		logf("ERROR: mutex: %v", err)
		return wininstall.ExitError
	}
	defer syscall.CloseHandle(mu)
	if already {
		logf("ERROR: another setup is running")
		return wininstall.ExitError
	}

	sys := wininstall.NewSystem()
	lang := opts.Lang
	if lang == "" {
		lang = winapi.UserUILanguage()
	}

	if opts.Uninstall {
		self, _ := os.Executable()
		res := wininstall.Uninstall(sys, wininstall.UninstallConfig{
			DataDir: config.DefaultDir(), SelfExe: self, PurgeData: opts.PurgeData,
		}, printfLogger(logf))
		if res.Err != nil {
			logf("ERROR: %v", res.Err)
		}
		logf("exit %d", res.Code)
		return res.Code
	}

	if !opts.Silent {
		logf("ERROR: interactive wizard not yet available; use /S for silent install")
		return wininstall.ExitError
	}

	files := payloadFiles()
	res := wininstall.Install(sys, wininstall.InstallConfig{
		Opts:       opts,
		DefaultDir: filepath.Join(os.Getenv("ProgramFiles"), "ExchangeChecker"),
		DataDir:    config.DefaultDir(),
		Files:      files,
		Version:    version.Version,
		Lang:       lang,
		Now:        time.Now(),
	}, printfLogger(logf))
	if res.Err != nil {
		logf("ERROR: %v", res.Err)
	}
	logf("exit %d", res.Code)
	return res.Code
}

type printfLogger func(string, ...interface{})

func (f printfLogger) Printf(format string, args ...interface{}) { f(format, args...) }

func openLog(path string) (func(string, ...interface{}), func(), error) {
	if path == "" {
		path = filepath.Join(os.TempDir(), fmt.Sprintf("ExchangeCheckerSetup-%s.log", time.Now().Format("20060102-150405")))
	}
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
