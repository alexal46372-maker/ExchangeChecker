package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/alert"
	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/ipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/monitor"
	"github.com/alexal46372-maker/ExchangeChecker/internal/version"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// LogFile is the service log (rotated) under the config directory.
const LogFile = `logs` + string(filepath.Separator) + "exchsvc.log"

// appConfig wires the service; zero fields = production defaults.
type appConfig struct {
	Dir     string
	Console bool
	// Listener overrides the named pipe (tests).
	Listener ipc.Listener
	// NewReader overrides sqlcheck.NewReader (tests).
	NewReader monitor.ReaderFactory
	// Events overrides the Windows Event Log (tests); nil = advapi32
	// source model.EventSource (skipped when it cannot be opened).
	Events       alert.Reporter
	Now          func() time.Time
	PollInterval time.Duration
	ConfigCheck  time.Duration
	// Test overrides the connection test of test_profile (tests).
	Test monitor.TestFunc
	// TestAfter overrides the timer of the test timeout (tests).
	TestAfter func(time.Duration) <-chan time.Time
	// Ready is called once the monitor and IPC server are running (tests).
	Ready func(*monitor.Monitor, *ipc.Server)
}

// stderrLog is the fallback logger when the log file cannot be opened.
type stderrLog struct{}

func (stderrLog) Printf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}

type teeLog struct{ a, b alert.Logger }

func (t teeLog) Printf(format string, args ...interface{}) {
	t.a.Printf(format, args...)
	t.b.Printf(format, args...)
}

// runService runs the monitor and the IPC server until ctx is canceled
// (SCM Stop / Shutdown or Ctrl+C), then stops both cleanly: the poll loop
// ends, readers (SQL pools) are closed, clients are disconnected, the pipe
// is closed.
func runService(ctx context.Context, ac appConfig) error {
	var log alert.Logger = stderrLog{}
	lf, err := alert.OpenRotatingFile(filepath.Join(ac.Dir, LogFile), alert.DefaultLogMaxBytes, alert.DefaultLogKeep)
	if err == nil {
		defer lf.Close()
		log = lf
		if ac.Console {
			log = teeLog{lf, stderrLog{}}
		}
	} else {
		log.Printf("log file: %v", err)
	}
	log.Printf("%s %s starting, config %s", model.ServiceName, version.Version, ac.Dir)

	store := config.NewStore(ac.Dir)
	settings, _ := store.LoadSettings()
	lang := config.EffectiveLanguage(settings)

	events := ac.Events
	if events == nil {
		if el, err := winapi.OpenEventLog(model.EventSource); err == nil {
			defer el.Close()
			events = el
		} else {
			log.Printf("event log unavailable: %v", err)
		}
	}
	report := func(typ uint16, id uint32, msg string) {
		if events != nil {
			if err := events.Report(typ, id, msg); err != nil {
				log.Printf("event log: %v", err)
			}
		}
	}

	var srv *ipc.Server
	mon := monitor.New(monitor.Options{
		Source:    monitor.FileConfig{Store: store},
		NewReader: ac.NewReader,
		Counters:  monitor.OpenFileCounterStore(ac.Dir, log),
		// alarm acknowledgements next to counters-state.json (CHK-IPC-02)
		Acks: monitor.OpenFileAckStore(ac.Dir, log),
		// one informational Event Log entry per acknowledgement; the
		// alert entries themselves are not changed by an ack
		OnAck: func(e model.AlertEvent) {
			report(alert.EventTypeInformation, alert.EventIDAck, alert.FormatAck(e, lang))
		},
		Test:      ac.Test,
		TestAfter: ac.TestAfter,
		Log:       log,
		Sink: alert.Multi{
			&alert.EventLogSink{R: events, Lang: lang, Fallback: log},
			alert.SinkFunc(func(e model.AlertEvent) { srv.Alert(e) }),
		},
		// config-load failures and hung readers (already rate-limited by
		// the monitor) -> Event Log warning (CHK-MON-10, CHK-MON-13)
		Warn:         func(msg string) { report(alert.EventTypeWarning, alert.EventIDInternal, msg) },
		OnSnapshot:   func(s model.StatusSnapshot) { srv.PublishStatus(s) },
		Now:          ac.Now,
		PollInterval: ac.PollInterval,
		ConfigCheck:  ac.ConfigCheck,
	})
	srv = ipc.NewServer(mon, log)

	l := ac.Listener
	if l == nil {
		pl, err := ipc.ListenPipe(model.PipeName, "")
		if err != nil {
			// keep monitoring (event log + log file) even without the tray
			log.Printf("ipc: cannot create %s: %v", model.PipeName, err)
			report(alert.EventTypeError, alert.EventIDInternal, "IPC pipe: "+err.Error())
		} else {
			l = pl
		}
	}
	served := make(chan struct{})
	if l != nil {
		go func() {
			defer close(served)
			if err := srv.Serve(l); err != nil {
				log.Printf("ipc: serve: %v", err)
			}
		}()
	} else {
		close(served)
	}
	report(alert.EventTypeInformation, alert.EventIDService, fmt.Sprintf("%s %s started", model.ServiceName, version.Version))
	if ac.Ready != nil {
		ac.Ready(mon, srv)
	}
	err = mon.Run(ctx)
	_ = srv.Close()
	<-served
	report(alert.EventTypeInformation, alert.EventIDService, fmt.Sprintf("%s stopped", model.ServiceName))
	log.Printf("%s stopped", model.ServiceName)
	return err
}
