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
	// Err is a non-fatal load problem (invalid file, DPAPI failure): the
	// monitor keeps running with what could be loaded and reports it.
	Err error
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

// Load implements ConfigSource. Invalid settings -> defaults; invalid
// profiles are skipped individually (the valid ones are still polled);
// unreadable secrets -> no passwords. Every problem is joined into Err.
func (f FileConfig) Load() Config {
	var errs []error
	st, err := f.Store.LoadSettings()
	if err != nil {
		errs = append(errs, err)
		if st.Validate() != nil {
			st = model.DefaultSettings()
		}
	}
	ps, err := f.Store.LoadProfiles()
	if err != nil {
		errs = append(errs, err)
		ps = validProfiles(ps)
	}
	pw := map[string]string{}
	sec, err := f.Store.LoadSecrets()
	if err != nil {
		errs = append(errs, err)
	}
	for id, v := range sec {
		pw[id] = v
	}
	return Config{Settings: st, Profiles: ps, Passwords: pw, Err: errors.Join(errs...)}
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
