//go:build windows

package wintray

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayalert"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
	"github.com/alexal46372-maker/ExchangeChecker/internal/wintray/ui"
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
	var warnN, errN ui.Node
	w.warn, warnN = w.buildProfile(idSndWarn, i18n.T(i18n.KeySoundWarning, lang))
	w.err, errN = w.buildProfile(idSndErr, i18n.T(i18n.KeySoundError, lang))
	w.info = child(hwnd, "STATIC", "", winapi.SS_LEFT, idSndInfo, 0, 0, 0, 0)
	w.save = child(hwnd, "BUTTON", i18n.T(i18n.KeySoundSave, lang), winapi.BS_DEFPUSHBUTTON|winapi.WS_TABSTOP, idSndSave, 0, 0, 0, 0)
	w.canc = child(hwnd, "BUTTON", i18n.T(i18n.KeySoundCancel, lang), winapi.WS_TABSTOP, idSndCancel, 0, 0, 0, 0)
	// phase 11 (CHK-TRAY-45): two profiles, the info line, the buttons
	fitForm(hwnd, &ui.Pad{Left: ui.P16, Top: ui.P16, Right: ui.P16, Child: &ui.Col{Gap: ui.P16, Items: []ui.Node{warnN, errN}}},
		&ui.Pad{All: ui.P16, Child: &ui.Col{Gap: ui.P8, Items: []ui.Node{
			&ui.Ctl{H: w.info, Kind: ui.Box, W: 540, Hgt: 36, Grow: true},
			&ui.Row{Right: true, Items: []ui.Node{uiCtl(w.save, ui.Button, ""), uiCtl(w.canc, ui.Button, "")}},
		}}}, 0, false)
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

func (w *soundWindow) buildProfile(base int32, title string) (profileCtl, ui.Node) {
	h, l := w.hwnd, w.lang
	edit := func(id int32, cx int32, num bool) (uintptr, *ui.Ctl) {
		st := uint32(winapi.WS_BORDER | winapi.WS_TABSTOP | winapi.ES_AUTOHSCROLL)
		if num {
			st |= winapi.ES_NUMBER
		}
		e := child(h, "EDIT", "", st, id, 0, 0, 0, 0)
		return e, &ui.Ctl{H: e, Kind: ui.Edit, W: cx}
	}
	label := func(t string) *ui.Ctl {
		return uiCtl(child(h, "STATIC", t, winapi.SS_LEFT, -1, 0, 0, 0, 0), ui.Label, t)
	}
	c := profileCtl{base: base}
	c.head = child(h, "STATIC", title, winapi.SS_LEFT, -1, 0, 0, 0, 0)
	c.beep = child(h, "BUTTON", i18n.T(i18n.KeySoundBeep, l), winapi.BS_AUTORADIOBUTTON|winapi.WS_TABSTOP|wsGroup, base+idSndBeep, 0, 0, 0, 0)
	c.file = child(h, "BUTTON", i18n.T(i18n.KeySoundFile, l), winapi.BS_AUTORADIOBUTTON, base+idSndFile, 0, 0, 0, 0)
	freqL := label(i18n.T(i18n.KeySoundFreq, l))
	var freq, dur, path, vol, gap *ui.Ctl
	c.freq, freq = edit(base+idSndFreq, 70, true)
	durL := label(i18n.T(i18n.KeySoundDur, l))
	c.dur, dur = edit(base+idSndDur, 70, true)
	c.path, path = edit(base+idSndPath, 430, false)
	path.Grow = true
	c.browse = child(h, "BUTTON", i18n.T(i18n.KeySoundBrowse, l), winapi.WS_TABSTOP, base+idSndBrowse, 0, 0, 0, 0)
	volL := label(i18n.T(i18n.KeySoundVolume, l))
	c.vol, vol = edit(base+idSndVol, 50, true)
	c.loop = child(h, "BUTTON", i18n.T(i18n.KeySoundLoop, l), winapi.BS_AUTOCHECKBOX|winapi.WS_TABSTOP, base+idSndLoop, 0, 0, 0, 0)
	gapL := label(i18n.T(i18n.KeySoundGap, l))
	c.gap, gap = edit(base+idSndGap, 70, true)
	c.test = child(h, "BUTTON", i18n.T(i18n.KeySoundTest, l), winapi.WS_TABSTOP, base+idSndTest, 0, 0, 0, 0)
	node := &ui.Col{Gap: ui.P8, Items: []ui.Node{
		uiCtl(c.head, ui.Label, title),
		&ui.Pad{Left: ui.P16, Child: &ui.Col{Gap: ui.P8, Items: []ui.Node{
			&ui.Row{Gap: ui.P16, Items: []ui.Node{uiCtl(c.beep, ui.Check, ""), uiCtl(c.file, ui.Check, "")}},
			&ui.Grid{Rows: []ui.GridRow{
				{Label: freqL, Field: &ui.Row{Items: []ui.Node{freq, &ui.Space{W: ui.P8}, durL, dur}}},
				{Label: volL, Field: &ui.Row{Items: []ui.Node{vol, &ui.Space{W: ui.P8}, uiCtl(c.loop, ui.Check, "")}}},
				{Label: gapL, Field: gap},
			}},
			&ui.Row{Grow: true, Items: []ui.Node{path, uiCtl(c.browse, ui.Button, "")}},
			&ui.Row{Right: true, Items: []ui.Node{uiCtl(c.test, ui.Button, "")}},
		}}},
	}}
	for ctl, k := range map[uintptr]string{c.beep: i18n.KeyTipSoundKind, c.file: i18n.KeyTipSoundKind,
		c.freq: i18n.KeyTipSoundFreq, c.dur: i18n.KeyTipSoundDur, c.path: i18n.KeyTipSoundFile,
		c.browse: i18n.KeyTipSoundBrowse, c.vol: i18n.KeyTipSoundVolume, c.loop: i18n.KeyTipSoundLoop,
		c.gap: i18n.KeyTipSoundGap, c.test: i18n.KeyTipSoundTest} {
		w.tips.set(ctl, i18n.T(k, l))
	}
	return c, node
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
		destroyWindow(w.hwnd)
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
	const W = 260
	v := &volumeWindow{}
	hwnd, err := createWindow(volumeClass, i18n.T(i18n.KeyVolumeTitle, a.opt.Lang), winapi.WS_EX_TOPMOST|winapi.WS_EX_TOOLWINDOW,
		winapi.WS_POPUP|winapi.WS_BORDER|winapi.WS_CAPTION|winapi.WS_SYSMENU, 0, 0, W, 74, a.hwnd,
		func(hw uintptr, msg uint32, wp, lp uintptr) (uintptr, bool) {
			switch msg {
			case winapi.WM_HSCROLL:
				a.volumeMoved()
				if volumeScrollEnded(wp) {
					a.volumePreviewCue()
				}
				return 0, true
			case winapi.WM_ACTIVATE:
				if wp&0xFFFF == 0 && !a.opt.HiddenPanel { // WA_INACTIVE
					winapi.PostMessage(hw, winapi.WM_CLOSE, 0, 0)
				}
			case winapi.WM_CLOSE:
				destroyWindow(hw)
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
	v.bar = child(hwnd, "msctls_trackbar32", "", winapi.WS_TABSTOP|winapi.TBS_AUTOTICKS, idVolBar, 0, 0, 0, 0)
	v.label = child(hwnd, "STATIC", "", winapi.SS_LEFT, -1, 0, 0, 0, 0)
	// phase 11 (CHK-TRAY-45): trackbar | «100 %», laid out by the framework
	f := newForm(&ui.Pad{All: ui.P8, Child: &ui.Row{Align: ui.Center, Gap: ui.P8, Items: []ui.Node{
		&ui.Ctl{H: v.bar, Kind: ui.Box, W: W - 70, Hgt: 30},
		uiCtl(v.label, ui.Label, "100 %"),
	}}}, 0)
	winapi.SendMessage(v.bar, winapi.TBM_SETRANGE, 1, 100<<16)
	winapi.SendMessage(v.bar, winapi.TBM_SETTICFREQ, 10, 0)
	winapi.SendMessage(v.bar, winapi.TBM_SETPAGESIZE, 0, 10)
	winapi.SendMessage(v.bar, winapi.TBM_SETPOS, 1, uintptr(a.soundPrefs.Master))
	winapi.SetWindowText(v.label, strconv.Itoa(a.soundPrefs.Master)+" %")
	v.tips = newTooltips(hwnd)
	v.tips.set(v.bar, i18n.T(i18n.KeyTipVolume, a.opt.Lang))
	a.volWin = v
	ow, oh := outerSize(f.W, f.H, winapi.WS_POPUP|winapi.WS_BORDER|winapi.WS_CAPTION|winapi.WS_SYSMENU, winapi.WS_EX_TOPMOST|winapi.WS_EX_TOOLWINDOW)
	x, y := a.panelPos(ow, oh)
	winapi.SetWindowPos(hwnd, winapi.HWND_TOPMOST, x, y, ow, oh, 0)
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

// volumeScrollEnded reports the trackbar thumb was released after a drag
// (SB_ENDSCROLL / TB_ENDTRACK; both are 8).
func volumeScrollEnded(wp uintptr) bool {
	code := uint16(wp & 0xFFFF)
	return code == winapi.SB_ENDSCROLL || code == winapi.TB_ENDTRACK
}

// volumePreviewFreqHz / volumePreviewDurMs: short Windows-mixer-like tick
// played at the current master volume after the user releases the slider.
const (
	volumePreviewFreqHz = 1000
	volumePreviewDurMs  = 60
)

// volumePreviewCue plays one short beep/tone at the current master volume
// via the existing «Прослушать» path (PlaySound sine WAV / Beep fallback).
func (a *App) volumePreviewCue() {
	a.testSound(trayalert.SoundProfile{
		Kind:   trayalert.SoundBeep,
		FreqHz: volumePreviewFreqHz,
		DurMs:  volumePreviewDurMs,
		Volume: 100,
	}, a.soundPrefs.Master)
}
