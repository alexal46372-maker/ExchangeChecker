package trayui

import (
	"strings"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

func u64(v uint64) *uint64 { return &v }

// CHK-TRAY-22: check_now replies and the wait for lastCheckSeq.
func TestCheckState(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, model.DBZone)
	r := ParseCheckReply(model.IPCMessage{Type: model.MsgError, Code: model.ErrCodeRateLimited, ErrorKey: model.MsgCheckRateLimited, RetryAfterMs: 12001}, nil, true, "ru")
	if !r.Warn || r.WaitSeq || r.Text != "Проверка уже запрашивалась недавно, повторите через 13 с" {
		t.Fatalf("rate limited %+v", r)
	}
	if r := ParseCheckReply(model.IPCMessage{Type: model.MsgError, ErrorKey: model.MsgMonitorProfileOff}, nil, true, "en"); !strings.Contains(r.Text, "disabled") {
		t.Errorf("error %+v", r)
	}
	var c CheckState
	c.Start(now)
	r = ParseCheckReply(model.IPCMessage{Type: model.MsgOK, Result: model.ResultAccepted, CheckSeq: u64(7)}, nil, true, "en")
	c.Reply(r)
	if !c.Busy || !r.WaitSeq || r.Text != "Check in progress…" {
		t.Fatalf("accepted %+v %+v", c, r)
	}
	if d, _ := c.Status(&model.StatusSnapshot{Seq: 8, LastCheckSeq: 3}, now); d || !c.Busy {
		t.Fatal("old lastCheckSeq must not end the wait")
	}
	if d, _ := c.Status(&model.StatusSnapshot{Seq: 9, LastCheckSeq: 7}, now); !d || c.Busy {
		t.Fatal("lastCheckSeq >= checkSeq ends the wait")
	}
	c.Start(now)
	c.Reply(ParseCheckReply(model.IPCMessage{Type: model.MsgOK, Result: model.ResultCheckCoalesced, CheckSeq: u64(9)}, nil, true, "en"))
	if _, to := c.Status(nil, now.Add(CheckWait)); !to || c.Busy {
		t.Fatal("timeout")
	}
	c.Start(now) // poll_now fallback: the reply ends it
	r = ParseCheckReply(model.IPCMessage{Type: model.MsgOK, Result: model.ResultAccepted}, nil, false, "ru")
	c.Reply(r)
	if c.Busy || r.Text != "Проверка запрошена" {
		t.Fatalf("poll_now %+v", c)
	}
}

// CHK-TRAY-23: «назад» from serverNow.
func TestServerClock(t *testing.T) {
	local := time.Date(2026, 10, 13, 12, 0, 0, 0, model.DBZone)
	var c ServerClock
	c.Update(&model.StatusSnapshot{Seq: 1, ServerNow: local.Add(5 * time.Minute)}, local)
	if !c.Known || c.Now(local.Add(time.Minute)) != local.Add(6*time.Minute) {
		t.Fatalf("offset %+v", c)
	}
	fresh := local.Add(-30 * time.Minute)
	if s, _ := AgoText(&fresh, c.Now(local), "ru"); s != "35 мин назад" {
		t.Errorf("ago %q", s)
	}
	c.Update(&model.StatusSnapshot{Seq: 2}, local) // older service
	if c.Known || c.Offset != 0 {
		t.Error("no serverNow = local clock")
	}
}

// CHK-TRAY-24: flapping overdue / recovery gives no balloon storm.
func TestDebouncer(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, model.DBZone)
	d := Debouncer{Window: DebounceWindow(60)}
	if d.Window != 2*time.Minute || DebounceWindow(240) != 8*time.Minute || DebounceWindow(3600) != 10*time.Minute {
		t.Fatal("window")
	}
	ev := func(from, to model.AlertLevel) model.AlertEvent {
		return model.AlertEvent{ProfileID: "a", ProfileName: "ERP", PointName: "Склад", Key: "k", From: from, To: to, LagMinutes: 40}
	}
	if b := d.Feed(ev(model.LevelOK, model.LevelWarning), now, "ru"); len(b) != 1 {
		t.Fatal("first overdue at once")
	}
	if b := d.Feed(ev(model.LevelWarning, model.LevelOK), now.Add(time.Minute), "ru"); len(b) != 0 {
		t.Fatal("recovery is held")
	}
	if b := d.Feed(ev(model.LevelOK, model.LevelWarning), now.Add(90*time.Second), "ru"); len(b) != 0 {
		t.Fatal("flap overdue suppressed")
	}
	if b := d.Due(now.Add(10 * time.Minute)); len(b) != 0 {
		t.Fatal("cancelled recovery must not appear")
	}
	if b := d.Feed(ev(model.LevelWarning, model.LevelError), now.Add(3*time.Minute), "ru"); len(b) != 1 || b[0].Level != model.LevelError {
		t.Fatal("escalation is shown")
	}
	d.Feed(ev(model.LevelError, model.LevelOK), now.Add(4*time.Minute), "ru")
	if _, ok := d.NextDue(); !ok {
		t.Fatal("pending")
	}
	if b := d.Due(now.Add(5 * time.Minute)); len(b) != 0 {
		t.Fatal("too early")
	}
	if b := d.Due(now.Add(6 * time.Minute)); len(b) != 1 || !b[0].Recovered {
		t.Fatal("confirmed recovery shown")
	}
	if b := d.Feed(ev(model.LevelOK, model.LevelWarning), now.Add(7*time.Minute), "ru"); len(b) != 1 {
		t.Fatal("new overdue after a shown recovery")
	}
}

// CHK-TRAY-25: events_since catch-up with dedup and resetRequired.
func TestEventTracker(t *testing.T) {
	var tr EventTracker
	if _, ok := tr.Request(); ok {
		t.Fatal("no run yet")
	}
	tr.SetRun("r1")
	e := func(id uint64) model.AlertEvent {
		return model.AlertEvent{RunID: "r1", EventID: id, ID: id, PointName: "p", From: model.LevelOK, To: model.LevelWarning}
	}
	if !tr.Observe(e(1)) || !tr.Observe(e(2)) || tr.Observe(e(2)) {
		t.Fatal("dedup")
	}
	req, _ := tr.Request()
	if req.Cmd != model.CmdEventsSince || req.RunID != "r1" || req.SinceID != 2 {
		t.Fatalf("req %+v", req)
	}
	fresh, more := tr.Apply(model.IPCMessage{Type: model.MsgEvents, RunID: "r1", LastID: u64(4), Truncated: true,
		Events: []model.AlertEvent{e(2), e(3), e(4)}})
	if len(fresh) != 2 || fresh[0].EventID != 3 || !more || tr.LastID != 4 {
		t.Fatalf("apply %v %v %d", fresh, more, tr.LastID)
	}
	fresh, more = tr.Apply(model.IPCMessage{Type: model.MsgEvents, RunID: "r2", LastID: u64(17), ResetRequired: true})
	if fresh != nil || more || tr.RunID != "r2" || tr.LastID != 17 {
		t.Fatalf("reset %+v", tr)
	}
	if !tr.Observe(model.AlertEvent{PointName: "x"}) {
		t.Fatal("events without ids pass")
	}
}

// CHK-TRAY-26: bucket pages merged by key, chart layout.
func TestHistoryChart(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, model.DBZone)
	from, to, bucket := SpanRange(SpanDay, now)
	if bucket != model.HistoryBucket5m || to.Sub(from) != 24*time.Hour {
		t.Fatal("day")
	}
	if _, _, b := SpanRange(SpanWeek, now); b != model.HistoryBucket1h {
		t.Fatal("week")
	}
	req := HistoryRequest("a", "k", from, to, bucket, nil, "c1")
	if req.Cmd != model.CmdHistory || req.ProfileID != "a" || req.PointKey != "k" || req.Cursor != "c1" || req.Limit != model.HistoryMaxLimit {
		t.Fatalf("req %+v", req)
	}
	s1 := from.Add(time.Hour)
	lvl := model.LevelWarning
	var bm BucketMerger
	bm.Add([]model.HistoryBucket{{Start: s1, Kind: model.HistoryKindPoint, ProfileID: "a", PointKey: "k", Records: 2, LagMin: &model.HistoryAgg{Min: 10, Max: 20, Avg: 15, Count: 2}}})
	bm.Add([]model.HistoryBucket{{Start: s1, Kind: model.HistoryKindPoint, ProfileID: "a", PointKey: "k", Records: 1, WorstLevel: &lvl, LagMin: &model.HistoryAgg{Min: 5, Max: 90, Avg: 30, Count: 1}},
		{Start: s1.Add(5 * time.Minute), Kind: model.HistoryKindPoint, ProfileID: "a", PointKey: "k", Records: 1, LagMin: &model.HistoryAgg{Min: 1, Max: 1, Avg: 1, Count: 1}}})
	bs := bm.Buckets()
	if len(bs) != 2 || bs[0].Records != 3 || bs[0].LagMin.Min != 5 || bs[0].LagMin.Max != 90 || bs[0].LagMin.Avg != 20 || *bs[0].WorstLevel != lvl {
		t.Fatalf("merge %+v", bs[0])
	}
	to2 := model.LevelOK
	ev := []model.HistoryRecord{{Kind: model.HistoryKindRecovered, CheckedAt: s1.Add(time.Minute), PointKey: "k", To: &to2},
		{Kind: model.HistoryKindEvent, CheckedAt: s1, PointKey: "other"}}
	c := BuildChart(from, to, bs, ev, "k", 30, 60)
	if len(c.Points) != 2 || len(c.Marks) != 1 || !c.Marks[0].Recov {
		t.Fatalf("chart %+v", c)
	}
	L := c.Layout(0, 0, 1000, 100, 5*time.Minute)
	if L.MaxY != 100 || L.WarnY != 70 || L.ErrY != 40 || len(L.Avg) != 1 || len(L.Avg[0]) != 2 || L.Avg[0][0].Y != 80 || L.MaxL[0][0].Y != 10 {
		t.Fatalf("layout %+v", L)
	}
	if len(L.XTicks) != 7 || L.XTicks[0].Text != "12:00" || len(L.Marks) != 1 {
		t.Fatalf("ticks %+v", L.XTicks)
	}
	if s := HistoryErrorText(model.IPCMessage{Type: model.MsgError, ErrorKey: model.MsgHistoryRangeTooLong}, nil, "ru"); s != "Интервал истории больше 31 дня." {
		t.Error(s)
	}
	for _, k := range []string{model.MsgHistoryDisabled, model.MsgHistoryBadRange, model.MsgHistoryBadCursor, model.MsgHistoryBadFilter, model.MsgHistoryBusy, model.MsgHistoryFailed} {
		if s := HistoryErrorText(model.IPCMessage{ErrorKey: k}, nil, "en"); s == "" || strings.Contains(s, k) {
			t.Errorf("%s: %q", k, s)
		}
	}
}

// CHK-TRAY-27: history export.
func TestHistoryExport(t *testing.T) {
	at := time.Date(2026, 10, 13, 12, 0, 5, 0, model.DBZone)
	lag, lat, dur := 42, int64(150), int64(17)
	w, ok := model.LevelWarning, model.LevelOK
	recs := []model.HistoryRecord{
		{Kind: model.HistoryKindPoint, CheckedAt: at, ProfileName: "ERP", PointName: "=Склад", Level: &w, LagMin: &lag},
		{Kind: model.HistoryKindProfile, CheckedAt: at, ProfileName: "<b>", LatencyMs: &lat},
		{Kind: model.HistoryKindRecovered, CheckedAt: at, ProfileName: "ERP", PointName: "p", From: &w, To: &ok, DurationMin: &dur},
	}
	c := string(ExportHistoryCSV(recs, "ru"))
	if !strings.HasPrefix(c, "\uFEFFВремя;База;Точка;") || strings.Count(c, "\r\n") != 4 ||
		!strings.Contains(c, "13.10.2026 12:00:05;ERP;'=Склад;опрос точки;предупреждение;42;") ||
		!strings.Contains(c, "предупреждение → норма;17;") || !strings.Contains(c, ";150;") {
		t.Fatalf("csv %q", c)
	}
	h := string(ExportHistoryHTML(recs, "ERP — p", at, at, "en"))
	if !strings.Contains(h, "&lt;b&gt;") || strings.Contains(h, "<b>") || !strings.Contains(h, "#fff3c4") || !strings.Contains(h, "recovery") {
		t.Fatalf("html %s", h)
	}
}
