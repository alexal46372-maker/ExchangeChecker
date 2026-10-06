// Command exchsvc is the Exchange Checker Windows service
// (ExchangeCheckerSvc.exe, PLAN.md §8): it polls the configured MSSQL
// databases (internal/monitor), serves the tray over the named pipe
// (internal/ipc) and reports alerts to the Windows Event Log and the log
// file. Started by the SCM it runs as a service (internal/winsvc, WinAPI
// only, no helper processes — POLICY); started from a console it runs
// interactively until Ctrl+C.
//
// Flags: -config <dir> (default %ProgramData%\ExchangeChecker).
//
// Check ID: CHK-MON-07; tray autostart in HKLM Run: CHK-CFG-04.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/version"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winsvc"
)

func main() {
	dir := flag.String("config", "", "config directory (default %ProgramData%\\ExchangeChecker)")
	flag.Parse()
	ac := appConfig{Dir: *dir}
	if ac.Dir == "" {
		ac.Dir = config.DefaultDir()
	}
	// the SCM-started service keeps HKLM Run in line with
	// settings.trayAutostart (CHK-CFG-04); a console run does not touch it
	ac.Autostart = config.MachineRunRegistry()
	err := winsvc.Run(model.ServiceName, func(ctx context.Context) error { return runService(ctx, ac) })
	if errors.Is(err, winsvc.ErrNotService) {
		fmt.Fprintf(os.Stderr, "%s %s: console mode, config %s (Ctrl+C to stop)\n", model.ServiceName, version.Version, ac.Dir)
		ac.Console = true
		ac.Autostart = nil
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		err = runService(ctx, ac)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "exchsvc:", err)
		os.Exit(1)
	}
}
