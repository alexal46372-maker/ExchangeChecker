package model

import (
	"errors"
	"fmt"
	"strings"
)

// AuthMode selects how the service logs in to MSSQL (both required in v1).
type AuthMode string

const (
	AuthSQL     AuthMode = "sql"
	AuthWindows AuthMode = "windows"
)

// Valid reports whether a is a supported auth mode.
func (a AuthMode) Valid() bool { return a == AuthSQL || a == AuthWindows }

// MappingMode tells how the mapping was obtained (PLAN.md §5.3).
type MappingMode string

const (
	MappingNone   MappingMode = ""       // not configured yet
	MappingAuto   MappingMode = "auto"   // confirmed Discovery candidate
	MappingManual MappingMode = "manual" // edited by hand / manual SQL fallback
)

// DefaultSQLPort is used when Profile.Port is 0.
const DefaultSQLPort = 1433

// Mapping is the discovered (or manually edited) location of exchange
// timestamps inside a 1C 8.1 database.
type Mapping struct {
	Mode            MappingMode `json:"mode,omitempty"`
	Table           string      `json:"table,omitempty"`
	PointIDColumn   string      `json:"pointIdColumn,omitempty"`
	PointNameColumn string      `json:"pointNameColumn,omitempty"`
	LastSendColumn  string      `json:"lastSendColumn,omitempty"`
	LastRecvColumn  string      `json:"lastRecvColumn,omitempty"`
	// ManualSQL is the fallback query for non-standard databases. It must
	// return point name, last send, last recv (validated in phase 2).
	ManualSQL string `json:"manualSql,omitempty"`
}

// Configured reports whether the mapping can be used for polling.
func (m Mapping) Configured() bool {
	return strings.TrimSpace(m.ManualSQL) != "" || strings.TrimSpace(m.Table) != ""
}

// Validate checks internal consistency of the mapping.
func (m Mapping) Validate() error {
	switch m.Mode {
	case MappingNone, MappingAuto, MappingManual:
	default:
		return fmt.Errorf("mapping: unknown mode %q", string(m.Mode))
	}
	if strings.TrimSpace(m.Table) != "" {
		if strings.TrimSpace(m.PointNameColumn) == "" && strings.TrimSpace(m.PointIDColumn) == "" {
			return errors.New("mapping: point name/id column is required when table is set")
		}
		if strings.TrimSpace(m.LastSendColumn) == "" && strings.TrimSpace(m.LastRecvColumn) == "" {
			return errors.New("mapping: at least one of last-send/last-recv columns is required")
		}
	}
	if m.Mode == MappingManual && !m.Configured() {
		return errors.New("mapping: manual mode requires a table mapping or manual SQL")
	}
	return nil
}

// Profile is one monitored MSSQL database. The password is NOT stored here:
// it lives in secrets.dpapi under the profile ID (HasPassword is a hint).
type Profile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Server is an IP or host name, optionally host\instance.
	Server string `json:"server"`
	// Port is optional; 0 means default (DefaultSQLPort or instance lookup).
	Port        int      `json:"port,omitempty"`
	Database    string   `json:"database"`
	AuthMode    AuthMode `json:"authMode"`
	Login       string   `json:"login,omitempty"`
	HasPassword bool     `json:"hasPassword,omitempty"`
	Enabled     bool     `json:"enabled"`
	Mapping     Mapping  `json:"mapping"`
}

// DisplayName returns Name, or "Server/Database" when Name is empty.
func (p Profile) DisplayName() string {
	if strings.TrimSpace(p.Name) != "" {
		return p.Name
	}
	return p.Server + "/" + p.Database
}

// EffectivePort returns Port or DefaultSQLPort when Port is 0.
func (p Profile) EffectivePort() int {
	if p.Port == 0 {
		return DefaultSQLPort
	}
	return p.Port
}

// Validate checks required fields of a single profile.
func (p Profile) Validate() error {
	if strings.TrimSpace(p.ID) == "" {
		return errors.New("profile: id is required")
	}
	if strings.TrimSpace(p.Server) == "" {
		return fmt.Errorf("profile %q: server is required", p.ID)
	}
	if p.Port < 0 || p.Port > 65535 {
		return fmt.Errorf("profile %q: port %d out of range", p.ID, p.Port)
	}
	if strings.TrimSpace(p.Database) == "" {
		return fmt.Errorf("profile %q: database is required", p.ID)
	}
	if !p.AuthMode.Valid() {
		return fmt.Errorf("profile %q: unknown auth mode %q (want %q or %q)", p.ID, string(p.AuthMode), AuthSQL, AuthWindows)
	}
	if p.AuthMode == AuthSQL && strings.TrimSpace(p.Login) == "" {
		return fmt.Errorf("profile %q: login is required for SQL authentication", p.ID)
	}
	if err := p.Mapping.Validate(); err != nil {
		return fmt.Errorf("profile %q: %w", p.ID, err)
	}
	return nil
}

// ValidateProfiles validates every profile and checks that IDs are unique.
func ValidateProfiles(ps []Profile) error {
	seen := make(map[string]bool, len(ps))
	var errs []error
	for _, p := range ps {
		if err := p.Validate(); err != nil {
			errs = append(errs, err)
			continue
		}
		if seen[p.ID] {
			errs = append(errs, fmt.Errorf("profile %q: duplicate id", p.ID))
		}
		seen[p.ID] = true
	}
	return errors.Join(errs...)
}
