package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// File and directory names.
const (
	AppDirName    = "ExchangeChecker"
	SettingsFile  = "settings.json"
	ProfilesFile  = "profiles.json"
	SecretsFile   = "secrets.dpapi"
	SchemaVersion = 1

	fallbackProgramData = `C:\ProgramData`
)

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// DefaultDir returns %ProgramData%\ExchangeChecker (fallback C:\ProgramData).
func DefaultDir() string {
	pd := os.Getenv("ProgramData")
	if pd == "" {
		pd = fallbackProgramData
	}
	return filepath.Join(pd, AppDirName)
}

// Store reads and writes configuration files in Dir. Protector encrypts
// secrets.dpapi; tests may inject a fake.
type Store struct {
	Dir       string
	Protector Protector
}

// NewStore returns a Store for dir ("" = DefaultDir()) with the DPAPI protector.
func NewStore(dir string) *Store {
	if dir == "" {
		dir = DefaultDir()
	}
	return &Store{Dir: dir, Protector: DefaultProtector()}
}

// Path returns the full path of a config file name inside the store dir.
func (s *Store) Path(name string) string { return filepath.Join(s.Dir, name) }

type settingsDoc struct {
	Version int `json:"version"`
	model.Settings
}

type profilesDoc struct {
	Version  int             `json:"version"`
	Profiles []model.Profile `json:"profiles"`
}

// ReadFileRaw returns the raw bytes of a config file (BOM included) or
// (nil, nil) when it does not exist. The monitor reads each file once with
// it and derives both the parsed configuration (Parse*, DecodeSecrets) and
// the configuration revision from the same bytes (phase 6b).
func (s *Store) ReadFileRaw(name string) ([]byte, error) {
	b, err := ReadFileShared(s.Path(name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", name, err)
	}
	if b == nil {
		b = []byte{}
	}
	return b, nil
}

// readFile is ReadFileRaw without the UTF-8 BOM.
func (s *Store) readFile(name string) ([]byte, error) {
	b, err := s.ReadFileRaw(name)
	if b == nil || err != nil {
		return nil, err
	}
	return bytes.TrimPrefix(b, utf8BOM), nil
}

func trimBOM(raw []byte) []byte {
	if raw == nil {
		return nil
	}
	return bytes.TrimPrefix(raw, utf8BOM)
}

func checkVersion(name string, v int) error {
	if v > SchemaVersion {
		return fmt.Errorf("config: %s: unsupported version %d (max %d)", name, v, SchemaVersion)
	}
	return nil
}

// LoadSettings reads settings.json. Missing file -> defaults, nil error.
// Fields absent from the file keep their default values. On a validation
// error the parsed settings are returned together with the error.
func (s *Store) LoadSettings() (model.Settings, error) {
	b, err := s.ReadFileRaw(SettingsFile)
	if err != nil {
		return model.DefaultSettings(), err
	}
	return ParseSettings(b)
}

// ParseSettings is LoadSettings over raw file bytes (ReadFileRaw; nil =
// missing file -> defaults).
func ParseSettings(raw []byte) (model.Settings, error) {
	b := trimBOM(raw)
	if b == nil {
		return model.DefaultSettings(), nil
	}
	doc := settingsDoc{Settings: model.DefaultSettings()}
	if err := json.Unmarshal(b, &doc); err != nil {
		return model.DefaultSettings(), fmt.Errorf("config: parse %s: %w", SettingsFile, err)
	}
	if err := checkVersion(SettingsFile, doc.Version); err != nil {
		return model.DefaultSettings(), err
	}
	// beep.enabled: missing -> the default (true) prefilled above; an
	// explicit null -> true as well; false stays false
	doc.Settings = doc.Settings.Normalized()
	v := doc.Settings.Viber.Stored() // hasToken / status are views only
	doc.Settings.Viber = &v
	e := doc.Settings.Email.Stored() // hasPassword / status are views only
	doc.Settings.Email = &e
	if err := doc.Settings.Validate(); err != nil {
		return doc.Settings, fmt.Errorf("config: %s: %w", SettingsFile, err)
	}
	return doc.Settings, nil
}

// SaveSettings validates and atomically writes settings.json.
func (s *Store) SaveSettings(st model.Settings) error {
	if err := st.Validate(); err != nil {
		return err
	}
	if st.Viber != nil { // hasToken / status are views only
		v := st.Viber.Stored()
		st.Viber = &v
	}
	if st.Email != nil { // hasPassword / status are views only
		e := st.Email.Stored()
		st.Email = &e
	}
	b, err := marshalJSON(settingsDoc{Version: SchemaVersion, Settings: st})
	if err != nil {
		return err
	}
	return writeFileAtomic(s.Dir, SettingsFile, b, 0o644)
}

// LoadProfiles reads profiles.json. Missing file -> empty list, nil error.
func (s *Store) LoadProfiles() ([]model.Profile, error) {
	b, err := s.ReadFileRaw(ProfilesFile)
	if err != nil {
		return []model.Profile{}, err
	}
	return ParseProfiles(b)
}

// ParseProfiles is LoadProfiles over raw file bytes (ReadFileRaw; nil =
// missing file -> empty list).
func ParseProfiles(raw []byte) ([]model.Profile, error) {
	b := trimBOM(raw)
	if b == nil {
		return []model.Profile{}, nil
	}
	var doc profilesDoc
	if err := json.Unmarshal(b, &doc); err != nil {
		return []model.Profile{}, fmt.Errorf("config: parse %s: %w", ProfilesFile, err)
	}
	if err := checkVersion(ProfilesFile, doc.Version); err != nil {
		return []model.Profile{}, err
	}
	if doc.Profiles == nil {
		doc.Profiles = []model.Profile{}
	}
	if err := model.ValidateProfiles(doc.Profiles); err != nil {
		return doc.Profiles, fmt.Errorf("config: %s: %w", ProfilesFile, err)
	}
	return doc.Profiles, nil
}

// SaveProfiles validates and atomically writes profiles.json.
func (s *Store) SaveProfiles(ps []model.Profile) error {
	if err := model.ValidateProfiles(ps); err != nil {
		return err
	}
	if ps == nil {
		ps = []model.Profile{}
	}
	b, err := marshalJSON(profilesDoc{Version: SchemaVersion, Profiles: ps})
	if err != nil {
		return err
	}
	return writeFileAtomic(s.Dir, ProfilesFile, b, 0o644)
}

// marshalJSON produces indented UTF-8 JSON without HTML escaping.
func marshalJSON(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("config: marshal: %w", err)
	}
	return buf.Bytes(), nil
}
