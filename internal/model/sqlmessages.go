package model

// SQL texts, warnings and manual SQL templates (phase 6, G1/G4,
// CHK-SQL-01/04): defined here, without the SQL driver, so the service
// can send them to the tray over IPC; sqlcheck delegates to them.

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
	ValSQLIdentifier = "validation.sql_identifier" // ValSQLComment: the manual SQL contains a comment (-- or /* */);
	// comments are not allowed in manual SQL (SQL Server nests /* */, a
	// validator that does not would see a different query, phase 6 review).
	ValSQLComment = "validation.sql_comment"
	// ValSQLManualHint: {2} = the table / join hint (only NOLOCK,
	// READUNCOMMITTED and NOWAIT are allowed).
	ValSQLManualHint = "validation.sql_manual_hint"
	// ValSQLManualOption: OPTION (...) query hints are not allowed.
	ValSQLManualOption = "validation.sql_manual_option"
)

// Accepted column types ({5} of ValSQLColumnType).
const (
	SQLTypesPointColumn = "char/varchar/nchar/nvarchar (<= 4000), int/bigint/smallint/tinyint/numeric/decimal"
	SQLTypesNumber      = "int/bigint/smallint/tinyint/numeric/decimal"
	SQLTypesDateTime    = "date/datetime/datetime2/smalldatetime"
	SQLTypesBinary1     = "binary(1)"
	SQLTypesBinary4     = "binary(4)"
)

var sqlMessages = map[string]string{
	// The texts name the real controls of the tray settings window
	// (phase 8, CHK-TRAY-14): «Шифрование», «Тип сервера», the «Разрешить
	// TLS 1.0 (SQL Server 2008)» and «Доверять сертификату сервера без
	// проверки» check boxes (internal/i18n set.*).
	MsgSQLTLSEnableTLS10: "Не удалось установить TLS-соединение с SQL Server. Для SQL Server 2005/2008 без обновлений TLS 1.2 включите флажок «Разрешить TLS 1.0 (SQL Server 2008)» (его включает и «Тип сервера: SQL Server 2008 / 2008 R2»).",

	MsgSQLTLSLegacyFailed: "TLS-соединение с SQL Server не установлено даже с TLS 1.0 (нет общего набора шифров или протокола). Установите на сервер обновление TLS 1.2 либо выберите «Шифрование: Без шифрования» (данные и логин передаются без TLS, пароль SQL фактически открытым текстом).",

	MsgSQLTLSCertificate: "Сертификат SQL Server не прошёл проверку. При «Шифрование: Весь сеанс (TLS)» включите флажок «Доверять сертификату сервера без проверки» или выберите «Шифрование: Только вход, если сервер умеет TLS» (сертификат не проверяется).",

	MsgSQLPresetMSSQL2008: "SQL Server 2008: все запросы совместимы с уровнем 100. По умолчанию подключение без шифрования. Если включить шифрование, а на сервере нет обновлений TLS 1.2, нужен TLS 1.0 (пресет разрешает его); Go предложит AES-CBC-SHA или 3DES, RC4 не поддерживается.",

	MsgSQLWarnPlaintextLogin: "Шифрование отключено: пароль SQL передаётся по сети практически открытым текстом (обратимая маскировка TDS). Рекомендуется Windows-аутентификация или отдельный логин только с ролью db_datareader.",

	ValSQLManualEmpty:      "Поле «{0}»: ручной SQL пуст.",
	ValSQLManualNotSelect:  "Поле «{0}»: нужен один запрос SELECT (или WITH … SELECT).",
	ValSQLManualMulti:      "Поле «{0}»: допускается только один запрос.",
	ValSQLManualLiteral:    "Поле «{0}»: не закрыта строка, [имя] или \"имя\".",
	ValSQLManualForbidden:  "Поле «{0}»: запрос должен только читать данные; недопустимое слово {2}.",
	ValSQLManualCompat2008: "Поле «{0}»: конструкции новее SQL Server 2008 (уровень совместимости 100): {2}. Используйте TOP, ROW_NUMBER() OVER (PARTITION BY … ORDER BY …), CASE, CONVERT, + для строк.",

	ValSQLManualColumns:    "Поле «{0}»: запрос должен вернуть 3 столбца (точка, последняя отправка, последний приём), вернул {2}.",
	ValSQLManualValueType:  "Поле «{0}»: столбец {2} должен быть датой/временем (получено {3}).",
	ValSQLTableNotFound:    "Поле «{0}»: таблица {2} не найдена.",
	ValSQLTableAmbiguous:   "Поле «{0}»: таблица {2} есть в нескольких схемах, укажите схему.",
	ValSQLColumnNotFound:   "Поле «{0}»: в таблице {2} нет столбца {3}.",
	ValSQLColumnType:       "Поле «{0}»: столбец {2}.{3} имеет тип {4}, нужен {5}.",
	ValSQLLinkTypeMismatch: "Поле «{0}»: типы столбцов связи различаются: {2} — {3}, {4} — {5}.",
	ValSQLNoTimestamp:      "Поле «{0}»: укажите столбец времени отправки или приёма.",
	ValSQLIdentifier:       "Поле «{0}»: недопустимое имя ({2}; не длиннее {3} символов, без NUL).",
	ValSQLComment:          "Поле «{0}»: комментарии (-- и /* */) в ручном SQL не допускаются, удалите их.",

	ValSQLManualHint: "Поле «{0}»: недопустимая подсказка {2}; разрешены только WITH (NOLOCK), READUNCOMMITTED, NOWAIT (подсказки блокировок мешают работе 1С).",

	ValSQLManualOption: "Поле «{0}»: OPTION (…) в ручном SQL не допускается.",
}

// SQLMessageKeys lists the sql.* UI text keys.
func SQLMessageKeys() []string {
	return []string{MsgSQLTLSEnableTLS10, MsgSQLTLSLegacyFailed, MsgSQLTLSCertificate, MsgSQLPresetMSSQL2008, MsgSQLWarnPlaintextLogin}
}

// SQLValidationKeys lists the validation.sql_* reason keys.
func SQLValidationKeys() []string {
	return []string{ValSQLManualEmpty, ValSQLManualNotSelect, ValSQLManualMulti, ValSQLManualLiteral, ValSQLManualForbidden,
		ValSQLManualCompat2008, ValSQLManualColumns, ValSQLManualValueType, ValSQLTableNotFound, ValSQLTableAmbiguous,
		ValSQLColumnNotFound, ValSQLColumnType, ValSQLLinkTypeMismatch, ValSQLNoTimestamp, ValSQLIdentifier,
		ValSQLComment, ValSQLManualHint, ValSQLManualOption}
}

// SQLMessages returns a copy of the SQL texts: sql.* and validation.sql_*
// keys -> text (for the IPC catalog sent to the tray).
func SQLMessages() map[string]string {
	out := make(map[string]string, len(sqlMessages))
	for k, v := range sqlMessages {
		out[k] = v
	}
	return out
}

// SQLMessage returns the (Russian) text of a sql.* / validation.sql_*
// key, or "" for an unknown key.
func SQLMessage(key string) string {
	return sqlMessages[key]
}

// SQLWarnings returns the message keys of the connection warnings of p
// for the setup GUI and the tray: MsgSQLWarnPlaintextLogin when the
// profile uses SQL authentication without encryption (encrypt "disable",
// also when unset). Windows authentication gets no warning. Warnings do
// not block saving or polling; texts: SQLMessage(key).
func SQLWarnings(p Profile) []string {
	var w []string
	if p.PlaintextLogin() {
		w = append(w, MsgSQLWarnPlaintextLogin)
	}
	return w
}
