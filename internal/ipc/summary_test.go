package ipc

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_SVC_07_FitSummary — over 64 KiB whole profiles are dropped from
// the end (truncated:true), kept profiles keep all buckets.
func TestCHK_SVC_07_FitSummary(t *testing.T) {
	t0 := time.Date(2026, 10, 20, 0, 0, 0, 0, model.DBZone)
	mk := func(n int) model.Summary {
		s := model.Summary{BucketMinutes: 5}
		for i := 0; i < n; i++ {
			p := model.SummaryProfile{ProfileID: "p" + string(rune('a'+i%26)) + string(rune('a'+i/26)), Name: "Профиль"}
			for j := 0; j < 288; j++ {
				l, lag := model.LevelWarning, 1000+j
				p.Buckets = append(p.Buckets, model.SummaryBucket{T: t0.Add(time.Duration(j) * 5 * time.Minute), Level: &l, LagMaxMin: &lag})
			}
			s.Profiles = append(s.Profiles, p)
		}
		return s
	}
	small := mk(1)
	fitSummary(&small, 7, model.IPCMaxInteractiveReply)
	if small.Truncated || len(small.Profiles) != 1 {
		t.Fatal("small truncated")
	}
	big := mk(20)
	fitSummary(&big, 7, model.IPCMaxInteractiveReply)
	b, _ := json.Marshal(model.IPCMessage{ID: 7, Type: model.MsgSummary, Summary: &big})
	if !big.Truncated || len(big.Profiles) == 0 || len(big.Profiles) >= 20 || len(b)+1 > model.IPCMaxInteractiveReply {
		t.Fatalf("truncated=%v kept=%d size=%d", big.Truncated, len(big.Profiles), len(b))
	}
	if big.Profiles[0].ProfileID != "paa" {
		t.Error("not dropped from the end")
	}
	for _, p := range big.Profiles {
		if len(p.Buckets) != 288 {
			t.Fatal("buckets shortened")
		}
	}
	// a single profile larger than the limit: empty list, truncated
	one := mk(1)
	fitSummary(&one, 7, 1000)
	if !one.Truncated || one.Profiles == nil || len(one.Profiles) != 0 {
		t.Fatal(one.Truncated, one.Profiles)
	}
}
