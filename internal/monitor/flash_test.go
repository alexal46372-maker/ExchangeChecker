package monitor

import (
	"context"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// flashHarness is one enabled profile, no Run() (no workers), a real
// config.Store so SetEnabled goes through reloadConfigLocked.
type flashHarness struct {
	t       *testing.T
	m       *Monitor
	f       *fakeFactory
	flashes []model.FlashEvent
	profile model.Profile
}

func newFlashHarness(t *testing.T) *flashHarness {
	t.Helper()
	st := config.NewStore(t.TempDir())
	st.Protector = plainProtector{}
	p := prof("db1")
	if err := st.SaveProfiles([]model.Profile{p}); err != nil {
		t.Fatal(err)
	}
	f := newFactory()
	f.set("db1", behavior{points: lagPoints(map[string]int{"Магазин 1": 600})})
	h := &flashHarness{t: t, f: f, profile: p}
	opt := baseOpt(f, FileConfig{Store: st}, &fakeClock{t: t0})
	opt.OnFlash = func(ev model.FlashEvent) { h.flashes = append(h.flashes, ev) }
	h.m = New(opt)
	h.m.reloadConfig(true)
	if n := h.m.Workers(); n != 0 {
		t.Fatalf("workers started outside Run: %d", n)
	}
	return h
}

func (h *flashHarness) pollOnce() {
	h.t.Helper()
	before := h.f.get(h.f.calls, "db1")
	h.m.pollAll(context.Background())
	if h.f.get(h.f.calls, "db1") != before+1 {
		h.t.Fatalf("poll did not run: calls %d", h.f.get(h.f.calls, "db1"))
	}
}

func (h *flashHarness) last() model.FlashEvent {
	h.t.Helper()
	if len(h.flashes) == 0 {
		h.t.Fatal("no flash events")
	}
	return h.flashes[len(h.flashes)-1]
}

func (h *flashHarness) assertNoWorkersOrExtraPolls(calls int) {
	h.t.Helper()
	if n := h.m.Workers(); n != 0 {
		h.t.Fatalf("workers %d", n)
	}
	if got := h.f.get(h.f.calls, "db1"); got != calls {
		h.t.Fatalf("unexpected poll: calls %d want %d", got, calls)
	}
}

// TestCHK_MON_23_DisableOnlyBaseFlashStop — disabling the only base
// emits flash_stop from set_enabled / reload itself, without any other
// profile's poll and without a later poll of this base (CHK-MON-23).
func TestCHK_MON_23_DisableOnlyBaseFlashStop(t *testing.T) {
	h := newFlashHarness(t)
	h.pollOnce()
	if ev := h.last(); ev.Stop || ev.EventID == 0 || ev.ProfileID != "db1" || ev.Level != model.LevelError {
		t.Fatalf("want error flash, got %+v", ev)
	}
	id := h.last().EventID
	calls := h.f.get(h.f.calls, "db1")
	if _, err := h.m.SetEnabled("db1", false, "", "test"); err != nil {
		t.Fatal(err)
	}
	h.assertNoWorkersOrExtraPolls(calls)
	if len(h.m.tracker.Active()) != 0 {
		t.Fatalf("disabled alerts still in tracker: %+v", h.m.tracker.Active())
	}
	if ev := h.last(); len(h.flashes) != 2 || !ev.Stop || ev.ProfileID != "db1" || ev.EventID != id {
		t.Fatalf("want flash_stop of %d, got %+v", id, h.flashes)
	}
}

// TestCHK_MON_23_DismissDisableEnableFlashesAgain — dismiss_flash, then
// disable and re-enable the same base: the dismiss does not survive, a
// new eventID is raised and it flashes again (CHK-MON-23, CHK-IPC-21).
func TestCHK_MON_23_DismissDisableEnableFlashesAgain(t *testing.T) {
	h := newFlashHarness(t)
	h.pollOnce()
	id := h.last().EventID
	if err := h.m.DismissFlash(id); err != nil {
		t.Fatal(err)
	}
	n := len(h.flashes)
	h.pollOnce() // still-failing check: dismissed, no new flash
	if len(h.flashes) != n {
		t.Fatalf("dismissed alert flashed again: %+v", h.flashes[n:])
	}
	calls := h.f.get(h.f.calls, "db1")
	if _, err := h.m.SetEnabled("db1", false, "", "test"); err != nil {
		t.Fatal(err)
	}
	h.assertNoWorkersOrExtraPolls(calls)
	if ev := h.last(); !ev.Stop || ev.EventID != id {
		t.Fatalf("want flash_stop of dismissed %d, got %+v", id, ev)
	}
	if _, err := h.m.SetEnabled("db1", true, "", "test"); err != nil {
		t.Fatal(err)
	}
	h.assertNoWorkersOrExtraPolls(calls) // enable does not poll by itself outside Run
	afterEnable := len(h.flashes)
	h.pollOnce()
	if ev := h.last(); len(h.flashes) != afterEnable+1 || ev.Stop || ev.EventID == 0 || ev.EventID == id || ev.ProfileID != "db1" {
		t.Fatalf("want a new flash after re-enable, got %+v (old id %d)", h.flashes, id)
	}
}

// TestCHK_MON_24_SuspendResumeSameEvent — dismiss_flash of an active
// alert emits flash_stop and silences further polls; resume_flash of
// the same unacked eventID immediately flashes again, and later polls
// keep flashing with that eventID. After recovery, a new error flashes
// with a new eventID (CHK-MON-24).
func TestCHK_MON_24_SuspendResumeSameEvent(t *testing.T) {
	h := newFlashHarness(t)
	h.pollOnce()
	id := h.last().EventID
	if id == 0 || h.last().Stop {
		t.Fatalf("want flash, got %+v", h.last())
	}
	if err := h.m.DismissFlash(id); err != nil {
		t.Fatal(err)
	}
	if ev := h.last(); !ev.Stop || ev.EventID != id || ev.ProfileID != "db1" {
		t.Fatalf("suspend want flash_stop of %d, got %+v", id, ev)
	}
	n := len(h.flashes)
	h.pollOnce()
	if len(h.flashes) != n {
		t.Fatalf("flash after suspend: %+v", h.flashes[n:])
	}
	ok, err := h.m.ResumeFlash(id)
	if err != nil || !ok {
		t.Fatalf("resume of active event: ok=%v err=%v", ok, err)
	}
	if ev := h.last(); ev.Stop || ev.EventID != id || ev.Level != model.LevelError || ev.ProfileID != "db1" {
		t.Fatalf("resume want flash of %d, got %+v", id, ev)
	}
	n = len(h.flashes)
	h.pollOnce()
	if ev := h.last(); len(h.flashes) != n+1 || ev.Stop || ev.EventID != id {
		t.Fatalf("repeat after resume: %+v (id %d)", h.flashes[n:], id)
	}
	// recovery (default ClearPolls=2), then a new error → new eventID flashes
	h.f.set("db1", behavior{points: lagPoints(map[string]int{"Магазин 1": 0})})
	h.pollOnce()
	h.pollOnce()
	if ev := h.last(); !ev.Stop || ev.EventID != id {
		t.Fatalf("recovery want flash_stop of %d, got %+v", id, ev)
	}
	h.f.set("db1", behavior{points: lagPoints(map[string]int{"Магазин 1": 600})})
	after := len(h.flashes)
	h.pollOnce()
	if ev := h.last(); len(h.flashes) != after+1 || ev.Stop || ev.EventID == 0 || ev.EventID == id {
		t.Fatalf("want a new flash after recovery, got %+v (old id %d)", h.flashes[after:], id)
	}
}

// TestCHK_MON_24_ResumeNoEffect — resume_flash after ack, after the
// alert has cleared, or for another eventID is a no-op (resumed=false,
// no flash). resume after ack must do nothing even if the event was
// never suspended (CHK-MON-24).
func TestCHK_MON_24_ResumeNoEffect(t *testing.T) {
	h := newFlashHarness(t)
	h.pollOnce()
	id := h.last().EventID
	n := len(h.flashes)

	ok, err := h.m.ResumeFlash(id + 1)
	if err != nil || ok {
		t.Fatalf("other eventId: ok=%v err=%v", ok, err)
	}
	if len(h.flashes) != n {
		t.Fatalf("resume of other eventId flashed: %+v", h.flashes[n:])
	}

	if _, err := h.m.AckEvent(id); err != nil {
		t.Fatal(err)
	}
	ok, err = h.m.ResumeFlash(id)
	if err != nil || ok {
		t.Fatalf("resume after ack (no dismiss): ok=%v err=%v", ok, err)
	}
	if len(h.flashes) != n {
		t.Fatalf("resume after ack flashed: %+v", h.flashes[n:])
	}

	h2 := newFlashHarness(t)
	h2.pollOnce()
	id2 := h2.last().EventID
	if err := h2.m.DismissFlash(id2); err != nil {
		t.Fatal(err)
	}
	if _, err := h2.m.AckEvent(id2); err != nil {
		t.Fatal(err)
	}
	n2 := len(h2.flashes)
	ok, err = h2.m.ResumeFlash(id2)
	if err != nil || ok {
		t.Fatalf("resume after dismiss+ack: ok=%v err=%v", ok, err)
	}
	if len(h2.flashes) != n2 {
		t.Fatalf("resume after dismiss+ack flashed: %+v", h2.flashes[n2:])
	}

	h3 := newFlashHarness(t)
	h3.pollOnce()
	id3 := h3.last().EventID
	if err := h3.m.DismissFlash(id3); err != nil {
		t.Fatal(err)
	}
	h3.f.set("db1", behavior{points: lagPoints(map[string]int{"Магазин 1": 0})})
	h3.pollOnce()
	h3.pollOnce()
	if ev := h3.last(); !ev.Stop || ev.EventID != id3 {
		t.Fatalf("clear want flash_stop of %d, got %+v", id3, ev)
	}
	n3 := len(h3.flashes)
	ok, err = h3.m.ResumeFlash(id3)
	if err != nil || ok {
		t.Fatalf("resume after clear: ok=%v err=%v", ok, err)
	}
	if len(h3.flashes) != n3 {
		t.Fatalf("resume after clear flashed: %+v", h3.flashes[n3:])
	}
}
