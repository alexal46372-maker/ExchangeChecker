//go:build windows

package wintray

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/ipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

type memBalloonStore struct {
	on    bool
	saved []bool
}

func (m *memBalloonStore) Load() (bool, error) { return m.on, nil }
func (m *memBalloonStore) Save(on bool) error  { m.saved = append(m.saved, on); return nil }

// pollService counts poll_now.
type pollService struct {
	alertService
	pmu   sync.Mutex
	polls []string
}

func (p *pollService) PollNow(id string) (bool, error) {
	p.pmu.Lock()
	defer p.pmu.Unlock()
	p.polls = append(p.polls, id)
	return false, nil
}

func (p *pollService) n() int { p.pmu.Lock(); defer p.pmu.Unlock(); return len(p.polls) }

func phase9Snapshot() model.StatusSnapshot {
	f := time.Now().Add(-37 * time.Minute)
	return model.StatusSnapshot{Seq: 1, Profiles: []model.ProfileSnapshot{
		{ProfileID: "a", Name: "ERP", Enabled: true, Polled: true, Level: model.LevelWarning, Points: []model.PointStatus{
			{ExchangePoint: model.ExchangePoint{Name: "Склад", Level: model.LevelOK, LastSend: &f}, FreshAt: &f},
			{ExchangePoint: model.ExchangePoint{Name: "Касса", Level: model.LevelWarning, LastRecv: &f}, FreshAt: &f},
		}},
		{ProfileID: "b", Name: "HR", Enabled: true, Polled: true, Level: model.LevelError, Error: "login failed"},
	}}
}

// TestCHK_TRAY_18_Balloons — point alert pushes of one drain give one
// balloon (overdue / recovery / summary); the switch of the right menu
// turns them off and is stored per user.
func TestCHK_TRAY_18_Balloons(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := &alertService{}
	app, _ := startAlertApp(t, svc, &testBeeper{})
	defer app.Exit() // windows close on this locked thread
	store := &memBalloonStore{on: true}
	app.opt.BalloonStore = store
	app.loadBalloonPref()

	push := func(evs ...model.AlertEvent) {
		for i := range evs {
			e := evs[i]
			app.onEvent(trayipc.Event{Kind: trayipc.EventAlert, Status: app.Status(), Alert: &e})
		}
	}
	ov := model.AlertEvent{ProfileName: "ERP", PointName: "Склад", From: model.LevelOK, To: model.LevelWarning, LagMinutes: 61}
	push(ov)
	pumpUntil(t, "overdue balloon", func() bool { return len(app.Balloons()) == 1 })
	if b := app.Balloons()[0]; b.Title != "Просрочка обмена" || !strings.Contains(b.Text, "ERP — Склад: нет обмена 61 мин") {
		t.Errorf("overdue %+v", b)
	}
	rec := ov
	rec.From, rec.To = model.LevelWarning, model.LevelOK
	push(rec)
	pumpUntil(t, "recovery balloon", func() bool { return len(app.Balloons()) == 2 })
	if b := app.Balloons()[1]; !b.Recovered || b.Title != "Обмен восстановлен" {
		t.Errorf("recovery %+v", b)
	}
	ov2 := ov
	ov2.PointName, ov2.To = "Касса", model.LevelError
	push(ov, ov2, model.AlertEvent{ProfileName: "HR", From: 0, To: model.LevelError}) // one drain
	pumpUntil(t, "summary balloon", func() bool { return len(app.Balloons()) >= 3 })
	pumpFor(50 * time.Millisecond)
	if n := len(app.Balloons()); n != 3 {
		t.Fatalf("balloons %d", n)
	}
	if b := app.Balloons()[2]; b.Level != model.LevelError || b.Text != "Просрочено точек: 2, восстановлено: 0" {
		t.Errorf("summary %+v", b)
	}

	m := app.RightMenu()
	var sw *trayui.Item
	for i := range m {
		if m[i].ID == trayui.CmdBalloons {
			sw = &m[i]
		}
	}
	if sw == nil || !sw.Checked || sw.Text != "Уведомления о просрочке" || m[len(m)-1].ID != trayui.CmdExit || m[2].Sub == nil {
		t.Fatalf("menu %+v", m)
	}
	app.Command(trayui.CmdBalloons)
	if app.BalloonsEnabled() || len(store.saved) != 1 || store.saved[0] {
		t.Fatalf("switch off: %v %v", app.BalloonsEnabled(), store.saved)
	}
	push(ov)
	pumpFor(100 * time.Millisecond)
	if len(app.Balloons()) != 3 {
		t.Error("balloon shown while off")
	}
	if got := truncU16(strings.Repeat("я", 300), 255); len(got) != 255 || got[254] != 0x2026 {
		t.Errorf("truncate %d", len(got))
	}
}

// TestCHK_TRAY_18_RegistryBalloonStore — HKCU value, missing = on.
func TestCHK_TRAY_18_RegistryBalloonStore(t *testing.T) {
	key := `Software\ExchangeChecker\TrayTest9`
	t.Cleanup(func() { winapi.RegDeleteKey(winapi.HKEY_CURRENT_USER, key) })
	s := RegistryBalloonStore{Key: key}
	if on, err := s.Load(); !on || err != nil {
		t.Fatalf("default %v %v", on, err)
	}
	if err := s.Save(false); err != nil {
		t.Fatal(err)
	}
	if on, _ := s.Load(); on {
		t.Error("saved off read as on")
	}
	s.Save(true)
	if on, _ := s.Load(); !on {
		t.Error("saved on read as off")
	}
}

func (sw *statusWindow) row(i int) []string {
	out := make([]string, len(trayui.StatusColumns))
	for c := range out {
		out[c] = winapi.ListViewText(sw.list, i, c)
	}
	return out
}

// TestCHK_TRAY_19_StatusWindow — «Состояние» lists every database and
// point of get_status with «N мин назад», row colours by NM_CUSTOMDRAW,
// updates on pushes, tooltips on every control.
func TestCHK_TRAY_19_StatusWindow(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := &alertService{snap: phase9Snapshot()}
	app, _ := startAlertApp(t, svc, &testBeeper{})
	defer app.Exit() // windows close on this locked thread
	app.opt.HiddenStatus = true
	pumpUntil(t, "status", func() bool { return app.Status().Snapshot != nil && len(app.Status().Snapshot.Profiles) == 2 })
	m := app.RightMenu()
	found := false
	for _, it := range m {
		found = found || it.ID == trayui.CmdStatusWin && it.Text == "Состояние…"
	}
	if !found {
		t.Fatalf("menu %+v", m)
	}
	app.Command(trayui.CmdStatusWin)
	sw := app.statusWin
	if sw == nil || !winapi.IsWindow(sw.hwnd) {
		t.Fatal("no status window")
	}
	if n := int(winapi.SendMessage(sw.list, winapi.LVM_GETITEMCOUNT, 0, 0)); n != 3 {
		t.Fatalf("rows %d", n)
	}
	if h := winapi.GetWindowText(sw.hwnd); h != "Состояние — Exchange Checker" {
		t.Errorf("title %q", h)
	}
	var okRow, errRow = -1, -1
	for i := 0; i < 3; i++ {
		r := sw.row(i)
		switch {
		case r[1] == "Склад":
			okRow = i
			if r[0] != "ERP" || (r[4] != "37 мин назад" && r[4] != "38 мин назад") || r[5] != "норма" {
				t.Errorf("Склад row %q", r)
			}
		case r[0] == "HR":
			errRow = i
			if r[8] != "login failed" {
				t.Errorf("HR row %q", r)
			}
		}
	}
	if okRow < 0 || errRow < 0 {
		t.Fatal("rows missing")
	}
	// NM_CUSTOMDRAW: pre-paint asks for items, the item gets its colour
	cd := winapi.NMLVCUSTOMDRAW{Hdr: winapi.NMHDR{From: sw.list, ID: idStList, Code: winapi.NM_CUSTOMDRAW}, DrawStage: winapi.CDDS_PREPAINT}
	if r := winapi.SendMessage(sw.hwnd, winapi.WM_NOTIFY, idStList, uintptr(unsafe.Pointer(&cd))); r != winapi.CDRF_NOTIFYITEMDRAW {
		t.Errorf("prepaint %x", r)
	}
	cd.DrawStage, cd.ItemSpec = winapi.CDDS_ITEMPREPAINT, uintptr(errRow)
	if r := winapi.SendMessage(sw.hwnd, winapi.WM_NOTIFY, idStList, uintptr(unsafe.Pointer(&cd))); r != winapi.CDRF_NEWFONT || cd.ClrTextBk != rowBk[trayui.ColorRed] {
		t.Errorf("item %x bk %06x", r, cd.ClrTextBk)
	}
	cd.ItemSpec = uintptr(okRow)
	winapi.SendMessage(sw.hwnd, winapi.WM_NOTIFY, idStList, uintptr(unsafe.Pointer(&cd)))
	if cd.ClrTextBk != rowBk[trayui.ColorGreen] {
		t.Errorf("ok bk %06x", cd.ClrTextBk)
	}
	runtime.KeepAlive(&cd)

	// a push refreshes the list
	s2 := phase9Snapshot()
	s2.Seq = 2
	s2.Profiles = s2.Profiles[:1]
	st := app.Status()
	st.Snapshot = &s2
	app.onEvent(trayipc.Event{Kind: trayipc.EventStatus, Status: st})
	pumpUntil(t, "refresh", func() bool { return int(winapi.SendMessage(sw.list, winapi.LVM_GETITEMCOUNT, 0, 0)) == 2 })

	want := map[uintptr]string{sw.list: i18n.KeyTipStList, sw.check: i18n.KeyTipCheckNow, sw.csv: i18n.KeyTipExportCSV,
		sw.html: i18n.KeyTipExportHTML, sw.close: i18n.KeyTipClose, sw.info: i18n.KeyTipStInfo}
	for h, k := range want {
		if h == 0 || sw.tips.text(h) != i18n.T(k, i18n.RU) {
			t.Errorf("tooltip %s: %q", k, sw.tips.text(h))
		}
	}
	winapi.SendMessage(sw.close, winapi.BM_CLICK, 0, 0)
	if winapi.IsWindowVisible(sw.hwnd) {
		t.Error("close must hide")
	}
	if app.OpenStatus() != sw {
		t.Error("window must be reused")
	}
}

// TestCHK_TRAY_20_CheckNow — «Проверить сейчас» (menu and window) sends
// poll_now for all databases; the reply is shown; disabled offline.
func TestCHK_TRAY_20_CheckNow(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := &pollService{alertService: alertService{snap: phase9Snapshot()}}
	app2 := startPollApp(t, svc)
	defer app2.Exit()
	pumpUntil(t, "connected", func() bool { return app2.connected() })
	var item trayui.Item
	for _, it := range app2.RightMenu() {
		if it.ID == trayui.CmdCheckNow {
			item = it
		}
	}
	if item.Text != "Проверить сейчас" || item.Disabled {
		t.Fatalf("item %+v", item)
	}
	app2.Command(trayui.CmdCheckNow)
	pumpUntil(t, "poll_now", func() bool { return svc.n() == 1 && len(app2.checkResults) == 1 })
	if svc.polls[0] != "" || app2.checkResults[0] != "Проверка запрошена" {
		t.Errorf("poll %q result %q", svc.polls, app2.checkResults)
	}
	app2.opt.HiddenStatus = true
	sw := app2.OpenStatus()
	winapi.SendMessage(sw.check, winapi.BM_CLICK, 0, 0)
	pumpUntil(t, "window poll_now", func() bool { return svc.n() == 2 && len(app2.checkResults) == 2 })
	if !winapi.IsWindowEnabled(sw.check) {
		t.Error("check button disabled while connected")
	}
	if s, warn := CheckNowText(model.IPCMessage{}, trayipc.ErrNotConnected, i18n.EN); !warn || !strings.HasPrefix(s, "Check not requested: ") {
		t.Errorf("offline text %q", s)
	}
	if s, warn := CheckNowText(model.IPCMessage{Type: model.MsgError, Error: "boom"}, nil, i18n.EN); !warn || !strings.Contains(s, "boom") {
		t.Errorf("error text %q", s)
	}
	if !trayui.Phase9Items(i18n.RU, true, false)[1].Disabled {
		t.Error("offline item must be disabled")
	}
}

// TestCHK_TRAY_21_Export — CSV / HTML of the window rows go to the
// path of the save dialog (hooked); a write error is reported.
func TestCHK_TRAY_21_Export(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := &alertService{snap: phase9Snapshot()}
	app, _ := startAlertApp(t, svc, &testBeeper{})
	defer app.Exit() // windows close on this locked thread
	app.opt.HiddenStatus = true
	pumpUntil(t, "status", func() bool { return app.Status().Snapshot != nil && len(app.Status().Snapshot.Profiles) == 2 })
	sw := app.OpenStatus()
	dir := t.TempDir()
	var asked []exportKind
	sw.savePath = func(k exportKind) (string, bool) {
		asked = append(asked, k)
		if k == exportCSV {
			return filepath.Join(dir, "s.csv"), true
		}
		return filepath.Join(dir, "s.html"), true
	}
	winapi.SendMessage(sw.csv, winapi.BM_CLICK, 0, 0)
	winapi.SendMessage(sw.html, winapi.BM_CLICK, 0, 0)
	c, err := os.ReadFile(filepath.Join(dir, "s.csv"))
	if err != nil || !strings.HasPrefix(string(c), "\uFEFFБаза;Точка;") || strings.Count(string(c), "\r\n") != 4 {
		t.Fatalf("csv %v %q", err, c)
	}
	h, err := os.ReadFile(filepath.Join(dir, "s.html"))
	if err != nil || !strings.Contains(string(h), "<td>Склад</td>") || !strings.Contains(string(h), "login failed") {
		t.Fatalf("html %v", err)
	}
	if len(asked) != 2 || asked[0] != exportCSV || asked[1] != exportHTML {
		t.Errorf("asked %v", asked)
	}
	if !strings.HasPrefix(winapi.GetWindowText(sw.info), "Сохранено: ") {
		t.Errorf("info %q", winapi.GetWindowText(sw.info))
	}
	sw.savePath = func(exportKind) (string, bool) { return filepath.Join(dir, "missing", "x.csv"), true }
	sw.export(exportCSV)
	if !strings.HasPrefix(winapi.GetWindowText(sw.info), "Не удалось сохранить: ") {
		t.Errorf("error info %q", winapi.GetWindowText(sw.info))
	}
	sw.savePath = func(exportKind) (string, bool) { return "", false } // cancel
	sw.lastSaved = ""
	sw.export(exportHTML)
	if sw.lastSaved != "" {
		t.Error("cancel must not save")
	}
}

func startPollApp(t *testing.T, svc *pollService) *App {
	t.Helper()
	l := ipc.NewMemListener()
	srv := ipc.NewServer(svc, nil)
	go srv.Serve(l)
	t.Cleanup(func() { srv.Close() })
	dial := func(context.Context) (io.ReadWriteCloser, error) { return l.Dial() }
	app, err := NewApp(Options{Lang: i18n.RU, NoNotifyIcon: true, HiddenAlerts: true, HiddenStatus: true, Beeper: &testBeeper{},
		Session: trayipc.Options{Dial: dial, BackoffMin: 10 * time.Millisecond, BackoffMax: 20 * time.Millisecond}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Exit)
	return app
}
