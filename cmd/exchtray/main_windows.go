//go:build windows

package main

import (
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/wintray"
)

// pipeDialTimeout bounds WaitNamedPipe when all instances are busy.
const pipeDialTimeout = 2 * time.Second

func init() {
	// windows and the message loop must stay on one OS thread
	runtime.LockOSThread()
}

func main() {
	os.Exit(run())
}

func run() int {
	session := trayipc.Options{
		Dial:   trayipc.PipeDialer(model.PipeName, pipeDialTimeout),
		Verify: trayipc.ServiceVerifier(model.ServiceName), // G7 before any command
	}
	for _, a := range os.Args[1:] {
		if a == wintray.SettingsArg {
			// elevated settings window (ShellExecuteExW "runas" from the
			// read-only window): own process, own connection
			return wintray.RunSettings(wintray.SettingsOptions{Lang: i18n.Default(), Session: session})
		}
	}
	release, ok, err := wintray.SingleInstance(wintray.MutexName)
	if err != nil {
		fmt.Fprintln(os.Stderr, "exchtray:", err)
		return 1
	}
	if !ok {
		return 0 // another tray of this session is running
	}
	defer release()

	app, err := wintray.NewApp(wintray.Options{
		Lang:    i18n.Default(),
		Session: session,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "exchtray:", err)
		return 1
	}
	return app.Loop()
}
