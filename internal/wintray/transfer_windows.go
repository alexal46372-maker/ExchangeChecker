//go:build windows

package wintray

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayalert"
	"github.com/alexal46372-maker/ExchangeChecker/internal/traysettings"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// Phase 10 E (CHK-TRAY-44): «Экспорт…» / «Импорт…» of the administrator
// settings window. Export: export_config + the tray block of this user
// (HKCU). Import: the file, import_config dryRun with a preview (merge /
// replace, «Также звук и вид трея»), then the real import with the
// revision of the dry run; the profiles of needPassword are opened one by
// one for the password. The tray block is written to HKCU and takes
// effect when the tray starts again.

// ImportClass is the window class of the import dialog.
const ImportClass = "ExchangeCheckerTrayImport"

const (
	idImpFile int32 = 100 + iota
	idImpMerge
	idImpReplace
	idImpTray
	idImpPreview
	idImpDo
	idImpCancel = winapi.IDCANCEL
)

const impCW, impCH = 600, 420

// trayStores are the per-user stores of the tray block (nil = not
// stored; export then writes the defaults).
type trayStores struct {
	Lang    LangStore
	Balloon BalloonStore
	Sound   SoundStore
}

func (w *settingsWindow) transferAvailable() bool {
	s := w.cfg.session
	return w.cfg.mode == traysettings.Admin && s.Supported(model.CmdExportConfig) && s.Supported(model.CmdImportConfig)
}

// fileDialog: the save / open dialog (tests replace cfg.fileDialog).
func (w *settingsWindow) fileDialog(save bool) (string, bool) {
	if w.cfg.fileDialog != nil {
		return w.cfg.fileDialog(w.hwnd, save)
	}
	filter := w.t(i18n.KeyXferFilter) + "\x00*.json"
	if save {
		return winapi.GetSaveFileName(w.hwnd, w.t(i18n.KeyXferExport), filter, "json", "ExchangeChecker-settings.json")
	}
	return winapi.GetOpenFileName(w.hwnd, w.t(i18n.KeyXferImport), filter, "")
}

func (w *settingsWindow) notice(text string) {
	if w.cfg.notice != nil {
		w.cfg.notice(text)
		return
	}
	winapi.MessageBox(w.hwnd, text, w.t(i18n.KeyAppName), winapi.MB_OK|winapi.MB_ICONWARNING)
}

// trayBlock reads the tray block of this user.
func (w *settingsWindow) trayBlock() traysettings.TrayBlock {
	st := w.cfg.stores
	t := traysettings.TrayBlock{Balloons: true, UIMode: w.uiMode}
	if st.Lang != nil {
		t.Language, _ = st.Lang.Load()
	}
	if st.Balloon != nil {
		if on, err := st.Balloon.Load(); err == nil {
			t.Balloons = on
		}
	}
	snd := trayalert.DefaultSoundPrefs(nil)
	if st.Sound != nil {
		if raw, err := st.Sound.Load(); err == nil {
			snd, _ = trayalert.ParseSoundPrefs(raw, snd)
		}
	}
	t.Sound = &snd
	if t.UIMode == "" {
		t.UIMode = traysettings.UISimple
	}
	return t
}

// saveTray writes an imported tray block to HKCU.
func (w *settingsWindow) saveTray(t *traysettings.TrayBlock) error {
	st := w.cfg.stores
	var first error
	keep := func(err error) {
		if err != nil && first == nil {
			first = err
		}
	}
	if st.Lang != nil {
		keep(st.Lang.Save(t.Language))
	}
	if st.Balloon != nil {
		keep(st.Balloon.Save(t.Balloons))
	}
	if st.Sound != nil && t.Sound != nil {
		keep(st.Sound.Save(t.Sound.String())) // paths as they are
	}
	if w.cfg.uiStore != nil {
		keep(w.cfg.uiStore.Save(t.UIMode))
	}
	return first
}

func (w *settingsWindow) exportConfig() {
	if w.busy || !w.transferAvailable() {
		return
	}
	path, ok := w.fileDialog(true)
	if !ok {
		return
	}
	w.status = w.t(i18n.KeySetLoading)
	w.call(model.IPCRequest{Cmd: model.CmdExportConfig}, func(m model.IPCMessage, err error) {
		switch {
		case err != nil:
			w.problem(w.ed.ErrorProblem(err, traysettings.CallTimeout))
		case m.Type == model.MsgError:
			w.problem(w.ed.Reply(m))
		case m.Export == nil:
			w.problem(&traysettings.Problem{Text: fmt.Sprintf(w.t(i18n.KeySetFailed), "export_config: "+m.Type)})
		default:
			var tray *traysettings.TrayBlock
			if !w.cfg.otherUser {
				t := w.trayBlock()
				tray = &t
			}
			b, err := traysettings.ExportFile(*m.Export, tray)
			if err == nil {
				err = os.WriteFile(path, b, 0o600)
			}
			if err != nil {
				w.problem(&traysettings.Problem{Text: fmt.Sprintf(w.t(i18n.KeyXferWriteFailed), err.Error())})
				return
			}
			st := fmt.Sprintf(w.t(i18n.KeyXferExported), path)
			if w.cfg.otherUser {
				st += " " + w.t(i18n.KeyXferOtherUser)
			}
			w.setStatus(st)
			if m.Export.ContainsLogins || tray != nil {
				w.notice(w.t(i18n.KeyXferContainsLogins))
			}
		}
	})
}

func (w *settingsWindow) importConfig() {
	if w.busy || w.imp != nil || !w.transferAvailable() || !w.discardOK() {
		return
	}
	path, ok := w.fileDialog(false)
	if !ok {
		return
	}
	var f *traysettings.ImportFile
	fi, err := os.Stat(path)
	if err == nil && fi.Size() > traysettings.TransferFileMax {
		err = fmt.Errorf(w.t(i18n.KeyXferTooLarge), traysettings.TransferFileMax/1024)
	} else if err == nil {
		var b []byte
		if b, err = os.ReadFile(path); err == nil {
			f, err = traysettings.ParseImport(b, w.cfg.lang)
		} else {
			err = fmt.Errorf(w.t(i18n.KeyXferReadFailed), err.Error())
		}
	} else {
		err = fmt.Errorf(w.t(i18n.KeyXferReadFailed), err.Error())
	}
	if err != nil {
		w.setStatus(err.Error())
		return
	}
	w.openImport(path, f)
}

// importDialog is the preview of an import.
type importDialog struct {
	w       *settingsWindow
	hwnd    uintptr
	ctl     map[int32]uintptr
	file    *traysettings.ImportFile
	res     *model.ImportResult
	rev     string // revision of the dry run
	running bool
	seq     int
	closed  bool
	tips    *tooltips
	preview string // test hook
	// head: the first line of the next previews (config_conflict)
	head string
}

func (w *settingsWindow) openImport(path string, f *traysettings.ImportFile) {
	d := &importDialog{w: w, ctl: map[int32]uintptr{}, file: f}
	style := uint32(winapi.WS_POPUP | winapi.WS_CAPTION | winapi.WS_SYSMENU)
	if !w.cfg.hidden {
		style |= winapi.WS_VISIBLE
	}
	hwnd, err := createWindow(ImportClass, w.t(i18n.KeyXferTitle), 0, style, 0, 0, impCW, impCH, w.hwnd, d.wndProc)
	if err != nil {
		w.setStatus(fmt.Sprintf(w.t(i18n.KeySetFailed), err.Error()))
		return
	}
	d.hwnd = hwnd
	w.imp = d
	m := int32(gridMargin)
	cw := int32(impCW - 2*gridMargin)
	add := func(class, text string, style uint32, id, x, y, wd, h int32) uintptr {
		c := child(hwnd, class, text, style, id, x, y, wd, h)
		d.ctl[id] = c
		return c
	}
	add("STATIC", filepath.Base(path), winapi.SS_LEFT, idImpFile, m, m, cw, gridLabelH)
	y := m + gridLabelH + gridGap
	add("BUTTON", w.t(i18n.KeyXferMerge), winapi.WS_TABSTOP|winapi.BS_AUTORADIOBUTTON, idImpMerge, m, y, cw, gridCheckH)
	y += gridCheckH + 2
	add("BUTTON", w.t(i18n.KeyXferReplace), winapi.WS_TABSTOP|winapi.BS_AUTORADIOBUTTON, idImpReplace, m, y, cw, gridCheckH)
	y += gridCheckH + gridGap
	add("BUTTON", w.t(i18n.KeyXferTrayCheck), winapi.WS_TABSTOP|winapi.BS_AUTOCHECKBOX, idImpTray, m, y, cw, gridCheckH)
	y += gridCheckH + gridGap
	by := int32(impCH - gridMargin - gridButtonH)
	pv := add("EDIT", "", winapi.WS_TABSTOP|winapi.WS_BORDER|winapi.WS_VSCROLL|winapi.ES_MULTILINE|winapi.ES_READONLY|winapi.ES_AUTOVSCROLL,
		idImpPreview, m, y, cw, by-gridGap-y)
	_ = pv
	add("BUTTON", w.t(i18n.KeyXferDo), winapi.WS_TABSTOP|winapi.BS_DEFPUSHBUTTON, idImpDo, impCW-gridMargin-2*120-gridGap, by, 120, gridButtonH)
	add("BUTTON", w.t(i18n.KeyStClose), winapi.WS_TABSTOP, idImpCancel, impCW-gridMargin-120, by, 120, gridButtonH)
	winapi.SendMessage(d.ctl[idImpMerge], winapi.BM_SETCHECK, winapi.BST_CHECKED, 0)
	if f.Tray != nil && !w.cfg.otherUser {
		winapi.SendMessage(d.ctl[idImpTray], winapi.BM_SETCHECK, winapi.BST_CHECKED, 0)
	} else {
		winapi.EnableWindow(d.ctl[idImpTray], false)
	}
	d.tips = newTooltips(hwnd)
	for id, k := range map[int32]string{idImpMerge: i18n.KeyTipXferMerge, idImpReplace: i18n.KeyTipXferReplace, idImpTray: i18n.KeyTipXferTray,
		idImpPreview: i18n.KeyTipXferPreview, idImpDo: i18n.KeyTipXferDo, idImpCancel: i18n.KeyTipClose} {
		d.tips.set(d.ctl[id], w.t(k))
	}
	winapi.EnableWindow(w.hwnd, false)
	d.dryRun()
}

func (d *importDialog) checked(id int32) bool {
	return winapi.SendMessage(d.ctl[id], winapi.BM_GETCHECK, 0, 0) == winapi.BST_CHECKED
}

func (d *importDialog) mode() string {
	if d.checked(idImpReplace) {
		return model.ImportReplace
	}
	return model.ImportMerge
}

func (d *importDialog) applyTray() bool {
	return d.file.Tray != nil && !d.w.cfg.otherUser && d.checked(idImpTray)
}

func (d *importDialog) setPreview(s string) {
	d.preview = s
	winapi.SetWindowText(d.ctl[idImpPreview], s)
}

func (d *importDialog) showPreview() {
	if d.res == nil {
		return
	}
	p := d.head + traysettings.Preview(*d.res, d.w.ed.Names(d.file), d.file.Tray, d.applyTray(), fileExists, d.w.cfg.lang)
	if d.w.cfg.otherUser {
		p += "\r\n" + d.w.t(i18n.KeyXferOtherUser)
	}
	d.setPreview(p)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func (d *importDialog) enable() {
	winapi.EnableWindow(d.ctl[idImpDo], !d.running && d.res != nil)
	winapi.EnableWindow(d.ctl[idImpMerge], !d.running)
	winapi.EnableWindow(d.ctl[idImpReplace], !d.running)
}

// run sends an import_config of the dialog; stale replies are dropped.
func (d *importDialog) run(dry bool, done func(model.IPCMessage, error)) {
	d.seq++
	seq := d.seq
	d.running = true
	d.enable()
	req := traysettings.ImportRequest(d.file, d.mode(), dry, d.rev)
	d.w.call(req, func(m model.IPCMessage, err error) {
		if d.closed || seq != d.seq {
			return
		}
		d.running = false
		done(m, err)
		if !d.closed {
			d.enable()
		}
	})
}

func (d *importDialog) problemText(m model.IPCMessage, err error) (*traysettings.Problem, bool) {
	switch {
	case err != nil:
		return d.w.ed.ErrorProblem(err, traysettings.CallTimeout), true
	case m.Type == model.MsgError:
		return d.w.ed.Reply(m), true
	case m.Import == nil:
		return &traysettings.Problem{Text: fmt.Sprintf(d.w.t(i18n.KeySetFailed), "import_config: "+m.Type)}, true
	}
	return nil, false
}

func (d *importDialog) dryRun() {
	d.res = nil
	d.setPreview(d.w.t(i18n.KeyXferChecking))
	d.run(true, func(m model.IPCMessage, err error) {
		if pr, bad := d.problemText(m, err); bad {
			d.setPreview(pr.Text)
			return
		}
		r := *m.Import
		d.res = &r
		d.rev = m.Revision
		if d.rev == "" {
			d.rev = r.Revision
		}
		d.showPreview()
	})
}

func (d *importDialog) doImport() {
	if d.running || d.res == nil {
		return
	}
	tray := d.applyTray()
	d.setPreview(d.w.t(i18n.KeyXferImporting))
	d.run(false, func(m model.IPCMessage, err error) {
		if pr, bad := d.problemText(m, err); bad {
			if pr.Reload { // config_conflict: the dry run again on the new configuration
				d.head = d.w.t(i18n.KeyXferConflict) + "\r\n"
				d.dryRun()
				return
			}
			d.res = nil
			d.setPreview(pr.Text)
			return
		}
		w := d.w
		status := w.t(i18n.KeyXferDone)
		if tray {
			if err := w.saveTray(d.file.Tray); err != nil {
				status += " " + fmt.Sprintf(w.t(i18n.KeyXferTrayFailed), err.Error())
			} else {
				status += " " + w.t(i18n.KeyXferTrayApplied)
			}
		}
		w.needPw = append([]string(nil), m.Import.NeedPassword...)
		d.close()
		w.afterImport = status
		w.ed.Revision = ""
		w.reload(false)
	})
}

func (d *importDialog) close() {
	if d.closed {
		return
	}
	d.closed = true
	w := d.w
	winapi.EnableWindow(w.hwnd, true)
	destroyWindow(d.hwnd)
	if w.imp == d {
		w.imp = nil
	}
}

func (d *importDialog) wndProc(hwnd uintptr, msg uint32, wp, lp uintptr) (uintptr, bool) {
	switch msg {
	case winapi.WM_COMMAND:
		id, code := int32(wp&0xFFFF), uint32(wp>>16&0xFFFF)
		switch id {
		case idImpMerge, idImpReplace:
			if code == winapi.BN_CLICKED && !d.running {
				d.head = ""
				d.dryRun()
			}
		case idImpTray:
			if code == winapi.BN_CLICKED {
				d.showPreview()
			}
		case idImpDo:
			d.doImport()
		case idImpCancel:
			d.close()
		}
		return 0, true
	case winapi.WM_CLOSE:
		d.close()
		return 0, true
	}
	if isTrayExit(msg) {
		d.w.exitNow()
		return 0, true
	}
	return 0, false
}

// nextNeedPassword opens the next profile of the import that needs its
// password again.
func (w *settingsWindow) nextNeedPassword() {
	for len(w.needPw) > 0 {
		id := w.needPw[0]
		for i, p := range w.ed.Profiles {
			if p.ID == id {
				w.ed.Select(i)
				w.fill()
				w.focus(traysettings.FieldPassword)
				w.setStatus(fmt.Sprintf(w.t(i18n.KeyXferEnterPw), p.DisplayName(), len(w.needPw)))
				return
			}
		}
		w.needPw = w.needPw[1:]
	}
}

// savedNeedPassword: the profile id was saved; the next one follows.
func (w *settingsWindow) savedNeedPassword(id string) {
	if len(w.needPw) > 0 && w.needPw[0] == id {
		w.needPw = w.needPw[1:]
		w.nextNeedPassword()
	}
}

// OwnerSIDArg passes the user SID of the tray to the elevated settings
// process (phase 10 E: elevation as another user, over the shoulder).
const OwnerSIDArg = "--owner-sid="

// CurrentUserSID is the user SID of this process token ("" on error).
func CurrentUserSID() string {
	tok, err := syscall.OpenCurrentProcessToken()
	if err != nil {
		return ""
	}
	defer tok.Close()
	u, err := tok.GetTokenUser()
	if err != nil {
		return ""
	}
	s, err := u.User.Sid.String()
	if err != nil {
		return ""
	}
	return s
}

// OwnerSIDFromArgs reads --owner-sid= of the command line.
func OwnerSIDFromArgs(args []string) string {
	for _, a := range args {
		if strings.HasPrefix(a, OwnerSIDArg) {
			return strings.TrimPrefix(a, OwnerSIDArg)
		}
	}
	return ""
}

// OtherUser: the tray owner is known and is not this user.
func OtherUser(owner, current string) bool {
	return owner != "" && current != "" && !strings.EqualFold(owner, current)
}
