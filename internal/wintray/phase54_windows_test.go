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

// TestCHK_TRAY_54_LayoutV2 — every tray window at 96 and 144 dpi: no
// horizontal scroll, button bar visible, field width ≥ cue, gaps ≤ 16×DPI;
// summary / alarm_test also fit a 1024×768 work area (PLAN §12 p.149).
func TestCHK_TRAY_54_LayoutV2(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer func() { testDPI = 0; testWorkArea = nil }()

	for _, dpi := range []int32{96, 144} {
		for _, lang := range []string{i18n.RU, i18n.EN} {
			testDPI = dpi
			testWorkArea = nil
			tag := fmt.Sprintf("%s %d dpi", lang, dpi)
			uiScenes(t, lang, func(name string, hwnd uintptr) {
				checkLayout(t, name+" "+tag, hwnd)
			})
		}
	}

	// summary + alarm_test under a constrained work area (demo screenshots)
	work := winapi.RECT{Right: 1024, Bottom: 768}
	for _, dpi := range []int32{96, 144} {
		for _, lang := range []string{i18n.RU, i18n.EN} {
			testDPI = dpi
			testWorkArea = &work
			tag := fmt.Sprintf("workarea %s %d dpi", lang, dpi)
			sw, err := createSummary(nil, lang, nil)
			if err != nil {
				t.Fatal(err)
			}
			sw.apply(sampleSummary())
			if r := winapi.GetWindowRect(sw.hwnd); !contains(work, r) {
				t.Errorf("summary %s: window %+v outside work area %+v", tag, r, work)
			}
			checkLayout(t, "summary "+tag, sw.hwnd)
			winapi.DestroyWindow(sw.hwnd)

			aw, err := createAlarmTest(nil, lang)
			if err != nil {
				t.Fatal(err)
			}
			aw.setResult(formatAlarmViber(&model.AlarmTestResult{OK: true, Viber: &model.AlarmTestViber{
				ViberTestResult: model.ViberTestResult{OK: true, Results: []model.ViberTestItem{
					{ID: "u1", OK: true}, {ID: "u2", OK: true},
				}},
			}}, lang))
			if r := winapi.GetWindowRect(aw.hwnd); !contains(work, r) {
				t.Errorf("alarm_test %s: window %+v outside work area %+v", tag, r, work)
			}
			checkLayout(t, "alarm_test "+tag, aw.hwnd)
			winapi.DestroyWindow(aw.hwnd)
		}
	}
}
