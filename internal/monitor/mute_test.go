package monitor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_MON_20_MuteUntilRecovered — mute suppresses Sink (Event Log /
// Viber / IPC alert) and alert_repeat while polling continues; status
// shows muted + mutedSince; unmute and return to OK clear the mute;
// state persists in mute-state.json; errors not_found / no_active_mute.
func TestCHK_MON_20_MuteUntilRecovered(t *testing.T) {
	store := &MemMuteStore{}
	e := newAckEnv(t, &MemAckStore{}, fast)
	e.m.opt.Mutes = store
	e.m.mutes = map[string]MuteRecord{}
	var reps []model.AlertEvent
	e.m.opt.OnAlertRepeat = func(ev model.AlertEvent) {
		e.mu.Lock()
		reps = append(reps, ev)
		e.mu.Unlock()
	}

	s := e.poll(45) // warning (default thresholds 30/60)
	if pt := pointA(s); pt.Level != model.LevelWarning {
		t.Fatalf("setup %+v", pt)
	}
	e.mu.Lock()
	e.evs = nil
	reps = nil
	e.mu.Unlock()

	if err := e.m.MuteUntilRecovered("a"); err != nil {
		t.Fatal(err)
	}
	s = e.m.Snapshot()
	ps := profileByID(s, "a")
	if !ps.Muted || ps.MutedSince == nil {
		t.Fatalf("muted flags %+v", ps)
	}
	raw, _ := json.Marshal(ps)
	if !strings.Contains(string(raw), `"muted":true`) || !strings.Contains(string(raw), `"mutedSince"`) {
		t.Fatalf("json %s", raw)
	}

	// still warning while muted: no Sink, no alert_repeat
	for i := 0; i < 3; i++ {
		e.poll(45)
	}
	e.mu.Lock()
	nEvs, nReps := len(e.evs), len(reps)
	e.mu.Unlock()
	if nEvs != 0 || nReps != 0 {
		t.Fatalf("outbound while muted: sink=%d reps=%d", nEvs, nReps)
	}
	if !profileByID(e.m.Snapshot(), "a").Muted {
		t.Fatal("mute lost while still warning")
	}
	// polling continued (checkedAt moves)
	if !profileByID(e.m.Snapshot(), "a").Polled {
		t.Fatal("not polled")
	}

	// idempotent mute
	if err := e.m.MuteUntilRecovered("a"); err != nil {
		t.Fatal(err)
	}

	// unmute
	if err := e.m.UnmuteProfile("a"); err != nil {
		t.Fatal(err)
	}
	if profileByID(e.m.Snapshot(), "a").Muted {
		t.Fatal("still muted after unmute")
	}
	if err := e.m.UnmuteProfile("a"); codeOf(err) != model.ErrCodeNoActiveMute {
		t.Fatalf("no_active_mute: %v", err)
	}
	if err := e.m.MuteUntilRecovered("zzz"); codeOf(err) != model.ErrCodeNotFound {
		t.Fatalf("not_found: %v", err)
	}

	// mute again, then recover -> auto clear
	if err := e.m.MuteUntilRecovered("a"); err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	e.evs = nil
	e.mu.Unlock()
	e.poll(1) // OK
	ps = profileByID(e.m.Snapshot(), "a")
	if ps.Muted || ps.Level != model.LevelOK {
		t.Fatalf("auto clear %+v", ps)
	}
	// recovery may emit via Sink once mute cleared in applyMutes before emit
	// (acceptable); mute file empty
	recs, _ := store.Load()
	if len(recs) != 0 {
		t.Fatalf("mutes after recover %+v", recs)
	}
}

// TestCHK_MON_20_MuteSurvivesRestart — mute-state.json next to ack-state
// is reloaded after New.
func TestCHK_MON_20_MuteSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	log := nopLogger{}
	ms := OpenFileMuteStore(dir, log)
	e := newAckEnv(t, OpenFileAckStore(dir, log), fast)
	e.m.opt.Mutes = ms
	e.m.mutes = map[string]MuteRecord{}
	e.m.loadMutes()
	e.poll(120)
	if err := e.m.MuteUntilRecovered("a"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, MutesFile)); err != nil {
		t.Fatal(err)
	}
	// new monitor, same store
	e2 := newAckEnv(t, OpenFileAckStore(dir, log), fast)
	e2.m.opt.Mutes = OpenFileMuteStore(dir, log)
	e2.m.mutes = map[string]MuteRecord{}
	e2.m.loadMutes()
	e2.poll(120)
	ps := profileByID(e2.m.Snapshot(), "a")
	if !ps.Muted || ps.MutedSince == nil {
		t.Fatalf("after restart %+v", ps)
	}
}

// TestCHK_CFG_07_HasTokenAlwaysInViews — get_config keeps hasToken:false
// present (CHK-SVC-06); export_config with includeViberRecipients still
// omits hasToken/status (ForExport).
func TestCHK_CFG_07_HasTokenAlwaysInViews(t *testing.T) {
	e := newCfgEnv(t)
	view, _, _ := e.m.GetConfig()
	b, _ := json.Marshal(view.Settings.Viber)
	if !strings.Contains(string(b), `"hasToken":false`) {
		t.Fatalf("get_config viber %s", b)
	}
	st := model.DefaultSettings()
	st.Viber.Enabled = true
	st.Viber.Recipients = []model.ViberRecipient{{ID: "r1", Name: "A"}}
	if _, err := e.m.SaveSettingsViber(st, "TOK", false, e.rev(), "a"); err != nil {
		t.Fatal(err)
	}
	x, err := e.m.ExportConfigOpts(true)
	raw, _ := json.Marshal(x)
	if err != nil || strings.Contains(string(raw), `"hasToken"`) || strings.Contains(string(raw), `"status"`) {
		t.Fatalf("export %s %v", raw, err)
	}
}
