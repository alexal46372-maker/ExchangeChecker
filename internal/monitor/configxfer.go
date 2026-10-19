package monitor

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/version"
)

// Phase 10 (EC Service, CHK-CFG-06): export_config / import_config (admin
// only, checked by the IPC server). Format model.ConfigFormatV1: settings
// + profiles with hasPassword; no password, no secret. The tray block is
// EC Tray's: never stored or exported by the service.

func importErr(key, field, valKey, msg string) error {
	return invalid(key, &model.FieldError{Field: field, Key: valKey, Msg: msg})
}

// ExportConfig implements ipc.ConfigTransfer.
func (m *Monitor) ExportConfig() (model.ConfigExport, error) {
	view, _, err := m.GetConfig()
	if err != nil {
		return model.ConfigExport{}, err
	}
	now := m.opt.Now().In(model.DBZone)
	st := view.Settings
	return model.ConfigExport{Format: model.ConfigFormatV1, ExportedAt: &now, ServiceVersion: version.Version,
		Settings: &st, Profiles: view.Profiles}, nil
}

// profileJSON compares profiles without the HasPassword hint.
func profileJSON(p model.Profile) string {
	p.HasPassword = false
	b, _ := json.Marshal(p)
	return string(b)
}

// ImportConfig implements ipc.ConfigTransfer: full validation first, then
// (unless dryRun) the write of secrets.dpapi, profiles.json and
// settings.json (each atomic; a failed step restores the previous files,
// best effort), a synchronous reload and event 105. mode "" = merge.
// expectedRevision is mandatory for a real import, checked when given for
// a dry run. Passwords: Windows authentication drops the saved one; SQL
// authentication keeps it only for the same profile id when the target
// is unchanged and model.CanReuseSavedPassword holds, else the secret is
// dropped and the id is listed in NeedPassword.
func (m *Monitor) ImportConfig(in *model.ConfigExport, mode, expectedRevision string, dryRun bool, account string) (model.ImportResult, error) {
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
	w := m.configWriter()
	if w == nil {
		return res, saveFailed(errNoStore)
	}
	if in.Settings != nil {
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
		case profileJSON(p) != profileJSON(o) || had != p.HasPassword:
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
	setChanged := in.Settings != nil && (setErr != nil || in.Settings.Normalized() != curSet.Normalized())
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
	if secChanged {
		if err := w.SaveSecrets(nsec); err != nil {
			return res, saveFailed(err)
		}
	}
	restore := func(stage string, err error) error {
		if secChanged {
			if rerr := w.SaveSecrets(sec); rerr != nil {
				m.opt.Log.Printf("config: import_config: restoring secrets failed: %v", rerr)
			}
		}
		if stage == "settings" && profChanged {
			if rerr := w.SaveProfiles(cur); rerr != nil {
				m.opt.Log.Printf("config: import_config: restoring profiles failed: %v", rerr)
			}
		}
		return saveFailed(fmt.Errorf("import %s: %w", stage, err))
	}
	if profChanged {
		if err := w.SaveProfiles(next); err != nil {
			return res, restore("profiles", err)
		}
	}
	if setChanged {
		if err := w.SaveSettings(*in.Settings); err != nil {
			return res, restore("settings", err)
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
