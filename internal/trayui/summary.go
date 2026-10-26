package trayui

import (
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// SummaryCard is one profile card of the summary dashboard (CHK-TRAY-53).
type SummaryCard struct {
	ProfileID    string
	Name         string
	Level        model.AlertLevel
	FreshnessMin *int
	LastOKAt     *time.Time
	Color        Color // card stripe / pale fill
}

// SummaryBucketPt is one painted bucket of the 24h chart.
type SummaryBucketPt struct {
	I     int
	Level *model.AlertLevel // nil = no data (grey)
	Lag   *int
}

// SummaryChart is the GDI model of the summary chart (one series of worst
// levels across profiles per bucket, or per-profile lanes).
type SummaryChart struct {
	Buckets []SummaryBucketPt
	From    time.Time
	To      time.Time
}

// BuildSummaryCards maps a Summary reply to cards (order preserved).
func BuildSummaryCards(s model.Summary) []SummaryCard {
	out := make([]SummaryCard, 0, len(s.Profiles))
	for _, p := range s.Profiles {
		out = append(out, SummaryCard{ProfileID: p.ProfileID, Name: p.Name, Level: p.Level,
			FreshnessMin: p.FreshnessMin, LastOKAt: p.LastOKAt, Color: colorOf(p.Level)})
	}
	return out
}

func colorOf(l model.AlertLevel) Color {
	switch l {
	case model.LevelError:
		return ColorRed
	case model.LevelWarning:
		return ColorYellow
	case model.LevelOK:
		return ColorGreen
	}
	return ColorGrey
}

// BuildSummaryChart builds bucket paints from the first profile's buckets
// when one profile is selected, else the worst level across profiles per
// bucket index (dashboard overview).
func BuildSummaryChart(s model.Summary) SummaryChart {
	ch := SummaryChart{From: s.From, To: s.To}
	n := s.BucketMinutes
	_ = n
	if len(s.Profiles) == 0 {
		return ch
	}
	nb := len(s.Profiles[0].Buckets)
	ch.Buckets = make([]SummaryBucketPt, nb)
	for i := 0; i < nb; i++ {
		var worst *model.AlertLevel
		var lag *int
		for _, p := range s.Profiles {
			if i >= len(p.Buckets) {
				continue
			}
			b := p.Buckets[i]
			if b.Level == nil {
				continue
			}
			if worst == nil || *b.Level > *worst {
				v := *b.Level
				worst = &v
			}
			if b.LagMaxMin != nil && (lag == nil || *b.LagMaxMin > *lag) {
				v := *b.LagMaxMin
				lag = &v
			}
		}
		ch.Buckets[i] = SummaryBucketPt{I: i, Level: worst, Lag: lag}
	}
	return ch
}

// BucketColor: nil level = grey (no data); else the state colour.
func BucketColor(l *model.AlertLevel) Color {
	if l == nil {
		return ColorGrey
	}
	return colorOf(*l)
}
