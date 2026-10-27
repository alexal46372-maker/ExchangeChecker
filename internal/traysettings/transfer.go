package traysettings

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayalert"
)

// Phase 10 E (CHK-TRAY-44) + phase 12 (CHK-TRAY-57): export / import of
// the configuration in the administrator settings window. The file is
// the export_config reply ("ec-config/1") with EC Tray's own "tray"
// block added (HKCU of the user who exports: language, balloons, sound
// with the master volume, window mode, theme). Viber recipients are
// optional via includeViberRecipients (never the token; CHK-CFG-07).

// TrayFormat is the format of the tray block.
const TrayFormat = "ec-tray/1"

// TransferFileMax is the largest file the tray reads (the import_config
// request limit, model.IPCMaxImportRequest).
const TransferFileMax = model.IPCMaxImportRequest

// TrayBlock is the "tray" block of the file.
type TrayBlock struct {
	Format string `json:"format"`
	// Language: "" = the language of Windows, "ru", "en".
	Language string                `json:"language"`
	Balloons bool                  `json:"balloons"`
	Sound    *trayalert.SoundPrefs `json:"sound,omitempty"`
	UIMode   UIMode                `json:"uiMode,omitempty"`
	// Theme: "light" / "dark" (HKCU Theme, CHK-TRAY-55/57); empty = light.
	Theme string `json:"theme,omitempty"`
}

// stripViber drops the Viber part of the settings (default export /
// import without includeViberRecipients).
func stripViber(s *model.Settings) *model.Settings {
	if s == nil {
		return nil
	}
	c := *s
	c.Viber = nil
	return &c
}

// keepViberStored keeps non-secret settings.viber (ForExport form: no
// hasToken / status) for a file with includeViberRecipients.
func keepViberStored(s *model.Settings) *model.Settings {
	if s == nil || s.Viber == nil {
		return stripViber(s)
	}
	c := *s
	v := s.Viber.ForExport()
	c.Viber = &v
	return &c
}

// ExportFile is the file of an export_config reply with the tray block.
// tray nil: no tray block (elevated as another user).
// includeViber: keep non-secret settings.viber from the service reply
// (CHK-TRAY-57 / CHK-CFG-07); otherwise strip it and clear
// containsViberRecipients.
func ExportFile(exp model.ConfigExport, tray *TrayBlock, includeViber bool) ([]byte, error) {
	if includeViber {
		exp.Settings = keepViberStored(exp.Settings)
	} else {
		exp.Settings = stripViber(exp.Settings)
		exp.ContainsViberRecipients = false
	}
	exp.Tray = nil
	if tray != nil {
		t := *tray
		t.Format = TrayFormat
		raw, err := json.Marshal(t)
		if err != nil {
			return nil, err
		}
		exp.Tray = raw
	}
	return json.MarshalIndent(exp, "", "  ")
}

// ImportFile is a parsed file: Config is sent to import_config (without
// Viber and without the tray block); Tray is nil when the file has none
// (or an unknown one).
type ImportFile struct {
	Config model.ConfigExport
	Tray   *TrayBlock
}

// ErrFileTooLarge: the file is above TransferFileMax.
var ErrFileTooLarge = errors.New("file too large")

// ParseImport reads a file. The error texts are for the user (lang).
func ParseImport(data []byte, lang string) (*ImportFile, error) {
	if len(data) > TransferFileMax {
		return nil, fmt.Errorf(i18n.T(i18n.KeyXferTooLarge, lang), TransferFileMax/1024)
	}
	data = []byte(strings.TrimPrefix(string(data), "\ufeff"))
	var cfg model.ConfigExport
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf(i18n.T(i18n.KeyXferBadFile, lang), err.Error())
	}
	if cfg.Format != model.ConfigFormatV1 {
		return nil, errors.New(i18n.Message(model.MsgImportFormat, lang))
	}
	if len(cfg.Tray) > model.ImportTrayMax {
		return nil, errors.New(i18n.Message(model.MsgImportTrayTooLarge, lang) + fmt.Sprintf(" (> %d KB)", model.ImportTrayMax/1024))
	}
	f := &ImportFile{}
	if len(cfg.Tray) > 0 {
		var t TrayBlock
		if json.Unmarshal(cfg.Tray, &t) == nil && t.Format == TrayFormat {
			if t.Sound != nil {
				p := t.Sound.Normalized(trayalert.DefaultSoundPrefs(nil))
				t.Sound = &p
			}
			if t.UIMode != UIExpert {
				t.UIMode = UISimple
			}
			switch t.Language {
			case "ru", "en":
			default:
				t.Language = ""
			}
			switch t.Theme {
			case "light", "dark":
			default:
				t.Theme = ""
			}
			f.Tray = &t
		}
	}
	cfg.Tray = nil
	// Keep non-secret viber for optional includeViberRecipients on
	// ImportRequest (CHK-TRAY-57); default ImportRequest still strips.
	cfg.Settings = keepViberStored(cfg.Settings)
	f.Config = cfg
	return f, nil
}

// MissingSounds lists the sound files of the tray block that do not
// exist here (paths are imported as they are; a missing file plays the
// tone).
func (t *TrayBlock) MissingSounds(exists func(string) bool) []string {
	if t == nil || t.Sound == nil {
		return nil
	}
	var out []string
	for _, p := range []trayalert.SoundProfile{t.Sound.Warning, t.Sound.Error} {
		if p.Kind == trayalert.SoundFile && p.File != "" && !exists(p.File) {
			out = append(out, p.File)
		}
	}
	return out
}

// Preview is the text of a dry run. names: profile id -> name (of the
// file and of the current configuration).
func Preview(res model.ImportResult, names map[string]string, tray *TrayBlock, applyTray bool, exists func(string) bool, lang string) string {
	list := func(ids []string) string {
		n := make([]string, 0, len(ids))
		for _, id := range ids {
			if s := names[id]; s != "" {
				n = append(n, s)
			} else {
				n = append(n, id)
			}
		}
		sort.Strings(n)
		return strings.Join(n, ", ")
	}
	var lines []string
	add := func(key string, ids []string) {
		if len(ids) > 0 {
			lines = append(lines, fmt.Sprintf(i18n.T(key, lang), len(ids), list(ids)))
		}
	}
	add(i18n.KeyXferAdded, res.Added)
	add(i18n.KeyXferUpdated, res.Updated)
	add(i18n.KeyXferRemoved, res.Removed)
	add(i18n.KeyXferNeedPw, res.NeedPassword)
	if len(res.Added)+len(res.Updated)+len(res.Removed) == 0 {
		lines = append(lines, i18n.T(i18n.KeyXferNoChanges, lang))
	}
	switch {
	case tray == nil:
		lines = append(lines, i18n.T(i18n.KeyXferNoTray, lang))
	case !applyTray:
		lines = append(lines, i18n.T(i18n.KeyXferTrayKept, lang))
	default:
		lines = append(lines, tray.Summary(lang))
		for _, f := range tray.MissingSounds(exists) {
			lines = append(lines, fmt.Sprintf(i18n.T(i18n.KeyXferNoSoundFile, lang), f))
		}
	}
	return strings.Join(lines, "\r\n")
}

// Summary describes the tray block in one line.
func (t *TrayBlock) Summary(lang string) string {
	l := i18n.T(i18n.KeyXferLangAuto, lang)
	switch t.Language {
	case "ru":
		l = i18n.T(i18n.KeyMenuLangRU, lang)
	case "en":
		l = i18n.T(i18n.KeyMenuLangEN, lang)
	}
	b := i18n.T(i18n.KeyXferOff, lang)
	if t.Balloons {
		b = i18n.T(i18n.KeyXferOn, lang)
	}
	vol := "—"
	if t.Sound != nil {
		vol = fmt.Sprintf("%d %%", t.Sound.Master)
	}
	m := i18n.T(i18n.KeySetModeSimple, lang)
	if t.UIMode == UIExpert {
		m = i18n.T(i18n.KeySetModeExpert, lang)
	}
	th := i18n.T(i18n.KeyGenThemeLight, lang)
	if t.Theme == "dark" {
		th = i18n.T(i18n.KeyGenThemeDark, lang)
	}
	return fmt.Sprintf(i18n.T(i18n.KeyXferTraySummary, lang), l, b, vol, m, th)
}

// ImportRequest is the import_config request (dryRun or the real one
// with the revision of the dry run). includeViber sets
// includeViberRecipients and keeps settings.viber; otherwise Viber is
// stripped (service keeps the stored one).
func ImportRequest(f *ImportFile, mode string, dryRun bool, revision string, includeViber bool) model.IPCRequest {
	cfg := f.Config
	if includeViber {
		cfg.Settings = keepViberStored(cfg.Settings)
	} else {
		cfg.Settings = stripViber(cfg.Settings)
	}
	return model.IPCRequest{Cmd: model.CmdImportConfig, Config: &cfg, Mode: mode, DryRun: dryRun, ExpectedRevision: revision,
		IncludeViberRecipients: includeViber}
}

// ExportRequest is the export_config request (CHK-TRAY-57).
func ExportRequest(includeViber bool) model.IPCRequest {
	return model.IPCRequest{Cmd: model.CmdExportConfig, IncludeViberRecipients: includeViber}
}

// Names maps the profile ids of the file and of the editor to names.
func (e *Editor) Names(f *ImportFile) map[string]string {
	m := map[string]string{}
	for _, p := range e.Profiles {
		m[p.ID] = p.DisplayName()
	}
	if f != nil {
		for _, p := range f.Config.Profiles {
			if p.ID != "" {
				m[p.ID] = p.DisplayName()
			}
		}
	}
	return m
}
