//go:build windows

package wintray

import (
	"fmt"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/traysettings"
)

// cloneButtonVisible is true for admin + CapCloneProfile (CHK-TRAY-58).
func cloneButtonVisible(mode traysettings.Mode, hasCap bool) bool {
	return mode == traysettings.Admin && hasCap
}

// cloneFocusField is FieldPassword when the clone needs a password.
func cloneFocusField(needPassword bool) traysettings.Field {
	if needPassword {
		return traysettings.FieldPassword
	}
	return traysettings.FieldNone
}

// cloneProfile runs clone_profile on the selected saved profile (CHK-TRAY-58).
// On success with needPassword the new profile is selected and the password
// field is focused. Hidden without CapCloneProfile (see update).
func (w *settingsWindow) cloneProfile() {
	if w.busy {
		return
	}
	if !w.cfg.session.Supported(model.CmdCloneProfile) {
		return
	}
	req, pr := w.ed.CloneRequest()
	if pr != nil {
		w.problem(pr)
		return
	}
	if !w.ask(fmt.Sprintf(w.t(i18n.KeySetCloneConfirm), w.ed.Base.DisplayName())) {
		return
	}
	w.status = w.t(i18n.KeySetLoading)
	w.call(req, func(m model.IPCMessage, err error) {
		switch {
		case err != nil:
			w.problem(w.ed.ErrorProblem(err, traysettings.CallTimeout))
		case m.Type == model.MsgError:
			w.problem(w.ed.Reply(m))
		default:
			w.status = w.ed.CloneDone(m)
			w.fill()
			if f := cloneFocusField(m.NeedPassword); f != traysettings.FieldNone {
				w.focus(f)
			}
		}
	})
}
