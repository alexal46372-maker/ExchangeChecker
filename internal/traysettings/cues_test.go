package traysettings

import (
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_TRAY_50_UIFieldCues — every UIField with Cue has RU+EN text; a
// field with HasDefault that is an edit-with-cue (Cue != "") never is
// required; CueText formats CueDefault (PLAN §12 p.145, CHK-TRAY-50).
func TestCHK_TRAY_50_UIFieldCues(t *testing.T) {
	n := 0
	for _, f := range UIFields {
		if f.Cue == "" {
			continue
		}
		n++
		for _, lang := range []string{i18n.RU, i18n.EN} {
			got := f.CueText(lang)
			if got == "" || got == f.Cue {
				t.Errorf("%s %s: cue %q", f.ID, lang, got)
			}
			if f.CueDefault > 0 && !containsDigit(got) {
				t.Errorf("%s %s: CueDefault %d not in %q", f.ID, lang, f.CueDefault, got)
			}
		}
		if f.HasDefault {
			for _, id := range RequiredFields(UIExpert, Form{Auth: model.AuthSQL}, false) {
				if id == f.ID {
					t.Errorf("%s has a default and a cue: must not be required", f.ID)
				}
			}
		}
	}
	if n < 10 {
		t.Fatalf("only %d fields with a cue", n)
	}
	// HasDefault without Cue: the default is the filled value (general
	// page numbers, check boxes, combos) — still never required
	for _, f := range UIFields {
		if !f.HasDefault || f.Cue != "" {
			continue
		}
		for _, id := range RequiredFields(UISimple, Form{Auth: model.AuthSQL}, false) {
			if id == f.ID {
				t.Errorf("%s HasDefault without cue but is required", f.ID)
			}
		}
	}
}

func containsDigit(s string) bool {
	for _, r := range s {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}
