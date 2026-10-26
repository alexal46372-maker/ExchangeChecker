package trayui

import (
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

func TestCHK_TRAY_53_SummaryBucketsGrey(t *testing.T) {
	warn := model.LevelWarning
	s := model.Summary{
		From: time.Unix(0, 0), To: time.Unix(3600, 0), BucketMinutes: 30,
		Profiles: []model.SummaryProfile{{
			ProfileID: "p", Name: "DB", Level: model.LevelOK,
			Buckets: []model.SummaryBucket{{}, {Level: &warn}, {}},
		}},
	}
	ch := BuildSummaryChart(s)
	if len(ch.Buckets) != 3 {
		t.Fatalf("buckets %d", len(ch.Buckets))
	}
	if BucketColor(ch.Buckets[0].Level) != ColorGrey {
		t.Fatalf("empty bucket not grey")
	}
	if BucketColor(ch.Buckets[1].Level) != ColorYellow {
		t.Fatalf("warn bucket %v", BucketColor(ch.Buckets[1].Level))
	}
	cards := BuildSummaryCards(s)
	if len(cards) != 1 || cards[0].Name != "DB" {
		t.Fatalf("cards %+v", cards)
	}
}
