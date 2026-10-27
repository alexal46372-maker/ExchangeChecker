//go:build windows

package wintray

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayalert"
	"github.com/alexal46372-maker/ExchangeChecker/internal/traysettings"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// xferService adds export_config / import_config to the fake service.
type xferService struct {
	*cfgService
	xmu      sync.Mutex
	imports  []xferCall
	conflict bool // the next real import answers config_conflict
}

type xferCall struct {
	mode, rev string
	dry       bool
	n         int
}

func newXferService() *xferService {
	x := &xferService{cfgService: newCfgService()}
	x.cfgService.handler = x
	return x
}

func (x *xferService) ExportConfig() (model.ConfigExport, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	s := model.DefaultSettings()
	return model.ConfigExport{Format: model.ConfigFormatV1, Settings: &s, Profiles: append([]model.Profile(nil), x.profiles...), ContainsLogins: true}, nil
}

func (x *xferService) ImportConfig(cfg *model.ConfigExport, mode, rev string, dry bool, account string) (model.ImportResult, error) {
	x.xmu.Lock()
	x.imports = append(x.imports, xferCall{mode: mode, rev: rev, dry: dry, n: len(cfg.Profiles)})
	conflict := x.conflict && !dry
	x.conflict = x.conflict && dry
	x.xmu.Unlock()
	if conflict {
		return model.ImportResult{}, &model.IPCError{Code: model.ErrCodeConflict, Key: model.MsgMonitorConfigConflict}
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	res := model.ImportResult{DryRun: dry, Mode: mode, Added: []string{}, Updated: []string{}, Removed: []string{}, NeedPassword: []string{}}
	if !dry && rev != x.revision() {
		return res, &model.IPCError{Code: model.ErrCodeConflict, Key: model.MsgMonitorConfigConflict}
	}
	next := []model.Profile{}
	if mode == model.ImportMerge {
		next = append(next, x.profiles...)
	} else {
		for _, p := range x.profiles {
			found := false
			for _, q := range cfg.Profiles {
				found = found || q.ID == p.ID
			}
			if !found {
				res.Removed = append(res.Removed, p.ID)
			}
		}
	}
	for _, q := range cfg.Profiles {
		i := -1
		for k, p := range next {
			if p.ID == q.ID {
				i = k
			}
		}
		if q.AuthMode != model.AuthWindows {
			res.NeedPassword = append(res.NeedPassword, q.ID)
			q.HasPassword = false
		}
		if i >= 0 {
			next[i] = q
			res.Updated = append(res.Updated, q.ID)
		} else {
			next = append(next, q)
			res.Added = append(res.Added, q.ID)
		}
	}
	if !dry {
		x.profiles = next
		x.rev++
	}
	res.Revision = x.revision()
	return res, nil
}

func (x *xferService) calls() []xferCall {
	x.xmu.Lock()
	defer x.xmu.Unlock()
	return append([]xferCall(nil), x.imports...)
}

type memLang struct{ v string }

func (m *memLang) Load() (string, error) { return m.v, nil }
func (m *memLang) Save(s string) error   { m.v = s; return nil }

type memBalloon struct{ on bool }

func (m *memBalloon) Load() (bool, error) { return m.on, nil }
func (m *memBalloon) Save(on bool) error  { m.on = on; return nil }

type memSound struct{ raw string }

func (m *memSound) Load() (string, error) { return m.raw, nil }
func (m *memSound) Save(s string) error   { m.raw = s; return nil }

type memUI struct{ m traysettings.UIMode }

func (m *memUI) Load() (traysettings.UIMode, error) { return m.m, nil }
func (m *memUI) Save(v traysettings.UIMode) error   { m.m = v; return nil }

type xferEnv struct {
	x       *xferService
	w       *settingsWindow
	path    string
	notices []string
	lang    *memLang
	bal     *memBalloon
	snd     *memSound
	ui      *memUI
}

func newXferEnv(t *testing.T, lang string, otherUser ...bool) *xferEnv {
	t.Helper()
	e := &xferEnv{x: newXferService(), path: filepath.Join(t.TempDir(), "cfg.json"),
		lang: &memLang{v: "en"}, bal: &memBalloon{on: false}, snd: &memSound{}, ui: &memUI{m: traysettings.UIExpert}}
	app := startCfgApp(t, e.x.cfgService, true, nil)
	snd := trayalert.DefaultSoundPrefs(nil)
	snd.Master = 35
	e.snd.raw = snd.String()
	w, err := newSettingsWindow(settingsConfig{lang: lang, mode: traysettings.Admin, session: app.session, hidden: true,
		uiMode: traysettings.UIExpert, uiStore: e.ui, stores: trayStores{Lang: e.lang, Balloon: e.bal, Sound: e.snd},
		fileDialog: func(uintptr, bool) (string, bool) { return e.path, true },
		notice:     func(s string) { e.notices = append(e.notices, s) }, otherUser: len(otherUser) > 0 && otherUser[0]})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.close)
	w.Reload()
	pumpUntil(t, "loaded", func() bool { return !w.busy && w.ed.HasProfile() && w.transferAvailable() })
	e.w = w
	return e
}

// TestCHK_TRAY_44_Export — export_config + the tray block of HKCU; the
// containsLogins warning; no Viber.
func TestCHK_TRAY_44_Export(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	for _, lang := range []string{i18n.RU, i18n.EN} {
		e := newXferEnv(t, lang)
		e.w.click(t, idSetExport)
		d := e.w.exp
		if d == nil {
			t.Fatalf("%s: export dialog: %q", lang, e.w.statusText())
		}
		winapi.SendMessage(d.ctl[idExpDo], winapi.BM_CLICK, 0, 0)
		pumpUntil(t, "exported", func() bool { return e.w.exp == nil && !e.w.busy })
		b, err := os.ReadFile(e.path)
		if err != nil {
			t.Fatalf("%s: %v (status %q)", lang, err, e.w.statusText())
		}
		var f struct {
			Format string                 `json:"format"`
			Tray   traysettings.TrayBlock `json:"tray"`
		}
		if err := json.Unmarshal(b, &f); err != nil || f.Format != model.ConfigFormatV1 || f.Tray.Format != traysettings.TrayFormat ||
			f.Tray.Language != "en" || f.Tray.Balloons || f.Tray.Sound == nil || f.Tray.Sound.Master != 35 || f.Tray.UIMode != traysettings.UIExpert {
			t.Fatalf("%s: file %v %s", lang, err, b)
		}
		if strings.Contains(string(b), `"viber"`) {
			t.Errorf("%s: Viber exported", lang)
		}
		if len(e.notices) != 1 || e.notices[0] != i18n.T(i18n.KeyXferContainsLogins, lang) {
			t.Errorf("%s: containsLogins warning %q", lang, e.notices)
		}
		want := map[string]string{i18n.RU: "Файл содержит адреса серверов, логины SQL (без паролей) и пути к звуковым файлам. Храните его в надёжном месте.",
			i18n.EN: "The file contains server addresses, SQL logins (no passwords) and sound file paths. Keep it in a safe place."}[lang]
		if i18n.T(i18n.KeyXferContainsLogins, lang) != want {
			t.Errorf("%s: warning text", lang)
		}
	}
}

func writeImportFile(t *testing.T, path string, tray *traysettings.TrayBlock, profiles ...model.Profile) {
	t.Helper()
	exp := model.ConfigExport{Format: model.ConfigFormatV1, Profiles: profiles}
	var b []byte
	var err error
	if tray != nil {
		b, err = traysettings.ExportFile(exp, tray, false)
	} else {
		b, err = json.Marshal(exp)
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestCHK_TRAY_44_Import — dry run preview, merge / replace, the tray
// check box, the real import with the revision of the dry run, the tray
// block to HKCU, then the needPassword profiles one by one.
func TestCHK_TRAY_44_Import(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	e := newXferEnv(t, i18n.RU)
	snd := trayalert.DefaultSoundPrefs(nil)
	snd.Master = 70
	snd.Error = trayalert.SoundProfile{Kind: trayalert.SoundFile, File: `Z:\нет\alarm.wav`, Volume: 100}
	p2 := model.NewProfile("p2")
	p2.Name, p2.Server, p2.Database, p2.Login = "Магазин", "srv2", "db2", "mon2"
	upd := e.x.profiles[0]
	upd.Database = "db1new"
	writeImportFile(t, e.path, &traysettings.TrayBlock{Language: "ru", Balloons: true, Sound: &snd, UIMode: traysettings.UISimple}, upd, p2)
	e.w.click(t, idSetImport)
	d := e.w.imp
	if d == nil || winapi.IsWindowEnabled(e.w.hwnd) {
		t.Fatalf("import dialog not open / owner enabled: %q", e.w.statusText())
	}
	checkLayout(t, "import dialog RU", d.hwnd)
	pumpUntil(t, "dry run", func() bool { return d.res != nil })
	for _, s := range []string{"Магазин", "Склад", `Z:\нет\alarm.wav`, "70 %", "Будут добавлены (1)", "Нужно будет заново ввести пароль (2)"} {
		if !strings.Contains(d.preview, s) {
			t.Errorf("preview misses %q: %q", s, d.preview)
		}
	}
	if !d.checked(idImpTray) || !d.checked(idImpMerge) {
		t.Error("defaults: merge and «Также звук и вид трея» checked")
	}
	// the tray check box only changes the preview
	winapi.SendMessage(d.ctl[idImpTray], winapi.BM_CLICK, 0, 0)
	if !strings.Contains(d.preview, i18n.T(i18n.KeyXferTrayKept, i18n.RU)) {
		t.Errorf("tray off: %q", d.preview)
	}
	winapi.SendMessage(d.ctl[idImpTray], winapi.BM_CLICK, 0, 0)
	// replace: a new dry run
	winapi.SendMessage(d.ctl[idImpReplace], winapi.BM_CLICK, 0, 0)
	pumpUntil(t, "dry run replace", func() bool { return d.res != nil && d.res.Mode == model.ImportReplace })
	winapi.SendMessage(d.ctl[idImpMerge], winapi.BM_CLICK, 0, 0)
	pumpUntil(t, "dry run merge", func() bool { return d.res != nil && d.res.Mode == model.ImportMerge })
	if e.lang.v != "en" || e.bal.on {
		t.Fatal("tray block applied before the import")
	}
	// a conflict on the real import: the dry run again, the dialog stays
	e.x.xmu.Lock()
	e.x.conflict = true
	e.x.xmu.Unlock()
	winapi.SendMessage(d.ctl[idImpDo], winapi.BM_CLICK, 0, 0)
	pumpUntil(t, "conflict", func() bool { n := e.x.calls(); return len(n) >= 5 && !d.running && d.res != nil })
	if e.w.imp != d || e.lang.v != "en" {
		t.Fatal("conflict closed the dialog / applied the tray")
	}
	if !strings.HasPrefix(d.preview, i18n.T(i18n.KeyXferConflict, i18n.RU)+"\r\n") || !strings.Contains(d.preview, "Магазин") {
		t.Errorf("conflict line not on top of the new preview: %q", d.preview)
	}
	winapi.SendMessage(d.ctl[idImpDo], winapi.BM_CLICK, 0, 0)
	pumpUntil(t, "imported", func() bool { return e.w.imp == nil && !e.w.busy && e.w.loaded && len(e.w.ed.Profiles) == 2 })
	calls := e.x.calls()
	last := calls[len(calls)-1]
	if last.dry || last.mode != model.ImportMerge || last.rev != "r1" || last.n != 2 {
		t.Errorf("real import %+v (all %+v)", last, calls)
	}
	if st := e.w.statusText(); !strings.Contains(st, i18n.T(i18n.KeyXferTrayApplied, i18n.RU)) ||
		strings.Contains(st, "перезапуск") || strings.Contains(strings.ToLower(st), "restart") {
		t.Errorf("status after import: %q", st)
	}
	if e.lang.v != "ru" || !e.bal.on || e.ui.m != traysettings.UISimple || !strings.Contains(e.snd.raw, `Z:\\нет\\alarm.wav`) {
		t.Errorf("tray block: %q %v %q %q", e.lang.v, e.bal.on, e.ui.m, e.snd.raw)
	}
	if !winapi.IsWindowEnabled(e.w.hwnd) {
		t.Error("owner disabled after the import")
	}
	// needPassword: «Склад» first, its password field focused
	if e.w.ed.Base.ID != "p1" || e.w.lastFocus != traysettings.FieldPassword || !strings.Contains(e.w.statusText(), "«Склад»") {
		t.Fatalf("need password: %q focus %q status %q", e.w.ed.Base.ID, e.w.lastFocus, e.w.statusText())
	}
	e.w.savedNeedPassword("p1")
	if e.w.ed.Base.ID != "p2" || !strings.Contains(e.w.statusText(), "«Магазин»") {
		t.Fatalf("next need password: %q %q", e.w.ed.Base.ID, e.w.statusText())
	}
	e.w.savedNeedPassword("p2")
	if len(e.w.needPw) != 0 {
		t.Error("queue not empty")
	}
}

// TestCHK_TRAY_44_ImportErrors — bad files never reach the service; the
// tray block of a file without one is off; read-only has no buttons.
func TestCHK_TRAY_44_ImportErrors(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	e := newXferEnv(t, i18n.EN)
	for name, data := range map[string]string{"format": `{"format":"x","profiles":[]}`, "json": `nope`,
		"large": `{"format":"ec-config/1","x":"` + strings.Repeat("a", traysettings.TransferFileMax) + `"}`} {
		os.WriteFile(e.path, []byte(data), 0o600)
		e.w.click(t, idSetImport)
		if e.w.imp != nil || e.w.statusText() == "" || len(e.x.calls()) != 0 {
			t.Errorf("%s: dialog %v status %q", name, e.w.imp != nil, e.w.statusText())
		}
	}
	os.Remove(e.path)
	e.w.click(t, idSetImport)
	if e.w.imp != nil || !strings.Contains(e.w.statusText(), "Could not read") {
		t.Errorf("missing file: %q", e.w.statusText())
	}
	writeImportFile(t, e.path, nil, e.x.profiles[0])
	e.w.click(t, idSetImport)
	d := e.w.imp
	if d == nil || winapi.IsWindowEnabled(d.ctl[idImpTray]) || d.applyTray() {
		t.Fatal("no tray block: check box must be off")
	}
	pumpUntil(t, "dry run", func() bool { return d.res != nil })
	if !strings.Contains(d.preview, i18n.T(i18n.KeyXferNoTray, i18n.EN)) {
		t.Errorf("preview %q", d.preview)
	}
	winapi.SendMessage(d.ctl[idImpCancel], winapi.BM_CLICK, 0, 0)
	PumpPending()
	if e.w.imp != nil || winapi.IsWindow(d.hwnd) || !winapi.IsWindowEnabled(e.w.hwnd) {
		t.Error("cancel must close the dialog")
	}
	ro, err := newSettingsWindow(settingsConfig{lang: i18n.EN, mode: traysettings.ReadOnly, session: e.w.cfg.session, hidden: true,
		elevator: &fakeElevator{}})
	if err != nil {
		t.Fatal(err)
	}
	defer ro.close()
	if ro.ctl[idSetExport] != 0 || ro.ctl[idSetImport] != 0 {
		t.Error("export / import in the read-only window")
	}
	_ = errors.New
}

// TestCHK_TRAY_44_ImportLayout — the import dialog in the layout test:
// RU / EN, 96 / 120 / 144 dpi, also in a 1024×768 work area.
func TestCHK_TRAY_44_ImportLayout(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer func() { testDPI = 0; testWorkArea = nil }()
	small := winapi.RECT{Right: 1024, Bottom: 728}
	for _, dpi := range []int32{96, 120, 144} {
		for _, lang := range []string{i18n.RU, i18n.EN} {
			for _, wa := range []*winapi.RECT{nil, &small} {
				testDPI, testWorkArea = dpi, wa
				e := newXferEnv(t, lang)
				writeImportFile(t, e.path, &traysettings.TrayBlock{Language: "ru"}, e.x.profiles[0])
				e.w.click(t, idSetImport)
				d := e.w.imp
				if d == nil {
					t.Fatalf("%s %d: no dialog: %q", lang, dpi, e.w.statusText())
				}
				pumpUntil(t, "dry run", func() bool { return d.res != nil })
				checkLayout(t, "import "+lang+" "+string(rune('0'+dpi/48)), d.hwnd)
				d.close()
				e.w.close()
			}
		}
	}
}

// TestCHK_TRAY_44_OtherUser — elevated as another Windows user (over the
// shoulder): no tray block in the export, «Также звук и вид трея» off and
// disabled, a RU / EN warning; HKCU is never written.
func TestCHK_TRAY_44_OtherUser(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if sid := CurrentUserSID(); !strings.HasPrefix(sid, "S-1-") || OtherUser(sid, sid) || OtherUser("", sid) {
		t.Fatalf("own SID %q", sid)
	}
	if !OtherUser("S-1-5-21-1-2-3-1001", "S-1-5-21-1-2-3-500") || OtherUser("s-1-5-21-1", "S-1-5-21-1") {
		t.Fatal("SID compare")
	}
	if OwnerSIDFromArgs([]string{SettingsArg, OwnerSIDArg + "S-1-5-21-9"}) != "S-1-5-21-9" || OwnerSIDFromArgs([]string{SettingsArg}) != "" {
		t.Fatal("argument")
	}
	for _, lang := range []string{i18n.RU, i18n.EN} {
		warn := i18n.T(i18n.KeyXferOtherUser, lang)
		e := newXferEnv(t, lang, true)
		e.w.click(t, idSetExport)
		if e.w.exp == nil {
			t.Fatalf("%s: export dialog", lang)
		}
		winapi.SendMessage(e.w.exp.ctl[idExpDo], winapi.BM_CLICK, 0, 0)
		pumpUntil(t, "exported", func() bool { return e.w.exp == nil && !e.w.busy })
		b, err := os.ReadFile(e.path)
		if err != nil || strings.Contains(string(b), `"tray"`) || !strings.Contains(e.w.statusText(), warn) {
			t.Fatalf("%s: export %v tray block present / status %q", lang, err, e.w.statusText())
		}
		snd := trayalert.DefaultSoundPrefs(nil)
		writeImportFile(t, e.path, &traysettings.TrayBlock{Language: "ru", Balloons: true, Sound: &snd}, e.x.profiles[0])
		e.w.click(t, idSetImport)
		d := e.w.imp
		if d == nil || d.checked(idImpTray) || winapi.IsWindowEnabled(d.ctl[idImpTray]) || d.applyTray() {
			t.Fatalf("%s: tray check box must be off and disabled", lang)
		}
		pumpUntil(t, "dry run", func() bool { return d.res != nil })
		if !strings.Contains(d.preview, warn) {
			t.Errorf("%s: preview %q", lang, d.preview)
		}
		winapi.SendMessage(d.ctl[idImpDo], winapi.BM_CLICK, 0, 0)
		pumpUntil(t, "imported", func() bool { return e.w.imp == nil && !e.w.busy && e.w.loaded })
		if e.lang.v != "en" || e.bal.on || e.ui.m != traysettings.UIExpert {
			t.Errorf("%s: HKCU written as another user", lang)
		}
	}
	if i18n.T(i18n.KeyXferTrayApplied, i18n.RU) != "Звук и вид трея применены" ||
		i18n.T(i18n.KeyXferTrayApplied, i18n.EN) != "Tray sound and appearance applied" {
		t.Error("applied hint text")
	}
	for _, lang := range []string{i18n.RU, i18n.EN} {
		s := i18n.T(i18n.KeyXferTrayApplied, lang)
		if strings.Contains(s, "перезапуск") || strings.Contains(strings.ToLower(s), "restart") {
			t.Errorf("%s: restart must not be required: %q", lang, s)
		}
	}
}

// TestCHK_TRAY_42_FocusScroll — focus on a page control (EN_SETFOCUS,
// CBN_SETFOCUS, BN_SETFOCUS: also by Tab) scrolls it into the view.
func TestCHK_TRAY_42_FocusScroll(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	testWorkArea = &winapi.RECT{Right: 1024, Bottom: 500}
	defer func() { testWorkArea = nil }()
	e := newXferEnv(t, i18n.RU)
	w := e.w
	contentRect := func(h uintptr) (winapi.RECT, bool) {
		if c := w.ctlOf[h]; c != nil && c.Placed {
			return winapi.RECT{Left: c.Rect.X, Top: c.Rect.Y, Right: c.Rect.X + c.Rect.W, Bottom: c.Rect.Y + c.Rect.H}, true
		}
		r, ok := w.hostRect[h]
		return r, ok
	}
	visible := func(h uintptr) bool {
		r, ok := contentRect(h)
		if !ok {
			return false
		}
		x, y := w.ScrollPos()
		return r.Top-y >= 0 && r.Bottom-y <= w.viewH && r.Left-x >= 0
	}
	low, lowEdit := uintptr(0), uintptr(0)
	var lowR, lowEditR winapi.RECT
	for _, h := range w.pageCtl[0] {
		if !w.underPagePanel(h) || winapi.WindowStyle(h)&winapi.WS_VISIBLE == 0 {
			continue
		}
		r, ok := contentRect(h)
		if !ok {
			continue
		}
		if w.notifyBtn[h] && (low == 0 || r.Bottom > lowR.Bottom) {
			low, lowR = h, r
		}
		if winapi.WindowStyle(h)&winapi.ES_MULTILINE == 0 && !w.notifyBtn[h] && (lowEdit == 0 || r.Bottom > lowEditR.Bottom) {
			lowEdit, lowEditR = h, r
		}
	}
	if low == 0 || lowEdit == 0 || visible(low) {
		t.Fatalf("no control below the view (view %d)", w.viewH)
	}
	if winapi.WindowStyle(low)&winapi.BS_NOTIFY == 0 {
		t.Error("page button without BS_NOTIFY")
	}
	notify := func(h uintptr, code uint32) {
		id := int32(0)
		for k, c := range w.ctl {
			if c == h {
				id = k
			}
		}
		winapi.SendMessage(w.host, winapi.WM_COMMAND, uintptr(code)<<16|uintptr(id&0xFFFF), h)
	}
	notify(low, winapi.BN_SETFOCUS)
	if _, y := w.ScrollPos(); y == 0 || !visible(low) {
		t.Errorf("BN_SETFOCUS: scroll %d", y)
	}
	top := w.ctl[idSetName]
	notify(top, winapi.EN_SETFOCUS)
	if !visible(top) {
		t.Error("EN_SETFOCUS did not scroll back")
	}
	notify(lowEdit, winapi.EN_SETFOCUS)
	if !visible(lowEdit) {
		t.Error("EN_SETFOCUS: low control not visible")
	}
	notify(w.ctl[idSetAuth], winapi.CBN_SETFOCUS)
	if !visible(w.ctl[idSetAuth]) {
		t.Error("CBN_SETFOCUS")
	}
}


// TestCHK_TRAY_57_ExportImportViberFlag — checkbox sets includeViberRecipients
// on the request JSON path (via ExportRequest / ImportRequest builders used
// by the dialogs); warning strings RU+EN; Theme in the tray block.
func TestCHK_TRAY_57_ExportImportViberFlag(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	for _, lang := range []string{i18n.RU, i18n.EN} {
		if i18n.T(i18n.KeyXferViberCheck, lang) == "" || i18n.T(i18n.KeyXferViberWarnExport, lang) == "" ||
			i18n.T(i18n.KeyXferViberWarnImport, lang) == "" {
			t.Fatalf("%s: viber strings", lang)
		}
		if strings.Contains(i18n.T(i18n.KeyXferViberWarnExport, lang), "токен") == (lang == i18n.EN) {
			// RU mentions токен; EN mentions token
		}
		tok := map[string]string{i18n.RU: "токен", i18n.EN: "token"}[lang]
		if !strings.Contains(strings.ToLower(i18n.T(i18n.KeyXferViberWarnExport, lang)), strings.ToLower(tok)) ||
			!strings.Contains(strings.ToLower(i18n.T(i18n.KeyXferViberWarnImport, lang)), strings.ToLower(tok)) {
			t.Errorf("%s: warn must mention token never transferred", lang)
		}
	}
	e := newXferEnv(t, i18n.RU)
	theme := &memTheme{v: "dark"}
	e.w.cfg.themeStore = theme
	e.w.click(t, idSetExport)
	d := e.w.exp
	if d == nil || d.checked(idExpViber) {
		t.Fatal("export dialog: viber off by default")
	}
	winapi.SendMessage(d.ctl[idExpViber], winapi.BM_CLICK, 0, 0)
	if !d.checked(idExpViber) {
		t.Fatal("viber check")
	}
	warn := winapi.GetWindowText(d.ctl[idExpWarn])
	if warn != i18n.T(i18n.KeyXferViberWarnExport, i18n.RU) {
		t.Errorf("export warn %q", warn)
	}
	winapi.SendMessage(d.ctl[idExpDo], winapi.BM_CLICK, 0, 0)
	pumpUntil(t, "exported", func() bool { return e.w.exp == nil && !e.w.busy })
	b, err := os.ReadFile(e.path)
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Tray traysettings.TrayBlock `json:"tray"`
	}
	if err := json.Unmarshal(b, &f); err != nil || f.Tray.Theme != "dark" {
		t.Fatalf("tray theme in file: %v %+v", err, f.Tray)
	}
	// import dialog shows viber checkbox (disabled without recipients in file)
	e.w.click(t, idSetImport)
	imp := e.w.imp
	if imp == nil {
		t.Fatal("import dialog")
	}
	pumpUntil(t, "dry run", func() bool { return imp.res != nil })
	if winapi.IsWindowEnabled(imp.ctl[idImpViber]) {
		t.Error("viber check enabled without recipients in file")
	}
	imp.close()
}

type memTheme struct{ v string }

func (m *memTheme) Load() (trayui.Theme, error) { return trayui.NormalizeTheme(m.v), nil }
func (m *memTheme) Save(t trayui.Theme) error   { m.v = string(t); return nil }

// TestCHK_TRAY_57_ReloadPrefsMessage — RegisterWindowMessage for
// TrayReloadPrefs; handler rereads HKCU without Exit.
func TestCHK_TRAY_57_ReloadPrefsMessage(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	m := winapi.RegisterWindowMessage(model.TrayReloadPrefsMessage)
	if m == 0 || m != trayReloadPrefsMsg() || !isTrayReloadPrefs(m) || isTrayExit(m) {
		t.Fatalf("reload msg %#x exit %#x", m, trayExitMsg())
	}
	lang := &memLang{v: "en"}
	bal := &memBalloon{on: false}
	snd := &memSound{raw: trayalert.DefaultSoundPrefs(nil).String()}
	theme := &memTheme{v: "light"}
	ui := &memUI{m: traysettings.UIExpert}
	app := startCfgApp(t, newCfgService(), false, nil)
	app.opt.LangStore = lang
	app.opt.BalloonStore = bal
	app.opt.SoundStore = snd
	app.opt.ThemeStore = theme
	app.opt.UIModeStore = ui
	app.loadBalloonPref()
	if app.balloonsOn || appClosed(app) {
		t.Fatalf("pre: balloons %v closed %v", app.balloonsOn, appClosed(app))
	}
	lang.v = "ru"
	bal.on = true
	theme.v = "dark"
	prefs := trayalert.DefaultSoundPrefs(nil)
	prefs.Master = 55
	snd.raw = prefs.String()
	ui.m = traysettings.UISimple
	winapi.PostMessage(app.hwnd, m, 0x111, 0x222)
	pumpUntil(t, "reload prefs", func() bool { return app.balloonsOn && app.opt.Lang == i18n.RU && trayui.IsDark() })
	if appClosed(app) || app.opt.LangPref != "ru" || !app.balloonsOn || !trayui.IsDark() {
		t.Fatalf("after reload: closed %v lang %q pref %q balloons %v dark %v",
			appClosed(app), app.opt.Lang, app.opt.LangPref, app.balloonsOn, trayui.IsDark())
	}
	if app.soundPrefs.Master != 55 {
		t.Errorf("sound master %d", app.soundPrefs.Master)
	}
}

// TestCHK_TRAY_57_TrayBlockApplyNoRestart — import of the tray block writes
// HKCU, broadcasts reload, status has no mandatory restart string; OTS
// unchanged (covered by TestCHK_TRAY_44_OtherUser).
func TestCHK_TRAY_57_TrayBlockApplyNoRestart(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	e := newXferEnv(t, i18n.EN)
	theme := &memTheme{v: "light"}
	e.w.cfg.themeStore = theme
	snd := trayalert.DefaultSoundPrefs(nil)
	snd.Master = 40
	writeImportFile(t, e.path, &traysettings.TrayBlock{Language: "ru", Balloons: true, Sound: &snd, UIMode: traysettings.UISimple, Theme: "dark"}, e.x.profiles[0])
	e.w.click(t, idSetImport)
	d := e.w.imp
	if d == nil {
		t.Fatal(e.w.statusText())
	}
	pumpUntil(t, "dry run", func() bool { return d.res != nil })
	winapi.SendMessage(d.ctl[idImpDo], winapi.BM_CLICK, 0, 0)
	pumpUntil(t, "imported", func() bool { return e.w.imp == nil && !e.w.busy && e.w.loaded })
	st := e.w.statusText()
	if !strings.Contains(st, i18n.T(i18n.KeyXferTrayApplied, i18n.EN)) ||
		strings.Contains(strings.ToLower(st), "restart") || strings.Contains(st, "перезапуск") {
		t.Errorf("status %q", st)
	}
	if e.lang.v != "ru" || !e.bal.on || e.ui.m != traysettings.UISimple || theme.v != "dark" {
		t.Errorf("tray block: lang %q bal %v ui %q theme %q", e.lang.v, e.bal.on, e.ui.m, theme.v)
	}
}

