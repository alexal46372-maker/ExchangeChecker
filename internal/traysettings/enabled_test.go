package traysettings

import (
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_TRAY_91_SetEnabledEditor — set_enabled request of the «Опрос»
// checkbox (admin, saved profile, expectedRevision) and ApplyEnabled: the
// flag and revision change, other unsaved edits of the form stay.
func TestCHK_TRAY_91_SetEnabledEditor(t *testing.T) {
	a, b := model.NewProfile("a"), model.NewProfile("b")
	a.Name, b.Name = "A", "B"
	e := NewEditor(Admin)
	e.LoadConfig(model.ConfigView{Settings: model.DefaultSettings(), Profiles: []model.Profile{a, b}}, "r1")
	e.Select(1)
	e.Form.Name = "B2" // unsaved edit
	req, ok := e.SetEnabledRequest(1, false)
	if !ok || req.Cmd != model.CmdSetEnabled || req.ProfileID != "b" || req.Enabled == nil || *req.Enabled || req.ExpectedRevision != "r1" {
		t.Fatalf("request %+v %v", req, ok)
	}
	if _, ok := e.SetEnabledRequest(5, true); ok {
		t.Error("out of range")
	}
	if i := e.ApplyEnabled("b", false, "r2"); i != 1 {
		t.Fatalf("index %d", i)
	}
	if e.Profiles[1].Enabled || e.Base.Enabled || e.Form.Enabled || e.Revision != "r2" || e.Form.Name != "B2" {
		t.Fatalf("apply: %+v rev %s", e.Form, e.Revision)
	}
	if e.ApplyEnabled("zz", true, "") != -1 || e.Revision != "r2" {
		t.Error("unknown profile")
	}
	e.Form.Name = "B"
	if e.ProfileUnsaved() {
		t.Error("only the applied flag changed: nothing unsaved")
	}
	ro := NewEditor(ReadOnly)
	ro.LoadConfig(model.ConfigView{Profiles: []model.Profile{a}}, "r1")
	if _, ok := ro.SetEnabledRequest(0, false); ok {
		t.Error("read-only mode")
	}
}
