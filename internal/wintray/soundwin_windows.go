//go:build windows

package wintray

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayalert"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// Phase 10 A windows: «Звук тревог» (profiles, CHK-TRAY-33/34) and the
// master volume trackbar by the clock (CHK-TRAY-35).

const (
	soundClass  = "ExchangeCheckerTraySound"
	volumeClass = "ExchangeCheckerTrayVolume"

	idSndSave   = 500
	idSndCancel = winapi.IDCANCEL
	idSndInfo   = 501
	// per profile: base + offset (warning 510, error 530)
	idSndBeep   = 0
	idSndFile   = 1
	idSndFreq   = 2
	idSndDur    = 3
	idSndPath   = 4
	idSndBrowse = 5
	idSndVol    = 6
	idSndLoop   = 7
	idSndGap    = 8
	idSndTest   = 9
	idSndWarn   = 510
	idSndErr    = 530

	idVolBar = 600
)

const wsGroup = 0x00020000

// profileCtl are the controls of one profile.
type profileCtl struct {
	base                                int32
	beep, file, freq, dur, path, browse uintptr
	vol, loop, gap, test, head          uintptr
}

// soundWindow edits the two profiles; «Сохранить» stores them in HKCU.
type soundWindow struct {
	a          *App
	hwnd, info uintptr
	save, canc uintptr
	warn, err  profileCtl
	tips       *tooltips
	lang       string
	// pickFile replaces the open dialog (tests)
	pickFile func(cur string) (string, bool)
}

// OpenSound opens (or focuses) «Звук тревог».
func (a *App) OpenSound() *soundWindow {
	if w := a.soundWin; w != nil && winapi.IsWindow(w.hwnd) {
		if !a.opt.HiddenPanel {
			frontWindow(w.hwnd)
		}
		return w
	}
	w, err := newSoundWindow(a)
	if err != nil {
		return nil
	}
	a.soundWin = w
	return w
}

func newSoundWindow(a *App) (*soundWindow, error) {
	const W, H = 600, 470
	lang := a.opt.Lang
	w := &soundWindow{a: a, lang: lang}
	w.pickFile = func(cur string) (string, bool) {
		f := i18n.T(i18n.KeySoundFilterAudio, lang) + "\x00*.wav;*.mp3;*.wma;*.mid;*.midi;*.rmi\x00" +
			i18n.T(i18n.KeySoundFilterAll, lang) + "\x00*.*"
		return winapi.GetOpenFileName(w.hwnd, i18n.T(i18n.KeySoundTitle, lang), f, cur)
	}
	x, y := centered(W, H)
	hwnd, err := createWindow(soundClass, i18n.T(i18n.KeySoundTitle, lang), winapi.WS_EX_DLGMODALFRAME,
		winapi.WS_OVERLAPPED|winapi.WS_CAPTION|winapi.WS_SYSMENU, x, y, W, H, 0, w.wndProc)
	if err != nil {
		return nil, err
	}
	w.hwnd = hwnd
	w.tips = newTooltips(hwnd)
	w.warn = w.buildProfile(idSndWarn, i18n.T(i18n.KeySoundWarning, lang), 12)
	w.err = w.buildProfile(idSndErr, i18n.T(i18n.KeySoundError, lang), 200)
	w.info = child(hwnd, "STATIC", "", winapi.SS_LEFT, idSndInfo, 16, H-110, W-48, 36)
	w.save = child(hwnd, "BUTTON", i18n.T(i18n.KeySoundSave, lang), winapi.BS_DEFPUSHBUTTON|winapi.WS_TABSTOP, idSndSave, W-264, H-70, 120, 28)
	w.canc = child(hwnd, "BUTTON", i18n.T(i18n.KeySoundCancel, lang), winapi.WS_TABSTOP, idSndCancel, W-136, H-70, 104, 28)
	w.tips.set(w.save, i18n.T(i18n.KeyTipSoundSave, lang))
	w.load(a.soundPrefs)
	if !a.opt.HiddenPanel {
		frontWindow(hwnd)
	}
	return w, nil
}

func centered(w, h int32) (int32, int32) {
	return (winapi.GetSystemMetrics(winapi.SM_CXSCREEN) - w) / 2, (winapi.GetSystemMetrics(winapi.SM_CYSCREEN) - h) / 3
}

func (w *soundWindow) buildProfile(base int32, title string, y int32) profileCtl {
	h, l := w.hwnd, w.lang
	edit := func(id int32, x, y, cx int32, num bool) uintptr {
		st := uint32(winapi.WS_BORDER | winapi.WS_TABSTOP | winapi.ES_AUTOHSCROLL)
		if num {
			st |= winapi.ES_NUMBER
		}
		return child(h, "EDIT", "", st, id, x, y, cx, 22)
	}
	label := func(t string, x, y, cx int32) { child(h, "STATIC", t, winapi.SS_LEFT, -1, x, y+3, cx, 20) }
	c := profileCtl{base: base}
	c.head = child(h, "STATIC", title, winapi.SS_LEFT, -1, 16, y, 300, 20)
	c.beep = child(h, "BUTTON", i18n.T(i18n.KeySoundBeep, l), winapi.BS_AUTORADIOBUTTON|winapi.WS_TABSTOP|wsGroup, base+idSndBeep, 32, y+24, 120, 22)
	c.file = child(h, "BUTTON", i18n.T(i18n.KeySoundFile, l), winapi.BS_AUTORADIOBUTTON, base+idSndFile, 160, y+24, 120, 22)
	label(i18n.T(i18n.KeySoundFreq, l), 32, y+52, 130)
	c.freq = edit(base+idSndFreq, 166, y+52, 70, true)
	label(i18n.T(i18n.KeySoundDur, l), 252, y+52, 150)
	c.dur = edit(base+idSndDur, 406, y+52, 70, true)
	c.path = edit(base+idSndPath, 32, y+80, 430, false)
	c.browse = child(h, "BUTTON", i18n.T(i18n.KeySoundBrowse, l), winapi.WS_TABSTOP, base+idSndBrowse, 470, y+79, 100, 24)
	label(i18n.T(i18n.KeySoundVolume, l), 32, y+108, 130)
	c.vol = edit(base+idSndVol, 166, y+108, 50, true)
	c.loop = child(h, "BUTTON", i18n.T(i18n.KeySoundLoop, l), winapi.BS_AUTOCHECKBOX|winapi.WS_TABSTOP, base+idSndLoop, 252, y+108, 300, 22)
	label(i18n.T(i18n.KeySoundGap, l), 32, y+136, 200)
	c.gap = edit(base+idSndGap, 236, y+136, 70, true)
	c.test = child(h, "BUTTON", i18n.T(i18n.KeySoundTest, l), winapi.WS_TABSTOP, base+idSndTest, 450, y+134, 120, 26)
	for ctl, k := range map[uintptr]string{c.beep: i18n.KeyTipSoundKind, c.file: i18n.KeyTipSoundKind,
		c.freq: i18n.KeyTipSoundFreq, c.dur: i18n.KeyTipSoundDur, c.path: i18n.KeyTipSoundFile,
		c.browse: i18n.KeyTipSoundBrowse, c.vol: i18n.KeyTipSoundVolume, c.loop: i18n.KeyTipSoundLoop,
		c.gap: i18n.KeyTipSoundGap, c.test: i18n.KeyTipSoundTest} {
		w.tips.set(ctl, i18n.T(k, l))
	}
	return c
}

func setCheck(h uintptr, on bool) {
	v := uintptr(0)
	if on {
		v = winapi.BST_CHECKED
	}
	winapi.SendMessage(h, winapi.BM_SETCHECK, v, 0)
}

func isChecked(h uintptr) bool {
	return winapi.SendMessage(h, winapi.BM_GETCHECK, 0, 0) == winapi.BST_CHECKED
}

func (w *soundWindow) load(p trayalert.SoundPrefs) {
	for _, x := range []struct {
		c  profileCtl
		pr trayalert.SoundProfile
	}{{w.warn, p.Warning}, {w.err, p.Error}} {
		setCheck(x.c.beep, x.pr.Kind != trayalert.SoundFile)
		setCheck(x.c.file, x.pr.Kind == trayalert.SoundFile)
		winapi.SetWindowText(x.c.freq, strconv.Itoa(x.pr.FreqHz))
		winapi.SetWindowText(x.c.dur, strconv.Itoa(x.pr.DurMs))
		winapi.SetWindowText(x.c.path, x.pr.File)
		winapi.SetWindowText(x.c.vol, strconv.Itoa(x.pr.Volume))
		setCheck(x.c.loop, x.pr.Loop)
		winapi.SetWindowText(x.c.gap, strconv.Itoa(x.pr.GapMs))
	}
}

// read returns the edited profile or the text of the first problem.
func (w *soundWindow) read(c profileCtl) (trayalert.SoundProfile, string) {
	num := func(h uintptr, lo, hi int, name string) (int, string) {
		v, err := strconv.Atoi(strings.TrimSpace(winapi.GetWindowText(h)))
		if err != nil || v < lo || v > hi {
			return 0, fmt.Sprintf(i18n.T(i18n.KeySoundBadValue, w.lang), strings.TrimSuffix(name, ":"))
		}
		return v, ""
	}
	var p trayalert.SoundProfile
	var bad string
	p.Kind = trayalert.SoundBeep
	if isChecked(c.file) {
		p.Kind = trayalert.SoundFile
	}
	if p.FreqHz, bad = num(c.freq, 37, 32767, i18n.T(i18n.KeySoundFreq, w.lang)); bad != "" {
		return p, bad
	}
	if p.DurMs, bad = num(c.dur, 1, 10000, i18n.T(i18n.KeySoundDur, w.lang)); bad != "" {
		return p, bad
	}
	if p.Volume, bad = num(c.vol, 0, 100, i18n.T(i18n.KeySoundVolume, w.lang)); bad != "" {
		return p, bad
	}
	if p.GapMs, bad = num(c.gap, 0, 60000, i18n.T(i18n.KeySoundGap, w.lang)); bad != "" {
		return p, bad
	}
	p.Loop = isChecked(c.loop)
	p.File = strings.TrimSpace(winapi.GetWindowText(c.path))
	if p.Kind == trayalert.SoundFile && p.File == "" {
		return p, fmt.Sprintf(i18n.T(i18n.KeySoundBadFile, w.lang), "—")
	}
	return p, ""
}

// Values are the edited settings (bad = the first problem, "" = valid).
func (w *soundWindow) Values() (trayalert.SoundPrefs, string) {
	p := w.a.soundPrefs
	var bad string
	if p.Warning, bad = w.read(w.warn); bad != "" {
		return p, i18n.T(i18n.KeySoundWarning, w.lang) + ": " + bad
	}
	if p.Error, bad = w.read(w.err); bad != "" {
		return p, i18n.T(i18n.KeySoundError, w.lang) + ": " + bad
	}
	return p, ""
}

func (w *soundWindow) wndProc(hw uintptr, msg uint32, wp, lp uintptr) (uintptr, bool) {
	switch msg {
	case winapi.WM_COMMAND:
		w.command(int32(wp & 0xFFFF))
		return 0, true
	case winapi.WM_CLOSE:
		w.close()
		return 0, true
	}
	return 0, false
}

func (w *soundWindow) command(id int32) {
	switch id {
	case idSndSave:
		p, bad := w.Values()
		if bad != "" {
			winapi.SetWindowText(w.info, bad)
			return
		}
		if err := w.a.SaveSoundPrefs(p); err != nil {
			winapi.SetWindowText(w.info, err.Error())
			return
		}
		w.close()
		return
	case idSndCancel:
		w.close()
		return
	}
	for _, c := range []profileCtl{w.warn, w.err} {
		switch id - c.base {
		case idSndBrowse:
			if f, ok := w.pickFile(winapi.GetWindowText(c.path)); ok {
				winapi.SetWindowText(c.path, f)
				setCheck(c.beep, false)
				setCheck(c.file, true)
			}
		case idSndTest:
			p, bad := w.read(c)
			winapi.SetWindowText(w.info, bad)
			if bad == "" {
				w.a.testSound(p, w.a.soundPrefs.Master)
			}
		}
	}
}

func (w *soundWindow) close() {
	w.a.stopTestSound()
	if winapi.IsWindow(w.hwnd) {
		winapi.DestroyWindow(w.hwnd)
	}
	if w.a.soundWin == w {
		w.a.soundWin = nil
	}
}

// volumeWindow is the small master volume trackbar by the clock; it
// closes when it loses the focus. Every move is stored at once.
type volumeWindow struct {
	hwnd, bar, label uintptr
	tips             *tooltips
}

// OpenVolume opens the master volume window.
func (a *App) OpenVolume() *volumeWindow {
	if w := a.volWin; w != nil && winapi.IsWindow(w.hwnd) {
		if !a.opt.HiddenPanel {
			frontWindow(w.hwnd)
		}
		return w
	}
	winapi.InitTooltips() // ICC_WIN95_CLASSES: msctls_trackbar32
	const W, H = 260, 74
	v := &volumeWindow{}
	hwnd, err := createWindow(volumeClass, i18n.T(i18n.KeyVolumeTitle, a.opt.Lang), winapi.WS_EX_TOPMOST|winapi.WS_EX_TOOLWINDOW,
		winapi.WS_POPUP|winapi.WS_BORDER|winapi.WS_CAPTION|winapi.WS_SYSMENU, 0, 0, W, H, a.hwnd,
		func(hw uintptr, msg uint32, wp, lp uintptr) (uintptr, bool) {
			switch msg {
			case winapi.WM_HSCROLL:
				a.volumeMoved()
				return 0, true
			case winapi.WM_ACTIVATE:
				if wp&0xFFFF == 0 && !a.opt.HiddenPanel { // WA_INACTIVE
					winapi.PostMessage(hw, winapi.WM_CLOSE, 0, 0)
				}
			case winapi.WM_CLOSE:
				winapi.DestroyWindow(hw)
				if a.volWin == v {
					a.volWin = nil
				}
				return 0, true
			}
			return 0, false
		})
	if err != nil {
		return nil
	}
	v.hwnd = hwnd
	v.bar = child(hwnd, "msctls_trackbar32", "", winapi.WS_TABSTOP|winapi.TBS_AUTOTICKS, idVolBar, 8, 6, W-70, 30)
	v.label = child(hwnd, "STATIC", "", winapi.SS_LEFT, -1, W-58, 12, 48, 20)
	winapi.SendMessage(v.bar, winapi.TBM_SETRANGE, 1, 100<<16)
	winapi.SendMessage(v.bar, winapi.TBM_SETTICFREQ, 10, 0)
	winapi.SendMessage(v.bar, winapi.TBM_SETPAGESIZE, 0, 10)
	winapi.SendMessage(v.bar, winapi.TBM_SETPOS, 1, uintptr(a.soundPrefs.Master))
	winapi.SetWindowText(v.label, strconv.Itoa(a.soundPrefs.Master)+" %")
	v.tips = newTooltips(hwnd)
	v.tips.set(v.bar, i18n.T(i18n.KeyTipVolume, a.opt.Lang))
	a.volWin = v
	x, y := a.panelPos(W, H)
	winapi.SetWindowPos(hwnd, winapi.HWND_TOPMOST, x, y, W, H, 0)
	if !a.opt.HiddenPanel {
		frontWindow(hwnd)
	}
	return v
}

func (a *App) volumeMoved() {
	v := a.volWin
	if v == nil {
		return
	}
	pos := int(winapi.SendMessage(v.bar, winapi.TBM_GETPOS, 0, 0))
	winapi.SetWindowText(v.label, strconv.Itoa(pos)+" %")
	if pos != a.soundPrefs.Master {
		if err := a.SetMasterVolume(pos); err != nil {
			a.logf("tray: sound: save master volume: %v", err)
		}
	}
}
