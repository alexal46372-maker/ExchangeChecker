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
	// MappingCounters is the fallback for configurations that store no
	// exchange times at all: the service watches the _SentNo/_ReceivedNo
	// message counters of the exchange-plan table and uses the moment a
	// counter changed as LastSend/LastRecv (PLAN.md §5.3, ApplyCounters).
	MappingCounters MappingMode = "counters"
)

// DefaultSQLPort is used when Profile.Port is 0.
const DefaultSQLPort = 1433

// Mapping is the discovered (or manually edited) location of exchange
// timestamps inside a 1C 8.1 database.
//
// Single-table mapping: Table holds point name/id and the send/recv columns.
// Linked mapping (TimesTable set): Table is the points directory, TimesTable
// holds the send/recv columns and TimesLinkColumn (binary(16) ref) points to
// Table.PointKeyColumn (usually _IDRRef). All phase-2 fields are omitempty,
// so phase-1 profiles.json documents load unchanged.
type Mapping struct {
	Mode            MappingMode `json:"mode,omitempty"`
	Schema          string      `json:"schema,omitempty"` // "" = dbo
	Table           string      `json:"table,omitempty"`
	PointIDColumn   string      `json:"pointIdColumn,omitempty"`
	PointNameColumn string      `json:"pointNameColumn,omitempty"`
	// PointKeyColumn is the key of Table referenced by TimesLinkColumn
	// (linked mode) or used as the counter-state key (counters mode).
	PointKeyColumn  string `json:"pointKeyColumn,omitempty"`
	TimesSchema     string `json:"timesSchema,omitempty"` // "" = Schema
	TimesTable      string `json:"timesTable,omitempty"`
	TimesLinkColumn string `json:"timesLinkColumn,omitempty"`
	// Composite 1C reference (8.1): TimesLinkColumn is _FldNNN_RRRef,
	// TimesLinkTypeColumn _FldNNN_TYPE (binary(1), 0x08 = reference),
	// TimesLinkRTRefColumn _FldNNN_RTRef (binary(4), big-endian table
	// number) and TimesLinkRTRef the expected table number (NNN of _NodeNNN).
	TimesLinkTypeColumn  string `json:"timesLinkTypeColumn,omitempty"`
	TimesLinkRTRefColumn string `json:"timesLinkRTRefColumn,omitempty"`
	TimesLinkRTRef       int    `json:"timesLinkRTRef,omitempty"`
	// KeepUnlinkedPoints lists points without a row in TimesTable (as
	// no_timestamps errors) instead of skipping them (LEFT vs INNER JOIN).
	KeepUnlinkedPoints bool   `json:"keepUnlinkedPoints,omitempty"`
	LastSendColumn     string `json:"lastSendColumn,omitempty"`
	LastRecvColumn     string `json:"lastRecvColumn,omitempty"`
	// YearOffset is subtracted from stored years: 2000 for 1C databases
	// created with the default offset, 0 otherwise (see YearOffset1C).
	YearOffset int `json:"yearOffset,omitempty"`
	// SentNoColumn / ReceivedNoColumn override the counter columns in
	// counters mode (default _SentNo / _ReceivedNo).
	SentNoColumn     string `json:"sentNoColumn,omitempty"`
	ReceivedNoColumn string `json:"receivedNoColumn,omitempty"`
	// PlanLabel is an optional user-editable label of the exchange plan
	// (e.g. «Основной», «Магазин»); empty by default, filled by the tray.
	PlanLabel string `json:"planLabel,omitempty"`
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
	case MappingNone, MappingAuto, MappingManual, MappingCounters:
	default:
		return fieldErr("mode", ValEnum, fmt.Sprintf("mapping: unknown mode %q", string(m.Mode)), string(m.Mode))
	}
	if strings.TrimSpace(m.Table) != "" {
		if strings.TrimSpace(m.PointNameColumn) == "" && strings.TrimSpace(m.PointIDColumn) == "" {
			return fieldErr("pointNameColumn", ValRequired, "mapping: point name/id column is required when table is set")
		}
		if m.Mode != MappingCounters && strings.TrimSpace(m.LastSendColumn) == "" && strings.TrimSpace(m.LastRecvColumn) == "" {
			return fieldErr("lastSendColumn", ValRequired, "mapping: at least one of last-send/last-recv columns is required")
		}
	}
	if err := m.validateLinked(); err != nil {
		return err
	}
	if err := m.validateCounters(); err != nil {
		return err
	}
	if err := m.validateComposite(); err != nil {
		return err
	}
	if m.Mode == MappingManual && !m.Configured() {
		return fieldErr("manualSql", ValRequired, "mapping: manual mode requires a table mapping or manual SQL")
	}
	if len(m.ManualSQL) > MaxManualSQLBytes {
		return fieldErr("manualSql", ValTooLong, fmt.Sprintf("mapping: manualSql longer than %d bytes", MaxManualSQLBytes), itoa(MaxManualSQLBytes))
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
	// Mappings are additional mappings of the same database, e.g. one per
	// exchange-plan table (_Node123, _Node124); points of all mappings are
	// listed together, tagged with their plan table (see AllMappings).
	Mappings []Mapping `json:"mappings,omitempty"`

	// Connection options (phase 2, all optional; see sqlprofile.go).
	// Encrypt "" (field omitted) means DefaultEncrypt = "disable" since
	// phase 2d: no TLS at all, not even for the login packet (PLAN §12
	// item 31; see EncryptMode.Effective and sqlcheck.Warnings).
	Encrypt         EncryptMode `json:"encrypt,omitempty"`
	TrustServerCert bool        `json:"trustServerCert,omitempty"`
	// TLSMinVersion is "", "1.0", "1.1" or "1.2" ("1.0" for SQL 2005/2008).
	TLSMinVersion string `json:"tlsMinVersion,omitempty"`
	// ServerPreset is "" or ServerPresetMSSQL2008 (target server SQL
	// Server 2008 / compatibility level 100, see ApplyServerPreset).
	ServerPreset      string `json:"serverPreset,omitempty"`
	ConnectTimeoutSec int    `json:"connectTimeoutSec,omitempty"`
	QueryTimeoutSec   int    `json:"queryTimeoutSec,omitempty"`

	// WarnMinutes / ErrorMinutes override the global thresholds of
	// settings.json for this profile (phase 3, CHK-MON-01); 0 = global
	// value (defaults 30 / 60 min). See EffectiveSettings.
	WarnMinutes  int `json:"warnMinutes,omitempty"`
	ErrorMinutes int `json:"errorMinutes,omitempty"`
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
		return fieldErr("id", ValRequired, "profile: id is required")
	}
	if fe := validateServer(p.Server); fe != nil {
		return fmt.Errorf("profile %q: %w", p.ID, fe)
	}
	if p.Port < 0 || p.Port > 65535 {
		return fmt.Errorf("profile %q: %w", p.ID, fieldErr("port", ValRange, fmt.Sprintf("port %d out of range", p.Port), "0", "65535", itoa(p.Port)))
	}
	if strings.TrimSpace(p.Database) == "" {
		return fmt.Errorf("profile %q: %w", p.ID, fieldErr("database", ValRequired, "database is required"))
	}
	if !p.AuthMode.Valid() {
		return fmt.Errorf("profile %q: %w", p.ID, fieldErr("authMode", ValEnum,
			fmt.Sprintf("unknown auth mode %q (want %q or %q)", string(p.AuthMode), AuthSQL, AuthWindows), string(p.AuthMode)))
	}
	if p.AuthMode == AuthSQL && strings.TrimSpace(p.Login) == "" {
		return fmt.Errorf("profile %q: %w", p.ID, fieldErr("login", ValRequired, "login is required for SQL authentication"))
	}
	if err := p.validateConn(); err != nil {
		return fmt.Errorf("profile %q: %w", p.ID, err)
	}
	if err := p.validateThresholds(); err != nil {
		return fmt.Errorf("profile %q: %w", p.ID, err)
	}
	if err := p.Mapping.Validate(); err != nil {
		return fmt.Errorf("profile %q: %w", p.ID, withFieldPrefix(err, "mapping."))
	}
	if err := p.validateMappings(); err != nil {
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
			errs = append(errs, fmt.Errorf("profile %q: %w", p.ID, fieldErr("id", ValDuplicate, "duplicate id")))
		}
		seen[p.ID] = true
	}
	return errors.Join(errs...)
}
