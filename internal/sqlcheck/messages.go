package sqlcheck

import (
	"crypto/tls"
	"errors"
	"strings"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Localizable messages. Phase 1 has no message catalog; its i18n convention
// is the UI language "ru"/"en" (model.LangRU / model.LangEN, resolved by
// config.EffectiveLanguage, fallback "ru"). sqlcheck therefore returns
// stable message keys (Error.Key, MessageKeyOf) and a small RU/EN table
// (Message) that the tray / setup UI can show or move to a shared catalog.
const (
	// MsgTLSEnableTLS10: the TLS handshake failed and the profile still
	// requires TLS >= 1.2 (Go 1.20 client default).
	MsgTLSEnableTLS10 = "sql.tls_enable_tls10"
	// MsgTLSLegacyFailed: the handshake failed although TLS 1.0 is allowed.
	MsgTLSLegacyFailed = "sql.tls_legacy_failed"
	// MsgTLSCertificate: the server certificate was rejected (x509).
	MsgTLSCertificate = "sql.tls_certificate"
	// MsgPresetMSSQL2008: hint shown with the "SQL Server 2008" preset.
	MsgPresetMSSQL2008 = "sql.preset_mssql2008_hint"
)

var messages = map[string][2]string{ // key -> {ru, en}
	MsgTLSEnableTLS10: {
		"Не удалось установить TLS-соединение с SQL Server. Для SQL Server 2005/2008 без обновлений TLS 1.2 включите TLS 1.0 в профиле (пресет «SQL Server 2008» или «Минимальная версия TLS: 1.0»).",
		"The TLS handshake with SQL Server failed. For SQL Server 2005/2008 without the TLS 1.2 updates, enable TLS 1.0 in the profile (\"SQL Server 2008\" preset or \"Minimum TLS version: 1.0\").",
	},
	MsgTLSLegacyFailed: {
		"TLS-соединение с SQL Server не установлено даже с TLS 1.0 (нет общего набора шифров или протокола). Установите на сервер обновление TLS 1.2 либо выберите «Шифрование: отключено» (данные и логин передаются без TLS, пароль SQL фактически открытым текстом).",
		"The TLS handshake with SQL Server failed even with TLS 1.0 (no common cipher suite or protocol). Install the TLS 1.2 update on the server or choose \"Encryption: disable\" (no TLS for data and login; the SQL password is effectively sent in clear text).",
	},
	MsgTLSCertificate: {
		"Сертификат SQL Server не прошёл проверку. Включите «Доверять сертификату сервера» или используйте шифрование по умолчанию (только логин, без проверки сертификата).",
		"The SQL Server certificate was rejected. Enable \"Trust server certificate\" or use the default encryption (login only, certificate not validated).",
	},
	MsgPresetMSSQL2008: {
		"SQL Server 2008: все запросы совместимы с уровнем 100. Если на сервере не установлены обновления TLS 1.2, нужен TLS 1.0 (пресет включает его); Go предложит AES-CBC-SHA или 3DES, RC4 не поддерживается.",
		"SQL Server 2008: every query is compatible with level 100. Servers without the TLS 1.2 updates need TLS 1.0 (the preset enables it); Go offers AES-CBC-SHA or 3DES, RC4 is not supported.",
	},
}

// Message returns the text of key in lang ("en", anything else = "ru"),
// or "" for an unknown key.
func Message(key, lang string) string {
	m, ok := messages[key]
	if !ok {
		return ""
	}
	if lang == model.LangEN {
		return m[1]
	}
	return m[0]
}

// MessageKeys lists the known message keys (tests, UI catalogs).
func MessageKeys() []string {
	return []string{MsgTLSEnableTLS10, MsgTLSLegacyFailed, MsgTLSCertificate, MsgPresetMSSQL2008}
}

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
