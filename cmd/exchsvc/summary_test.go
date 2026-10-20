package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_SVC_07_Summary — summary (capability summary): defaults 24 h /
// 48 buckets, one entry per configured profile with every bucket, the
// current level / freshness and the polled bucket filled from the
// history; bad hours / buckets -> invalid_request + monitor.bad_request.
func TestCHK_SVC_07_Summary(t *testing.T) {
	dir := t.TempDir()
	cs := config.NewStore(dir)
	cs.Protector = plainProtector{}
	p0 := model.NewProfile("db1")
	p0.Name, p0.Server, p0.Database, p0.AuthMode = "Склад", "srv", "db", model.AuthWindows
	p0.Mapping = model.Mapping{Table: "_Node1", PointNameColumn: "_Description", LastSendColumn: "s"}
	if err := cs.SaveProfiles([]model.Profile{p0}); err != nil {
		t.Fatal(err)
	}
	s := startSvcWith(t, dir, 40, nil, &fakeEvents{}, nil)
	defer s.stop()
	hello := s.waitPush(t, func(m model.IPCMessage) bool { return m.Type == model.MsgHello })
	if !strings.Contains(","+strings.Join(hello.Capabilities, ",")+",", ",summary,") {
		t.Fatalf("caps %v", hello.Capabilities)
	}
	st := s.polled(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, req := range []model.IPCRequest{{Hours: 25}, {Buckets: 289}, {Hours: 1, Buckets: 7}} {
		req.Cmd = model.CmdSummary
		if r, _ := s.c.Do(ctx, req); r.Code != model.ErrCodeInvalidRequest || r.ErrorKey != model.MsgMonitorBadRequest {
			t.Errorf("%+v: %+v", req, r)
		}
	}
	var sum *model.Summary
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		r, err := s.c.Do(ctx, model.IPCRequest{Cmd: model.CmdSummary})
		if err != nil || r.Type != model.MsgSummary || r.Summary == nil {
			t.Fatalf("summary %+v %v", r, err)
		}
		sum = r.Summary
		if len(sum.Profiles) == 1 && sum.Profiles[0].Buckets[47].Level != nil || time.Now().After(deadline) {
			break
		}
	}
	b, _ := json.Marshal(sum)
	if sum.BucketMinutes != 30 || sum.To.Sub(sum.From) != 24*time.Hour || len(sum.Profiles) != len(st.Profiles) || sum.Truncated {
		t.Fatalf("summary %s", b)
	}
	p := sum.Profiles[0]
	if p.ProfileID != st.Profiles[0].ProfileID || p.Level != model.LevelWarning || p.FreshnessMin == nil || *p.FreshnessMin != 40 ||
		len(p.Buckets) != 48 || p.Buckets[47].Level == nil || *p.Buckets[47].Level != model.LevelWarning ||
		p.Buckets[47].LagMaxMin == nil || *p.Buckets[47].LagMaxMin != 40 || p.Buckets[0].Level != nil || p.LastOKAt != nil {
		t.Fatalf("profile %s", b)
	}
	if r, _ := s.c.Do(ctx, model.IPCRequest{Cmd: model.CmdSummary, Hours: 1, Buckets: 12}); r.Summary == nil ||
		r.Summary.BucketMinutes != 5 || len(r.Summary.Profiles[0].Buckets) != 12 {
		t.Fatalf("1h/12 %+v", r.Summary)
	}
}
