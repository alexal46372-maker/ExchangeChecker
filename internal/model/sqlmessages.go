package model

// SQL texts, warnings and manual SQL templates (phase 6, G1/G4,
// CHK-SQL-01/04): defined here, without the SQL driver, so the service
// can send them to the tray over IPC; sqlcheck delegates to them.

// LocalizedText is a RU/EN text pair (JSON for IPC).
type LocalizedText struct {
	RU string `json:"ru"`
	EN string `json:"en"`
}

// In returns the text in lang ("en", anything else = "ru").
func (t LocalizedText) In(lang string) string {
	if lang == LangEN {
		return t.EN
	}
	return t.RU
}

// sql.* message keys (sqlcheck.Error.Key, ProfileSnapshot.Warnings).
const (
	// MsgSQLTLSEnableTLS10: the TLS handshake failed and the profile still
	// requires TLS >= 1.2 (Go 1.20 client default).
	MsgSQLTLSEnableTLS10 = "sql.tls_enable_tls10"
	// MsgSQLTLSLegacyFailed: the handshake failed although TLS 1.0 is allowed.
	MsgSQLTLSLegacyFailed = "sql.tls_legacy_failed"
	// MsgSQLTLSCertificate: the server certificate was rejected (x509).
	MsgSQLTLSCertificate = "sql.tls_certificate"
	// MsgSQLPresetMSSQL2008: hint shown with the "SQL Server 2008" preset.
	MsgSQLPresetMSSQL2008 = "sql.preset_mssql2008_hint"
	// MsgSQLWarnPlaintextLogin: warning (not an error) for profiles with
	// SQL authentication and encryption disabled (phase 2d), SQLWarnings.
	MsgSQLWarnPlaintextLogin = "sql.warn_plaintext_login"
)

// validation.sql_* reasons of the typed mapping / manual SQL errors
// (FieldError.Key; errorArgs = [field, key, args...], so {0} is the
// field and the parameters start at {2}). Field is the JSON name of the
// mapping field ("manualSql", "table", "lastSendColumn", ...), relative
// to the mapping like Mapping.Validate.
const (
	ValSQLManualEmpty     = "validation.sql_manual_empty"
	ValSQLManualNotSelect = "validation.sql_manual_not_select"
	ValSQLManualMulti     = "validation.sql_manual_multi"
	ValSQLManualLiteral   = "validation.sql_manual_literal"
	// ValSQLManualForbidden: {2} = the forbidden keyword (upper case).
	ValSQLManualForbidden = "validation.sql_manual_forbidden"
	// ValSQLManualCompat2008: {2} = the SQL Server 2012+ constructs, ", ".
	ValSQLManualCompat2008 = "validation.sql_manual_compat2008"
	// ValSQLManualColumns: {2} = number of columns returned.
	ValSQLManualColumns = "validation.sql_manual_columns"
	// ValSQLManualValueType: {2} = column number (2 = last send, 3 = last
	// recv), {3} = the value type seen.
	ValSQLManualValueType = "validation.sql_manual_value_type"
	// ValSQLTableNotFound: {2} = [schema.]table.
	ValSQLTableNotFound = "validation.sql_table_not_found"
	// ValSQLTableAmbiguous: {2} = table name (several schemas, none dbo).
	ValSQLTableAmbiguous = "validation.sql_table_ambiguous"
	// ValSQLColumnNotFound: {2} = table, {3} = column.
	ValSQLColumnNotFound = "validation.sql_column_not_found"
	// ValSQLColumnType: {2} = table, {3} = column, {4} = its type, {5} =
	// the accepted types (SQL type names, language-neutral).
	ValSQLColumnType = "validation.sql_column_type"
	// ValSQLLinkTypeMismatch: {2} = link column, {3} = its type, {4} = key
	// column, {5} = its type ("binary(16)").
	ValSQLLinkTypeMismatch = "validation.sql_link_type_mismatch"
	// ValSQLNoTimestamp: neither lastSendColumn nor lastRecvColumn is set.
	ValSQLNoTimestamp = "validation.sql_no_timestamp"
	// ValSQLIdentifier: {2} = invalid_utf8 | nul | too_long, {3} = max length.
	ValSQLIdentifier = "validation.sql_identifier"
)

// Accepted column types ({5} of ValSQLColumnType).
const (
	SQLTypesPointColumn = "char/varchar/nchar/nvarchar (<= 4000), int/bigint/smallint/tinyint/numeric/decimal"
	SQLTypesNumber      = "int/bigint/smallint/tinyint/numeric/decimal"
	SQLTypesDateTime    = "date/datetime/datetime2/smalldatetime"
	SQLTypesBinary1     = "binary(1)"
	SQLTypesBinary4     = "binary(4)"
)

var sqlMessages = map[string]LocalizedText{
	MsgSQLTLSEnableTLS10: {
		"Не удалось установить TLS-соединение с SQL Server. Для SQL Server 2005/2008 без обновлений TLS 1.2 включите TLS 1.0 в профиле (пресет «SQL Server 2008» или «Минимальная версия TLS: 1.0»).",
		"The TLS handshake with SQL Server failed. For SQL Server 2005/2008 without the TLS 1.2 updates, enable TLS 1.0 in the profile (\"SQL Server 2008\" preset or \"Minimum TLS version: 1.0\").",
	},
	MsgSQLTLSLegacyFailed: {
		"TLS-соединение с SQL Server не установлено даже с TLS 1.0 (нет общего набора шифров или протокола). Установите на сервер обновление TLS 1.2 либо выберите «Шифрование: отключено» (данные и логин передаются без TLS, пароль SQL фактически открытым текстом).",
		"The TLS handshake with SQL Server failed even with TLS 1.0 (no common cipher suite or protocol). Install the TLS 1.2 update on the server or choose \"Encryption: disable\" (no TLS for data and login; the SQL password is effectively sent in clear text).",
	},
	MsgSQLTLSCertificate: {
		"Сертификат SQL Server не прошёл проверку. Включите «Доверять сертификату сервера» или выберите «Шифрование: только логин» (без проверки сертификата).",
		"The SQL Server certificate was rejected. Enable \"Trust server certificate\" or choose \"Encryption: login only\" (certificate not validated).",
	},
	MsgSQLPresetMSSQL2008: {
		"SQL Server 2008: все запросы совместимы с уровнем 100. По умолчанию подключение без шифрования. Если включить шифрование, а на сервере нет обновлений TLS 1.2, нужен TLS 1.0 (пресет разрешает его); Go предложит AES-CBC-SHA или 3DES, RC4 не поддерживается.",
		"SQL Server 2008: every query is compatible with level 100. By default the connection is not encrypted. If encryption is turned on and the server lacks the TLS 1.2 updates, TLS 1.0 is needed (the preset allows it); Go offers AES-CBC-SHA or 3DES, RC4 is not supported.",
	},
	MsgSQLWarnPlaintextLogin: {
		"Шифрование отключено: пароль SQL передаётся по сети практически открытым текстом (обратимая маскировка TDS). Рекомендуется Windows-аутентификация или отдельный логин только с ролью db_datareader.",
		"Encryption is disabled: the SQL password travels over the network almost in plain text (reversible TDS scrambling). Use Windows authentication or a dedicated login with the db_datareader role only.",
	},
	ValSQLManualEmpty:     {"Поле «{0}»: ручной SQL пуст.", "Field \"{0}\": the manual SQL is empty."},
	ValSQLManualNotSelect: {"Поле «{0}»: нужен один запрос SELECT (или WITH … SELECT).", "Field \"{0}\": a single SELECT (or WITH ... SELECT) statement is required."},
	ValSQLManualMulti:     {"Поле «{0}»: допускается только один запрос.", "Field \"{0}\": only one statement is allowed."},
	ValSQLManualLiteral:   {"Поле «{0}»: не закрыта строка, [имя] или комментарий.", "Field \"{0}\": unterminated string, [identifier] or comment."},
	ValSQLManualForbidden: {"Поле «{0}»: запрос должен только читать данные; недопустимое слово {2}.", "Field \"{0}\": the query must be read-only; forbidden keyword {2}."},
	ValSQLManualCompat2008: {"Поле «{0}»: конструкции новее SQL Server 2008 (уровень совместимости 100): {2}. Используйте TOP, ROW_NUMBER() OVER (PARTITION BY … ORDER BY …), CASE, CONVERT, + для строк.",
		"Field \"{0}\": T-SQL newer than SQL Server 2008 (compatibility level 100): {2}. Use TOP, ROW_NUMBER() OVER (PARTITION BY ... ORDER BY ...), CASE, CONVERT, + for strings."},
	ValSQLManualColumns:    {"Поле «{0}»: запрос должен вернуть 3 столбца (точка, последняя отправка, последний приём), вернул {2}.", "Field \"{0}\": the query must return 3 columns (point, last send, last recv), it returned {2}."},
	ValSQLManualValueType:  {"Поле «{0}»: столбец {2} должен быть датой/временем (получено {3}).", "Field \"{0}\": column {2} must be a date/time (got {3})."},
	ValSQLTableNotFound:    {"Поле «{0}»: таблица {2} не найдена.", "Field \"{0}\": table {2} not found."},
	ValSQLTableAmbiguous:   {"Поле «{0}»: таблица {2} есть в нескольких схемах, укажите схему.", "Field \"{0}\": table {2} exists in several schemas, set the schema."},
	ValSQLColumnNotFound:   {"Поле «{0}»: в таблице {2} нет столбца {3}.", "Field \"{0}\": table {2} has no column {3}."},
	ValSQLColumnType:       {"Поле «{0}»: столбец {2}.{3} имеет тип {4}, нужен {5}.", "Field \"{0}\": column {2}.{3} is {4}, want {5}."},
	ValSQLLinkTypeMismatch: {"Поле «{0}»: типы столбцов связи различаются: {2} — {3}, {4} — {5}.", "Field \"{0}\": the link columns have different types: {2} is {3}, {4} is {5}."},
	ValSQLNoTimestamp:      {"Поле «{0}»: укажите столбец времени отправки или приёма.", "Field \"{0}\": set the last send or last recv column."},
	ValSQLIdentifier:       {"Поле «{0}»: недопустимое имя ({2}; не длиннее {3} символов, без NUL).", "Field \"{0}\": invalid name ({2}; at most {3} characters, no NUL)."},
}

// SQLMessageKeys lists the sql.* UI text keys.
func SQLMessageKeys() []string {
	return []string{MsgSQLTLSEnableTLS10, MsgSQLTLSLegacyFailed, MsgSQLTLSCertificate, MsgSQLPresetMSSQL2008, MsgSQLWarnPlaintextLogin}
}

// SQLValidationKeys lists the validation.sql_* reason keys.
func SQLValidationKeys() []string {
	return []string{ValSQLManualEmpty, ValSQLManualNotSelect, ValSQLManualMulti, ValSQLManualLiteral, ValSQLManualForbidden,
		ValSQLManualCompat2008, ValSQLManualColumns, ValSQLManualValueType, ValSQLTableNotFound, ValSQLTableAmbiguous,
		ValSQLColumnNotFound, ValSQLColumnType, ValSQLLinkTypeMismatch, ValSQLNoTimestamp, ValSQLIdentifier}
}

// SQLMessages returns a copy of the SQL texts: sql.* and validation.sql_*
// keys -> RU/EN (for the IPC catalog sent to the tray).
func SQLMessages() map[string]LocalizedText {
	out := make(map[string]LocalizedText, len(sqlMessages))
	for k, v := range sqlMessages {
		out[k] = v
	}
	return out
}

// SQLMessage returns the text of a sql.* / validation.sql_* key in lang
// ("en", anything else = "ru"), or "" for an unknown key.
func SQLMessage(key, lang string) string {
	m, ok := sqlMessages[key]
	if !ok {
		return ""
	}
	return m.In(lang)
}

// SQLWarnings returns the message keys of the connection warnings of p
// for the setup GUI and the tray: MsgSQLWarnPlaintextLogin when the
// profile uses SQL authentication without encryption (encrypt "disable",
// also when unset). Windows authentication gets no warning. Warnings do
// not block saving or polling; texts: SQLMessage(key, lang).
func SQLWarnings(p Profile) []string {
	var w []string
	if p.PlaintextLogin() {
		w = append(w, MsgSQLWarnPlaintextLogin)
	}
	return w
}
