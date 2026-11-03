package traysettings

import "github.com/alexal46372-maker/ExchangeChecker/internal/model"

// Polling on / off without saving the profile (phase 13, CHK-TRAY-91,
// capability profile_enabled): the «Опрос» checkbox of the database list
// sends set_enabled at once; the profile_enabled push (also after a click
// on the check mark in the tray menu) updates the list.

// SetEnabledRequest is set_enabled for saved profile i (ok = false for a
// new / unknown profile).
func (e *Editor) SetEnabledRequest(i int, on bool) (model.IPCRequest, bool) {
	if e.Mode != Admin || i < 0 || i >= len(e.Profiles) || e.Profiles[i].ID == "" {
		return model.IPCRequest{}, false
	}
	v := on
	return model.IPCRequest{Cmd: model.CmdSetEnabled, ProfileID: e.Profiles[i].ID, Enabled: &v, ExpectedRevision: e.Revision}, true
}

// ApplyEnabled stores a set_enabled reply / profile_enabled push: the
// stored profile, the saved base and the form of the selected profile get
// the new flag (other unsaved edits stay); rev ("" = keep) is the new
// config revision. It returns the list index of the profile (-1 = none).
func (e *Editor) ApplyEnabled(id string, on bool, rev string) int {
	if rev != "" {
		e.Revision = rev
	}
	for i := range e.Profiles {
		if e.Profiles[i].ID != id {
			continue
		}
		e.Profiles[i].Enabled = on
		if i == e.Sel && !e.IsNew {
			e.Base.Enabled = on
			e.Form.Enabled = on
		}
		return i
	}
	return -1
}
