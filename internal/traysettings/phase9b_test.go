package traysettings

import (
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_TRAY_29_SaveErrors — validation.sql_server_forbidden (through
// monitor.profile_invalid, with the reason) focuses «Сервер»;
// config.password_required_on_target_change focuses «Пароль».
func TestCHK_TRAY_29_SaveErrors(t *testing.T) {
	for _, lang := range []string{i18n.RU, i18n.EN} {
		e := NewEditor(lang, Admin)
		for reason, key := range forbiddenReasonKeys {
			pr := e.Reply(model.IPCMessage{Type: model.MsgError, Code: model.ErrCodeInvalidRequest, ErrorKey: model.MsgMonitorProfileInvalid,
				ErrorArgs: []string{"server", model.ValSQLServerForbidden, reason}})
			if pr == nil || pr.Field != FieldHost || !strings.Contains(pr.Text, i18n.T(key, lang)) ||
				!strings.Contains(pr.Text, strings.SplitN(model.MonitorMessage(model.ValSQLServerForbidden, lang), ":", 2)[0]) {
				t.Errorf("%s %s: %+v", lang, reason, pr)
			}
		}
		pr := e.Reply(model.IPCMessage{Type: model.MsgError, Code: model.ErrCodeInvalidRequest, ErrorKey: model.MsgPasswordTargetChange,
			ErrorArgs: []string{"password", model.ValRequired}})
		if pr == nil || pr.Field != Field("password") || pr.Text != model.MonitorMessage(model.MsgPasswordTargetChange, lang) {
			t.Errorf("%s target change: %+v", lang, pr)
		}
	}
	// local validation gives the same text
	e := NewEditor(i18n.RU, Admin)
	fe := model.ValidateSQLServerAddr(`np:\\srv\pipe\sql`).(*model.FieldError)
	pr := e.fieldProblem(fe)
	if pr.Field != FieldHost || !strings.Contains(pr.Text, "именованный канал") {
		t.Errorf("local %+v", pr)
	}
}
