package sqlcheck

import (
	"crypto/tls"
	"errors"
	"strings"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Localizable messages (phase 6, G1): the keys, the RU/EN texts and the
// connection warnings live in package model (model.SQLMessage,
// model.SQLWarnings, sqlmessages.go) so the service can send them to the
// tray without the SQL driver. sqlcheck returns the stable keys
// (Error.Key, MessageKeyOf) and keeps these names as thin wrappers.
const (
	// MsgTLSEnableTLS10: the TLS handshake failed and the profile still
	// requires TLS >= 1.2 (Go 1.20 client default).
	MsgTLSEnableTLS10 = model.MsgSQLTLSEnableTLS10
	// MsgTLSLegacyFailed: the handshake failed although TLS 1.0 is allowed.
	MsgTLSLegacyFailed = model.MsgSQLTLSLegacyFailed
	// MsgTLSCertificate: the server certificate was rejected (x509).
	MsgTLSCertificate = model.MsgSQLTLSCertificate
	// MsgPresetMSSQL2008: hint shown with the "SQL Server 2008" preset.
	MsgPresetMSSQL2008 = model.MsgSQLPresetMSSQL2008
	// MsgWarnPlaintextLogin: warning (not an error) for profiles with SQL
	// authentication and encryption disabled (phase 2d), see Warnings.
	MsgWarnPlaintextLogin = model.MsgSQLWarnPlaintextLogin
)

// Message returns the text of key in lang ("en", anything else = "ru"),
// or "" for an unknown key (model.SQLMessage).
func Message(key, lang string) string { return model.SQLMessage(key, lang) }

// MessageKeys lists the known sql.* message keys (model.SQLMessageKeys).
func MessageKeys() []string { return model.SQLMessageKeys() }

// Warnings returns the message keys of the connection warnings of p
// (model.SQLWarnings): MsgWarnPlaintextLogin for SQL authentication
// without encryption; Windows authentication gets none.
func Warnings(p model.Profile) []string { return model.SQLWarnings(p) }

// MessageKeyOf returns the message key attached to err ("" if none).
func MessageKeyOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Key
	}
	return ""
}

// tlsKey classifies a TLS handshake failure. go-mssqldb v1.8.2 reports it
// as fmt.Errorf("TLS Handshake failed: %v", err) (the cause is flattened
// to text), so the check is textual, plus the typed crypto/tls errors for
// other paths.
func tlsKey(err error) string {
	var rh tls.RecordHeaderError
	msg := strings.ToLower(err.Error())
	isTLS := errors.As(err, &rh) || strings.Contains(msg, "tls handshake failed") ||
		strings.Contains(msg, "tls: ") || strings.Contains(msg, "cannot read handshake packet") ||
		strings.Contains(msg, "cannot send handshake packet")
	if !isTLS {
		return ""
	}
	if strings.Contains(msg, "x509:") || strings.Contains(msg, "certificate") {
		return MsgTLSCertificate
	}
	return MsgTLSEnableTLS10
}

// refineTLS adapts a TLS message key to the profile: when TLS 1.0 is
// already allowed, "enable TLS 1.0" is replaced by MsgTLSLegacyFailed.
func refineTLS(err error, p model.Profile) error {
	var e *Error
	if errors.As(err, &e) && e.Key == MsgTLSEnableTLS10 && p.EffectiveTLSMinVersion() == model.TLSMin10 {
		e.Key = MsgTLSLegacyFailed
	}
	return err
}
