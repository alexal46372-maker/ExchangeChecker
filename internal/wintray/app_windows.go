//go:build windows

package wintray

import (
	"context"
	"sync"
	"unsafe"

	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayalert"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/traysettings"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

const (
	mainClass = model.TrayWindowClass
	// msgEvents: session events are queued (drained on the UI thread).
	msgEvents = winapi.WM_APP + 1
	// msgNotify: NotifyIcon callback message.
	msgNotify = winapi.WM_APP + 2
	trayID    = 1
)

var (
	trayExitOnce sync.Once
	trayExitID   uint32

	trayReloadPrefsOnce sync.Once
	trayReloadPrefsID   uint32
)

// trayExitMsg is the registered model.TrayExitMessage (0 if registration
// failed: the message is then never matched).
func trayExitMsg() uint32 {
	trayExitOnce.Do(func() { trayExitID = winapi.RegisterWindowMessage(model.TrayExitMessage) })
	return trayExitID
}

// isTrayExit reports whether msg is the registered tray exit message.
func isTrayExit(msg uint32) bool {
	m := trayExitMsg()
	return m != 0 && msg == m
}

// trayReloadPrefsMsg is the registered model.TrayReloadPrefsMessage (0 if
// registration failed: the message is then never matched). CHK-TRAY-57.
func trayReloadPrefsMsg() uint32 {
	trayReloadPrefsOnce.Do(func() {
		trayReloadPrefsID = winapi.RegisterWindowMessage(model.TrayReloadPrefsMessage)
	})
	return trayReloadPrefsID
}

// isTrayReloadPrefs reports whether msg is the registered tray prefs
// reload message (CHK-TRAY-57).
func isTrayReloadPrefs(msg uint32) bool {
	m := trayReloadPrefsMsg()
	return m != 0 && msg == m
}

// Options configure the tray application.
type Options struct {
	Lang string
	// LangPref is the per-user language preference Lang was resolved from
	// ("" = as in Windows); LangStore keeps a new choice of the menu
	// (nil = not stored, tests).
	LangPref  string
	LangStore LangStore
	Session   trayipc.Options // Dial and Verify; OnEvent is set by the App
	// NoNotifyIcon skips Shell_NotifyIconW (tests without Explorer).
	NoNotifyIcon bool
	// Beeper plays the alert sound; nil = kernel32 Beep (tests: a fake).
	Beeper trayalert.Beeper
	// HiddenAlerts keeps the alert window hidden (tests); its state is
	// still maintained.
	HiddenAlerts bool
	// Elevator starts `exchtray --settings` elevated; nil =
	// ShellExecuteExW "runas" (tests: a fake).
	Elevator Elevator
	// HiddenSettings keeps the settings window hidden (tests).
	HiddenSettings bool
	// Confirm asks the Yes/No questions of the settings window (delete,
	// «Отменить изменения?»); nil = MessageBoxW, default «Нет» (tests: a
	// fake).
	Confirm func(owner uintptr, text, caption string) bool
	// Service controls the service from the «Служба» submenu; nil = the
	// SCM for model.ServiceName (phase 8, CHK-TRAY-16; tests: a fake).
	Service ServiceControl
	// Notify shows a tray message (service start / stop result); nil =
	// MessageBoxW (tests: a fake).
	Notify func(text string, warn bool)
	// BalloonStore keeps the per-user balloon switch (nil = on, not
	// stored; phase 9a, CHK-TRAY-18).
	BalloonStore BalloonStore
	// Log receives tray log lines (nil = package log; tests: a fake).
	Log func(line string)
	// BalloonDebounce overrides the hold time of recovery balloons (0 =
	// twice the poll interval, < 0 = none; tests).
	BalloonDebounce time.Duration
	// HiddenStatus keeps the «Состояние» window hidden (tests).
	HiddenStatus bool
	// Sounder plays the alarm sounds (phase 10, CHK-TRAY-33); nil = the
	// Windows one, or Beeper through trayalert.BeeperSounder when set.
	Sounder trayalert.Sounder
	// SoundStore keeps the per-user sound profiles (nil = not stored).
	SoundStore SoundStore
	// HiddenPanel keeps the problem panel and the volume window hidden
	// (tests); their state is maintained.
	HiddenPanel bool
	// UIModeStore keeps «Простой» / «Экспертный» of the settings window
	// (phase 10 D; nil = every field, tests).
	UIModeStore UIModeStore
	// ThemeStore keeps light/dark chrome (phase 12, CHK-TRAY-55; nil = light).
	ThemeStore ThemeStore
	// NoBlink disables the icon blinking timer (tests drive blinkTick).
	NoBlink bool
}

// exitWait bounds the wait for the session goroutine in Exit (well under
// the installer's 5 s before TerminateProcess).
var exitWait = 2 * time.Second

// exitWaitHook runs when Exit starts waiting for the session (test hook).
var exitWaitHook func()

// ackTimeout bounds one ack_alert request.
const ackTimeout = 10 * time.Second

// App is the tray application. All methods except the session callback
// run on the thread that called NewApp (a locked OS thread).
type App struct {
	opt     Options
	hwnd    uintptr
	session *trayipc.Session
	cancel  context.CancelFunc
	done    chan struct{}

	qmu     sync.Mutex
	queue   []trayipc.Event
	tasks   []func()
	closing bool

	alerts  *trayalert.Controller
	player  *trayalert.SoundPlayer
	tester  *trayalert.SoundPlayer
	sounder trayalert.Sounder
	// testSounder plays «Прослушать» (own MCI alias / PlaySound owner)
	testSounder trayalert.Sounder
	previewing  bool
	// phase 10: sound profiles (CHK-TRAY-33..35), problem list, blinking
	// icon and panel (CHK-TRAY-38/39)
	soundPrefs  trayalert.SoundPrefs
	soundStored bool
	soundWin    *soundWindow
	volWin      *volumeWindow
	problems    []trayalert.Problem
	statusExt   trayalert.StatusExt
	blink       trayalert.Blink
	blinkOff    bool // the icon is in its dark phase
	blinkTimer  bool
	blinks      int // icon changes by the blink timer (test hook)
	panel       *panelWindow
	awin        *alertWindow
	alertView   trayalert.View

	status         trayipc.Status
	ind            trayui.Indicator
	icons          icons
	squares        bitmaps
	nidAdded       bool
	nidV4          bool
	taskbarCreated uint32
	about          uintptr
	settings       *settingsWindow
	// iconRemoves counts NIM_DELETE calls (test hook, CHK-TRAY-11)
	iconRemoves int
	// taskbarRecreated counts TaskbarCreated messages (test hook)
	taskbarRecreated int
	// phase 9a: balloons (CHK-TRAY-18), «Состояние» (CHK-TRAY-19/21),
	// «Проверить сейчас» results (CHK-TRAY-20, test hook)
	balloonsOn   bool
	balloons     []trayui.Balloon
	statusWin    *statusWindow
	checkResults []string
	// phase 9b: check_now wait (CHK-TRAY-22), service clock (CHK-TRAY-23),
	// balloon debounce (CHK-TRAY-24), events_since catch-up (CHK-TRAY-25),
	// history window (CHK-TRAY-26)
	check        trayui.CheckState
	clock        trayui.ServerClock
	debounce     trayui.Debouncer
	dueTimer     *time.Timer
	tracker      trayui.EventTracker
	wasConnected bool
	catchUps     int
	caughtUp     []model.AlertEvent
	histWin      *histWindow
	summaryWin   *summaryWindow // CHK-TRAY-53
}

// NewApp creates the hidden owner window, the tray icon and starts the
// service session. Call on a locked OS thread, then Loop.
func NewApp(opt Options) (*App, error) {
	a := &App{opt: opt, done: make(chan struct{})}
	a.opt.Lang = i18n.Normalize(opt.Lang)
	LoadTheme(a.opt.ThemeStore) // phase 12 CHK-TRAY-55 (before any window chrome)
	hwnd, err := createWindow(mainClass, i18n.T(i18n.KeyAppName, a.opt.Lang), 0, winapi.WS_OVERLAPPED, 0, 0, 0, 0, 0, a.wndProc)
	if err != nil {
		return nil, err
	}
	a.hwnd = hwnd
	trayExitMsg()
	trayReloadPrefsMsg()
	a.taskbarCreated = winapi.RegisterWindowMessage("TaskbarCreated")
	a.alerts = trayalert.New(a.opt.Lang)
	a.sounder = opt.Sounder
	if a.sounder == nil && opt.Beeper != nil {
		a.sounder = trayalert.BeeperSounder{B: opt.Beeper}
	}
	a.testSounder = a.sounder
	if a.sounder == nil {
		logf := func(l string) { a.logf("%s", l) }
		lang := func() string { return a.opt.Lang }
		a.sounder = &winSounder{alias: aliasAlarm, log: logf, lang: lang}
		a.testSounder = &winSounder{alias: aliasTest, log: logf, lang: lang}
	}
	a.player = trayalert.NewSoundPlayer(a.sounder)
	a.loadSoundPrefs()
	a.ind = trayui.Evaluate(a.status, a.opt.Lang)
	a.loadBalloonPref()
	a.addIcon()

	so := opt.Session
	so.OnEvent = a.onEvent
	a.session = trayipc.New(so)
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	go func() {
		defer close(a.done)
		a.session.Run(ctx)
	}()
	return a, nil
}

// onEvent runs on the session dispatcher goroutine.
func (a *App) onEvent(e trayipc.Event) {
	a.qmu.Lock()
	if a.closing {
		a.qmu.Unlock()
		return
	}
	first := len(a.queue) == 0 && len(a.tasks) == 0
	a.queue = append(a.queue, e)
	a.qmu.Unlock()
	if first {
		winapi.PostMessage(a.hwnd, msgEvents, 0, 0)
	}
}

// runOnUI queues f for the UI thread (from any goroutine).
func (a *App) runOnUI(f func()) {
	a.qmu.Lock()
	if a.closing {
		a.qmu.Unlock()
		return
	}
	first := len(a.queue) == 0 && len(a.tasks) == 0
	a.tasks = append(a.tasks, f)
	a.qmu.Unlock()
	if first {
		winapi.PostMessage(a.hwnd, msgEvents, 0, 0)
	}
}

// AlertView is the current alert state (UI thread).
func (a *App) AlertView() trayalert.View { return a.alertView }

// AlertWindow is the alert window handle (0 before the first alert).
func (a *App) AlertWindow() uintptr {
	if a.awin == nil {
		return 0
	}
	return a.awin.hwnd
}

// applyAlert shows the alert view: window and sound.
func (a *App) applyAlert(v trayalert.View) {
	a.alertView = v
	a.player.Set(a.alarmOn(v), a.alertSound(v))
	if a.awin == nil {
		if !v.Visible {
			return
		}
		// unowned: own taskbar button (flashes when the foreground lock refuses)
		aw, err := newAlertWindow(0, a.opt.Lang, a.ackAlerts, a.closeAlerts)
		if err != nil {
			return
		}
		aw.onMute = a.muteAlerts
		a.awin = aw
	}
	a.awin.apply(v, !a.opt.HiddenAlerts)
}

// closeAlerts: the user closed the alert window (mute until the next
// change, PLAN §12 p.50).
func (a *App) closeAlerts() { a.applyAlert(a.alerts.Close()) }

// muteAlerts: «Звук вкл/выкл» of the current alarm (CHK-TRAY-36).
func (a *App) muteAlerts() { a.applyAlert(a.alerts.ToggleMute()) }

// ackAlerts: «Подтвердить» — ack_alert for the shown alerts, off the UI
// thread; replies come back through runOnUI.
func (a *App) ackAlerts() {
	reqs := a.alerts.BeginAck()
	if reqs == nil {
		return
	}
	a.applyAlert(a.alerts.View())
	s := a.session
	go func() {
		res := make([]trayalert.AckResult, 0, len(reqs))
		for _, r := range reqs {
			ctx, cancel := context.WithTimeout(context.Background(), ackTimeout)
			m, err := s.Do(ctx, r.Req)
			cancel()
			res = append(res, trayalert.AckResult{Req: r, Msg: m, Err: err})
		}
		a.runOnUI(func() {
			v, refresh := a.alerts.AckDone(res)
			a.applyAlert(v)
			if refresh {
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), ackTimeout)
					defer cancel()
					s.Refresh(ctx)
				}()
			}
		})
	}()
}

// Indicator is the current tray state (UI thread).
func (a *App) Indicator() trayui.Indicator { return a.ind }

// Status is the last session status applied on the UI thread.
func (a *App) Status() trayipc.Status { return a.status }

// Session is the service session.
func (a *App) Session() *trayipc.Session { return a.session }

// HasNotifyIcon reports whether the tray icon is shown.
func (a *App) HasNotifyIcon() bool { return a.nidAdded }

// Window is the hidden owner window.
func (a *App) Window() uintptr { return a.hwnd }

// Loop runs the message loop until Exit.
func (a *App) Loop() int {
	var m winapi.MSG
	for {
		r := winapi.GetMessage(&m)
		if r == 0 || r == -1 {
			break
		}
		if w := a.settings; w != nil && w.disc != nil && winapi.IsWindow(w.disc.hwnd) && winapi.IsDialogMessage(w.disc.hwnd, &m) {
			continue
		}
		if w := a.settings; w != nil && winapi.IsWindow(w.hwnd) && winapi.IsDialogMessage(w.hwnd, &m) {
			continue
		}
		winapi.TranslateMessage(&m)
		winapi.DispatchMessage(&m)
	}
	return int(m.WParam)
}

// Exit removes the icon, stops the session and ends Loop.
func (a *App) Exit() {
	a.qmu.Lock()
	already := a.closing
	a.closing = true
	a.qmu.Unlock()
	if already {
		return
	}
	a.removeIcon()
	a.player.Close()
	if a.tester != nil {
		a.tester.Close()
	}
	a.stopBlink()
	if a.panel != nil && winapi.IsWindow(a.panel.hwnd) {
		winapi.DestroyWindow(a.panel.hwnd)
	}
	if a.volWin != nil && winapi.IsWindow(a.volWin.hwnd) {
		winapi.DestroyWindow(a.volWin.hwnd)
	}
	if a.soundWin != nil && winapi.IsWindow(a.soundWin.hwnd) {
		winapi.DestroyWindow(a.soundWin.hwnd)
	}
	a.cancel()
	// bounded: a hung session must not keep the tray (the icon is already
	// gone) — the installer terminates it after 5 s (CHK-TRAY-11)
	if exitWaitHook != nil {
		exitWaitHook()
	}
	select {
	case <-a.done:
	case <-time.After(exitWait):
	}
	if a.awin != nil {
		winapi.DestroyWindow(a.awin.hwnd)
	}
	if a.histWin != nil && a.histWin.alive() {
		winapi.DestroyWindow(a.histWin.hwnd) // cancels its history request
	}
	if a.summaryWin != nil && winapi.IsWindow(a.summaryWin.hwnd) {
		winapi.DestroyWindow(a.summaryWin.hwnd)
	}
	if a.dueTimer != nil {
		a.dueTimer.Stop()
	}
	if a.statusWin != nil && winapi.IsWindow(a.statusWin.hwnd) {
		winapi.DestroyWindow(a.statusWin.hwnd)
	}
	if a.about != 0 {
		winapi.DestroyWindow(a.about)
	}
	if a.settings != nil {
		a.settings.exitNow() // no questions; cancels a running test
	}
	winapi.DestroyWindow(a.hwnd) // WM_DESTROY -> PostQuitMessage
	a.icons.free()
	a.squares.free()
}

func (a *App) wndProc(hwnd uintptr, msg uint32, wp, lp uintptr) (uintptr, bool) {
	switch {
	case msg == msgEvents:
		a.drain()
		return 0, true
	case msg == msgNotify:
		a.onNotify(uint32(lp & 0xFFFF))
		return 0, true
	case a.taskbarCreated != 0 && msg == a.taskbarCreated:
		// Explorer restarted: the icon is gone, add it again (with
		// NOTIFYICON_VERSION_4, CHK-TRAY-37)
		a.nidAdded = false
		a.taskbarRecreated++
		a.addIcon()
		return 0, true
	case msg == winapi.WM_TIMER && wp == blinkTimerID:
		a.blinkTick()
		return 0, true
	case isTrayReloadPrefs(msg):
		// CHK-TRAY-57: reread Sound / UIMode / Theme / balloons / lang
		// from HKCU without Exit (wParam / lParam ignored)
		a.reloadTrayPrefs()
		return 0, true
	case isTrayExit(msg), msg == winapi.WM_CLOSE:
		// CHK-TRAY-11: the installer's exit message and WM_CLOSE remove
		// the icon and end the tray (wParam / lParam ignored)
		a.Exit()
		return 0, true
	case msg == winapi.WM_DESTROY:
		winapi.PostQuitMessage(0)
		return 0, false
	}
	return 0, false
}

// drain applies queued session events (UI thread).
func (a *App) drain() {
	a.qmu.Lock()
	q, tasks := a.queue, a.tasks
	a.queue, a.tasks = nil, nil
	a.qmu.Unlock()
	for _, f := range tasks {
		f()
	}
	if len(q) == 0 {
		return
	}
	// every event carries the full status; the last one wins. Alert push
	// events are followed by a status push; the active alerts of the
	// snapshot drive the alert window (trayalert).
	a.status = q[len(q)-1].Status
	if w := a.settings; w != nil {
		for _, e := range q { // every transition, not only the last state
			switch e.Kind {
			case trayipc.EventState:
				w.onSessionState(e.Status)
			case trayipc.EventStatus: // e.g. trayAutostartError after a reconnection
				w.onSessionStatus()
			}
		}
	}
	for _, e := range q { // alert_repeat: push only (phase 10)
		if e.Kind == trayipc.EventAlertRepeat && e.Alert != nil {
			if r, ok := trayalert.RepeatOf(*e.Alert); ok {
				a.alerts.Repeat(r.ProfileID)
			}
		}
		if e.Kind == trayipc.EventAlert && e.Alert != nil && e.Alert.Test {
			a.applyAlert(a.alerts.PushTest(*e.Alert))
		}
	}
	a.statusExt = trayalert.ExtOf(a.status)
	a.applyAlert(a.alerts.Update(a.status, a.session.Supported(model.CmdAckAlert)))
	old := a.ind
	a.ind = trayui.Evaluate(a.status, a.opt.Lang)
	if !a.nidAdded {
		a.addIcon()
	} else if old.Icon != a.ind.Icon || old.Tooltip != a.ind.Tooltip {
		a.modifyIcon()
	}
	a.updateProblems()
	a.afterDrain(q)
	a.balloonEvents(q)
	if sw := a.statusWin; sw != nil && winapi.IsWindow(sw.hwnd) {
		sw.update(a.status, a.connected() && !a.check.Busy)
	}
}

func (a *App) onNotify(ev uint32) {
	switch ev {
	case winapi.NIN_SELECT, winapi.NIN_KEYSELECT:
		a.ShowLeftMenu()
	case winapi.WM_CONTEXTMENU:
		a.ShowRightMenu()
	case winapi.WM_LBUTTONUP:
		if !a.nidV4 {
			a.ShowLeftMenu()
		}
	case winapi.WM_RBUTTONUP:
		if !a.nidV4 {
			a.ShowRightMenu()
		}
	}
}

// LeftMenu / RightMenu are the current menu models.
func (a *App) LeftMenu() []trayui.Item { return trayui.BuildLeftMenu(a.status, a.opt.Lang) }
func (a *App) RightMenu() []trayui.Item {
	sum := a.session != nil && (a.session.Supported(model.CmdSummary) || a.session.Status().Has(model.CapSummary))
	return trayui.BuildTrayRightMenu(trayui.RightMenuOpts{
		Lang: a.opt.Lang, Pref: a.opt.LangPref, Svc: a.serviceState(),
		Theme: trayui.Current(), Balloons: a.balloonsOn,
		Connected: a.connected() && !a.check.Busy, SummaryAvailable: sum,
	})
}

// ShowLeftMenu shows the databases menu at the cursor.
func (a *App) ShowLeftMenu() { a.Command(trackMenu(a.hwnd, a.LeftMenu(), &a.squares)) }

// ShowRightMenu shows Settings / About / Exit at the cursor.
func (a *App) ShowRightMenu() { a.Command(trackMenu(a.hwnd, a.RightMenu(), &a.squares)) }

// Command executes a menu command.
func (a *App) Command(cmd int) {
	a.qmu.Lock()
	closing := a.closing
	a.qmu.Unlock()
	if closing { // e.g. the exit message arrived while a menu was open
		return
	}
	switch cmd {
	case trayui.CmdSettings:
		a.OpenSettings()
	case trayui.CmdAbout:
		a.ShowAbout(true)
	case trayui.CmdExit:
		a.Exit()
	case trayui.CmdLangAuto, trayui.CmdLangRU, trayui.CmdLangEN:
		a.SetLanguage(trayui.LangCommands[cmd])
	case trayui.CmdSvcStart, trayui.CmdSvcStop:
		a.serviceCommand(cmd == trayui.CmdSvcStart)
	case trayui.CmdSummary:
		a.openSummary()
	case trayui.CmdStatusWin:
		a.OpenStatus()
	case trayui.CmdCheckNow:
		a.CheckNow()
	case trayui.CmdBalloons:
		a.SetBalloons(!a.balloonsOn)
	case trayui.CmdSound: // not in tray menu (CHK-TRAY-62); kept for tests
		a.OpenSound()
	case trayui.CmdVolume:
		a.OpenVolume()
	case trayui.CmdPinHelp:
		a.ShowPinHelp()
	case trayui.CmdThemeLight, trayui.CmdThemeDark:
		a.SetChromeTheme(trayui.ThemeCommands[cmd])
	default:
		if i, mute, ok := trayui.MuteProfileIndex(cmd); ok {
			a.muteProfile(i, mute)
		}
	}
}

// muteProfile sends mute_until_recovered / unmute_profile for
// status.Snapshot.Profiles[i] (CHK-TRAY-59).
func (a *App) muteProfile(i int, mute bool) {
	if a.session == nil || !a.session.Supported(model.CmdMuteUntilRecovered) {
		return
	}
	st := a.session.Status()
	if st.Snapshot == nil || i < 0 || i >= len(st.Snapshot.Profiles) {
		return
	}
	id := st.Snapshot.Profiles[i].ProfileID
	cmd := model.CmdMuteUntilRecovered
	if !mute {
		cmd = model.CmdUnmuteProfile
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), ackTimeout)
		defer cancel()
		m, err := a.session.Do(ctx, model.IPCRequest{Cmd: cmd, ProfileID: id})
		if err != nil || m.Type == model.MsgError {
			return // menu is ephemeral; next status push refreshes the indicator
		}
		_ = a.session.Refresh(ctx)
	}()
}

// SetLanguage applies a per-user language preference at once to the
// menus, the icon tooltip and the alert window, and stores it (HKCU).
// Already open settings / About windows take it when opened next time.
func (a *App) SetLanguage(pref string) error {
	pref = NormalizePref(pref)
	var err error
	if a.opt.LangStore != nil {
		err = a.opt.LangStore.Save(pref)
	}
	a.opt.LangPref = pref
	a.opt.Lang = ResolveLang(pref)
	a.alerts.SetLang(a.opt.Lang)
	if a.awin != nil {
		a.awin.setLang(a.opt.Lang)
		a.awin.apply(a.alerts.View(), !a.opt.HiddenAlerts)
	}
	old := a.ind
	a.ind = trayui.Evaluate(a.status, a.opt.Lang)
	if a.nidAdded && old.Tooltip != a.ind.Tooltip {
		a.modifyIcon()
	}
	return err
}

// reloadTrayPrefs rereads Sound / UIMode / Theme / balloons / lang from
// HKCU and applies them live without Exit (CHK-TRAY-57).
func (a *App) reloadTrayPrefs() {
	if a.opt.LangStore != nil {
		if pref, err := a.opt.LangStore.Load(); err == nil {
			a.opt.LangPref = pref
			a.opt.Lang = ResolveLang(pref)
			a.alerts.SetLang(a.opt.Lang)
			if a.awin != nil {
				a.awin.setLang(a.opt.Lang)
			}
			a.ind = trayui.Evaluate(a.status, a.opt.Lang)
			a.modifyIcon()
		}
	}
	a.loadBalloonPref()
	a.loadSoundPrefs()
	_ = loadUIMode(a.opt.UIModeStore) // next settings open reads the store
	LoadTheme(a.opt.ThemeStore)
	ApplyWindowTheme(a.hwnd)
	if a.settings != nil && winapi.IsWindow(a.settings.hwnd) {
		ApplyWindowTheme(a.settings.hwnd)
		if a.settings.host != 0 {
			ApplyWindowTheme(a.settings.host)
		}
	}
	if a.awin != nil && winapi.IsWindow(a.awin.hwnd) {
		ApplyWindowTheme(a.awin.hwnd)
	}
}


// OpenSettings opens (or focuses) the settings window and returns its
// handle (the open Discovery dialog's when there is one, brought to the
// front): the administrator mode in this process when the service reports
// it elevated (clientElevated) and accepts config writes, otherwise the
// read-only mode from the last get_status, with «Изменить
// (администратор)» starting `exchtray --settings` elevated.
func (a *App) OpenSettings() uintptr {
	if w := a.settings; w != nil && winapi.IsWindow(w.hwnd) {
		if !a.opt.HiddenSettings {
			// the open Discovery dialog comes to the front (its owner is
			// disabled until it is closed)
			frontWindow(w.hwnd)
		}
		if d := w.disc; d != nil && winapi.IsWindow(d.hwnd) {
			// the open Discovery dialog comes to the front (its owner is
			// disabled until it is closed); its handle is returned
			if !a.opt.HiddenSettings {
				frontWindow(d.hwnd)
			}
			return d.hwnd
		}
		return w.hwnd
	}
	mode := traysettings.ReadOnly
	if a.status.Elevated() && a.session.Supported(model.CmdSaveProfile) && a.session.Supported(model.CmdGetConfig) {
		mode = traysettings.Admin
	}
	var w *settingsWindow
	w, err := newSettingsWindow(settingsConfig{lang: a.opt.Lang, mode: mode, session: a.session, elevator: a.opt.Elevator,
		uiMode: loadUIMode(a.opt.UIModeStore), uiStore: a.opt.UIModeStore, themeStore: a.opt.ThemeStore,
		stores:  trayStores{Lang: a.opt.LangStore, Balloon: a.opt.BalloonStore, Sound: a.opt.SoundStore},
		confirm: a.opt.Confirm, hidden: a.opt.HiddenSettings,
		onSoundSave: a.SaveSoundPrefs, onSoundTest: a.testSound, soundPrefs: func() trayalert.SoundPrefs { return a.SoundPrefs() },
		onClose: func() {
			if a.settings == w {
				a.settings = nil
			}
		}})
	if err != nil {
		return 0
	}
	a.settings = w
	w.connected = a.status.State == trayipc.StateConnected
	if mode == traysettings.Admin {
		w.Reload()
	} else {
		w.loadSnapshot(a.status.Snapshot)
	}
	return w.hwnd
}

// ShowAbout opens (or focuses) the About window; returns its handle.
func (a *App) ShowAbout(show bool) uintptr {
	if a.about != 0 && winapi.IsWindow(a.about) {
		if show {
			winapi.SetForegroundWindow(a.about)
		}
		return a.about
	}
	lang := a.opt.Lang
	h, err := newAboutWindow(a.hwnd, trayui.AboutTitle(lang), trayui.AboutText(a.session.Status(), lang),
		i18n.T(i18n.KeyAboutOK, lang), i18n.T(i18n.KeyTipAboutOK, lang), show, func() { a.about = 0 })
	if err != nil {
		return 0
	}
	a.about = h
	return h
}

func (a *App) nid() winapi.NOTIFYICONDATAW {
	var n winapi.NOTIFYICONDATAW
	n.Size = uint32(unsafe.Sizeof(n))
	n.Wnd = a.hwnd
	n.ID = trayID
	n.Flags = winapi.NIF_MESSAGE | winapi.NIF_ICON | winapi.NIF_TIP | winapi.NIF_SHOWTIP
	n.CallbackMessage = msgNotify
	n.Icon = a.icons.get(a.shownIcon())
	tip, _ := utf16From(a.ind.Tooltip)
	copy(n.Tip[:len(n.Tip)-1], tip)
	return n
}

func (a *App) addIcon() {
	if a.opt.NoNotifyIcon || a.nidAdded {
		return
	}
	n := a.nid()
	if err := winapi.ShellNotifyIcon(winapi.NIM_ADD, &n); err != nil {
		// Explorer not ready yet (logon): retried on TaskbarCreated and on
		// the next status change
		return
	}
	a.nidAdded = true
	n.Version = winapi.NOTIFYICON_VERSION_4
	a.nidV4 = winapi.ShellNotifyIcon(winapi.NIM_SETVERSION, &n) == nil
}

func (a *App) modifyIcon() {
	n := a.nid()
	if winapi.ShellNotifyIcon(winapi.NIM_MODIFY, &n) != nil {
		a.nidAdded = false
		a.addIcon()
	}
}

func (a *App) removeIcon() {
	if !a.nidAdded {
		return
	}
	n := a.nid()
	n.Flags = 0
	winapi.ShellNotifyIcon(winapi.NIM_DELETE, &n)
	a.nidAdded = false
	a.iconRemoves++
}
