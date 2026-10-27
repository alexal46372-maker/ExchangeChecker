package traysettings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/ipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
)

// Mode of the settings window.
type Mode int

// Modes.
const (
	// ReadOnly: an ordinary tray; data from get_status (no get_config,
	// no save_*); test (saved profile) and poll_now are allowed.
	ReadOnly Mode = iota
	// Admin: an elevated process; data from get_config, save/delete.
	Admin
)

// Timeouts of the requests (the service answers a test within 15 s).
const (
	TestTimeout = 25 * time.Second
	CallTimeout = 10 * time.Second
)

// Editor is the state of the settings window: the profile list, the
// selected (or new) profile, its form and the configuration revision
// required by save_profile / delete_profile (expectedRevision).
type Editor struct {
	Lang string
	Mode Mode
	// Revision is the configuration revision: get_config.revision /
	// get_status.configRevision, then the revision of every save_* reply.
	Revision string
	Profiles []model.Profile
	// Sel is the index in Profiles of the edited profile, -1 for a new
	// profile or none (see IsNew).
	Sel   int
	IsNew bool
	// Base is the saved profile being edited (model.NewProfile("") for a
	// new one); Form is its edited state.
	Base model.Profile
	Form Form
	// MapSummary per profile id (read-only mode).
	MapSummary map[string]*model.MappingSummary
	// Settings are the saved general settings (same revision as the
	// profiles), General the edited page; HasSettings once loaded.
	Settings    model.Settings
	General     GeneralForm
	HasSettings bool
	// Viber is the edited «Viber» page (phase 10 D4), HasViber once
	// settings.viber is loaded.
	Viber    ViberForm
	HasViber bool
	// Email is the edited «e-mail» page (CHK-TRAY-64), HasEmail once
	// settings.email is loaded.
	Email    EmailForm
	HasEmail bool
	// MapEdit / MapsEdit: mapping chosen in the Discovery dialog (6d,
	// CHK-TRAY-07), not saved yet; nil = the profile's own mapping.
	// MapsEdit (the merged additional mappings, MergeMappings) replaces
	// Profile.Mappings while MapEdit is set.
	MapEdit  *model.Mapping
	MapsEdit []model.Mapping
	// settingsCtx: the last request was save_settings (general page), so
	// warnMinutes / errorMinutes of a reply are the general thresholds;
	// otherwise they are the profile's own (phase 8, CHK-TRAY-14).
	settingsCtx bool
}

// fromProfile fills the form with the inherited thresholds of the saved
// general settings.
func (e *Editor) fromProfile(p model.Profile) Form { return FromProfileIn(p, e.Settings) }

// NewEditor returns an empty editor.
func NewEditor(lang string, mode Mode) *Editor {
	return &Editor{Lang: lang, Mode: mode, Sel: -1}
}

func (e *Editor) t(key string) string { return i18n.T(key, e.Lang) }

// LoadConfig replaces the profiles with get_config (administrator mode).
// The selected profile is kept by id; unsaved edits are dropped.
func (e *Editor) LoadConfig(cfg model.ConfigView, revision string) {
	e.Revision = revision
	e.LoadSettings(cfg.Settings)
	e.load(append([]model.Profile(nil), cfg.Profiles...))
}

// LoadSnapshot replaces the profiles with a get_status snapshot
// (read-only mode).
func (e *Editor) LoadSnapshot(snap model.StatusSnapshot) {
	e.Revision = snap.ConfigRevision
	if snap.Settings != nil {
		e.LoadSettings(*snap.Settings)
	} else {
		// no settings (e.g. configError monitor.config_invalid): nothing
		// to show or save, also when an earlier snapshot had them
		e.Settings, e.General, e.HasSettings = model.Settings{}, GeneralForm{}, false
	}
	ps := make([]model.Profile, 0, len(snap.Profiles))
	e.MapSummary = map[string]*model.MappingSummary{}
	for _, p := range snap.Profiles {
		ps = append(ps, ProfileFromSnapshot(p))
		e.MapSummary[p.ProfileID] = p.MappingSummary
	}
	e.load(ps)
}

func (e *Editor) load(ps []model.Profile) {
	id := ""
	if !e.IsNew && e.Sel >= 0 && e.Sel < len(e.Profiles) {
		id = e.Profiles[e.Sel].ID
	}
	e.Profiles = ps
	e.IsNew = false
	e.Sel = -1
	for i := range ps {
		if ps[i].ID == id {
			e.Sel = i
		}
	}
	if e.Sel < 0 && len(ps) > 0 {
		e.Sel = 0
	}
	e.Select(e.Sel)
}

// Select edits profile i (-1 = none). Unsaved edits are dropped.
func (e *Editor) Select(i int) {
	e.IsNew = false
	e.clearMapping()
	if i < 0 || i >= len(e.Profiles) {
		e.Sel = -1
		e.Base = model.Profile{}
		e.Form = Form{}
		return
	}
	e.Sel = i
	e.Base = e.Profiles[i]
	e.Form = e.fromProfile(e.Base)
}

// New starts a new profile with the defaults of model.NewProfile (SQL
// authentication, no encryption, TLS 1.0 preset of SQL Server 2008).
func (e *Editor) New() {
	e.clearMapping()
	e.Sel = -1
	e.IsNew = true
	e.Base = model.NewProfile("")
	e.Form = e.fromProfile(e.Base)
}

// HasProfile: a profile (saved or new) is being edited.
func (e *Editor) HasProfile() bool { return e.IsNew || e.Sel >= 0 }

// CanEdit: administrator mode with a profile.
func (e *Editor) CanEdit() bool { return e.Mode == Admin && e.HasProfile() }

// Dirty reports whether the form differs from the saved profile (a typed
// password counts).
func (e *Editor) Dirty() bool {
	if e.IsNew {
		return true
	}
	if !e.HasProfile() {
		return false
	}
	if e.MapEdit != nil {
		return true
	}
	return e.formDirty()
}

// formDirty: the connection form differs from the saved profile (the
// mapping edit of the Discovery dialog does not count).
func (e *Editor) formDirty() bool {
	if e.IsNew {
		return true
	}
	if !e.HasProfile() {
		return false
	}
	saved := e.fromProfile(e.Base)
	f := e.Form
	if f.Auth == model.AuthWindows && saved.Auth == model.AuthWindows {
		f.Login = saved.Login
	}
	return f != saved
}

// Unsaved reports edits that would be lost on switching the profile,
// «Добавить», «Перечитать» or closing: a changed form (a new profile
// counts only once something was typed).
func (e *Editor) Unsaved() bool {
	return e.ProfileUnsaved() || e.GeneralUnsaved()
}

// ProfileUnsaved tells whether the profile form has unsaved edits.
func (e *Editor) ProfileUnsaved() bool {
	if e.Mode != Admin || !e.HasProfile() {
		return false
	}
	if e.IsNew {
		return e.Form != e.fromProfile(e.Base) || e.MapEdit != nil
	}
	return e.Dirty()
}

// Candidate is the profile the form would save (nil field error = valid).
func (e *Editor) Candidate() (model.Profile, *model.FieldError) {
	base := e.Base
	if e.MapEdit != nil {
		base.Mapping = *e.MapEdit
		base.Mappings = append([]model.Mapping(nil), e.MapsEdit...)
	}
	return e.Form.Apply(base)
}

// connCandidate is the profile of the connection form without any
// mapping: test_profile and the Discovery commands need only the
// connection, so an invalid mapping edit never blocks them.
func (e *Editor) connCandidate() (model.Profile, *model.FieldError) {
	e.settingsCtx = false
	base := e.Base
	base.Mapping, base.Mappings = model.Mapping{}, nil
	return e.Form.Apply(base)
}

// SetMapping takes the mapping chosen in the Discovery dialog into the
// edited profile (saved with save_profile like any other edit); maps are
// the merged additional mappings (MergeMappings), they replace
// Profile.Mappings.
func (e *Editor) SetMapping(m model.Mapping, maps []model.Mapping) {
	e.MapEdit = &m
	e.MapsEdit = append([]model.Mapping(nil), maps...)
}

// EffectiveMappings are the additional mappings the profile would be
// saved with.
func (e *Editor) EffectiveMappings() []model.Mapping {
	if e.MapEdit != nil {
		return e.MapsEdit
	}
	return e.Base.Mappings
}

func (e *Editor) clearMapping() { e.MapEdit, e.MapsEdit = nil, nil }

// EffectiveMapping is the mapping the profile would be saved with.
func (e *Editor) EffectiveMapping() model.Mapping {
	if e.MapEdit != nil {
		return *e.MapEdit
	}
	return e.Base.Mapping
}

// PasswordHint is the text next to the password box.
func (e *Editor) PasswordHint() string {
	f := e.Form
	if e.Mode == ReadOnly {
		if f.Auth == model.AuthWindows {
			return e.t(i18n.KeySetPwWindows)
		}
		if e.Base.HasPassword {
			return e.t(i18n.KeySetPwSaved)
		}
		return e.t(i18n.KeySetPwNotSaved)
	}
	saved := !e.IsNew && e.Base.HasPassword
	switch {
	case f.Auth == model.AuthWindows && saved:
		return e.t(i18n.KeySetPwWindowsDrop)
	case f.Auth == model.AuthWindows:
		return e.t(i18n.KeySetPwWindows)
	case f.ClearPassword && saved:
		return e.t(i18n.KeySetPwClear)
	case f.PasswordTyped && saved:
		return e.t(i18n.KeySetPwReplace)
	case f.PasswordTyped:
		return ""
	case e.IsNew:
		return e.t(i18n.KeySetPwNew)
	case !saved:
		return e.t(i18n.KeySetPwNone)
	}
	if p, fe := e.Candidate(); fe == nil && !model.CanReuseSavedPassword(p, e.Base) {
		return e.t(i18n.KeySetPwReenter)
	}
	return e.t(i18n.KeySetPwKeep)
}

// Hints are the warnings under the form: no encryption with SQL
// authentication (the password travels almost in clear text) and "port
// wins over the instance".
func (e *Editor) Hints() []string {
	if !e.HasProfile() {
		return nil
	}
	var out []string
	f := e.Form
	if f.Auth != model.AuthWindows && f.Encrypt.Effective() == model.EncryptDisable {
		out = append(out, e.t(i18n.KeySetHintPlaintext))
	}
	if strings.TrimSpace(f.Instance) != "" && strings.TrimSpace(f.Port) != "" && strings.TrimSpace(f.Port) != "0" {
		out = append(out, e.t(i18n.KeySetHintPortInst))
	}
	return out
}

// Problem is a refused request or an error reply: the text to show, the
// control to focus and whether "Reload" should be offered (conflict).
type Problem struct {
	Text   string
	Field  Field
	Reload bool
}

// fieldLabel is the localized name of a JSON path ({0} of validation
// texts); unknown paths are returned unchanged.
func (e *Editor) fieldLabel(path string) string {
	if l, ok := emailFieldLabel(path, e.Lang); ok {
		return l
	}
	if l, ok := viberFieldLabel(path, e.Lang); ok {
		return l
	}
	if l, ok := discoveryFieldLabel(path, e.Lang); ok {
		return l
	}
	key := map[string]string{
		"name": i18n.KeySetName, "server": i18n.KeySetHost, "port": i18n.KeySetPort, "database": i18n.KeySetDatabase,
		"authMode": i18n.KeySetAuth, "login": i18n.KeySetLogin, "password": i18n.KeySetPassword,
		"clearPassword": i18n.KeySetClearPassword, "encrypt": i18n.KeySetEncrypt, "tlsMinVersion": i18n.KeySetTLS10,
		"trustServerCert": i18n.KeySetTrust, "enabled": i18n.KeySetEnabled,
		"pollIntervalSec": i18n.KeyGenPoll, "warnMinutes": i18n.KeyGenWarn, "errorMinutes": i18n.KeyGenError,
		"beep.enabled": i18n.KeyGenBeepEnabled, "beep.warningFreqHz": i18n.KeyGenWarnFreq, "beep.warningDurMs": i18n.KeyGenWarnDur,
		"beep.errorFreqHz": i18n.KeyGenErrorFreq, "beep.errorDurMs": i18n.KeyGenErrorDur, "beep.gapMs": i18n.KeyGenGap,
		"language": i18n.KeyGenLanguage, "trayAutostart": i18n.KeyGenAutostart,
		"serverPreset": i18n.KeySetPreset, "connectTimeoutSec": i18n.KeySetConnTimeout, "queryTimeoutSec": i18n.KeySetQueryTimeout,
	}[path]
	if !e.settingsCtx { // the profile's own thresholds (phase 8)
		switch path {
		case "warnMinutes":
			key = i18n.KeySetWarnMin
		case "errorMinutes":
			key = i18n.KeySetErrorMin
		}
	}
	if key == "" {
		return path
	}
	return strings.TrimSuffix(e.t(key), ":")
}

// validationText renders errorArgs [path, validation.*, params...].
func (e *Editor) validationText(args []string) string {
	if len(args) < 2 || !strings.HasPrefix(args[1], "validation.") {
		return ""
	}
	a := append([]string(nil), args...)
	a[0] = e.fieldLabel(a[0])
	// parameters that name a field or a value get their label, never the
	// raw JSON name (phase 8, CHK-TRAY-14): «Ошибка» must be greater than
	// «Предупреждение», not "warnMinutes"
	switch {
	case args[1] == model.ValGreaterThan && len(a) > 2:
		a[2] = e.fieldLabel(a[2])
	case args[1] == model.ValPresetConflict && len(a) > 3:
		if i := EncryptIndex(model.EncryptMode(a[2])); EncryptChoices[i] == model.EncryptMode(a[2]) {
			a[2] = e.t(encryptKeys[i])
		}
		a[3] = e.t(presetKeys[PresetIndex(a[3])])
	}
	text := model.FormatMessage(args[1], a, e.Lang)
	if args[1] == model.ValSQLServerForbidden && len(args) > 2 { // phase 9b (CHK-TRAY-29): the reason
		if k, ok := forbiddenReasonKeys[args[2]]; ok {
			text += " " + e.t(k)
		}
	}
	return text
}

// forbiddenReasonKeys: {0} of validation.sql_server_forbidden.
var forbiddenReasonKeys = map[string]string{
	"np": i18n.KeyValReasonNp, "lpc": i18n.KeyValReasonLpc, "admin": i18n.KeyValReasonAdmin,
	"unc": i18n.KeyValReasonUnc, "backslash": i18n.KeyValReasonBackslash, "pipe": i18n.KeyValReasonPipe,
	"tcp": i18n.KeyValReasonTcp, "sm": i18n.KeyValReasonSm, "via": i18n.KeyValReasonVia, "slash": i18n.KeyValReasonSlash,
}

func (e *Editor) fieldProblem(fe *model.FieldError) *Problem {
	if fe.Field == "" {
		return &Problem{Text: fmt.Sprintf(e.t(i18n.KeySetFailed), fe.Msg)}
	}
	return &Problem{Text: e.validationText(fe.IPCArgs()), Field: e.focus(fe.Field, fe.Args)}
}

// focus is the control of a field error in the current request context.
func (e *Editor) focus(path string, params []string) Field {
	if e.settingsCtx {
		return FocusField(path, params)
	}
	return FocusProfileField(path, params)
}

// encryptKeys / presetKeys are the combo box texts of EncryptChoices and
// PresetChoices.
var (
	encryptKeys = []string{i18n.KeySetEncDisable, i18n.KeySetEncOptional, i18n.KeySetEncOn, i18n.KeySetEncStrict}
	presetKeys  = []string{i18n.KeySetPreset2008, i18n.KeySetPresetNone}
)

// EncryptKeys / PresetKeys: catalog keys of the combo box items, in
// EncryptChoices / PresetChoices order.
func EncryptKeys() []string { return append([]string(nil), encryptKeys...) }
func PresetKeys() []string  { return append([]string(nil), presetKeys...) }

// fits checks the 64 KiB request limit before sending (the id is added
// by the client; a 16-digit one is assumed).
func fits(req model.IPCRequest) bool {
	req.ID = 1 << 53
	b, err := json.Marshal(req)
	return err == nil && len(b)+1 <= model.IPCMaxRequest
}

func (e *Editor) checkSize(req model.IPCRequest) *Problem {
	if !fits(req) {
		return &Problem{Text: e.t(i18n.KeySetTooLarge)}
	}
	return nil
}

// SaveRequest is save_profile of the form: local validation, password
// rules (new SQL profile needs a password; empty = keep only when the
// saved one may be reused), expectedRevision, size limit. password is
// the content of the password box, read by the caller only for this
// request (used only when Form.PasswordTyped).
func (e *Editor) SaveRequest(password string) (model.IPCRequest, *Problem) {
	if !e.CanEdit() {
		return model.IPCRequest{}, &Problem{Text: e.t(i18n.KeySetNoProfile)}
	}
	if e.Revision == "" {
		return model.IPCRequest{}, &Problem{Text: e.t(i18n.KeySetNoRevision), Reload: true}
	}
	e.settingsCtx = false
	p, fe := e.Candidate()
	if fe != nil {
		return model.IPCRequest{}, e.fieldProblem(fe)
	}
	p.HasPassword = false // derived by the service
	req := model.IPCRequest{Cmd: model.CmdSaveProfile, Profile: &p, ExpectedRevision: e.Revision}
	if p.AuthMode == model.AuthSQL {
		pw := ""
		if e.Form.PasswordTyped {
			pw = password
		}
		switch {
		case pw != "" && e.Form.ClearPassword:
			return model.IPCRequest{}, e.fieldProblem(&model.FieldError{Field: "clearPassword", Key: model.ValConflict})
		case pw != "":
			req.Password = pw
		case e.Form.ClearPassword:
			req.ClearPassword = true
		case e.IsNew || !e.Base.HasPassword || !model.CanReuseSavedPassword(p, e.Base):
			return model.IPCRequest{}, &Problem{Text: i18n.Message(model.MsgTestPasswordRequired, e.Lang), Field: FieldPassword}
		}
	}
	if pr := e.checkSize(req); pr != nil {
		return model.IPCRequest{}, pr
	}
	return req, nil
}

// SaveDone applies a successful save_profile reply: the new revision, the
// saved profile (with the id the service assigned and HasPassword)
// replaces or joins the list, the password box is cleared. The text
// tells whether a poll was scheduled.
func (e *Editor) SaveDone(m model.IPCMessage) string {
	if m.Revision != "" {
		e.Revision = m.Revision
	}
	if m.Profile != nil {
		p := *m.Profile
		idx := -1
		for i := range e.Profiles {
			if e.Profiles[i].ID == p.ID {
				idx = i
			}
		}
		if idx < 0 {
			e.Profiles = append(e.Profiles, p)
			idx = len(e.Profiles) - 1
		} else {
			e.Profiles[idx] = p
		}
		e.Select(idx)
	} else {
		e.Form.PasswordTyped, e.Form.ClearPassword = false, false
	}
	if m.PollScheduled {
		return e.t(i18n.KeySetSavedPoll)
	}
	return e.t(i18n.KeySetSaved)
}

// DeleteRequest is delete_profile of the selected saved profile.
func (e *Editor) DeleteRequest() (model.IPCRequest, *Problem) {
	if e.Mode != Admin || e.IsNew || e.Sel < 0 {
		return model.IPCRequest{}, &Problem{Text: e.t(i18n.KeySetNoProfile)}
	}
	if e.Revision == "" {
		return model.IPCRequest{}, &Problem{Text: e.t(i18n.KeySetNoRevision), Reload: true}
	}
	return model.IPCRequest{Cmd: model.CmdDeleteProfile, ProfileID: e.Base.ID, ExpectedRevision: e.Revision}, nil
}

// DeleteDone applies a successful delete_profile reply.
func (e *Editor) DeleteDone(m model.IPCMessage) string {
	if m.Revision != "" {
		e.Revision = m.Revision
	}
	id := m.ProfileID
	if id == "" {
		id = e.Base.ID
	}
	for i := range e.Profiles {
		if e.Profiles[i].ID == id {
			e.Profiles = append(e.Profiles[:i], e.Profiles[i+1:]...)
			break
		}
	}
	next := e.Sel
	if next >= len(e.Profiles) {
		next = len(e.Profiles) - 1
	}
	e.Select(next)
	return e.t(i18n.KeySetDeleted)
}

// CloneRequest is clone_profile of the selected saved profile (CHK-TRAY-58).
// newId / newName empty = service defaults (generate id, "<name> (копия)").
func (e *Editor) CloneRequest() (model.IPCRequest, *Problem) {
	if e.Mode != Admin || e.IsNew || e.Sel < 0 {
		return model.IPCRequest{}, &Problem{Text: e.t(i18n.KeySetNoProfile)}
	}
	if e.Revision == "" {
		return model.IPCRequest{}, &Problem{Text: e.t(i18n.KeySetNoRevision), Reload: true}
	}
	return model.IPCRequest{Cmd: model.CmdCloneProfile, ProfileID: e.Base.ID, ExpectedRevision: e.Revision}, nil
}

// CloneDone applies a successful clone_profile reply: selects the new
// profile (password not copied). Caller focuses the password field when
// m.NeedPassword.
func (e *Editor) CloneDone(m model.IPCMessage) string {
	if m.Revision != "" {
		e.Revision = m.Revision
	}
	if m.Profile != nil {
		p := *m.Profile
		e.Profiles = append(e.Profiles, p)
		e.Select(len(e.Profiles) - 1)
		name := p.DisplayName()
		if m.NeedPassword {
			return fmt.Sprintf(e.t(i18n.KeySetClonedNeedPw), name)
		}
		return fmt.Sprintf(e.t(i18n.KeySetCloned), name)
	}
	// no profile body: select by ProfileID if listed after a reload path
	if m.ProfileID != "" {
		for i := range e.Profiles {
			if e.Profiles[i].ID == m.ProfileID {
				e.Select(i)
				break
			}
		}
	}
	return fmt.Sprintf(e.t(i18n.KeySetCloned), m.ProfileID)
}


// TestRequest is test_profile: the saved profile by id when nothing was
// edited (always in read-only mode), else the form with the typed
// password (empty = the service reuses the saved one if allowed).
func (e *Editor) TestRequest(password string) (model.IPCRequest, *Problem) {
	if !e.HasProfile() {
		return model.IPCRequest{}, &Problem{Text: e.t(i18n.KeySetNoProfile)}
	}
	if e.Mode == ReadOnly || !e.formDirty() {
		return model.IPCRequest{Cmd: model.CmdTestProfile, ProfileID: e.Base.ID}, nil
	}
	p, fe := e.connCandidate() // the mapping is not needed to connect
	if fe != nil {
		return model.IPCRequest{}, e.fieldProblem(fe)
	}
	p.HasPassword = false
	req := model.IPCRequest{Cmd: model.CmdTestProfile, ProfileID: e.Base.ID, Profile: &p}
	if p.AuthMode == model.AuthSQL && e.Form.PasswordTyped {
		req.Password = password
	}
	if pr := e.checkSize(req); pr != nil {
		return model.IPCRequest{}, pr
	}
	return req, nil
}

// TestText renders a test_profile result: success with server version,
// compatibility level, encryption and time; failure with the category and
// its text (errorKey, technical error as detail).
func (e *Editor) TestText(r model.TestProfileResult) string {
	var lines []string
	if r.OK {
		lines = append(lines, fmt.Sprintf(e.t(i18n.KeySetTestOK), r.DurationMs))
		if v := strings.TrimSpace(strings.Join([]string{r.ServerVersion, r.ProductLevel, r.Edition}, " ")); v != "" {
			lines = append(lines, fmt.Sprintf(e.t(i18n.KeySetTestVersion), strings.Join(strings.Fields(v), " ")))
		}
		if r.CompatLevel > 0 {
			lines = append(lines, fmt.Sprintf(e.t(i18n.KeySetTestCompat), r.CompatLevel))
		}
	} else {
		why := i18n.Message(model.TestCategoryKey(r.Category), e.Lang)
		lines = append(lines, fmt.Sprintf(e.t(i18n.KeySetTestFailed), why))
		if r.ErrorKey != "" && r.ErrorKey != model.TestCategoryKey(r.Category) {
			lines = append(lines, i18n.Message(r.ErrorKey, e.Lang))
		}
		if r.Category == model.TestTimeout {
			lines = append(lines, e.t(i18n.KeySetTimeoutHint))
		}
		if r.Error != "" {
			lines = append(lines, r.Error)
		}
	}
	if r.Encrypt != "" {
		enc := r.Encrypt
		if r.TLSMin != "" && r.Encrypt != string(model.EncryptDisable) {
			enc += ", TLS ≥ " + r.TLSMin
		}
		lines = append(lines, fmt.Sprintf(e.t(i18n.KeySetTestEncrypt), enc))
	}
	return strings.Join(lines, "\r\n")
}

// TestReply renders the reply of test_profile (result or error).
func (e *Editor) TestReply(m model.IPCMessage, err error) *Problem {
	if err != nil {
		return e.ErrorProblem(err, TestTimeout)
	}
	if m.Type == model.MsgTestResult && m.Test != nil {
		return &Problem{Text: e.TestText(*m.Test)}
	}
	return e.Reply(m)
}

// PollRequest is poll_now of the selected saved profile.
func (e *Editor) PollRequest() (model.IPCRequest, *Problem) {
	if e.IsNew || e.Sel < 0 {
		return model.IPCRequest{}, &Problem{Text: e.t(i18n.KeySetNoProfile)}
	}
	return model.IPCRequest{Cmd: model.CmdPollNow, ProfileID: e.Base.ID}, nil
}

// PollText renders a successful poll_now reply.
func (e *Editor) PollText(m model.IPCMessage) string {
	if m.Result == model.ResultCoalesced {
		return e.t(i18n.KeySetPollCoalesced)
	}
	return e.t(i18n.KeySetPolled)
}

// ErrorProblem renders a transport-level error of a request.
func (e *Editor) ErrorProblem(err error, timeout time.Duration) *Problem {
	switch {
	case errors.Is(err, trayipc.ErrNotConnected), errors.Is(err, ipc.ErrClientClosed):
		return &Problem{Text: e.t(i18n.KeySetNotConnected)}
	case errors.Is(err, trayipc.ErrUnsupported):
		return &Problem{Text: e.t(i18n.KeySetUnsupported)}
	case errors.Is(err, ipc.ErrTooLarge):
		return &Problem{Text: e.t(i18n.KeySetTooLarge)}
	case errors.Is(err, context.DeadlineExceeded):
		return &Problem{Text: fmt.Sprintf(e.t(i18n.KeySetTestNoReply), int(timeout/time.Second))}
	case errors.Is(err, context.Canceled):
		return &Problem{Text: e.t(i18n.KeySetTestCanceled)}
	}
	return &Problem{Text: fmt.Sprintf(e.t(i18n.KeySetFailed), err.Error())}
}

// Reply renders an error reply of the service (nil for a non-error
// message). invalid_request focuses errorArgs[0] of the current profile;
// a validation error of another (hand-edited) profile of the file comes
// with profileId (set by the service only for another profile, PLAN
// §6.2), which is then named instead of focusing a field.
func (e *Editor) Reply(m model.IPCMessage) *Problem {
	if m.Type != model.MsgError {
		return nil
	}
	base := i18n.Message(m.ErrorKey, e.Lang)
	if m.ErrorKey == "" {
		base = m.Error
		if base == "" {
			base = m.Code
		}
		base = fmt.Sprintf(e.t(i18n.KeySetFailed), base)
	}
	switch m.Code {
	case model.ErrCodeAccessDenied:
		pr := &Problem{Text: base}
		if e.Mode == ReadOnly {
			pr.Text += " " + e.t(i18n.KeySetAccessHint)
		}
		return pr
	case model.ErrCodeConflict:
		return &Problem{Text: base + " " + e.t(i18n.KeySetConflictHint), Reload: true}
	case model.ErrCodeInvalidRequest:
		if len(m.ErrorArgs) == 0 {
			if f, ok := emailKeyFocus[m.ErrorKey]; ok {
				return &Problem{Text: base, Field: f}
			}
			if f, ok := viberKeyFocus[m.ErrorKey]; ok {
				return &Problem{Text: base, Field: f}
			}
			return &Problem{Text: base}
		}
		path := m.ErrorArgs[0]
		var params []string
		if len(m.ErrorArgs) > 2 {
			params = m.ErrorArgs[2:]
		}
		detail := e.validationText(m.ErrorArgs)
		if m.ProfileID != "" { // only set for ANOTHER profile of the file
			return e.otherProfile(m, detail)
		}
		if f, ok := emailKeyFocus[m.ErrorKey]; ok && !strings.HasPrefix(path, "email") {
			return &Problem{Text: base, Field: f}
		}
		if f, ok := viberKeyFocus[m.ErrorKey]; ok && !strings.HasPrefix(path, "viber") {
			return &Problem{Text: base, Field: f} // notify.viber_no_token / _no_receivers
		}
		if path == "expectedRevision" {
			return &Problem{Text: e.t(i18n.KeySetNoRevision), Reload: true}
		}
		text := base
		if detail != "" && m.ErrorKey != model.MsgTestPasswordRequired && m.ErrorKey != model.MsgPasswordTargetChange {
			text += " " + detail
		}
		return &Problem{Text: text, Field: e.focus(path, params)}
	case model.ErrCodeRateLimited:
		return &Problem{Text: base + " " + e.t(i18n.KeySetRateHint)}
	}
	if f, ok := emailKeyFocus[m.ErrorKey]; ok {
		return &Problem{Text: base, Field: f}
	}
	if f, ok := viberKeyFocus[m.ErrorKey]; ok {
		return &Problem{Text: base, Field: f}
	}
	return &Problem{Text: base}
}

// otherProfile is the error of another (hand-edited) profile of the file
// named by the profileId of the reply.
func (e *Editor) otherProfile(m model.IPCMessage, detail string) *Problem {
	name := m.ProfileID
	for _, p := range e.Profiles {
		if p.ID == m.ProfileID {
			name = p.DisplayName()
		}
	}
	if detail == "" {
		detail = m.Error
	}
	return &Problem{Text: fmt.Sprintf(e.t(i18n.KeySetOtherInvalid), name, strings.TrimSuffix(detail, "."))}
}
