package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/alert"
	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/history"
	"github.com/alexal46372-maker/ExchangeChecker/internal/ipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/monitor"
	"github.com/alexal46372-maker/ExchangeChecker/internal/sqlcheck"
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
	// TestWithTimeout overrides the context carrying the single test
	// deadline (tests; default context.WithTimeout).
	TestWithTimeout func(context.Context, time.Duration) (context.Context, context.CancelFunc)
	// Authorize overrides the pipe client check of admin commands
	// (tests over a MemListener; nil = ImpersonateNamedPipeClient).
	Authorize func(io.ReadWriteCloser) (ipc.Peer, error)
	// SecureSecrets overrides the start-up DACL fix of secrets.dpapi
	// (tests; nil = config.Store.SecureSecretsFile).
	SecureSecrets func(*config.Store) (bool, error)
	// Autostart is the Run key of the tray autostart (CHK-CFG-04): main
	// sets config.MachineRunRegistry() (HKLM Run, 64-bit view) when the
	// SCM starts the service; nil = not managed (console mode, tests).
	Autostart config.RunRegistry
	// TrayCommand overrides the Run value (tests); "" = the quoted full
	// path of ExchangeCheckerTray.exe next to the service executable.
	TrayCommand string
	// SecureHistory overrides the DACL of the history folder (tests; nil =
	// secureHistoryDir: protected SYSTEM + Administrators on Windows).
	SecureHistory func(dir string) error
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
	// phase 6b (CHK-CFG-03): an existing secrets.dpapi (e.g. written before
	// phase 6 with the inherited Users-read DACL) is brought to SY+BA,
	// protected, at start; a failure is a warning, the service keeps going.
	secure := ac.SecureSecrets
	if secure == nil {
		secure = (*config.Store).SecureSecretsFile
	}
	if present, err := secure(store); err != nil {
		log.Printf("warning: %s: cannot set the SYSTEM+Administrators DACL: %v", config.SecretsFile, err)
	} else if present {
		log.Printf("%s: DACL SYSTEM+Administrators (protected)", config.SecretsFile)
	}
	settings, settingsErr := store.LoadSettings()
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

	// tray autostart (CHK-CFG-04): HKLM Run brought in line with
	// settings.trayAutostart at start; a registry error is a warning, the
	// service keeps going
	autostart := trayAutostart(ac, log)
	switch {
	case autostart == nil:
		if ac.Console {
			log.Printf("console mode: the tray autostart (HKLM Run) is not managed")
		}
	case settingsErr != nil:
		autostart.setCode(model.AutostartErrSettings)
		msg := fmt.Sprintf("warning: autostart: %s not loaded (%v): HKLM Run %s left unchanged", config.SettingsFile, settingsErr, config.TrayRunValue)
		log.Printf("%s", msg)
		report(alert.EventTypeWarning, alert.EventIDInternal, msg)
	default:
		if err := autostart.Apply(settings); err != nil {
			msg := fmt.Sprintf("warning: autostart at start: %v", err)
			log.Printf("%s", msg)
			report(alert.EventTypeWarning, alert.EventIDInternal, msg)
		}
	}

	var applySettings func(model.Settings) error
	if autostart != nil {
		applySettings = autostart.Apply
	}

	test := ac.Test
	if test == nil {
		test = monitor.SQLTest
	}
	// phase 9 (CHK-SVC-03): poll history under <dir>\history, SYSTEM +
	// Administrators only; a failure disables the history (logged), the
	// service keeps monitoring
	secureHist := ac.SecureHistory
	if secureHist == nil {
		secureHist = func(dir string) error { return secureHistoryDir(dir, log.Printf) }
	}
	hist := history.Open(history.Options{Dir: filepath.Join(ac.Dir, model.HistoryDir), Prepare: secureHist, Now: ac.Now, Log: log})
	defer hist.Close()

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
		// configuration changed through IPC by an administrator (phase
		// 6): one informational entry 105 with account, action, profile
		// id (CHK-MON-05 extension); the installer of phase 7 registers
		// 104 and 105 in the message file
		OnConfigChange: func(c model.ConfigChange) {
			report(alert.EventTypeInformation, alert.EventIDConfig, alert.FormatConfigChange(c, lang))
		},
		// save_settings -> HKLM Run (CHK-CFG-04); an error is logged and
		// sent to Warn, the settings stay saved
		ApplySettings: applySettings,
		// test_profile: sqlcheck.TestConnectionInfo + ConnCategory
		// (CHK-IPC-03); tests inject a fake
		Test:            test,
		TestCategory:    sqlcheck.ConnCategory,
		TestWithTimeout: ac.TestWithTimeout,
		Log:             log,
		Sink: alert.Multi{
			&alert.EventLogSink{R: events, Lang: lang, Fallback: log},
			alert.SinkFunc(func(e model.AlertEvent) { srv.Alert(e) }),
			hist, // transition records of the history (CHK-SVC-03)
		},
		// config-load failures and hung readers (already rate-limited by
		// the monitor) -> Event Log warning (CHK-MON-10, CHK-MON-13)
		Warn: func(msg string) { report(alert.EventTypeWarning, alert.EventIDInternal, msg) },
		// repeat pushes of active alerts after every failing check, to
		// the tray only (CHK-MON-17)
		OnRepeat: func(e model.AlertEvent) { srv.Alert(e) },
		// the distinct alert_repeat event (also in events_since)
		OnAlertRepeat: func(e model.AlertEvent) { srv.AlertRepeat(e) },
		OnSnapshot: func(s model.StatusSnapshot) {
			srv.PublishStatus(s)
			hist.RecordSnapshot(s) // profiles polled since the last snapshot
		},
		Now:          ac.Now,
		PollInterval: ac.PollInterval,
		ConfigCheck:  ac.ConfigCheck,
	})
	srv = ipc.NewServer(mon, log)
	srv.Authorize = ac.Authorize
	srv.History = historySource{h: hist, mon: mon}
	if autostart != nil {
		// get_status.trayAutostartError + capability tray_autostart_status
		srv.TrayAutostartError = autostart.ErrorCode
	}
	// first configuration load before the pipe accepts clients: a tray
	// connecting at service start never sees a snapshot without settings /
	// configRevision / profiles (windows CI 37123714167)
	mon.LoadConfig()

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

// trayAutostarter applies settings.trayAutostart to the Run value
// config.TrayRunValue (CHK-CFG-04) and keeps the code of the last failed
// application for get_status.trayAutostartError. Enabled: the quoted full
// path of config.TrayExeName in the folder of the service executable
// (REG_SZ, written when missing or different); disabled: the value is
// removed (missing is fine).
type trayAutostarter struct {
	reg    config.RunRegistry
	cmd    string
	cmdErr error
	log    alert.Logger

	mu   sync.Mutex
	code string // model.AutostartErr* of the last application, "" = ok
}

// trayAutostart returns the autostarter, nil when the service does not
// manage the Run value (ac.Autostart nil: console mode, tests).
func trayAutostart(ac appConfig, log alert.Logger) *trayAutostarter {
	if ac.Autostart == nil {
		return nil
	}
	a := &trayAutostarter{reg: ac.Autostart, cmd: ac.TrayCommand, log: log}
	if a.cmd == "" {
		exe, err := os.Executable()
		if err != nil {
			err = fmt.Errorf("%w: %v", config.ErrTrayPath, err)
		} else {
			a.cmd, err = config.TrayRunCommand(exe)
		}
		if err != nil {
			a.cmd, a.cmdErr = "", err
		}
	}
	return a
}

// Apply brings the Run value in line with st.TrayAutostart and records
// the result (model.AutostartErr* code or "").
func (a *trayAutostarter) Apply(st model.Settings) error {
	err := a.apply(st)
	a.setCode(config.AutostartErrorCode(err))
	return err
}

func (a *trayAutostarter) apply(st model.Settings) error {
	if st.TrayAutostart && a.cmdErr != nil {
		return a.cmdErr
	}
	act, err := config.ApplyTrayAutostart(a.reg, st.TrayAutostart, a.cmd)
	if err != nil {
		return err
	}
	switch act {
	case config.AutostartWritten:
		a.log.Printf("autostart: HKLM Run %s = %s", config.TrayRunValue, a.cmd)
	case config.AutostartDeleted:
		a.log.Printf("autostart: HKLM Run %s removed (trayAutostart off)", config.TrayRunValue)
	}
	return nil
}

func (a *trayAutostarter) setCode(code string) {
	a.mu.Lock()
	a.code = code
	a.mu.Unlock()
}

// ErrorCode is get_status.trayAutostartError: the model.AutostartErr*
// code of the last application ("" after a successful one).
func (a *trayAutostarter) ErrorCode() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.code
}
