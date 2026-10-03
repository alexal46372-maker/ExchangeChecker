package model

import (
	"fmt"
	"strings"
	"time"
)

// EncryptMode is the TDS encryption setting of a profile (PLAN.md §5.2).
// Customer decision (phase 2d): the target is SQL Server 2008 R2 RTM
// without service packs and the primary mode is no encryption, so an
// unset mode means EncryptDisable (EffectiveEncrypt). TLS stays available
// as a profile option: EncryptOptional / EncryptOn, with TLSMinVersion
// "1.0" for servers without the TLS 1.2 update.
type EncryptMode string

const (
	// EncryptDefault is the unset mode (field omitted in profiles.json).
	// Since phase 2d it means EncryptDisable (DefaultEncrypt); profiles
	// written earlier without the field therefore connect without TLS.
	EncryptDefault EncryptMode = ""
	// EncryptOptional encrypts only the login packet, and only when the
	// server supports TLS; the server certificate is not validated (the
	// go-mssqldb default, which EncryptDefault meant before phase 2d).
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

// DefaultEncrypt is the encryption mode of new profiles, of the
// "mssql2008" preset and of profiles without the field (phase 2d).
const DefaultEncrypt = EncryptDisable

// Effective resolves the unset mode to DefaultEncrypt; explicit modes are
// returned unchanged.
func (e EncryptMode) Effective() EncryptMode {
	if e == EncryptDefault {
		return DefaultEncrypt
	}
	return e
}

// EffectiveEncrypt is the encryption mode used for the connection.
func (p Profile) EffectiveEncrypt() EncryptMode { return p.Encrypt.Effective() }

// ApplySQLDefaults fills the connection defaults of a new profile (setup
// GUI "new profile"): Encrypt = DefaultEncrypt when unset. Explicit values
// are kept.
func (p *Profile) ApplySQLDefaults() {
	if p.Encrypt == EncryptDefault {
		p.Encrypt = DefaultEncrypt
	}
}

// PlaintextLogin reports whether the SQL password travels without TLS:
// SQL authentication with the effective mode EncryptDisable. The TDS
// login packet then only XOR-scrambles the password (reversible), see
// PLAN.md §12. Windows authentication (SSPI/NTLM/Kerberos) never sends
// the password itself, so it is not affected.
func (p Profile) PlaintextLogin() bool {
	return p.AuthMode == AuthSQL && p.EffectiveEncrypt() == EncryptDisable
}

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
// sets TLSMinVersion "1.0" (tls.Config.MinVersion = tls.VersionTLS10) for
// profiles that turn encryption on. Customer decision (phase 2d): the
// server is 2008 R2 RTM without SP and the preset connects without
// encryption by default (Encrypt "disable").
const ServerPresetMSSQL2008 = "mssql2008"

// ApplyServerPreset fills the connection defaults of a preset. For
// ServerPresetMSSQL2008: Encrypt "disable" when unset (phase 2d), Encrypt
// "strict" (TDS 8.0, SQL 2022+, forbidden with the preset) is replaced by
// "disable", explicit "optional"/"on"/"disable" are kept; TLSMinVersion
// "1.0" unless set explicitly (used only if encryption is turned on).
// Other fields are kept. preset "" clears ServerPreset only.
func (p *Profile) ApplyServerPreset(preset string) error {
	switch preset {
	case "":
	case ServerPresetMSSQL2008:
		if p.TLSMinVersion == TLSMinDefault {
			p.TLSMinVersion = TLSMin10
		}
		if p.Encrypt == EncryptDefault || p.Encrypt == EncryptStrict {
			p.Encrypt = DefaultEncrypt
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
		return fieldErr("encrypt", ValEnum, fmt.Sprintf("unknown encrypt mode %q (want \"\", %q, %q, %q or %q)",
			string(p.Encrypt), EncryptOptional, EncryptDisable, EncryptOn, EncryptStrict), string(p.Encrypt))
	}
	switch p.TLSMinVersion {
	case TLSMinDefault, TLSMin10, TLSMin11, TLSMin12:
	default:
		return fieldErr("tlsMinVersion", ValEnum, fmt.Sprintf("unsupported tlsMinVersion %q (want \"\", %q, %q or %q)",
			p.TLSMinVersion, TLSMin10, TLSMin11, TLSMin12), p.TLSMinVersion)
	}
	switch p.ServerPreset {
	case "":
	case ServerPresetMSSQL2008:
		if p.Encrypt == EncryptStrict {
			return fieldErr("encrypt", ValPresetConflict, "encrypt \"strict\" (TDS 8.0) needs SQL Server 2022+, not the \"mssql2008\" preset", string(p.Encrypt), p.ServerPreset)
		}
	default:
		return fieldErr("serverPreset", ValEnum, fmt.Sprintf("unknown serverPreset %q (want \"\" or %q)", p.ServerPreset, ServerPresetMSSQL2008), p.ServerPreset)
	}
	if p.ConnectTimeoutSec < 0 || p.ConnectTimeoutSec > MaxTimeoutSec {
		return fieldErr("connectTimeoutSec", ValRange, fmt.Sprintf("connectTimeoutSec must be in [0..%d], got %d", MaxTimeoutSec, p.ConnectTimeoutSec),
			"0", itoa(MaxTimeoutSec), itoa(p.ConnectTimeoutSec))
	}
	if p.QueryTimeoutSec < 0 || p.QueryTimeoutSec > MaxTimeoutSec {
		return fieldErr("queryTimeoutSec", ValRange, fmt.Sprintf("queryTimeoutSec must be in [0..%d], got %d", MaxTimeoutSec, p.QueryTimeoutSec),
			"0", itoa(MaxTimeoutSec), itoa(p.QueryTimeoutSec))
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
		return fieldErr("yearOffset", ValEnum, fmt.Sprintf("mapping: yearOffset must be 0 or %d, got %d", YearOffset1C, m.YearOffset), itoa(m.YearOffset))
	}
	if !m.Linked() {
		if strings.TrimSpace(m.TimesLinkColumn) != "" || strings.TrimSpace(m.TimesSchema) != "" {
			return fieldErr("timesTable", ValMapping, "mapping: timesLinkColumn/timesSchema require timesTable", "timesTable")
		}
		return nil
	}
	if strings.TrimSpace(m.Table) == "" {
		return fieldErr("table", ValMapping, "mapping: timesTable requires the points table", "table")
	}
	if strings.TrimSpace(m.PointKeyColumn) == "" {
		return fieldErr("pointKeyColumn", ValRequired, "mapping: pointKeyColumn is required in linked mode")
	}
	if strings.TrimSpace(m.TimesLinkColumn) == "" {
		return fieldErr("timesLinkColumn", ValRequired, "mapping: timesLinkColumn is required in linked mode")
	}
	return nil
}
