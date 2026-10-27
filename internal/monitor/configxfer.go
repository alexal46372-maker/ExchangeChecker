package monitor

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/version"
)

// Phase 10 (EC Service, CHK-CFG-06) + phase 12 (CHK-CFG-07 / CHK-IPC-14):
// export_config / import_config (admin only, checked by the IPC server).
// Format model.ConfigFormatV1: settings + profiles with hasPassword; no
// password, no secret, no Viber token / hasToken / status / viber.dpapi.
// includeViberRecipients (default false) opts into non-secret settings.viber
// on export and applying it on import. The tray block is EC Tray's: never
// stored or exported by the service.

func importErr(key, field, valKey, msg string) error {
	return invalid(key, &model.FieldError{Field: field, Key: valKey, Msg: msg})
}

// ExportConfig implements ipc.ConfigTransfer (includeViberRecipients false).
func (m *Monitor) ExportConfig() (model.ConfigExport, error) {
	return m.ExportConfigOpts(false)
}

// ExportConfigOpts implements the optional ipc viber-recipients transfer
// (phase 12, CHK-CFG-07): includeViberRecipients includes non-secret
// settings.viber (recipients, enabled, schedule, events, reminderMinutes)
// and sets containsViberRecipients when at least one recipient is present.
// hasToken, status and the token are never exported.
func (m *Monitor) ExportConfigOpts(includeViberRecipients bool) (model.ConfigExport, error) {
	view, _, err := m.GetConfig()
	if err != nil {
		return model.ConfigExport{}, err
	}
	now := m.opt.Now().In(model.DBZone)
	st := view.Settings
	viberRecipients := false
	if includeViberRecipients && st.Viber != nil {
		v := st.Viber.Stored() // drops hasToken / status; hasToken omitempty
		st.Viber = &v
		viberRecipients = len(v.Recipients) > 0
	} else {
		st.Viber = nil // default: settings.viber is not exported (CHK-SVC-06)
	}
	logins := false
	for _, p := range view.Profiles {
		if p.AuthMode == model.AuthSQL && strings.TrimSpace(p.Login) != "" {
			logins = true
		}
	}
	return model.ConfigExport{Format: model.ConfigFormatV1, ExportedAt: &now, ServiceVersion: version.Version,
		Settings: &st, Profiles: view.Profiles, ContainsLogins: logins, ContainsViberRecipients: viberRecipients}, nil
}

// sameProfile compares two profiles field by field (reflect.DeepEqual of
// the struct), ignoring the HasPassword hint.
func sameProfile(a, b model.Profile) bool {
	a.HasPassword, b.HasPassword = false, false
	return reflect.DeepEqual(normalizeEmpty(a), normalizeEmpty(b))
}

// normalizeEmpty returns a copy of v with every nil slice and map
// (nested, incl. behind pointers) made empty, so that e.g. "mappings":
// [] and an absent mappings compare equal (CHK-MON-16).
func normalizeEmpty(v interface{}) interface{} {
	rv := reflect.ValueOf(v)
	out := reflect.New(rv.Type()).Elem()
	out.Set(normalizeValue(rv))
	return out.Interface()
}

func normalizeValue(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Slice:
		if v.IsNil() {
			return reflect.MakeSlice(v.Type(), 0, 0)
		}
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			out.Index(i).Set(normalizeValue(v.Index(i)))
		}
		return out
	case reflect.Map:
		out := reflect.MakeMapWithSize(v.Type(), v.Len())
		for _, k := range v.MapKeys() {
			out.SetMapIndex(k, normalizeValue(v.MapIndex(k)))
		}
		return out
	case reflect.Ptr:
		if v.IsNil() {
			return v
		}
		out := reflect.New(v.Type().Elem())
		out.Elem().Set(normalizeValue(v.Elem()))
		return out
	case reflect.Struct:
		out := reflect.New(v.Type()).Elem()
		out.Set(v)
		for i := 0; i < v.NumField(); i++ {
			if out.Field(i).CanSet() {
				out.Field(i).Set(normalizeValue(v.Field(i)))
			}
		}
		return out
	}
	return v
}

// ImportConfig implements ipc.ConfigTransfer (includeViberRecipients false:
// settings.viber in the file is ignored, the stored one is kept).
func (m *Monitor) ImportConfig(in *model.ConfigExport, mode, expectedRevision string, dryRun bool, account string) (model.ImportResult, error) {
	return m.ImportConfigOpts(in, mode, expectedRevision, dryRun, false, account)
}

// ImportConfigOpts implements the optional ipc viber-recipients transfer
// (phase 12, CHK-CFG-07): includeViberRecipients applies non-secret
// settings.viber from the file (token / hasToken / status ignored; the
// saved token in viber.dpapi is never touched). Absent settings.viber
// keeps the stored Viber settings even when the flag is set.
func (m *Monitor) ImportConfigOpts(in *model.ConfigExport, mode, expectedRevision string, dryRun, includeViberRecipients bool, account string) (model.ImportResult, error) {
	if mode == "" {
		mode = model.ImportMerge
	}
	res := model.ImportResult{DryRun: dryRun, Mode: mode, Added: []string{}, Updated: []string{}, Removed: []string{}, NeedPassword: []string{}}
	if mode != model.ImportMerge && mode != model.ImportReplace {
		return res, importErr(model.MsgImportMode, "mode", model.ValEnum, "mode must be merge or replace")
	}
	if !dryRun {
		if err := requireRevision(expectedRevision); err != nil {
			return res, err
		}
	}
	if in == nil {
		return res, importErr(model.MsgImportNoConfig, "config", model.ValRequired, "config is required")
	}
	if in.Format != model.ConfigFormatV1 {
		return res, importErr(model.MsgImportFormat, "format", model.ValEnum, "format must be "+model.ConfigFormatV1)
	}
	if len(in.Tray) > model.ImportTrayMax {
		return res, invalid(model.MsgImportTrayTooLarge, &model.FieldError{Field: "tray", Key: model.ValTooLong,
			Args: []string{strconv.Itoa(model.ImportTrayMax)}, Msg: "tray block longer than " + strconv.Itoa(model.ImportTrayMax) + " bytes"})
	}
	w := m.configWriter()
	if w == nil {
		return res, saveFailed(errNoStore)
	}
	var importViber *model.ViberSettings
	if in.Settings != nil {
		cp := *in.Settings
		if includeViberRecipients && cp.Viber != nil {
			v := cp.Viber.Stored() // strip hasToken / status; never a token field
			importViber = &v
			cp.Viber = &v
		} else {
			// default / no block: settings.viber is not imported (CHK-SVC-06)
			cp.Viber = nil
		}
		in.Settings = &cp
		if err := in.Settings.Validate(); err != nil {
			return res, invalid(model.MsgMonitorSettingsInvalid, err)
		}
	}
	imp := make([]model.Profile, len(in.Profiles))
	seen := map[string]bool{}
	for i, p := range in.Profiles {
		if strings.TrimSpace(p.ID) == "" {
			id, err := newProfileID()
			if err != nil {
				return res, saveFailed(err)
			}
			p.ID = id
		}
		if seen[p.ID] {
			ie := importErr(model.MsgImportDuplicateID, "profiles["+strconv.Itoa(i)+"].id", model.ValDuplicate, "duplicate profile id").(*model.IPCError)
			ie.ProfileID = p.ID
			return res, ie
		}
		seen[p.ID] = true
		p.HasPassword = false
		if err := p.Validate(); err != nil {
			ie := invalid(model.MsgMonitorProfileInvalid, err).(*model.IPCError)
			ie.ProfileID = p.ID
			return res, ie
		}
		imp[i] = p
	}
	m.cfgMu.Lock()
	defer m.cfgMu.Unlock()
	if expectedRevision != "" {
		if err := m.checkRevisionLocked(w, expectedRevision); err != nil {
			return res, err
		}
	}
	cur, sec, err := loadForWrite(w)
	if err != nil {
		return res, err
	}
	curSet, setErr := w.LoadSettings()
	if in.Settings != nil {
		if importViber != nil {
			in.Settings.Viber = importViber // non-secret only; token stays in viber.dpapi
		} else {
			in.Settings.Viber = curSet.Normalized().Viber
		}
	}
	old := map[string]model.Profile{}
	for _, p := range cur {
		old[p.ID] = p
	}
	nsec := config.Secrets{}
	for k, v := range sec {
		nsec[k] = v
	}
	byID := map[string]model.Profile{}
	for _, p := range imp {
		o, existed := old[p.ID]
		had := sec[p.ID] != ""
		switch {
		case p.AuthMode == model.AuthWindows:
			delete(nsec, p.ID)
		case existed && had && !model.TargetChanged(p, o) && model.CanReuseSavedPassword(p, o):
			// the saved secret stays
		default:
			delete(nsec, p.ID)
			res.NeedPassword = append(res.NeedPassword, p.ID)
		}
		p.HasPassword = nsec[p.ID] != ""
		byID[p.ID] = p
		switch {
		case !existed:
			res.Added = append(res.Added, p.ID)
		case !sameProfile(p, o) || had != p.HasPassword:
			res.Updated = append(res.Updated, p.ID)
		}
	}
	var next []model.Profile
	if mode == model.ImportReplace {
		next = append(next, imp...)
		for i := range next {
			next[i] = byID[next[i].ID]
		}
		for _, p := range cur {
			if !seen[p.ID] {
				res.Removed = append(res.Removed, p.ID)
				delete(nsec, p.ID)
			}
		}
	} else {
		for _, p := range cur {
			if q, ok := byID[p.ID]; ok {
				p = q
			}
			next = append(next, p)
		}
		for _, p := range imp {
			if _, ok := old[p.ID]; !ok {
				next = append(next, byID[p.ID])
			}
		}
	}
	if next == nil {
		next = []model.Profile{}
	}
	if err := model.ValidateProfiles(next); err != nil {
		return res, invalidOther(err, "")
	}
	setChanged := in.Settings != nil && (setErr != nil || !in.Settings.Equal(curSet))
	secChanged := len(nsec) != len(sec)
	for k, v := range nsec {
		if sec[k] != v {
			secChanged = true
		}
	}
	profChanged := len(res.Added)+len(res.Updated)+len(res.Removed) > 0 || len(next) != len(cur)
	if dryRun {
		rev, err := configRevision(w)
		if err != nil {
			return res, saveFailed(err)
		}
		res.Revision = rev
		return res, nil
	}
	// write order (architect review): profiles -> settings -> secrets
	// (import only deletes passwords, so the secrets go last); a failed
	// step restores the files written before it; a failed restore is
	// reported (monitor.import_rollback_failed), not only logged
	var restored []error
	undo := func(stage string, err error, prof, set bool) error {
		if prof {
			if rerr := w.SaveProfiles(cur); rerr != nil {
				restored = append(restored, fmt.Errorf("profiles: %w", rerr))
			}
		}
		if set {
			if setErr != nil {
				restored = append(restored, fmt.Errorf("settings: the previous settings.json was unreadable: %w", setErr))
			} else if rerr := w.SaveSettings(curSet); rerr != nil {
				restored = append(restored, fmt.Errorf("settings: %w", rerr))
			}
		}
		if prof || set {
			m.reloadConfigLocked(true) // the snapshot shows what is on disk
		}
		if len(restored) > 0 {
			msg := fmt.Sprintf("import_config: %s failed (%v) and the previous configuration was not restored: %v", stage, err, restored)
			m.opt.Log.Printf("config: %s", msg)
			if m.opt.Warn != nil {
				m.opt.Warn(msg)
			}
			return &model.IPCError{Code: model.ErrCodeSaveFailed, Key: model.MsgImportRollbackFailed,
				Message: "monitor: " + msg + " (state not restored)"}
		}
		return saveFailed(fmt.Errorf("import %s: %w", stage, err))
	}
	if profChanged {
		if err := w.SaveProfiles(next); err != nil {
			return res, saveFailed(fmt.Errorf("import profiles: %w", err))
		}
	}
	if setChanged {
		if err := w.SaveSettings(*in.Settings); err != nil {
			return res, undo("settings", err, profChanged, false)
		}
	}
	if secChanged {
		if err := w.SaveSecrets(nsec); err != nil {
			return res, undo("secrets", err, profChanged, setChanged)
		}
	}
	res.Revision = m.afterWriteLocked(w)
	if setChanged && m.opt.ApplySettings != nil {
		if err := m.opt.ApplySettings(*in.Settings); err != nil {
			msg := fmt.Sprintf("warning: import_config: settings saved, but applying them failed: %v", err)
			m.opt.Log.Printf("%s", msg)
			if m.opt.Warn != nil {
				m.opt.Warn(msg)
			}
		}
	}
	m.configChanged(model.ConfigChange{Account: account, Action: model.CmdImportConfig})
	return res, nil
}
