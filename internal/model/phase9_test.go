package model

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestCHK_CFG_05_ServerNameForbidden — named pipe / LPC / UNC targets are
// rejected by Profile.Validate (shared by tray and service).
func TestCHK_CFG_05_ServerNameForbidden(t *testing.T) {
	for s, want := range map[string]string{
		"np:srv":                  "np",
		`NP:\\srv\pipe\sql\query`: "np",
		"lpc:srv":                 "lpc",
		"LPC:.":                   "lpc",
		`\\srv\share`:             "unc",
		"//srv/share":             "unc",
		`srv\pipe\sql`:            "pipe",
		`srv\PIPE\x`:              "pipe",
		"srv":                     "",
		`srv\INST`:                "",
		"tcp:srv":                 "",
		"10.0.0.1":                "",
		"pipeline-host":           "",
	} {
		if got := ServerNameForbidden(s); got != want {
			t.Errorf("%q: %q, want %q", s, got, want)
		}
	}
	p := Profile{ID: "p1", Server: `np:\\srv\pipe\sql\query`, Database: "db", AuthMode: AuthWindows}
	fe := AsFieldError(p.Validate())
	if fe == nil || fe.Field != "server" || fe.Key != ValServerForbidden || len(fe.Args) != 1 || fe.Args[0] != "np" {
		t.Fatalf("validate: %+v", fe)
	}
	if MonitorMessage(ValServerForbidden, LangEN) == "" || MonitorMessage(MsgPasswordTargetChange, LangRU) == "" {
		t.Error("no text")
	}
}

// TestCHK_CFG_05_TargetChanged — host, instance or port changes require
// the password again.
func TestCHK_CFG_05_TargetChanged(t *testing.T) {
	base := Profile{Server: `srv\INST`, Login: "u", AuthMode: AuthSQL}
	for _, c := range []struct {
		mod  func(*Profile)
		want bool
	}{
		{func(p *Profile) {}, false},
		{func(p *Profile) { p.Server = `SRV\inst` }, false},
		{func(p *Profile) { p.Name, p.Database = "x", "y" }, false},
		{func(p *Profile) { p.Server = `other\INST` }, true},
		{func(p *Profile) { p.Server = `srv\B` }, true},
		{func(p *Profile) { p.Port = 1500 }, true},
	} {
		p := base
		c.mod(&p)
		if got := TargetChanged(p, base); got != c.want {
			t.Errorf("%+v: %v", p, got)
		}
	}
	a, b := Profile{Server: "srv"}, Profile{Server: "srv", Port: 1433}
	if TargetChanged(a, b) {
		t.Error("port 0 = 1433 without an instance")
	}
}

// TestCHK_SVC_03_JSONShapes — IPC uses the full field names (EC Tray).
func TestCHK_SVC_03_JSONShapes(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, DBZone)
	lvl, lag, ms, seq, last := LevelWarning, 5, int64(7), uint64(9), uint64(3)
	m := IPCMessage{ID: 1, Type: MsgHistory, History: &HistoryPage{Records: []HistoryRecord{
		{Kind: HistoryKindPoint, CheckedAt: now, ProfileID: "db1", PointKey: "k", PointName: "n", Level: &lvl, FreshAt: &now, LagMinutes: &lag},
		{Kind: HistoryKindProfile, CheckedAt: now, ProfileID: "db1", Level: &lvl, LatencyMs: &ms, Status: HistoryStatusOK, ErrorKey: "x"},
	}, NextCursor: "c", Truncated: true, TruncatedReason: TruncatedSize}, RetryAfterMs: 5, CheckSeq: &seq, LastID: &last, RunID: "r"}
	b, _ := json.Marshal(m)
	for _, k := range []string{`"history":{"records":[`, `"kind":"point"`, `"checkedAt":"2026-10-13T12:00:00+03:00"`, `"profileId":"db1"`,
		`"pointKey":"k"`, `"pointName":"n"`, `"level":"warning"`, `"freshAt"`, `"lagMinutes":5`, `"latencyMs":7`, `"status":"ok"`,
		`"errorKey":"x"`, `"nextCursor":"c"`, `"truncatedReason":"size"`, `"retryAfterMs":5`, `"checkSeq":9`, `"lastId":3`, `"runId":"r"`} {
		if !strings.Contains(string(b), k) {
			t.Errorf("missing %s in %s", k, b)
		}
	}
	empty, _ := json.Marshal(IPCMessage{Type: MsgOK})
	for _, k := range []string{"history", "retryAfterMs", "checkSeq", "events", "runId", "lastId", "reset"} {
		if strings.Contains(string(empty), k) {
			t.Errorf("%s not omitted: %s", k, empty)
		}
	}
	var req IPCRequest
	if err := json.Unmarshal([]byte(`{"id":1,"cmd":"history","profileId":"db1","from":"2026-10-12T00:00:00+03:00","to":"2026-10-13T00:00:00+03:00","kinds":["event"],"limit":10,"cursor":"x","runId":"r","sinceId":4}`), &req); err != nil ||
		req.From == nil || req.To == nil || req.Cursor != "x" || len(req.Kinds) != 1 || req.RunID != "r" || req.SinceID != 4 {
		t.Errorf("request %+v %v", req, err)
	}
}
