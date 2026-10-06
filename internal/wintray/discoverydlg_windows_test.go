//go:build windows

package wintray

import (
	"context"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/traysettings"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// discService is cfgService with the interactive commands (G2/G3,
// ipc.Discoverer): all four capabilities are announced.
type discService struct {
	*cfgService
	dmu      sync.Mutex
	reqs     []model.IPCRequest
	block    bool
	canceled int
	check    model.MappingCheck
}

func newDiscService() *discService {
	d := &discService{cfgService: newCfgService()}
	d.cfgService.handler = d
	return d
}

func (d *discService) record(ctx context.Context, req model.IPCRequest) error {
	d.dmu.Lock()
	d.reqs = append(d.reqs, req)
	block := d.block
	d.dmu.Unlock()
	if !block {
		return nil
	}
	<-ctx.Done()
	d.dmu.Lock()
	d.canceled++
	d.dmu.Unlock()
	return ctx.Err()
}

func (d *discService) n() (reqs, canceled int) {
	d.dmu.Lock()
	defer d.dmu.Unlock()
	return len(d.reqs), d.canceled
}

func (d *discService) last() model.IPCRequest {
	d.dmu.Lock()
	defer d.dmu.Unlock()
	return d.reqs[len(d.reqs)-1]
}

func (d *discService) setBlock(b bool) {
	d.dmu.Lock()
	d.block = b
	d.dmu.Unlock()
}

func testCandidate(table string) model.DiscoveryCandidate {
	at := time.Date(2026, 10, 6, 9, 0, 0, 0, model.DBZone)
	return model.DiscoveryCandidate{Mode: model.CandidateLinked, Schema: "dbo", Table: table, PointCol: "_Description",
		PointKeyCol: "_IDRRef", TimesTable: "_InfoRg88", LinkCol: "_Fld89RRef", SendCol: "_Fld90", RecvCol: "_Fld91",
		YearOffset: model.YearOffset1C, PlanTable: table, Points: 2, FreshestAt: &at, Score: 50,
		Hints: []string{model.HintLinked}, SampleRows: []model.SampleRow{{Point: "Склад", LastSend: &at}}}
}

func (d *discService) Discover(ctx context.Context, req model.IPCRequest) ([]model.DiscoveryCandidate, string, error) {
	if err := d.record(ctx, req); err != nil {
		return nil, "", err
	}
	return []model.DiscoveryCandidate{testCandidate("_Node10"), testCandidate("_Node20")}, model.TruncatedBudget, nil
}

func (d *discService) DiscoverByValue(ctx context.Context, req model.IPCRequest) ([]model.DiscoveryCandidate, string, error) {
	if err := d.record(ctx, req); err != nil {
		return nil, "", err
	}
	return []model.DiscoveryCandidate{testCandidate("_Node30")}, "", nil
}

func (d *discService) ListNodePoints(ctx context.Context, req model.IPCRequest) ([]model.NodePoint, string, error) {
	if err := d.record(ctx, req); err != nil {
		return nil, "", err
	}
	return []model.NodePoint{{PlanTable: "_Node10", Ref: "ab01", Code: "001", Name: "Склад"}}, "", nil
}

func (d *discService) CheckMapping(ctx context.Context, req model.IPCRequest) (model.MappingCheck, error) {
	if err := d.record(ctx, req); err != nil {
		return model.MappingCheck{}, err
	}
	d.dmu.Lock()
	defer d.dmu.Unlock()
	return d.check, nil
}

func (dd *discoveryDialog) click(t *testing.T, id int32) {
	t.Helper()
	h := dd.ctl[id]
	if h == 0 || !winapi.IsWindowEnabled(h) {
		t.Fatalf("dialog control %d missing or disabled", id)
	}
	winapi.SendMessage(h, winapi.BM_CLICK, 0, 0)
}

func (dd *discoveryDialog) set(id int32, s string) {
	winapi.SendMessageString(dd.ctl[id], winapi.WM_SETTEXT, 0, s)
}

// TestCHK_TRAY_07_HiddenWithoutCaps — without the interactive
// capabilities «Таблицы обмена…» stays disabled and does not open.
func TestCHK_TRAY_07_HiddenWithoutCaps(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	app := startCfgApp(t, newCfgService(), true, nil)
	w := openAdmin(t, app)
	if h := w.ctl[idSetDiscover]; h == 0 || winapi.IsWindowEnabled(h) {
		t.Fatal("discovery button must exist and be disabled without the capabilities")
	}
	w.openDiscovery()
	if w.disc != nil {
		t.Fatal("dialog opened without the capabilities")
	}
}

// TestCHK_TRAY_07_ReadOnlyNoDiscovery — the read-only window (ordinary
// tray) has no «Таблицы обмена…» even when the service supports it.
func TestCHK_TRAY_07_ReadOnlyNoDiscovery(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	app := startCfgApp(t, newDiscService().cfgService, false, &fakeElevator{})
	app.Command(1001) // trayui.CmdSettings
	w := app.settings
	if w == nil || w.ed.Mode != traysettings.ReadOnly {
		t.Fatal("read-only window not opened")
	}
	if w.ctl[idSetDiscover] != 0 {
		t.Error("read-only window has no discovery button")
	}
	w.openDiscovery()
	if w.disc != nil {
		t.Error("read-only window opened the dialog")
	}
}

// TestCHK_TRAY_07_DiscoveryDialogSmoke — the dialog of the administrator
// window: discover by profileId (truncated result), the candidate in the
// mapping editor, list_node_points (limit), the empty point refused
// locally, discover_by_value by pointRef, «Поиск… N с» with cancel
// {ref}, check_mapping errors highlighted, a manual SQL template, close
// cancels, Apply -> save_profile with the mapping and expectedRevision.
func TestCHK_TRAY_07_DiscoveryDialogSmoke(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ds := newDiscService()
	app := startCfgApp(t, ds.cfgService, true, nil)
	w := openAdmin(t, app)
	pumpUntil(t, "discovery button enabled", func() bool {
		w.update()
		return winapi.IsWindowEnabled(w.ctl[idSetDiscover])
	})
	w.click(t, idSetDiscover)
	dd := w.disc
	if dd == nil || winapi.GetClassName(dd.hwnd) != DiscoveryClass {
		t.Fatal("dialog not opened")
	}
	if winapi.IsWindowEnabled(w.hwnd) || winapi.IsWindowEnabled(w.ctl[idSetDiscover]) {
		t.Error("the owner is disabled while the dialog is open")
	}
	wait := func(what string, reqs int) {
		t.Helper()
		pumpUntil(t, what, func() bool { n, _ := ds.n(); return n == reqs && dd.cancel == nil })
	}

	dd.click(t, idDiscFind)
	wait("discover", 1)
	if r := ds.last(); r.Cmd != model.CmdDiscover || r.ProfileID != "p1" || r.Profile != nil {
		t.Errorf("discover request %+v", r)
	}
	if n := winapi.SendMessage(dd.ctl[idDiscList], winapi.LB_GETCOUNT, 0, 0); n != 2 || dd.d.Sel != 0 {
		t.Errorf("candidates %d sel %d", n, dd.d.Sel)
	}
	if dd.text(idDiscTable) != "_Node10" || dd.text(idDiscTimesTable) != "_InfoRg88" || !dd.checked(idDiscYear) {
		t.Errorf("mapping editor: table %q times %q", dd.text(idDiscTable), dd.text(idDiscTimesTable))
	}
	if st := dd.text(idDiscState); !strings.Contains(st, i18n.T(i18n.KeyDiscReasonBudget, i18n.RU)) {
		t.Errorf("truncated status %q", st)
	}
	if !strings.Contains(dd.text(idDiscDetails), traysettings.HintText(model.HintLinked, i18n.RU)) {
		t.Errorf("details %q", dd.text(idDiscDetails))
	}

	dd.click(t, idDiscPoints)
	wait("list_node_points", 2)
	if r := ds.last(); r.Cmd != model.CmdListNodePoints || r.Limit != traysettings.DefaultNodePointsLimit {
		t.Errorf("node points request %+v", r)
	}
	if n := winapi.SendMessage(dd.ctl[idDiscPoint], winapi.CB_GETCOUNT, 0, 0); n != 1 {
		t.Errorf("points in the box: %d", n)
	}

	dd.set(idDiscPoint, "  ")
	dd.click(t, idDiscByValue)
	PumpPending()
	if n, _ := ds.n(); n != 2 || dd.lastField != string(traysettings.FieldDiscPoint) ||
		dd.text(idDiscState) != i18n.Message(model.MsgDiscoverPointNameEmpty, i18n.RU) {
		t.Errorf("empty point: reqs %d field %q state %q", n, dd.lastField, dd.text(idDiscState))
	}
	winapi.SendMessage(dd.ctl[idDiscPoint], winapi.CB_SETCURSEL, 0, 0)
	dd.set(idDiscTime, "06.10.2026 09:30")
	dd.click(t, idDiscByValue)
	wait("discover_by_value", 3)
	if r := ds.last(); r.Cmd != model.CmdDiscoverByValue || r.Query == nil || r.Query.PointRef != "ab01" || r.Query.ApproxTime == nil {
		t.Errorf("by value request %+v", r)
	}
	if dd.text(idDiscTable) != "_Node30" {
		t.Errorf("by value candidate %q", dd.text(idDiscTable))
	}

	// running: «Поиск… N с», «Отменить» sends cancel {ref}
	ds.setBlock(true)
	dd.click(t, idDiscFind)
	pumpUntil(t, "discover running", func() bool { n, _ := ds.n(); return n == 4 })
	if winapi.IsWindowEnabled(dd.ctl[idDiscFind]) || !winapi.IsWindowEnabled(dd.ctl[idDiscCancel]) ||
		!strings.HasPrefix(dd.text(idDiscState), "Поиск…") {
		t.Errorf("running state %q", dd.text(idDiscState))
	}
	dd.click(t, idDiscCancel)
	pumpUntil(t, "canceled", func() bool { _, c := ds.n(); return c == 1 && dd.cancel == nil })
	if dd.text(idDiscState) != i18n.T(i18n.KeyDiscCanceled, i18n.RU) {
		t.Errorf("canceled state %q", dd.text(idDiscState))
	}
	ds.setBlock(false)

	// check_mapping: a failed check highlights the field
	ds.dmu.Lock()
	ds.check = model.MappingCheck{ErrorKey: model.MsgDiscoverMappingInvalid, ErrorArgs: []string{"mapping.lastSendColumn", model.ValRequired}}
	ds.dmu.Unlock()
	dd.click(t, idDiscCheck)
	wait("check_mapping failed", 5)
	if r := ds.last(); r.Cmd != model.CmdCheckMapping || r.Mapping == nil || r.Mapping.Table != "_Node30" {
		t.Errorf("check request %+v", r)
	}
	if dd.lastField != "mapping.lastSendColumn" || !strings.Contains(dd.text(idDiscResult), i18n.Message(model.MsgDiscoverMappingInvalid, i18n.RU)) {
		t.Errorf("failed check: field %q result %q", dd.lastField, dd.text(idDiscResult))
	}
	// manual SQL template, ok with preview rows
	dd.template(0)
	if dd.d.Map.Mode != model.MappingManual || dd.text(idDiscSQL) == "" || strings.Contains(strings.ReplaceAll(dd.text(idDiscSQL), "\r\n", ""), "\n") {
		t.Errorf("template: mode %q sql %q", dd.d.Map.Mode, dd.text(idDiscSQL))
	}
	at := time.Date(2026, 10, 6, 9, 0, 0, 0, model.DBZone)
	ds.dmu.Lock()
	ds.check = model.MappingCheck{OK: true, SampleRows: []model.SampleRow{{Point: "A", LastSend: &at, LastRecv: &at}}}
	ds.dmu.Unlock()
	dd.click(t, idDiscCheck)
	wait("check_mapping ok", 6)
	if r := ds.last(); r.Mapping == nil || r.Mapping.Mode != model.MappingManual || r.Mapping.ManualSQL != strings.TrimSpace(model.SQLManualTemplates()[0].SQL) {
		t.Errorf("manual check request %+v", r.Mapping)
	}
	if !strings.HasPrefix(dd.text(idDiscResult), i18n.T(i18n.KeyDiscCheckOK, i18n.RU)) || !strings.Contains(dd.text(idDiscResult), "A: 06.10.2026 09:00") {
		t.Errorf("ok result %q", dd.text(idDiscResult))
	}

	// closing the dialog cancels a running request and enables the owner
	ds.setBlock(true)
	dd.click(t, idDiscFind)
	pumpUntil(t, "discover running 2", func() bool { n, _ := ds.n(); return n == 7 })
	winapi.SendMessage(dd.hwnd, winapi.WM_CLOSE, 0, 0)
	pumpUntil(t, "canceled on close", func() bool { _, c := ds.n(); return c == 2 })
	if w.disc != nil || !winapi.IsWindowEnabled(w.hwnd) || winapi.IsWindow(dd.hwnd) {
		t.Fatal("dialog not closed / owner disabled")
	}
	ds.setBlock(false)

	// Apply: the mapping (and all plans) as an unsaved edit, then Save
	w.click(t, idSetDiscover)
	dd = w.disc
	if dd == nil {
		t.Fatal("dialog not reopened")
	}
	dd.click(t, idDiscFind)
	wait("discover 2", 8)
	dd.set(idDiscPlanLabel, "Магазины")
	if !winapi.IsWindowEnabled(dd.ctl[idDiscAllPlans]) {
		t.Fatal("«все планы обмена» disabled with two plans")
	}
	winapi.SendMessage(dd.ctl[idDiscAllPlans], winapi.BM_SETCHECK, winapi.BST_CHECKED, 0)
	dd.profileID = "other"
	dd.click(t, idDiscApply)
	if w.disc == nil || dd.text(idDiscState) != i18n.T(i18n.KeyDiscProfileChanged, i18n.RU) {
		t.Fatalf("apply to another profile: %q", dd.text(idDiscState))
	}
	dd.profileID = "p1"
	dd.click(t, idDiscApply)
	if w.disc != nil || w.ed.MapEdit == nil || !w.ed.Unsaved() || w.statusText() != i18n.T(i18n.KeyDiscApplied, i18n.RU) {
		t.Fatalf("applied: status %q", w.statusText())
	}
	saves := ds.count("save")
	w.click(t, idSetSave)
	pumpUntil(t, "saved", func() bool { return ds.count("save") == saves+1 && !w.busy })
	sc := ds.lastSave()
	if sc.rev != "r1" || sc.p.Mapping.Table != "_Node10" || sc.p.Mapping.PlanLabel != "Магазины" || len(sc.p.Mappings) != 1 ||
		sc.p.Mappings[0].Table != "_Node20" {
		t.Errorf("save_profile: rev %q mapping %+v mappings %+v", sc.rev, sc.p.Mapping, sc.p.Mappings)
	}
	if w.ed.MapEdit != nil || w.ed.Unsaved() {
		t.Error("the saved mapping is no longer an edit")
	}
}
