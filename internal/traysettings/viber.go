package traysettings

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Phase 10 D4 (CHK-TRAY-43, PLAN §12 p.123): the «Viber» page of the
// settings window, shown only with capability viber. settings.viber comes
// with get_config and get_status; saving is save_settings with the whole
// settings and viberToken / clearViberToken (the token is write-only:
// the tray only ever sees hasToken). viber_test {recipientId?,
// viberToken?} answers viberTest {ok, results[{id, ok, errorKey?}]}.

// ViberForm is the editable state of the page (text boxes as typed).
type ViberForm struct {
	Enabled bool
	// Recipients: one per line, "id name" (the id has no spaces).
	Recipients string
	Days       [7]bool // Monday .. Sunday
	From, To   string
	Events     [4]bool // model.ViberEventKinds order
	Reminder   string
	// Token typed in the write-only box ("" = keep the saved one);
	// ClearToken removes the saved token.
	Token      string
	ClearToken bool
}

// FromViber fills the page from settings.viber.
func FromViber(v model.ViberSettings) ViberForm {
	f := ViberForm{Enabled: v.Enabled, From: v.Schedule.From, To: v.Schedule.To, Reminder: strconv.Itoa(v.ReminderMinutes)}
	var lines []string
	for _, r := range v.Recipients {
		lines = append(lines, strings.TrimSpace(r.ID+" "+r.Name))
	}
	f.Recipients = strings.Join(lines, "\r\n")
	for _, d := range v.Schedule.Days {
		if d >= 1 && d <= 7 {
			f.Days[d-1] = true
		}
	}
	for i, k := range model.ViberEventKinds() {
		f.Events[i] = v.HasEvent(k)
	}
	return f
}

// ParseRecipients reads the recipients box: "id name" per line, empty
// lines skipped; a bad id is a field error of its index.
func ParseRecipients(text string) ([]model.ViberRecipient, *model.FieldError) {
	out := []model.ViberRecipient{}
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		id, name := line, ""
		if i := strings.IndexAny(line, " \t"); i >= 0 {
			id, name = line[:i], strings.TrimSpace(line[i+1:])
		}
		path := "viber.recipients[" + strconv.Itoa(len(out)) + "]"
		if !model.ValidViberID(id) {
			return out, fieldErr(path+".id", model.ValRequired)
		}
		if len([]rune(name)) > model.ViberMaxNameLen {
			return out, fieldErr(path+".name", model.ValTooLong, strconv.Itoa(model.ViberMaxNameLen))
		}
		out = append(out, model.ViberRecipient{ID: id, Name: name})
	}
	return out, nil
}

// Apply returns base with the page applied and validated (model rules).
func (f ViberForm) Apply(base model.ViberSettings) (model.ViberSettings, *model.FieldError) {
	v := base.Stored()
	v.Enabled = f.Enabled
	rs, fe := ParseRecipients(f.Recipients)
	if fe != nil {
		return v, fe
	}
	v.Recipients = rs
	v.Schedule.Days = []int{}
	for i, on := range f.Days {
		if on {
			v.Schedule.Days = append(v.Schedule.Days, i+1)
		}
	}
	v.Schedule.From, v.Schedule.To = strings.TrimSpace(f.From), strings.TrimSpace(f.To)
	v.Events = []string{}
	for i, k := range model.ViberEventKinds() {
		if f.Events[i] {
			v.Events = append(v.Events, k)
		}
	}
	t := strings.TrimSpace(f.Reminder)
	n, err := strconv.Atoi(t)
	if err != nil {
		return v, fieldErr("viber.reminderMinutes", model.ValRange, "15", "1440", t)
	}
	v.ReminderMinutes = n
	if f.Token != "" && !model.ValidViberToken(f.Token) {
		return v, fieldErr("viberToken", model.ValRequired)
	}
	if err := v.Validate(); err != nil {
		if fe := model.AsFieldError(err); fe != nil {
			return v, fe
		}
		return v, &model.FieldError{Key: model.ValRequired, Msg: err.Error()}
	}
	return v, nil
}

// ViberFocus is the control of a viber field path ("" path = none).
func ViberFocus(path string) (Field, bool) {
	switch {
	case path == "viberToken" || path == "clearViberToken" || path == "viber.token":
		return FieldViberToken, true
	case strings.HasPrefix(path, "viber.recipients"):
		return FieldViberRecipients, true
	case strings.HasPrefix(path, "viber.schedule.days"):
		return FieldViberDays, true
	case path == "viber.schedule.from":
		return FieldViberFrom, true
	case path == "viber.schedule.to":
		return FieldViberTo, true
	case strings.HasPrefix(path, "viber.events"):
		return FieldViberEvents, true
	case path == "viber.reminderMinutes":
		return FieldViberReminder, true
	case path == "viber.enabled":
		return FieldViberRecipients, true
	}
	return FieldNone, false
}

// viberKeyFocus: errors of a whole block focus their field.
var viberKeyFocus = map[string]Field{
	model.MsgViberNoToken: FieldViberToken, model.MsgViberNoRecipients: FieldViberRecipients,
	model.MsgViberAuthFailed: FieldViberToken,
}

// IsViber tells whether f is on the «Viber» page.
func IsViber(f Field) bool { return strings.HasPrefix(string(f), "viber.") }

// viberFieldLabel is the label of a viber path in validation texts.
func viberFieldLabel(path, lang string) (string, bool) {
	f, ok := ViberFocus(path)
	if !ok {
		return "", false
	}
	key := map[Field]string{
		FieldViberToken: i18n.KeyVbToken, FieldViberRecipients: i18n.KeyVbRecipients, FieldViberDays: i18n.KeyVbDays,
		FieldViberFrom: i18n.KeyVbFrom, FieldViberTo: i18n.KeyVbTo, FieldViberEvents: i18n.KeyVbEvents,
		FieldViberReminder: i18n.KeyVbReminder,
	}[f]
	l := strings.TrimSuffix(strings.TrimSpace(i18n.T(key, lang)), ":")
	if i := strings.Index(path, "["); i >= 0 && f == FieldViberRecipients {
		if n, err := strconv.Atoi(strings.TrimSuffix(strings.SplitN(path[i+1:], "]", 2)[0], "]")); err == nil {
			l += fmt.Sprintf(" (%d)", n+1)
		}
	}
	return l, true
}

// LoadViber resets the page from the saved settings.
func (e *Editor) loadViber() {
	if e.Settings.Viber != nil {
		e.Viber = FromViber(*e.Settings.Viber)
		e.HasViber = true
	}
}

// ViberUnsaved tells whether the page has unsaved edits.
func (e *Editor) ViberUnsaved() bool {
	return e.CanEditViber() && (e.Viber != FromViber(*e.Settings.Viber))
}

// CanEditViber tells whether the page is editable.
func (e *Editor) CanEditViber() bool { return e.Mode == Admin && e.HasSettings && e.HasViber }

// ViberRequest is save_settings of the page: the whole saved settings
// with the new viber block, the token only when typed, expectedRevision.
func (e *Editor) ViberRequest() (model.IPCRequest, *Problem) {
	if !e.CanEditViber() || e.Revision == "" {
		return model.IPCRequest{}, &Problem{Text: e.t(i18n.KeySetNoRevision), Reload: true}
	}
	e.settingsCtx = true
	v, fe := e.Viber.Apply(*e.Settings.Viber)
	if fe != nil {
		return model.IPCRequest{}, e.fieldProblem(fe)
	}
	s := e.Settings
	s.Viber = &v
	req := model.IPCRequest{Cmd: model.CmdSaveSettings, Settings: &s, ExpectedRevision: e.Revision}
	if e.Viber.ClearToken {
		req.ClearViberToken = true
	} else if e.Viber.Token != "" {
		req.ViberToken = e.Viber.Token
	}
	if pr := e.checkSize(req); pr != nil {
		return model.IPCRequest{}, pr
	}
	return req, nil
}

// ViberDone applies an ok reply of the page's save_settings: the sent
// settings become the saved ones; hasToken follows the sent token.
func (e *Editor) ViberDone(m model.IPCMessage, req model.IPCRequest) string {
	if m.Revision != "" {
		e.Revision = m.Revision
	}
	s := *req.Settings
	v := *s.Viber
	v.HasToken = e.Settings.Viber != nil && e.Settings.Viber.HasToken
	switch {
	case req.ClearViberToken:
		v.HasToken = false
	case req.ViberToken != "":
		v.HasToken = true
	}
	if e.Settings.Viber != nil {
		v.Status = e.Settings.Viber.Status
	}
	s.Viber = &v
	e.LoadSettings(s)
	return e.t(i18n.KeyVbSaved)
}

// ViberTestRequest is viber_test for one recipient ("" = all saved) with
// the typed token (sent once, not saved; "" = the saved token).
func (e *Editor) ViberTestRequest(recipientID string) (model.IPCRequest, *Problem) {
	if e.Viber.Token != "" && !model.ValidViberToken(e.Viber.Token) {
		e.settingsCtx = true
		return model.IPCRequest{}, e.fieldProblem(fieldErr("viberToken", model.ValRequired))
	}
	if e.Viber.Token == "" && (e.Settings.Viber == nil || !e.Settings.Viber.HasToken) {
		return model.IPCRequest{}, &Problem{Text: i18n.Message(model.MsgViberNoToken, e.Lang), Field: FieldViberToken}
	}
	return model.IPCRequest{Cmd: model.CmdViberTest, RecipientID: recipientID, ViberToken: e.Viber.Token}, nil
}

// ViberTestText renders viberTest: one line per recipient (name, ok or
// the text of errorKey).
func (e *Editor) ViberTestText(r *model.ViberTestResult) string {
	if r == nil {
		return e.t(i18n.KeyVbTestNoResult)
	}
	names := map[string]string{}
	if e.Settings.Viber != nil {
		for _, rc := range e.Settings.Viber.Recipients {
			names[rc.ID] = rc.Name
		}
	}
	lines := []string{e.t(map[bool]string{true: i18n.KeyVbTestOK, false: i18n.KeyVbTestFailed}[r.OK])}
	for _, it := range r.Results {
		who := it.ID
		if n := names[it.ID]; n != "" {
			who = n + " (" + it.ID + ")"
		}
		res := e.t(i18n.KeyVbTestItemOK)
		if !it.OK {
			res = i18n.Message(it.ErrorKey, e.Lang)
			if it.ErrorKey == "" {
				res = i18n.Message(model.MsgViberFailed, e.Lang)
			}
		}
		lines = append(lines, who+": "+res)
	}
	return strings.Join(lines, "\r\n")
}

// ViberStatusText renders settings.viber.status (times in the local zone
// of the tray machine).
func ViberStatusText(v *model.ViberSettings, lang string) string {
	if v == nil || v.Status == nil {
		return ""
	}
	st := v.Status
	t := fmt.Sprintf(i18n.T(i18n.KeyVbStatus, lang), st.Sent, st.Queued, st.Dropped)
	if st.LastOKAt != nil {
		t += "\r\n" + fmt.Sprintf(i18n.T(i18n.KeyVbLastOK, lang), st.LastOKAt.Local().Format("02.01.2006 15:04"))
	}
	if st.LastErrorAt != nil {
		t += "\r\n" + fmt.Sprintf(i18n.T(i18n.KeyVbLastError, lang), st.LastErrorAt.Local().Format("02.01.2006 15:04"),
			strings.TrimSuffix(i18n.Message(st.LastErrorKey, lang), "."))
	}
	return t
}

// TokenStateText: «Токен сохранён» / «Токен не задан» (+ the pending change).
func (e *Editor) TokenStateText() string {
	switch {
	case e.Viber.ClearToken:
		return e.t(i18n.KeyVbTokenClear)
	case e.Viber.Token != "":
		return e.t(i18n.KeyVbTokenNew)
	case e.Settings.Viber != nil && e.Settings.Viber.HasToken:
		return e.t(i18n.KeyVbTokenSaved)
	}
	return e.t(i18n.KeyVbTokenNone)
}
