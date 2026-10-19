package monitor

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Configuration writes through IPC (phase 6, G5; CHK-IPC-06, CHK-CFG-03):
// the service is the only writer of settings.json, profiles.json and
// secrets.dpapi. The IPC server has already checked that the client is an
// elevated administrator. Every write: validate -> lock the write mutex ->
// compare expectedRevision -> atomic write (temp + MoveFileEx;
// secrets.dpapi with its protected DACL) -> synchronous reload (the new
// profile is in the snapshot and accepts poll_now when the reply is sent)
// -> event 105. Passwords never reach the log, a reply or the event.

// ConfigWriter is the configuration storage written by the service
// (*config.Store).
type ConfigWriter interface {
	LoadSettings() (model.Settings, error)
	LoadProfiles() ([]model.Profile, error)
	LoadSecrets() (config.Secrets, error)
	SaveSettings(model.Settings) error
	SaveProfiles([]model.Profile) error
	SaveSecrets(config.Secrets) error
	Path(name string) string
}

var _ ConfigWriter = (*config.Store)(nil)

// configWriter returns Options.ConfigStore, else the store of a FileConfig
// source, else nil.
func (m *Monitor) configWriter() ConfigWriter {
	if m.opt.ConfigStore != nil {
		return m.opt.ConfigStore
	}
	if fc, ok := m.opt.Source.(FileConfig); ok && fc.Store != nil {
		return fc.Store
	}
	return nil
}

func saveFailed(err error) error {
	return &model.IPCError{Code: model.ErrCodeSaveFailed, Key: model.MsgMonitorConfigSaveFailed, Message: "monitor: " + err.Error()}
}

var errNoStore = errors.New("no configuration store")

// ErrConfigConflict: expectedRevision does not match (nothing written).
var ErrConfigConflict = &model.IPCError{Code: model.ErrCodeConflict, Key: model.MsgMonitorConfigConflict,
	Message: "monitor: the configuration changed (revision mismatch)"}

// errPasswordRequired: SQL authentication without a usable password.
var errPasswordRequired = &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgTestPasswordRequired,
	Message: "monitor: enter the password", Args: []string{"password", model.ValRequired}}

// errPasswordTargetChange: save_profile changed the server or port of a
// profile with a saved password and sent no new password (phase 9,
// CHK-CFG-05).
var errPasswordTargetChange = &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgPasswordTargetChange,
	Message: "monitor: the server or port changed, enter the password again", Args: []string{"password", model.ValRequired}}

// invalidOther is invalid(monitor.profile_invalid) for a write refused
// because ANOTHER profile in profiles.json is invalid (edited by hand): the
// reply carries that profile's id (IPCMessage.profileId, from
// FieldError.ProfileID set by model.ValidateProfiles); errorArgs and the
// text are unchanged. self = the profile being saved / deleted: never
// reported as "the other one".
func invalidOther(err error, self string) error {
	ie := invalid(model.MsgMonitorProfileInvalid, err).(*model.IPCError)
	if fe := model.AsFieldError(err); fe != nil && fe.ProfileID != self {
		ie.ProfileID = fe.ProfileID
	}
	return ie
}

func invalid(key string, err error) error {
	ie := &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: key, Message: err.Error()}
	if fe := model.AsFieldError(err); fe != nil {
		ie.Args = fe.IPCArgs()
	}
	return ie
}

// configRevision is the hex of the first 16 bytes of SHA-256 over
// settings.json || 0x00 || profiles.json || 0x00 || secrets.dpapi (raw
// bytes, a missing file = empty). A manual edit changes it too; it is the
// same after a restart. "" when there is no store.
func configRevision(w ConfigWriter) (string, error) {
	if w == nil {
		return "", nil
	}
	var raw [3][]byte
	for i, name := range []string{config.SettingsFile, config.ProfilesFile, config.SecretsFile} {
		b, err := os.ReadFile(w.Path(name))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		raw[i] = b
	}
	return revisionOf(raw[0], raw[1], raw[2]), nil
}

// revisionOf is the revision of the raw bytes of settings.json,
// profiles.json and secrets.dpapi (nil = missing file). FileConfig.Load
// uses it on the bytes it parsed, so the snapshot's configRevision always
// describes exactly the applied data (phase 6b).
func revisionOf(settings, profiles, secrets []byte) string {
	h := sha256.New()
	h.Write(settings)
	h.Write([]byte{0})
	h.Write(profiles)
	h.Write([]byte{0})
	h.Write(secrets)
	return hex.EncodeToString(h.Sum(nil)[:16])
}

// requireRevision: expectedRevision is mandatory for save_profile,
// delete_profile and save_settings (phase 6b, Вика): without it a client
// could overwrite a change it has not seen. Missing -> invalid_request +
// monitor.bad_request, errorArgs ["expectedRevision", "validation.required"].
func requireRevision(expected string) error {
	if expected != "" {
		return nil
	}
	return invalid(model.MsgMonitorBadRequest, &model.FieldError{Field: "expectedRevision", Key: model.ValRequired,
		Msg: "expectedRevision is required (revision of get_config or configRevision of get_status)"})
}

// checkRevisionLocked compares expected with the revision on disk (cfgMu
// held); expected is non-empty (requireRevision).
func (m *Monitor) checkRevisionLocked(w ConfigWriter, expected string) error {
	rev, err := configRevision(w)
	if err != nil {
		return saveFailed(err)
	}
	if rev != expected {
		return ErrConfigConflict
	}
	return nil
}

// afterWriteLocked reloads synchronously and returns the new revision
// (cfgMu held).
func (m *Monitor) afterWriteLocked(w ConfigWriter) string {
	if m.reloadConfigLocked(true) {
		// the revision of the bytes the reload just applied (= the
		// snapshot's configRevision)
		m.mu.Lock()
		rev := m.revision
		m.mu.Unlock()
		if rev != "" {
			return rev
		}
	}
	rev, err := configRevision(w)
	if err != nil {
		m.opt.Log.Printf("config: revision after write: %v", err)
	}
	return rev
}

func (m *Monitor) configChanged(c model.ConfigChange) {
	m.opt.Log.Printf("config: %s by %q (profile %q)", c.Action, c.Account, c.ProfileID)
	if m.opt.OnConfigChange != nil {
		m.opt.OnConfigChange(c)
	}
}

func newProfileID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = b[6]&0x0f | 0x40 // version 4
	b[8] = b[8]&0x3f | 0x80 // RFC 4122 variant
	h := hex.EncodeToString(b[:])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], nil
}

// GetConfig implements ipc.ConfigManager (admin only): settings and
// profiles with HasPassword (never a password) and the revision.
func (m *Monitor) GetConfig() (model.ConfigView, string, error) {
	w := m.configWriter()
	if w == nil {
		return model.ConfigView{}, "", saveFailed(errNoStore)
	}
	m.cfgMu.Lock()
	defer m.cfgMu.Unlock()
	st, err := w.LoadSettings()
	if err != nil && st.Equal(model.DefaultSettings()) {
		// unreadable (not just invalid) settings.json: show the defaults
		// the service runs with; the snapshot reports the config error
		m.opt.Log.Printf("config: get_config: %v", err)
	}
	ps, err := w.LoadProfiles()
	if err != nil && len(ps) == 0 {
		return model.ConfigView{}, "", saveFailed(err)
	}
	sec, err := w.LoadSecrets()
	if err != nil {
		return model.ConfigView{}, "", saveFailed(err)
	}
	out := make([]model.Profile, len(ps))
	for i, p := range ps {
		p.HasPassword = sec[p.ID] != ""
		out[i] = p
	}
	rev, err := configRevision(w)
	if err != nil {
		return model.ConfigView{}, "", saveFailed(err)
	}
	st = st.Normalized()
	m.viberView(&st)
	return model.ConfigView{Settings: st, Profiles: out}, rev, nil
}

// loadForWrite reads profiles and secrets for a write. A profiles.json or
// secrets.dpapi that cannot be read / parsed / decrypted refuses the
// write (save_failed): writing would drop the other profiles' data.
func loadForWrite(w ConfigWriter) ([]model.Profile, config.Secrets, error) {
	ps, err := w.LoadProfiles()
	if err != nil && len(ps) == 0 {
		return nil, nil, saveFailed(err)
	}
	sec, err := w.LoadSecrets()
	if err != nil {
		return nil, nil, saveFailed(err)
	}
	if sec == nil {
		sec = config.Secrets{}
	}
	return ps, sec, nil
}

// SaveProfile implements ipc.ConfigManager (admin only). Password rules
// (Вика): Windows authentication deletes the password; a non-empty
// password replaces it; clearPassword deletes it; an empty password keeps
// the saved one only when model.CanReuseSavedPassword(new, saved), a new
// SQL-auth profile (or one without a saved password) gets
// invalid_request + monitor.test_password_required.
func (m *Monitor) SaveProfile(p model.Profile, password string, clearPassword bool, expectedRevision, account string) (model.SavedProfile, error) {
	if err := requireRevision(expectedRevision); err != nil {
		return model.SavedProfile{}, err
	}
	w := m.configWriter()
	if w == nil {
		return model.SavedProfile{}, saveFailed(errNoStore)
	}
	if password != "" && clearPassword {
		return model.SavedProfile{}, invalid(model.MsgMonitorProfileInvalid, &model.FieldError{Field: "clearPassword", Key: model.ValConflict,
			Msg: "password and clearPassword are mutually exclusive"})
	}
	if strings.TrimSpace(p.ID) == "" {
		id, err := newProfileID()
		if err != nil {
			return model.SavedProfile{}, saveFailed(err)
		}
		p.ID = id
	}
	p.HasPassword = false // derived from secrets.dpapi below, never trusted from the client
	if err := p.Validate(); err != nil {
		return model.SavedProfile{}, invalid(model.MsgMonitorProfileInvalid, err)
	}
	m.cfgMu.Lock()
	defer m.cfgMu.Unlock()
	if err := m.checkRevisionLocked(w, expectedRevision); err != nil {
		return model.SavedProfile{}, err
	}
	ps, sec, err := loadForWrite(w)
	if err != nil {
		return model.SavedProfile{}, err
	}
	idx := -1
	for i := range ps {
		if ps[i].ID == p.ID {
			idx = i
			break
		}
	}
	oldSecret, hadSecret := sec[p.ID]
	hadSecret = hadSecret && oldSecret != ""
	switch {
	case p.AuthMode == model.AuthWindows:
		delete(sec, p.ID)
	case password != "":
		sec[p.ID] = password
	case clearPassword:
		delete(sec, p.ID)
	default: // empty password, SQL authentication
		if idx >= 0 && hadSecret && model.TargetChanged(p, ps[idx]) {
			// phase 9 (CHK-CFG-05): the saved secret is never reused for
			// a new server / port
			return model.SavedProfile{}, errPasswordTargetChange
		}
		if idx < 0 || !hadSecret || !model.CanReuseSavedPassword(p, ps[idx]) {
			return model.SavedProfile{}, errPasswordRequired
		}
	}
	newSecret, has := sec[p.ID]
	has = has && newSecret != ""
	p.HasPassword = has
	next := append([]model.Profile(nil), ps...)
	if idx < 0 {
		next = append(next, p)
	} else {
		next[idx] = p
	}
	if err := model.ValidateProfiles(next); err != nil {
		// another saved profile is invalid (e.g. written by hand): refuse,
		// nothing is dropped silently; the reply names it (profileId)
		return model.SavedProfile{}, invalidOther(err, p.ID)
	}
	secretsChanged := has != hadSecret || (has && newSecret != oldSecret)
	if secretsChanged {
		if err := w.SaveSecrets(sec); err != nil {
			return model.SavedProfile{}, saveFailed(err)
		}
	}
	if err := w.SaveProfiles(next); err != nil {
		if secretsChanged {
			// best effort: put the previous password back
			if hadSecret {
				sec[p.ID] = oldSecret
			} else {
				delete(sec, p.ID)
			}
			if rerr := w.SaveSecrets(sec); rerr != nil {
				m.opt.Log.Printf("config: save_profile %q: restoring secrets failed: %v", p.ID, rerr)
			}
		}
		return model.SavedProfile{}, saveFailed(err)
	}
	rev := m.afterWriteLocked(w)
	scheduled := false
	if p.Enabled {
		m.cmdMu.Lock()
		m.pendIDs[p.ID] = true
		m.cmdMu.Unlock()
		select {
		case m.pollNow <- struct{}{}:
		default:
		}
		scheduled = true
	}
	m.configChanged(model.ConfigChange{Account: account, Action: model.CmdSaveProfile, ProfileID: p.ID})
	return model.SavedProfile{Profile: p, Revision: rev, PollScheduled: scheduled}, nil
}

// DeleteProfile implements ipc.ConfigManager (admin only): the profile,
// its password, counters state, alerts and acknowledgements go.
func (m *Monitor) DeleteProfile(profileID, expectedRevision, account string) (string, error) {
	if err := requireRevision(expectedRevision); err != nil {
		return "", err
	}
	w := m.configWriter()
	if w == nil {
		return "", saveFailed(errNoStore)
	}
	m.cfgMu.Lock()
	defer m.cfgMu.Unlock()
	if err := m.checkRevisionLocked(w, expectedRevision); err != nil {
		return "", err
	}
	ps, sec, err := loadForWrite(w)
	if err != nil {
		return "", err
	}
	next := make([]model.Profile, 0, len(ps))
	found := false
	for _, p := range ps {
		if p.ID == profileID {
			found = true
			continue
		}
		next = append(next, p)
	}
	if !found || profileID == "" {
		return "", ErrUnknownProfile
	}
	if err := model.ValidateProfiles(next); err != nil {
		// another (remaining) profile in the file is invalid
		return "", invalidOther(err, profileID)
	}
	if err := w.SaveProfiles(next); err != nil {
		return "", saveFailed(err)
	}
	if _, ok := sec[profileID]; ok {
		delete(sec, profileID)
		if err := w.SaveSecrets(sec); err != nil {
			// the profile is gone; an orphaned encrypted secret is harmless
			m.opt.Log.Printf("config: delete_profile %q: removing the password failed: %v", profileID, err)
		}
	}
	rev := m.afterWriteLocked(w)
	m.configChanged(model.ConfigChange{Account: account, Action: model.CmdDeleteProfile, ProfileID: profileID})
	return rev, nil
}

// SaveSettings implements ipc.ConfigManager (admin only): the whole
// settings object incl. trayAutostart.
func (m *Monitor) SaveSettings(st model.Settings, expectedRevision, account string) (string, error) {
	return m.SaveSettingsViber(st, "", false, expectedRevision, account)
}

// viberTokenStore is the Viber token part of config.Store (CHK-SVC-06).
type viberTokenStore interface {
	HasViberToken() bool
	LoadViberToken() (string, error)
	SaveViberToken(string) error
}

// viberView fills the read-only settings.viber.hasToken / status (st
// normalized: Viber is a private copy).
func (m *Monitor) viberView(st *model.Settings) {
	if st.Viber == nil {
		return
	}
	if ts, ok := m.configWriter().(viberTokenStore); ok && m.configWriter() != nil {
		st.Viber.HasToken = ts.HasViberToken()
	}
	if m.opt.ViberStatus != nil {
		st.Viber.Status = m.opt.ViberStatus()
	}
}

// SaveSettingsViber implements ipc.ViberSettingsSaver: save_settings with
// viberToken (set; "" = no change) / clearViberToken. settings.viber
// absent = the stored one is kept; enabled needs a token (saved or
// given). The token is written first, a failed settings write restores
// it (best effort).
func (m *Monitor) SaveSettingsViber(st model.Settings, token string, clearToken bool, expectedRevision, account string) (string, error) {
	if err := requireRevision(expectedRevision); err != nil {
		return "", err
	}
	w := m.configWriter()
	if w == nil {
		return "", saveFailed(errNoStore)
	}
	if err := st.Validate(); err != nil {
		return "", invalid(model.MsgMonitorSettingsInvalid, err)
	}
	if token != "" && !model.ValidViberToken(token) {
		return "", &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgViberInvalid,
			Message: "viber: invalid token", Args: []string{"viberToken", model.ValRequired}}
	}
	m.cfgMu.Lock()
	defer m.cfgMu.Unlock()
	if err := m.checkRevisionLocked(w, expectedRevision); err != nil {
		return "", err
	}
	if st.Viber == nil {
		// a client that does not know settings.viber: keep the stored one
		cur, _ := w.LoadSettings()
		st.Viber = cur.Normalized().Viber
	}
	if st.Viber != nil {
		v := st.Viber.Stored()
		st.Viber = &v
	}
	ts, _ := w.(viberTokenStore)
	if (token != "" || clearToken) && ts == nil {
		return "", saveFailed(errors.New("viber: no token store"))
	}
	var oldTok string
	hasTok := false
	if ts != nil {
		var err error
		if oldTok, err = ts.LoadViberToken(); err != nil && (token != "" || clearToken) {
			return "", saveFailed(fmt.Errorf("viber token: %w", err))
		}
		hasTok = oldTok != "" || ts.HasViberToken()
	}
	newTok, tokChanged := oldTok, false
	if clearToken {
		newTok, hasTok, tokChanged = "", false, oldTok != "" || ts.HasViberToken()
	}
	if token != "" {
		newTok, hasTok, tokChanged = token, true, token != oldTok
	}
	if st.Viber != nil && st.Viber.Enabled && !hasTok {
		return "", &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgViberNoToken,
			Message: "viber: enabled without a token", Args: []string{"viberToken", model.ValRequired}}
	}
	if tokChanged {
		if err := ts.SaveViberToken(newTok); err != nil {
			return "", saveFailed(fmt.Errorf("save viber token: %w", err))
		}
	}
	if st.Beep.Enabled == model.OptUnset {
		// beep.enabled not sent (a client that does not know the field,
		// e.g. tray 6c): keep the stored value instead of silently
		// switching the sound back on
		st.Beep.Enabled = model.OptTrue
		if cur, err := w.LoadSettings(); err == nil || !cur.Equal(model.DefaultSettings()) {
			st.Beep.Enabled = cur.Normalized().Beep.Enabled
		}
	}
	if err := w.SaveSettings(st); err != nil {
		if tokChanged {
			if rerr := ts.SaveViberToken(oldTok); rerr != nil {
				m.opt.Log.Printf("config: save_settings: restoring the Viber token failed: %v", rerr)
			}
		}
		return "", saveFailed(fmt.Errorf("save settings: %w", err))
	}
	if tokChanged && m.opt.OnViberToken != nil {
		m.opt.OnViberToken()
	}
	rev := m.afterWriteLocked(w)
	if m.opt.ApplySettings != nil {
		// the settings are saved; a failure here (e.g. the HKLM Run
		// value) is a warning only (CHK-CFG-04, PLAN §6.2)
		if err := m.opt.ApplySettings(st); err != nil {
			msg := fmt.Sprintf("warning: save_settings: settings saved, but applying them failed: %v", err)
			m.opt.Log.Printf("%s", msg)
			if m.opt.Warn != nil {
				m.opt.Warn(msg)
			}
		}
	}
	m.configChanged(model.ConfigChange{Account: account, Action: model.CmdSaveSettings})
	return rev, nil
}
