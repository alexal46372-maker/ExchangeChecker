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

// buildSoundSection embeds the «Звук тревог» profiles one-to-one into the
// General tab as a «Звук» group (CHK-TRAY-62). Save persists HKCU Sound;
// «Прослушать» uses the tray App preview when available.
func (w *settingsWindow) buildSoundSection(
	add func(class, text string, ex, style uint32, id int32, kind ui.Kind, wd, hg int32) *ui.Ctl,
	label func(text string, id int32) *ui.Ctl,
) ui.Node {
	lang := w.cfg.lang
	w.sndPick = func(cur string) (string, bool) {
		f := i18n.T(i18n.KeySoundFilterAudio, lang) + "\x00*.wav;*.mp3;*.wma;*.mid;*.midi;*.rmi\x00" +
			i18n.T(i18n.KeySoundFilterAll, lang) + "\x00*.*"
		return winapi.GetOpenFileName(w.hwnd, i18n.T(i18n.KeySoundTitle, lang), f, cur)
	}
	var warnN, errN ui.Node
	w.sndWarn, warnN = w.buildEmbeddedProfile(add, label, idSndWarn, i18n.T(i18n.KeySoundWarning, lang))
	w.sndErr, errN = w.buildEmbeddedProfile(add, label, idSndErr, i18n.T(i18n.KeySoundError, lang))
	info := add("STATIC", "", 0, winapi.SS_LEFT, idSndInfo, ui.Box, 540, 36)
	info.Grow = true
	w.sndInfo = info.H
	save := add("BUTTON", i18n.T(i18n.KeySoundSave, lang), 0, winapi.BS_PUSHBUTTON|winapi.WS_TABSTOP, idSndSave, ui.Button, 0, 0)
	w.sndSave = save.H
	if w.tips != nil {
		w.tips.set(w.sndSave, i18n.T(i18n.KeyTipSoundSave, lang))
	}
	gBox := add("BUTTON", i18n.T(i18n.KeySetGroupSound, lang), 0, winapi.BS_GROUPBOX, 0, ui.Group, 0, 0)
	w.groups[i18n.KeySetGroupSound] = gBox.H
	return &ui.GroupBox{Box: gBox, Child: &ui.Col{Gap: ui.P8, Items: []ui.Node{
		warnN, errN,
		&ui.Col{Gap: ui.P8, Items: []ui.Node{
			info,
			&ui.Row{Right: true, Items: []ui.Node{save}},
		}},
	}}}
}

func (w *settingsWindow) buildEmbeddedProfile(
	add func(class, text string, ex, style uint32, id int32, kind ui.Kind, wd, hg int32) *ui.Ctl,
	label func(text string, id int32) *ui.Ctl,
	base int32, title string,
) (profileCtl, ui.Node) {
	lang := w.cfg.lang
	edit := func(id int32, cx int32, num bool) *ui.Ctl {
		st := uint32(winapi.WS_BORDER | winapi.WS_TABSTOP | winapi.ES_AUTOHSCROLL)
		if num {
			st |= winapi.ES_NUMBER
		}
		return add("EDIT", "", winapi.WS_EX_CLIENTEDGE, st, id, ui.Edit, cx, 0)
	}
	c := profileCtl{base: base}
	head := add("STATIC", title, 0, winapi.SS_LEFT, 0, ui.Label, 0, 0)
	c.head = head.H
	beep := add("BUTTON", i18n.T(i18n.KeySoundBeep, lang), 0, winapi.BS_AUTORADIOBUTTON|winapi.WS_TABSTOP|wsGroup, base+idSndBeep, ui.Check, 0, 0)
	file := add("BUTTON", i18n.T(i18n.KeySoundFile, lang), 0, winapi.BS_AUTORADIOBUTTON, base+idSndFile, ui.Check, 0, 0)
	c.beep, c.file = beep.H, file.H
	freqL := label(i18n.T(i18n.KeySoundFreq, lang), 0)
	freq := edit(base+idSndFreq, 70, true)
	c.freq = freq.H
	durL := label(i18n.T(i18n.KeySoundDur, lang), 0)
	dur := edit(base+idSndDur, 70, true)
	c.dur = dur.H
	path := edit(base+idSndPath, 430, false)
	path.Grow = true
	c.path = path.H
	browse := add("BUTTON", i18n.T(i18n.KeySoundBrowse, lang), 0, winapi.WS_TABSTOP, base+idSndBrowse, ui.Button, 0, 0)
	c.browse = browse.H
	volL := label(i18n.T(i18n.KeySoundVolume, lang), 0)
	vol := edit(base+idSndVol, 50, true)
	c.vol = vol.H
	loop := add("BUTTON", i18n.T(i18n.KeySoundLoop, lang), 0, winapi.BS_AUTOCHECKBOX|winapi.WS_TABSTOP, base+idSndLoop, ui.Check, 0, 0)
	c.loop = loop.H
	gapL := label(i18n.T(i18n.KeySoundGap, lang), 0)
	gap := edit(base+idSndGap, 70, true)
	c.gap = gap.H
	test := add("BUTTON", i18n.T(i18n.KeySoundTest, lang), 0, winapi.WS_TABSTOP, base+idSndTest, ui.Button, 0, 0)
	c.test = test.H
	node := &ui.Col{Gap: ui.P8, Items: []ui.Node{
		uiCtl(c.head, ui.Label, title),
		&ui.Pad{Left: ui.P16, Child: &ui.Col{Gap: ui.P8, Items: []ui.Node{
			&ui.Row{Gap: ui.P16, Items: []ui.Node{beep, file}},
			&ui.Grid{Rows: []ui.GridRow{
				{Label: freqL, Field: &ui.Row{Items: []ui.Node{freq, &ui.Space{W: ui.P8}, durL, dur}}},
				{Label: volL, Field: &ui.Row{Items: []ui.Node{vol, &ui.Space{W: ui.P8}, loop}}},
				{Label: gapL, Field: gap},
			}},
			&ui.Row{Grow: true, Items: []ui.Node{path, browse}},
			&ui.Row{Right: true, Items: []ui.Node{test}},
		}}},
	}}
	if w.tips != nil {
		for ctl, k := range map[uintptr]string{c.beep: i18n.KeyTipSoundKind, c.file: i18n.KeyTipSoundKind,
			c.freq: i18n.KeyTipSoundFreq, c.dur: i18n.KeyTipSoundDur, c.path: i18n.KeyTipSoundFile,
			c.browse: i18n.KeyTipSoundBrowse, c.vol: i18n.KeyTipSoundVolume, c.loop: i18n.KeyTipSoundLoop,
			c.gap: i18n.KeyTipSoundGap, c.test: i18n.KeyTipSoundTest} {
			w.tips.set(ctl, i18n.T(k, lang))
		}
	}
	return c, node
}

// loadSoundSection fills the embedded profiles from the App or HKCU store.
func (w *settingsWindow) loadSoundSection() {
	if w.sndWarn.freq == 0 {
		return
	}
	p := trayalert.DefaultSoundPrefs(nil)
	if w.cfg.soundPrefs != nil {
		p = w.cfg.soundPrefs()
	} else if st := w.cfg.stores.Sound; st != nil {
		if raw, err := st.Load(); err == nil {
			p, _ = trayalert.ParseSoundPrefs(raw, p)
		}
	}
	w.sndMaster = p.Master
	w.fillSoundProfiles(p)
	if w.sndInfo != 0 {
		winapi.SetWindowText(w.sndInfo, "")
	}
}

func (w *settingsWindow) fillSoundProfiles(p trayalert.SoundPrefs) {
	for _, x := range []struct {
		c  profileCtl
		pr trayalert.SoundProfile
	}{{w.sndWarn, p.Warning}, {w.sndErr, p.Error}} {
		if x.c.freq == 0 {
			continue
		}
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

func (w *settingsWindow) readSoundProfile(c profileCtl) (trayalert.SoundProfile, string) {
	num := func(h uintptr, lo, hi int, name string) (int, string) {
		v, err := strconv.Atoi(strings.TrimSpace(winapi.GetWindowText(h)))
		if err != nil || v < lo || v > hi {
			return 0, fmt.Sprintf(i18n.T(i18n.KeySoundBadValue, w.cfg.lang), strings.TrimSuffix(name, ":"))
		}
		return v, ""
	}
	var p trayalert.SoundProfile
	var bad string
	p.Kind = trayalert.SoundBeep
	if isChecked(c.file) {
		p.Kind = trayalert.SoundFile
	}
	if p.FreqHz, bad = num(c.freq, 37, 32767, i18n.T(i18n.KeySoundFreq, w.cfg.lang)); bad != "" {
		return p, bad
	}
	if p.DurMs, bad = num(c.dur, 1, 10000, i18n.T(i18n.KeySoundDur, w.cfg.lang)); bad != "" {
		return p, bad
	}
	if p.Volume, bad = num(c.vol, 0, 100, i18n.T(i18n.KeySoundVolume, w.cfg.lang)); bad != "" {
		return p, bad
	}
	if p.GapMs, bad = num(c.gap, 0, 60000, i18n.T(i18n.KeySoundGap, w.cfg.lang)); bad != "" {
		return p, bad
	}
	p.Loop = isChecked(c.loop)
	p.File = strings.TrimSpace(winapi.GetWindowText(c.path))
	if p.Kind == trayalert.SoundFile && p.File == "" {
		return p, fmt.Sprintf(i18n.T(i18n.KeySoundBadFile, w.cfg.lang), "—")
	}
	return p, ""
}

func (w *settingsWindow) soundValues() (trayalert.SoundPrefs, string) {
	p := trayalert.DefaultSoundPrefs(nil)
	if w.cfg.soundPrefs != nil {
		p = w.cfg.soundPrefs()
	}
	p.Master = w.sndMaster
	var bad string
	if p.Warning, bad = w.readSoundProfile(w.sndWarn); bad != "" {
		return p, i18n.T(i18n.KeySoundWarning, w.cfg.lang) + ": " + bad
	}
	if p.Error, bad = w.readSoundProfile(w.sndErr); bad != "" {
		return p, i18n.T(i18n.KeySoundError, w.cfg.lang) + ": " + bad
	}
	return p, ""
}

// soundCommand handles the embedded sound section; true = consumed.
func (w *settingsWindow) soundCommand(id int32) bool {
	if w.sndWarn.freq == 0 {
		return false
	}
	if id == idSndSave {
		p, bad := w.soundValues()
		if bad != "" {
			winapi.SetWindowText(w.sndInfo, bad)
			return true
		}
		var err error
		if w.cfg.onSoundSave != nil {
			err = w.cfg.onSoundSave(p)
		} else if w.cfg.stores.Sound != nil {
			err = w.cfg.stores.Sound.Save(p.Normalized(trayalert.DefaultSoundPrefs(nil)).String())
		}
		if err != nil {
			winapi.SetWindowText(w.sndInfo, err.Error())
			return true
		}
		winapi.SetWindowText(w.sndInfo, i18n.T(i18n.KeySoundSaved, w.cfg.lang))
		return true
	}
	for _, c := range []profileCtl{w.sndWarn, w.sndErr} {
		switch id - c.base {
		case idSndBrowse:
			pick := w.sndPick
			if pick == nil {
				return true
			}
			if f, ok := pick(winapi.GetWindowText(c.path)); ok {
				winapi.SetWindowText(c.path, f)
				setCheck(c.beep, false)
				setCheck(c.file, true)
			}
			return true
		case idSndTest:
			pr, bad := w.readSoundProfile(c)
			winapi.SetWindowText(w.sndInfo, bad)
			if bad == "" {
				w.previewSound(pr)
			}
			return true
		case idSndBeep, idSndFile, idSndLoop:
			return true // radio/check handled by Windows
		}
	}
	return false
}

func (w *settingsWindow) previewSound(p trayalert.SoundProfile) {
	if w.cfg.onSoundTest != nil {
		w.cfg.onSoundTest(p, w.sndMaster)
		return
	}
	if w.sndPreviewer == nil {
		w.sndPreviewer = &winSounder{alias: aliasTest, lang: func() string { return w.cfg.lang }}
	}
	if w.sndPreview == nil {
		w.sndPreview = trayalert.NewSoundPlayer(w.sndPreviewer)
	}
	w.sndPreview.Set(false, trayalert.Sound{})
	w.sndPreview.Set(true, trayalert.Sound{Profile: p, Volume: trayalert.EffectiveVolume(p.Volume, w.sndMaster)})
}

func (w *settingsWindow) stopSoundPreview() {
	if w.sndPreview != nil {
		w.sndPreview.Set(false, trayalert.Sound{})
	}
	if w.cfg.onSoundTest == nil {
		return
	}
	// App.testSound path: stop via empty — App.stopTestSound is not
	// exposed; preview is fire-and-forget for the section.
}

// applySoundTips sets tooltips on the embedded sound controls (tips are
// created after the page tree in build).
func (w *settingsWindow) applySoundTips() {
	if w.tips == nil || w.sndWarn.freq == 0 {
		return
	}
	lang := w.cfg.lang
	w.tips.set(w.sndSave, i18n.T(i18n.KeyTipSoundSave, lang))
	for _, c := range []profileCtl{w.sndWarn, w.sndErr} {
		for ctl, k := range map[uintptr]string{c.beep: i18n.KeyTipSoundKind, c.file: i18n.KeyTipSoundKind,
			c.freq: i18n.KeyTipSoundFreq, c.dur: i18n.KeyTipSoundDur, c.path: i18n.KeyTipSoundFile,
			c.browse: i18n.KeyTipSoundBrowse, c.vol: i18n.KeyTipSoundVolume, c.loop: i18n.KeyTipSoundLoop,
			c.gap: i18n.KeyTipSoundGap, c.test: i18n.KeyTipSoundTest} {
			w.tips.set(ctl, i18n.T(k, lang))
		}
	}
}
