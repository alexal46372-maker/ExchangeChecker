package sqlcheck

import (
	"strings"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// MinValidYear: after YearOffset is removed, timestamps before this year are
// treated as NULL. 1C writes the "empty date" as 0001-01-01 (2001-01-01
// with the +2000 offset) or as the datetime minimum 1753-01-01; such a
// value must not look like a real (very old) exchange.
const MinValidYear = 1900

// shiftYears moves a naive wall clock by years. With offsets that are
// multiples of 400 (0, 2000) the leap-year calendar is identical, so
// Feb 29 maps to Feb 29.
func shiftYears(t time.Time, years int) time.Time {
	y, mo, d := t.Date()
	h, mi, s := t.Clock()
	return time.Date(y+years, mo, d, h, mi, s, t.Nanosecond(), time.UTC)
}

// ConvertDBTime turns a naive database timestamp into an instant: first
// YearOffset is subtracted, then the wall clock is read as UTC+3
// (model.FromDBNaive). nil and "empty 1C dates" (< MinValidYear) -> nil.
func ConvertDBTime(t *time.Time, yearOffset int) *time.Time {
	if t == nil {
		return nil
	}
	n := shiftYears(*t, -yearOffset)
	if n.Year() < MinValidYear {
		return nil
	}
	x := model.FromDBNaive(n)
	return &x
}

// pointName picks the display name: name column, else id/code column.
func pointName(r RawPoint) string {
	if n := strings.TrimSpace(r.Name); n != "" {
		return n
	}
	if id := strings.TrimSpace(r.ID); id != "" {
		return id
	}
	return "?"
}

// toPoints converts raw rows into phase-1 ExchangePoints and evaluates them
// (Level / Message / CheckedAt via ExchangePoint.Refresh: both timestamps
// NULL -> LevelError + model.ReasonNoTimestamps).
func toPoints(raw []RawPoint, yearOffset int, profileID string, s model.Settings, now time.Time) []model.ExchangePoint {
	out := make([]model.ExchangePoint, 0, len(raw))
	for _, r := range raw {
		p := model.ExchangePoint{
			ProfileID: profileID,
			Name:      pointName(r),
			LastSend:  ConvertDBTime(r.Send, yearOffset),
			LastRecv:  ConvertDBTime(r.Recv, yearOffset),
		}
		p.Refresh(s, now)
		out = append(out, p)
	}
	return out
}

// toSampleRows converts raw rows for DiscoveryCandidate.SampleRows.
func toSampleRows(raw []RawPoint, yearOffset int) []model.SampleRow {
	out := make([]model.SampleRow, 0, len(raw))
	for _, r := range raw {
		out = append(out, model.SampleRow{
			Point:    pointName(r),
			LastSend: ConvertDBTime(r.Send, yearOffset),
			LastRecv: ConvertDBTime(r.Recv, yearOffset),
		})
	}
	return out
}
