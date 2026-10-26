//go:build windows

package wintray


import (
	"context"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/traysettings"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// wizRetryTimerID: busy / rate_limited countdown (CHK-TRAY-52).

// capHas reports a Hello capability (browse_subnet / test_steps…), not a
// command name — Session.Supported maps commands, Status.Has maps caps.
func (wd *wizardDialog) capHas(c string) bool {
	return wd.w.cfg.session.Status().Has(c)
}

const wizRetryTimerID = 52

// runProgress is run with a live progress callback (sql_browse_progress /
// sql_test_steps_progress). Progress is posted to the UI thread; cancel
// {ref} still goes through DoCancel when ctx ends.
func (wd *wizardDialog) runProgress(req model.IPCRequest, timeout time.Duration, onProg func(model.IPCMessage), done func(model.IPCMessage, error)) {
	if wd.cancel != nil {
		wd.cancel()
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	wd.cancel = cancel
	wd.seq++
	seq := wd.seq
	wd.lastCmds = append(wd.lastCmds, req.Cmd)
	wd.show()
	s := wd.w.cfg.session
	s.SetProgress(func(m model.IPCMessage) {
		wd.w.post(func() {
			if wd.closed || seq != wd.seq || onProg == nil {
				return
			}
			onProg(m)
		})
	})
	go func() {
		m, err := s.DoCancel(ctx, req)
		cancel()
		s.SetProgress(nil)
		wd.w.post(func() {
			if wd.closed || seq != wd.seq {
				return
			}
			wd.cancel = nil
			done(m, err)
			if !wd.closed {
				wd.show()
			}
		})
	}()
}

func (wd *wizardDialog) cancelBrowse() {
	if wd.cancel != nil {
		wd.cancel()
		wd.cancel = nil
		wd.seq++
		wd.w.cfg.session.SetProgress(nil)
		wd.setResult(wd.t(i18n.KeyDiscCanceled))
		wd.show()
	}
}

func (wd *wizardDialog) onBrowseProgress(m model.IPCMessage) {
	if m.BrowseProgress == nil {
		return
	}
	wd.setResult(wd.z.BrowseProgressLine(*m.BrowseProgress))
}

func (wd *wizardDialog) onTestStepProgress(m model.IPCMessage) {
	if m.TestStep == nil {
		return
	}
	st := *m.TestStep
	// replace or append by id
	found := false
	for i := range wd.z.CheckSteps {
		if wd.z.CheckSteps[i].ID == st.ID {
			wd.z.CheckSteps[i] = st
			found = true
			break
		}
	}
	if !found {
		wd.z.CheckSteps = append(wd.z.CheckSteps, st)
	}
	wd.refreshStepsList()
	if !st.OK {
		if adv := wd.z.TestStepsAdvice(wd.z.CheckSteps); adv != "" {
			wd.setResult(adv)
		}
	}
}

func (wd *wizardDialog) applyTestSteps(res model.SQLServerCheckResult) {
	wd.z.CheckSteps = append([]model.SQLCheckStep(nil), res.Steps...)
	wd.refreshStepsList()
	if !res.OK {
		if adv := wd.z.TestStepsAdvice(res.Steps); adv != "" {
			wd.setResult(adv)
			return
		}
		wd.setResult(wd.t(i18n.KeyWizNoneFound))
		return
	}
	wd.setResult(wd.t(i18n.KeyWizTestOK))
}

func (wd *wizardDialog) refreshStepsList() {
	h := wd.ctl[idWizSteps]
	if h == 0 {
		return
	}
	winapi.SendMessage(h, winapi.LB_RESETCONTENT, 0, 0)
	for _, st := range wd.z.CheckSteps {
		winapi.SendMessageString(h, winapi.LB_ADDSTRING, 0, wd.z.TestStepLine(st))
	}
}

// failRetry is fail plus a retryAfterMs countdown for busy / rate_limited.
func (wd *wizardDialog) failRetry(m model.IPCMessage, err error, prefix string) {
	wd.fail(m, err, prefix)
	if err != nil {
		return
	}
	switch m.Code {
	case model.ErrCodeRateLimited, model.ErrCodeBusy:
		if m.RetryAfterMs > 0 {
			wd.startRetry(m.Code, m.RetryAfterMs)
		}
	}
}

func (wd *wizardDialog) startRetry(code string, ms int64) {
	wd.stopRetry()
	wd.retryCode = code
	wd.retryLeft = traysettings.RetryAfterSeconds(ms)
	wd.retryTimer = winapi.SetTimer(wd.hwnd, wizRetryTimerID, 1000)
	wd.paintRetry()
}

func (wd *wizardDialog) stopRetry() {
	if wd.retryTimer {
		winapi.KillTimer(wd.hwnd, wizRetryTimerID)
		wd.retryTimer = false
	}
	wd.retryLeft = 0
}

func (wd *wizardDialog) tickRetry() {
	if wd.retryLeft <= 0 {
		wd.stopRetry()
		return
	}
	wd.retryLeft--
	if wd.retryLeft <= 0 {
		wd.stopRetry()
		wd.setResult("")
		return
	}
	wd.paintRetry()
}

func (wd *wizardDialog) paintRetry() {
	pr := wd.w.ed.WizardProblem(model.IPCMessage{Type: model.MsgError, Code: wd.retryCode, RetryAfterMs: wd.retryLeft * 1000}, nil)
	if pr != nil {
		wd.setResult(pr.Text)
	}
}
