package model

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// EncryptMode is the TDS encryption setting of a profile (PLAN.md §5.2).
// Legacy SQL Server 2005/2008 (1C 8.1) usually needs EncryptDisable, or
// EncryptDefault/EncryptOn together with TLSMinVersion "1.0".
type EncryptMode string

const (
	// EncryptDefault is the driver default: only the login packet is
	// encrypted, and only when the server supports TLS; the server
	// certificate is not validated.
	EncryptDefault EncryptMode = ""
	// EncryptOptional is an explicit alias of EncryptDefault.
	EncryptOptional EncryptMode = "optional"
	// EncryptDisable turns TLS off completely (no login encryption either).
	EncryptDisable EncryptMode = "disable"
	// EncryptOn encrypts the whole session; the certificate is validated
	// unless Profile.TrustServerCert is set.
	EncryptOn EncryptMode = "on"
	// EncryptStrict is TDS 8.0 (SQL Server 2022+): TLS before prelogin,
	// certificate always validated. Not usable with legacy servers.
	EncryptStrict EncryptMode = "strict"
)

// Valid reports whether e is a supported encryption mode.
func (e EncryptMode) Valid() bool {
	switch e {
	case EncryptDefault, EncryptOptional, EncryptDisable, EncryptOn, EncryptStrict:
		return true
	}
	return false
}

// Minimum TLS versions accepted in Profile.TLSMinVersion ("" = Go default,
// TLS 1.2 for clients in Go 1.20). "1.0" is for legacy SQL 2005/2008.
const (
	TLSMinDefault = ""
	TLSMin10      = "1.0"
	TLSMin11      = "1.1"
	TLSMin12      = "1.2"
)

// ServerPresetMSSQL2008 is the target-server preset of the customer: SQL
// Server 2008 (possibly R2; treated as compatibility level 100, no 2008
// R2-only features). Every query of the service is T-SQL of SQL 2008
// (CHK-SQL-05). Legacy servers without the TLS 1.2 update speak TLS 1.0
// only, while Go 1.20 clients default to TLS 1.2 minimum, so the preset
// sets TLSMinVersion "1.0" (tls.Config.MinVersion = tls.VersionTLS10).
const ServerPresetMSSQL2008 = "mssql2008"

// ApplyServerPreset fills the connection defaults of a preset. For
// ServerPresetMSSQL2008: TLSMinVersion "1.0" (unless set explicitly),
// Encrypt "strict" (TDS 8.0, SQL 2022+) falls back to the driver default
// (TLS for the login packet only, certificate not validated). Other
// fields are kept. preset "" clears ServerPreset only.
func (p *Profile) ApplyServerPreset(preset string) error {
	switch preset {
	case "":
	case ServerPresetMSSQL2008:
		if p.TLSMinVersion == TLSMinDefault {
			p.TLSMinVersion = TLSMin10
		}
		if p.Encrypt == EncryptStrict {
			p.Encrypt = EncryptDefault
		}
	default:
		return fmt.Errorf("unknown server preset %q", preset)
	}
	p.ServerPreset = preset
	return nil
}

// EffectiveTLSMinVersion is the minimum TLS version used for the
// connection: TLSMinVersion if set, "1.0" for the SQL 2008 preset, else ""
// (Go default: TLS 1.2 for clients in Go 1.20).
func (p Profile) EffectiveTLSMinVersion() string {
	if p.TLSMinVersion == TLSMinDefault && p.ServerPreset == ServerPresetMSSQL2008 {
		return TLSMin10
	}
	return p.TLSMinVersion
}

// Connection timeout defaults and limits (seconds).
const (
	DefaultConnectTimeoutSec = 15
	DefaultQueryTimeoutSec   = 30
	MaxTimeoutSec            = 600
)

// YearOffset1C is the 1C 8.x date offset: with it the database stores the
// year 4026 for the calendar year 2026.
const YearOffset1C = 2000

// ConnectTimeout returns the TCP dial / login timeout of the profile.
func (p Profile) ConnectTimeout() time.Duration {
	if p.ConnectTimeoutSec <= 0 {
		return DefaultConnectTimeoutSec * time.Second
	}
	return time.Duration(p.ConnectTimeoutSec) * time.Second
}

// QueryTimeout returns the per-query timeout of the profile.
func (p Profile) QueryTimeout() time.Duration {
	if p.QueryTimeoutSec <= 0 {
		return DefaultQueryTimeoutSec * time.Second
	}
	return time.Duration(p.QueryTimeoutSec) * time.Second
}

// validateConn checks the phase-2 connection options. Zero values are
// always valid, so profiles written by phase 1 keep loading.
func (p Profile) validateConn() error {
	if !p.Encrypt.Valid() {
		return fmt.Errorf("unknown encrypt mode %q (want \"\", %q, %q, %q or %q)",
			string(p.Encrypt), EncryptOptional, EncryptDisable, EncryptOn, EncryptStrict)
	}
	switch p.TLSMinVersion {
	case TLSMinDefault, TLSMin10, TLSMin11, TLSMin12:
	default:
		return fmt.Errorf("unsupported tlsMinVersion %q (want \"\", %q, %q or %q)",
			p.TLSMinVersion, TLSMin10, TLSMin11, TLSMin12)
	}
	switch p.ServerPreset {
	case "":
	case ServerPresetMSSQL2008:
		if p.Encrypt == EncryptStrict {
			return errors.New("encrypt \"strict\" (TDS 8.0) needs SQL Server 2022+, not the \"mssql2008\" preset")
		}
	default:
		return fmt.Errorf("unknown serverPreset %q (want \"\" or %q)", p.ServerPreset, ServerPresetMSSQL2008)
	}
	if p.ConnectTimeoutSec < 0 || p.ConnectTimeoutSec > MaxTimeoutSec {
		return fmt.Errorf("connectTimeoutSec must be in [0..%d], got %d", MaxTimeoutSec, p.ConnectTimeoutSec)
	}
	if p.QueryTimeoutSec < 0 || p.QueryTimeoutSec > MaxTimeoutSec {
		return fmt.Errorf("queryTimeoutSec must be in [0..%d], got %d", MaxTimeoutSec, p.QueryTimeoutSec)
	}
	return nil
}

// Linked reports whether the mapping uses the two-table mode: points from
// Table, timestamps from TimesTable joined on
// TimesTable.TimesLinkColumn = Table.PointKeyColumn (PLAN.md §5.3).
func (m Mapping) Linked() bool { return strings.TrimSpace(m.TimesTable) != "" }

// validateLinked checks the phase-2 mapping fields (linked mode, YearOffset).
func (m Mapping) validateLinked() error {
	if m.YearOffset != 0 && m.YearOffset != YearOffset1C {
		return fmt.Errorf("mapping: yearOffset must be 0 or %d, got %d", YearOffset1C, m.YearOffset)
	}
	if !m.Linked() {
		if strings.TrimSpace(m.TimesLinkColumn) != "" || strings.TrimSpace(m.TimesSchema) != "" {
			return errors.New("mapping: timesLinkColumn/timesSchema require timesTable")
		}
		return nil
	}
	if strings.TrimSpace(m.Table) == "" {
		return errors.New("mapping: timesTable requires the points table")
	}
	if strings.TrimSpace(m.PointKeyColumn) == "" {
		return errors.New("mapping: pointKeyColumn is required in linked mode")
	}
	if strings.TrimSpace(m.TimesLinkColumn) == "" {
		return errors.New("mapping: timesLinkColumn is required in linked mode")
	}
	return nil
}
