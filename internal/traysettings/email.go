package traysettings

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Phase 13a (CHK-TRAY-64): the «Уведомления e-mail» page of the settings
// window, shown only with capability email in Expert mode. settings.email
// comes with get_config / get_status; saving is save_settings with the
// whole settings and smtpPassword / clearSmtpPassword (write-only:
// the tray only ever sees hasPassword). email_test {recipientId?, addr?,
// smtpPassword?} answers emailTest {ok, results[{id?, addr, ok, errorKey?}]}.
// tlsMode none = no AUTH (password ignored / field disabled); starttls/tls
// require a password when enabled (AUTH username = From).

// EmailForm is the editable state of the page (text boxes as typed).
type EmailForm struct {
	Enabled bool
	Host    string
	Port    string
	TLSMode string // none | starttls | tls
	From    string
	// Recipients: one per line, "id addr name" (id/addr have no spaces).
	Recipients string
	Days       [7]bool
	FromTime, ToTime string
	Events     [4]bool // model.ViberEventKinds order
	// Password typed in the write-only box ("" = keep the saved one);
	// ClearPassword removes the saved SMTP password.
	Password      string
	ClearPassword bool
}

// FromEmail fills the page from settings.email.
func FromEmail(e model.EmailSettings) EmailForm {
	f := EmailForm{
		Enabled: e.Enabled, Host: e.Host, Port: strconv.Itoa(e.Port),
		TLSMode: e.TLSMode, From: e.From,
		FromTime: e.Schedule.From, ToTime: e.Schedule.To,
	}
	if f.TLSMode == "" {
		f.TLSMode = model.EmailTLSStartTLS
	}
	if e.Port == 0 {
		f.Port = ""
	}
	var lines []string
	for _, r := range e.Recipients {
		line := strings.TrimSpace(r.ID + " " + r.Addr)
		if n := strings.TrimSpace(r.Name); n != "" {
			line += " " + n
		}
		lines = append(lines, line)
	}
	f.Recipients = strings.Join(lines, "\r\n")
	for _, d := range e.Schedule.Days {
		if d >= 1 && d <= 7 {
			f.Days[d-1] = true
		}
	}
	for i, k := range model.ViberEventKinds() {
		f.Events[i] = e.HasEvent(k)
	}
	return f
}

// ParseEmailRecipients reads the recipients box: "id addr name" per line.
func ParseEmailRecipients(text string) ([]model.EmailRecipient, *model.FieldError) {
	out := []model.EmailRecipient{}
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		path := "email.recipients[" + strconv.Itoa(len(out)) + "]"
		if len(parts) < 2 {
			return out, fieldErr(path+".addr", model.ValRequired)
		}
		id, addr := parts[0], parts[1]
		name := ""
		if len(parts) > 2 {
			name = strings.Join(parts[2:], " ")
		}
		if !model.ValidEmailID(id) {
			return out, fieldErr(path+".id", model.ValRequired)
		}
		if !model.ValidEmailAddr(addr) {
			return out, fieldErr(path+".addr", model.ValRequired)
		}
		if len([]rune(name)) > model.EmailMaxNameLen {
			return out, fieldErr(path+".name", model.ValTooLong, strconv.Itoa(model.EmailMaxNameLen))
		}
		out = append(out, model.EmailRecipient{ID: id, Addr: addr, Name: name})
	}
	return out, nil
}

// Apply returns base with the page applied and validated (model rules).
func (f EmailForm) Apply(base model.EmailSettings) (model.EmailSettings, *model.FieldError) {
	e := base.Stored()
	e.Enabled = f.Enabled
	e.Host = strings.TrimSpace(f.Host)
	e.TLSMode = strings.TrimSpace(f.TLSMode)
	if e.TLSMode == "" {
		e.TLSMode = model.EmailTLSStartTLS
	}
	e.From = strings.TrimSpace(f.From)
	portStr := strings.TrimSpace(f.Port)
	if portStr == "" {
		e.Port = 0
	} else {
		n, err := strconv.Atoi(portStr)
		if err != nil {
			return e, fieldErr("email.port", model.ValRange, "1", "65535", portStr)
		}
		e.Port = n
	}
	rs, fe := ParseEmailRecipients(f.Recipients)
	if fe != nil {
		return e, fe
	}
	e.Recipients = rs
	e.Schedule.Days = []int{}
	for i, on := range f.Days {
		if on {
			e.Schedule.Days = append(e.Schedule.Days, i+1)
		}
	}
	e.Schedule.From, e.Schedule.To = strings.TrimSpace(f.FromTime), strings.TrimSpace(f.ToTime)
	e.Events = []string{}
	for i, k := range model.ViberEventKinds() {
		if f.Events[i] {
			e.Events = append(e.Events, k)
		}
	}
	if model.NeedsSMTPAuth(e.TLSMode) {
		if f.Password != "" && !model.ValidSMTPPassword(f.Password) {
			return e, fieldErr("smtpPassword", model.ValRequired)
		}
	}
	if err := e.Validate(); err != nil {
		if fe := model.AsFieldError(err); fe != nil {
			return e, fe
		}
		return e, &model.FieldError{Key: model.ValRequired, Msg: err.Error()}
	}
	return e, nil
}

// EmailFocus is the control of an email field path ("" path = none).
func EmailFocus(path string) (Field, bool) {
	switch {
	case path == "smtpPassword" || path == "clearSmtpPassword" || path == "email.password":
		return FieldEmailPassword, true
	case path == "email.host":
		return FieldEmailHost, true
	case path == "email.port":
		return FieldEmailPort, true
	case path == "email.tlsMode":
		return FieldEmailTLSMode, true
	case path == "email.from":
		return FieldEmailFrom, true
	case strings.HasPrefix(path, "email.recipients"):
		return FieldEmailRecipients, true
	case strings.HasPrefix(path, "email.schedule.days"):
		return FieldEmailDays, true
	case path == "email.schedule.from":
		return FieldEmailSchedFrom, true
	case path == "email.schedule.to":
		return FieldEmailSchedTo, true
	case strings.HasPrefix(path, "email.events"):
		return FieldEmailEvents, true
	case path == "email.enabled":
		return FieldEmailHost, true
	}
	return FieldNone, false
}

// emailKeyFocus: errors of a whole block focus their field.
var emailKeyFocus = map[string]Field{
	model.MsgEmailNoPassword:   FieldEmailPassword,
	model.MsgEmailNoRecipients: FieldEmailRecipients,
	model.MsgEmailNoHost:       FieldEmailHost,
	model.MsgEmailNoFrom:       FieldEmailFrom,
	model.MsgEmailAuthFailed:   FieldEmailPassword,
	model.MsgEmailInvalidAddr:  FieldEmailFrom,
}

// IsEmail tells whether f is on the «e-mail» page.
func IsEmail(f Field) bool { return strings.HasPrefix(string(f), "email.") }

// emailFieldLabel is the label of an email path in validation texts.
func emailFieldLabel(path, lang string) (string, bool) {
	f, ok := EmailFocus(path)
	if !ok {
		return "", false
	}
	key := map[Field]string{
		FieldEmailPassword: i18n.KeyEmPassword, FieldEmailHost: i18n.KeyEmHost,
		FieldEmailPort: i18n.KeyEmPort, FieldEmailTLSMode: i18n.KeyEmTLSMode,
		FieldEmailFrom: i18n.KeyEmFromAddr, FieldEmailRecipients: i18n.KeyEmRecipients,
		FieldEmailDays: i18n.KeyEmDays, FieldEmailSchedFrom: i18n.KeyEmFrom,
		FieldEmailSchedTo: i18n.KeyEmTo, FieldEmailEvents: i18n.KeyEmEvents,
	}[f]
	l := strings.TrimSuffix(strings.TrimSpace(i18n.T(key, lang)), ":")
	if i := strings.Index(path, "["); i >= 0 && f == FieldEmailRecipients {
		if n, err := strconv.Atoi(strings.TrimSuffix(strings.SplitN(path[i+1:], "]", 2)[0], "]")); err == nil {
			l += fmt.Sprintf(" (%d)", n+1)
		}
	}
	return l, true
}

// loadEmail resets the page from the saved settings.
func (e *Editor) loadEmail() {
	if e.Settings.Email != nil {
		e.Email = FromEmail(*e.Settings.Email)
		e.HasEmail = true
	}
}

// EmailUnsaved tells whether the page has unsaved edits.
func (e *Editor) EmailUnsaved() bool {
	return e.CanEditEmail() && (e.Email != FromEmail(*e.Settings.Email))
}

// CanEditEmail tells whether the page is editable.
func (e *Editor) CanEditEmail() bool { return e.Mode == Admin && e.HasSettings && e.HasEmail }

// EmailRequest is save_settings of the page.
func (e *Editor) EmailRequest() (model.IPCRequest, *Problem) {
	if !e.CanEditEmail() || e.Revision == "" {
		return model.IPCRequest{}, &Problem{Text: e.t(i18n.KeySetNoRevision), Reload: true}
	}
	e.settingsCtx = true
	em, fe := e.Email.Apply(*e.Settings.Email)
	if fe != nil {
		return model.IPCRequest{}, e.fieldProblem(fe)
	}
	// tlsMode none: never send / clear password (AUTH off).
	if !model.NeedsSMTPAuth(em.TLSMode) {
		e.Email.Password = ""
		// keep ClearPassword if user asked; otherwise ignore typed password
	} else if em.Enabled {
		has := e.Settings.Email != nil && e.Settings.Email.HasPassword
		if e.Email.ClearPassword {
			has = false
		}
		if e.Email.Password == "" && !has && !e.Email.ClearPassword {
			return model.IPCRequest{}, &Problem{Text: i18n.Message(model.MsgEmailNoPassword, e.Lang), Field: FieldEmailPassword}
		}
	}
	s := e.Settings
	s.Email = &em
	req := model.IPCRequest{Cmd: model.CmdSaveSettings, Settings: &s, ExpectedRevision: e.Revision}
	if model.NeedsSMTPAuth(em.TLSMode) {
		if e.Email.ClearPassword {
			req.ClearSmtpPassword = true
		} else if e.Email.Password != "" {
			req.SmtpPassword = e.Email.Password
		}
	}
	if pr := e.checkSize(req); pr != nil {
		return model.IPCRequest{}, pr
	}
	return req, nil
}

// EmailDone applies an ok reply of the page's save_settings.
func (e *Editor) EmailDone(m model.IPCMessage, req model.IPCRequest) string {
	if m.Revision != "" {
		e.Revision = m.Revision
	}
	s := *req.Settings
	em := *s.Email
	em.HasPassword = e.Settings.Email != nil && e.Settings.Email.HasPassword
	switch {
	case req.ClearSmtpPassword:
		em.HasPassword = false
	case req.SmtpPassword != "":
		em.HasPassword = true
	}
	if e.Settings.Email != nil {
		em.Status = e.Settings.Email.Status
	}
	s.Email = &em
	e.LoadSettings(s)
	return e.t(i18n.KeyEmSaved)
}

// EmailTestRequest is email_test for one recipient ("" = all) and/or a
// one-off addr; typed smtpPassword is sent once (not saved).
func (e *Editor) EmailTestRequest(recipientID, addr string) (model.IPCRequest, *Problem) {
	tlsMode := model.EmailTLSStartTLS
	if e.Settings.Email != nil && e.Settings.Email.TLSMode != "" {
		tlsMode = e.Settings.Email.TLSMode
	}
	if e.Email.TLSMode != "" {
		tlsMode = e.Email.TLSMode
	}
	pw := e.Email.Password
	if model.NeedsSMTPAuth(tlsMode) {
		if pw != "" && !model.ValidSMTPPassword(pw) {
			e.settingsCtx = true
			return model.IPCRequest{}, e.fieldProblem(fieldErr("smtpPassword", model.ValRequired))
		}
		if pw == "" && (e.Settings.Email == nil || !e.Settings.Email.HasPassword) {
			return model.IPCRequest{}, &Problem{Text: i18n.Message(model.MsgEmailNoPassword, e.Lang), Field: FieldEmailPassword}
		}
	} else {
		pw = "" // none: never AUTH
	}
	if addr != "" && !model.ValidEmailAddr(addr) {
		e.settingsCtx = true
		return model.IPCRequest{}, e.fieldProblem(fieldErr("email.from", model.ValRequired))
	}
	return model.IPCRequest{Cmd: model.CmdEmailTest, RecipientID: recipientID, Addr: addr, SmtpPassword: pw}, nil
}

// EmailTestText renders emailTest: one line per result.
func (e *Editor) EmailTestText(r *model.EmailTestResult) string {
	if r == nil {
		return e.t(i18n.KeyEmTestNoResult)
	}
	names := map[string]string{}
	if e.Settings.Email != nil {
		for _, rc := range e.Settings.Email.Recipients {
			names[rc.ID] = rc.Name
			if names[rc.ID] == "" {
				names[rc.ID] = rc.Addr
			}
		}
	}
	lines := []string{e.t(map[bool]string{true: i18n.KeyEmTestOK, false: i18n.KeyEmTestFailed}[r.OK])}
	for _, it := range r.Results {
		who := it.Addr
		if it.ID != "" {
			who = it.ID
			if n := names[it.ID]; n != "" {
				who = n + " (" + it.ID + ")"
			}
			if it.Addr != "" {
				who += " <" + it.Addr + ">"
			}
		}
		res := e.t(i18n.KeyEmTestItemOK)
		if !it.OK {
			res = i18n.Message(it.ErrorKey, e.Lang)
			if it.ErrorKey == "" {
				res = i18n.Message(model.MsgEmailFailed, e.Lang)
			}
		}
		lines = append(lines, who+": "+res)
	}
	return strings.Join(lines, "\r\n")
}

// EmailStatusText renders settings.email.status.
func EmailStatusText(e *model.EmailSettings, lang string) string {
	if e == nil || e.Status == nil {
		return ""
	}
	st := e.Status
	t := fmt.Sprintf(i18n.T(i18n.KeyEmStatus, lang), st.Sent, st.Queued, st.Dropped)
	if st.LastOKAt != nil {
		t += "\r\n" + fmt.Sprintf(i18n.T(i18n.KeyEmLastOK, lang), st.LastOKAt.Local().Format("02.01.2006 15:04"))
	}
	if st.LastErrorAt != nil {
		t += "\r\n" + fmt.Sprintf(i18n.T(i18n.KeyEmLastError, lang), st.LastErrorAt.Local().Format("02.01.2006 15:04"),
			strings.TrimSuffix(i18n.Message(st.LastErrorKey, lang), "."))
	}
	return t
}

// PasswordStateText: «Пароль сохранён» / «Пароль не задан» (+ pending).
func (e *Editor) PasswordStateText() string {
	switch {
	case !model.NeedsSMTPAuth(e.Email.TLSMode):
		return e.t(i18n.KeyEmPasswordNoneTLS)
	case e.Email.ClearPassword:
		return e.t(i18n.KeyEmPasswordClear)
	case e.Email.Password != "":
		return e.t(i18n.KeyEmPasswordNew)
	case e.Settings.Email != nil && e.Settings.Email.HasPassword:
		return e.t(i18n.KeyEmPasswordSaved)
	}
	return e.t(i18n.KeyEmPasswordNone)
}

// EmailTLSModes is the ordered list for the tlsMode combo.
func EmailTLSModes() []string {
	return []string{model.EmailTLSNone, model.EmailTLSStartTLS, model.EmailTLSImplicit}
}

// EmailTLSModeIndex returns the combo index of mode (default starttls).
func EmailTLSModeIndex(mode string) int {
	for i, m := range EmailTLSModes() {
		if m == mode {
			return i
		}
	}
	return 1 // starttls
}
