//go:build windows

package wintray

import (
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// sampleSummary is demo data for the summary window screenshots (CHK-TRAY-54).
func sampleSummary() model.Summary {
	now := time.Date(2026, 10, 26, 18, 0, 0, 0, time.FixedZone("UTC+3", 3*3600))
	from := now.Add(-24 * time.Hour)
	warn, errL, ok := model.LevelWarning, model.LevelError, model.LevelOK
	buckets := make([]model.SummaryBucket, 48)
	for i := range buckets {
		buckets[i].T = from.Add(time.Duration(i) * 30 * time.Minute)
		switch {
		case i%7 == 0:
			// empty = grey
		case i%11 == 0:
			buckets[i].Level = &errL
		case i%5 == 0:
			buckets[i].Level = &warn
		default:
			buckets[i].Level = &ok
		}
	}
	fm := 12
	fm2 := 45
	return model.Summary{
		From: from, To: now, BucketMinutes: 30,
		Profiles: []model.SummaryProfile{
			{ProfileID: "p1", Name: "Склад", Level: model.LevelOK, FreshnessMin: &fm, Buckets: buckets},
			{ProfileID: "p2", Name: "Магазин 12", Level: model.LevelWarning, FreshnessMin: &fm2, Buckets: append([]model.SummaryBucket(nil), buckets...)},
			{ProfileID: "p3", Name: "Офис", Level: model.LevelError, Buckets: append([]model.SummaryBucket(nil), buckets...)},
		},
		Truncated: true,
	}
}

// TestCHK_TRAY_54_LayoutV2 — every tray window (incl. summary + alarm_test)
// fits the work area at 96 and 144 dpi: no horizontal scroll, button bar
// visible, field width ≥ cue, gaps ≤ 16×DPI (PLAN §12 p.149).
func TestCHK_TRAY_54_LayoutV2(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer func() { testDPI = 0; testWorkArea = nil }()
	// Typical full-HD work area: every window must fit (fitForm / placeWindow).
	work := winapi.RECT{Right: 1920, Bottom: 1080}
	for _, dpi := range []int32{96, 144} {
		for _, lang := range []string{i18n.RU, i18n.EN} {
			testDPI = dpi
			testWorkArea = &work
			tag := fmt.Sprintf("%s %d dpi", lang, dpi)
			uiScenes(t, lang, func(name string, hwnd uintptr) {
				r := winapi.GetWindowRect(hwnd)
				if !contains(work, r) {
					t.Errorf("%s %s: window %+v outside work area %+v", name, tag, r, work)
				}
				checkLayout(t, name+" "+tag, hwnd)
			})
		}
	}
}
