//go:build windows

package main

import (
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/traylog"
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
	// per-user tray language (HKCU, menu «Язык (Language)»), else Windows
	langPref, lang := wintray.LoadLang(wintray.RegistryLangStore{})
	for _, a := range os.Args[1:] {
		if a == wintray.SettingsArg {
			// elevated settings window (ShellExecuteExW "runas" from the
			// read-only window): own process, own connection
			return wintray.RunSettings(wintray.SettingsOptions{Lang: lang, Session: session, UIModeStore: wintray.RegistryUIModeStore{}})
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
		Lang:         lang,
		LangPref:     langPref,
		LangStore:    wintray.RegistryLangStore{},
		BalloonStore: wintray.RegistryBalloonStore{},
		SoundStore:   wintray.RegistrySoundStore{},
		UIModeStore:  wintray.RegistryUIModeStore{},
		Session:      session,
		// %LOCALAPPDATA%\ExchangeChecker\tray.log, 1 MB + tray.log.1
		// (phase 9b, CHK-TRAY-28); no passwords / logins are written
		Log: (&traylog.Logger{Path: traylog.DefaultPath()}).Func(),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "exchtray:", err)
		return 1
	}
	return app.Loop()
}
