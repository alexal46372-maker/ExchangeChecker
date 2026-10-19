package monitor

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Config is one loaded configuration.
type Config struct {
	Settings  model.Settings
	Profiles  []model.Profile
	Passwords map[string]string
	// Err is a non-fatal load problem (invalid settings, an individually
	// invalid profile): the monitor applies what could be loaded and
	// reports it.
	Err error
	// Fatal is set when profiles.json or the secrets file could not be
	// read, decrypted or parsed. Such a Config must NOT be applied: the
	// monitor keeps the last good configuration (readers, counters state,
	// alerts), warns with anti-spam and retries on the next config check
	// (CHK-MON-10).
	Fatal error
	// SettingsFatal is set when settings.json exists but cannot be read or
	// parsed (I/O, broken JSON, unsupported version); Settings then holds
	// the defaults. The monitor keeps the last good settings (defaults on
	// the first start) and still applies the profiles (CHK-MON-10).
	SettingsFatal error
	// Revision is the configuration revision of exactly the bytes Load
	// parsed (FileConfig, phase 6b); "" = unknown (MemConfig, or a file
	// could not be read): the monitor then hashes the files itself.
	Revision string
}

// ConfigSource provides the configuration and a cheap change stamp.
type ConfigSource interface {
	// Load reads the configuration (never fails hard; see Config.Err).
	Load() Config
	// Stamp changes whenever a config file changes (size / mtime).
	Stamp() string
}

// FileConfig reads settings.json, profiles.json and secrets.dpapi from a
// config.Store (CHK-MON-04).
type FileConfig struct {
	Store *config.Store
}

// Stamp implements ConfigSource: name, size and mtime of every config
// file (os.Stat = GetFileAttributesExW on Windows; no watcher threads).
func (f FileConfig) Stamp() string {
	var b strings.Builder
	for _, name := range []string{config.SettingsFile, config.ProfilesFile, config.SecretsFile} {
		fi, err := os.Stat(f.Store.Path(name))
		if err != nil {
			fmt.Fprintf(&b, "%s:-;", name)
			continue
		}
		fmt.Fprintf(&b, "%s:%d:%d;", name, fi.Size(), fi.ModTime().UnixNano())
	}
	return b.String()
}

// Load implements ConfigSource. A missing settings.json -> defaults; one
// that cannot be read or parsed sets SettingsFatal; invalid settings values
// -> defaults; invalid
// profiles are skipped individually (the valid ones are still polled).
// Every such problem is joined into Err. A profiles.json that cannot be
// read or parsed (I/O error, broken JSON, unsupported version) and an
// unreadable / undecryptable / broken secrets file set Fatal instead
// (CHK-MON-10): applying the empty result would drop every profile and
// wipe the counters state. A missing file is not an error.
func (f FileConfig) Load() Config {
	var errs []error
	var settingsFatal error
	// each file is read once; the parsed data and the revision come from
	// the same bytes (phase 6b: configRevision cannot describe a newer
	// file than the one applied)
	rawSet, errSet := f.Store.ReadFileRaw(config.SettingsFile)
	rawProf, errProf := f.Store.ReadFileRaw(config.ProfilesFile)
	rawSec, errSec := f.Store.ReadFileRaw(config.SecretsFile)
	rev := ""
	if errSet == nil && errProf == nil && errSec == nil {
		rev = revisionOf(rawSet, rawProf, rawSec)
	}
	st, err := model.DefaultSettings(), errSet
	if err == nil {
		st, err = config.ParseSettings(rawSet)
	}
	if err != nil {
		// config.Store returns exactly the defaults for read / parse /
		// version errors and the parsed (hence non-default: the defaults
		// are valid) settings with a validation error.
		if st.Equal(model.DefaultSettings()) {
			settingsFatal = err
		} else {
			errs = append(errs, err)
			if st.Validate() != nil {
				st = model.DefaultSettings()
			}
		}
	}
	var fatal []error
	ps, err := []model.Profile{}, errProf
	if err == nil {
		ps, err = config.ParseProfiles(rawProf)
	}
	if err != nil {
		// config.Store returns the parsed list with a validation error and
		// an empty list for read / parse / version errors (an empty list
		// is always valid, so len == 0 means the file itself is unusable).
		if len(ps) == 0 {
			fatal = append(fatal, err)
		} else {
			errs = append(errs, err)
			ps = validProfiles(ps)
		}
	}
	pw := map[string]string{}
	var sec config.Secrets
	err = errSec
	if err == nil {
		sec, err = f.Store.DecodeSecrets(rawSec)
	}
	if err != nil {
		fatal = append(fatal, err)
	}
	for id, v := range sec {
		pw[id] = v
	}
	return Config{Settings: st, Profiles: ps, Passwords: pw, Err: errors.Join(errs...), Fatal: errors.Join(fatal...),
		SettingsFatal: settingsFatal, Revision: rev}
}

// validProfiles keeps the individually valid profiles with unique IDs.
func validProfiles(ps []model.Profile) []model.Profile {
	out := make([]model.Profile, 0, len(ps))
	seen := map[string]bool{}
	for _, p := range ps {
		if p.Validate() != nil || seen[p.ID] {
			continue
		}
		seen[p.ID] = true
		out = append(out, p)
	}
	return out
}

// MemConfig is an in-memory ConfigSource (tests, embedding): Set replaces
// the configuration and changes the stamp.
type MemConfig struct {
	mu  sync.Mutex
	cfg Config
	gen int
}

// NewMemConfig returns a MemConfig holding cfg.
func NewMemConfig(cfg Config) *MemConfig { return &MemConfig{cfg: cfg} }

// Set replaces the configuration.
func (m *MemConfig) Set(cfg Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg = cfg
	m.gen++
}

// Load implements ConfigSource.
func (m *MemConfig) Load() Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.cfg
	c.Profiles = append([]model.Profile(nil), c.Profiles...)
	return c
}

// Stamp implements ConfigSource.
func (m *MemConfig) Stamp() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return fmt.Sprint(m.gen)
}
