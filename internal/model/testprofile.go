package model

import (
	"strings"
)

// Connection test of a profile over IPC (phase 4, CHK-IPC-03): the tray
// sends test_profile, the service runs the sqlcheck connection test with a
// hard timeout and answers MsgTestResult.

// TestCategory values (TestProfileResult.Category). The failure categories
// are the values of sqlcheck.ConnCategory plus TestTimeout for the hard
// timeout of the service.
const (
	TestOK       = "ok"
	TestNetwork  = "network"  // host / port / instance unreachable, DNS
	TestLogin    = "login"    // login failed, password expired, ...
	TestDatabase = "database" // the database cannot be opened
	TestTLS      = "tls"      // TLS handshake / certificate
	TestTimeout  = "timeout"  // no answer within the time budget
	TestConfig   = "config"   // the profile cannot be tested as given
	TestOther    = "other"
)

// TestProfileResult is the reply of test_profile.
type TestProfileResult struct {
	ProfileID string `json:"profileId,omitempty"`
	OK        bool   `json:"ok"`
	// Category is TestOK or one of the failure categories.
	Category string `json:"category"`
	// ErrorKey is a localizable key: sql.* (TLS hints from sqlcheck) or
	// monitor.test_* (MonitorMessage).
	ErrorKey string `json:"errorKey,omitempty"`
	// Error is the technical message (never contains the password).
	Error string `json:"error,omitempty"`
	// Server information (when the connection succeeded and the server
	// returned it): SERVERPROPERTY ProductVersion / ProductLevel /
	// Edition and the compatibility level of the database (0 = unknown).
	ServerVersion string `json:"serverVersion,omitempty"`
	ProductLevel  string `json:"productLevel,omitempty"`
	Edition       string `json:"edition,omitempty"`
	CompatLevel   int    `json:"compatLevel,omitempty"`
	// Encrypt / TLSMin are the effective connection modes used.
	Encrypt    string `json:"encrypt"`
	TLSMin     string `json:"tlsMin,omitempty"`
	DurationMs int64  `json:"durationMs"`
}

// TestCategoryKey is the monitor.test_* message key of a category.
func TestCategoryKey(cat string) string {
	switch cat {
	case TestOK:
		return MsgTestOK
	case TestNetwork:
		return MsgTestNetwork
	case TestLogin:
		return MsgTestLogin
	case TestDatabase:
		return MsgTestDatabase
	case TestTLS:
		return MsgTestTLS
	case TestTimeout:
		return MsgTestTimeout
	case TestConfig:
		return MsgTestConfig
	}
	return MsgTestOther
}

// EncryptStrength orders the effective encryption of a profile: disable 0
// < optional (login only) 1 < on + TrustServerCert 2 < on 3 < strict 4.
func EncryptStrength(p Profile) int {
	switch p.EffectiveEncrypt() {
	case EncryptOptional:
		return 1
	case EncryptOn:
		if p.TrustServerCert {
			return 2
		}
		return 3
	case EncryptStrict:
		return 4
	}
	return 0
}

// tlsRank orders the effective minimum TLS version ("" = Go default 1.2).
func tlsRank(v string) int {
	switch v {
	case TLSMin10:
		return 10
	case TLSMin11:
		return 11
	}
	return 12
}

// EncryptNotWeaker reports whether req encrypts at least as strongly as
// saved: a higher EncryptStrength, or the same one with (when TLS is used)
// a minimum TLS version not lower than saved.
func EncryptNotWeaker(req, saved Profile) bool {
	a, b := EncryptStrength(req), EncryptStrength(saved)
	if a != b {
		return a > b
	}
	if a == 0 {
		return true
	}
	return tlsRank(req.EffectiveTLSMinVersion()) >= tlsRank(saved.EffectiveTLSMinVersion())
}

// splitHostInstance splits "host\instance" (case-insensitive compare).
func splitHostInstance(s string) (string, string) {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\\'); i >= 0 {
		return strings.ToLower(strings.TrimSpace(s[:i])), strings.ToLower(strings.TrimSpace(s[i+1:]))
	}
	return strings.ToLower(s), ""
}

// SameServerIdentity reports whether req targets the same server with the
// same credentials identity as saved: host, instance, port, auth mode and
// login are equal (host / instance case-insensitive, login exact).
func SameServerIdentity(req, saved Profile) bool {
	h1, i1 := splitHostInstance(req.Server)
	h2, i2 := splitHostInstance(saved.Server)
	return h1 == h2 && i1 == i2 && req.Port == saved.Port && req.AuthMode == saved.AuthMode &&
		strings.TrimSpace(req.Login) == strings.TrimSpace(saved.Login)
}

// CanReuseSavedPassword is the rule of test_profile with an empty password
// (CHK-IPC-03): the saved password may be sent only to the same server
// identity (SameServerIdentity) and never over a weaker encryption.
func CanReuseSavedPassword(req, saved Profile) bool {
	return SameServerIdentity(req, saved) && EncryptNotWeaker(req, saved)
}

// SameConnection reports whether every connection parameter of req equals
// saved (Windows authentication of an "unsaved" profile is tested only
// when it is exactly the saved profile, CHK-IPC-03).
func SameConnection(req, saved Profile) bool {
	return SameServerIdentity(req, saved) &&
		strings.TrimSpace(req.Database) == strings.TrimSpace(saved.Database) &&
		req.EffectiveEncrypt() == saved.EffectiveEncrypt() && req.TrustServerCert == saved.TrustServerCert &&
		req.EffectiveTLSMinVersion() == saved.EffectiveTLSMinVersion() &&
		req.ConnectTimeout() == saved.ConnectTimeout() && req.QueryTimeout() == saved.QueryTimeout()
}
