package model

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestCHK_CFG_05_ValidateSQLServerAddr — named pipe / LPC / DAC / UNC
// targets are rejected by ValidateSQLServerAddr and Profile.Validate
// (shared by tray, service and later EC SQL).
func TestCHK_CFG_05_ValidateSQLServerAddr(t *testing.T) {
	for s, want := range map[string]string{
		"np:srv":                  "np",
		`NP:\\srv\pipe\sql\query`: "np",
		"lpc:srv":                 "lpc",
		"LPC:.":                   "lpc",
		`\\srv\share`:             "unc",
		"//srv/share":             "unc",
		`srv\pipe\sql`:            "pipe",
		`srv\PIPE\x`:              "pipe",
		"admin:srv":               "admin",
		"Admin:srv\\INST":         "admin",
		`\srv`:                    "backslash",
		`\.\pipe\sql`:             "backslash",
		"srv":                     "",
		`srv\INST`:                "",
		"tcp:srv":                 "tcp",
		"TCP:srv,1433":            "tcp",
		"sm:srv":                  "sm",
		"Via:srv":                 "via",
		"/srv":                    "slash",
		"tcpserver":               "",
		"10.0.0.1":                "",
		"pipeline-host":           "",
	} {
		err := ValidateSQLServerAddr(s)
		fe := AsFieldError(err)
		got := ""
		if fe != nil && fe.Key == ValSQLServerForbidden && len(fe.Args) == 1 {
			got = fe.Args[0]
		} else if err != nil {
			got = "?" + err.Error()
		}
		if got != want {
			t.Errorf("%q: %q, want %q", s, got, want)
		}
	}
	p := Profile{ID: "p1", Server: `np:\\srv\pipe\sql\query`, Database: "db", AuthMode: AuthWindows}
	fe := AsFieldError(p.Validate())
	if fe == nil || fe.Field != "server" || fe.Key != ValSQLServerForbidden || len(fe.Args) != 1 || fe.Args[0] != "np" {
		t.Fatalf("validate: %+v", fe)
	}
	if MonitorMessage(ValSQLServerForbidden, LangEN) == "" || MonitorMessage(MsgPasswordTargetChange, LangRU) == "" {
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
		{Kind: HistoryKindPoint, CheckedAt: now, ProfileID: "db1", PointKey: "k", PointName: "n", Level: &lvl, FreshAt: &now, LagMin: &lag},
		{Kind: HistoryKindProfile, CheckedAt: now, ProfileID: "db1", Level: &lvl, LatencyMs: &ms, Status: HistoryStatusOK, ErrorKey: "x"},
	}, NextCursor: "c", Truncated: true, TruncatedReason: TruncatedSize}, RetryAfterMs: 5, CheckSeq: &seq, LastID: &last, RunID: "r"}
	b, _ := json.Marshal(m)
	for _, k := range []string{`"history":{"records":[`, `"kind":"point"`, `"checkedAt":"2026-10-13T12:00:00+03:00"`, `"profileId":"db1"`,
		`"pointKey":"k"`, `"pointName":"n"`, `"level":"warning"`, `"freshAt"`, `"lagMin":5`, `"latencyMs":7`, `"status":"ok"`,
		`"errorKey":"x"`, `"nextCursor":"c"`, `"truncatedReason":"size"`, `"retryAfterMs":5`, `"checkSeq":9`, `"lastId":3`, `"runId":"r"`, `"hasMore":false`, `"id":""`} {
		if !strings.Contains(string(b), k) {
			t.Errorf("missing %s in %s", k, b)
		}
	}
	empty, _ := json.Marshal(IPCMessage{Type: MsgOK})
	for _, k := range []string{"history", "retryAfterMs", "checkSeq", "events", "runId", "lastId", "resetRequired"} {
		if strings.Contains(string(empty), k) {
			t.Errorf("%s not omitted: %s", k, empty)
		}
	}
	var req IPCRequest
	if err := json.Unmarshal([]byte(`{"id":1,"cmd":"history","profileId":"db1","from":"2026-10-12T00:00:00+03:00","to":"2026-10-13T00:00:00+03:00","kinds":["event"],"limit":10,"cursor":"x","runId":"r","sinceId":4,"bucket":"5m"}`), &req); err != nil || req.Bucket != HistoryBucket5m ||
		req.From == nil || req.To == nil || req.Cursor != "x" || len(req.Kinds) != 1 || req.RunID != "r" || req.SinceID != 4 {
		t.Errorf("request %+v %v", req, err)
	}
	d := int64(4)
	ev, _ := json.Marshal(IPCMessage{Type: MsgEvents, ResetRequired: true, Events: []AlertEvent{{ID: 2, EventID: 2,
		Kind: AlertKindRecovered, DurationMin: &d, From: LevelError, To: LevelOK}}})
	for _, k := range []string{`"resetRequired":true`, `"eventId":2`, `"kind":"recovered"`, `"durationMin":4`} {
		if !strings.Contains(string(ev), k) {
			t.Errorf("missing %s in %s", k, ev)
		}
	}
	st, _ := json.Marshal(StatusSnapshot{ServerNow: now, LastCheckSeq: 3, Profiles: []ProfileSnapshot{{LastQueryMs: &ms}}})
	for _, k := range []string{`"serverNow":"2026-10-13T12:00:00+03:00"`, `"lastCheckSeq":3`, `"lastQueryMs":7`} {
		if !strings.Contains(string(st), k) {
			t.Errorf("missing %s in %s", k, st)
		}
	}
}
