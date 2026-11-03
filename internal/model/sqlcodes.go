package model

// Phase 9 (EC Tray request, CHK-SQL-09): one stable error code for the
// wizard on top of the existing ErrorKind / ConnCategory (which stay
// unchanged). ErrorKey names the "what to do" text (SQLDiagnoseCode,
// SQLMessage = the diagnosis title, so monitor.KnownErrorKey knows it).

// SQL error codes.
const (
	SQLCodeLoginFailed         = "login_failed"         // 18456, 18452, 18486-18488
	SQLCodeDatabaseUnavailable = "database_unavailable" // 4060, 4063, 4064
	SQLCodeTLS                 = "tls"
	SQLCodeTimeout             = "timeout"
	SQLCodeNetwork             = "network"
	SQLCodeBrowserUnreachable  = "browser_unreachable" // SQL Browser (UDP 1434) did not answer
	SQLCodeInstanceNotFound    = "instance_not_found"  // the Browser does not know the instance
	SQLCodePermissionDenied    = "permission_denied"   // 229, 297, 300, 916
	SQLCodeNot1CDatabase       = "not_1c_database"
	SQLCodeConfig              = "config"
	SQLCodeOther               = "other"
)

// SQLErrorInfo is the classified error of a wizard / test request.
type SQLErrorInfo struct {
	Code string `json:"code"`
	// Category is the ConnCategory of the error (unchanged phase 4 value).
	Category string `json:"category,omitempty"`
	// ErrorKey: the diagnosis key (sql.diag_*); TLSKey: sql.tls_* if any.
	ErrorKey  string `json:"errorKey"`
	TLSKey    string `json:"tlsKey,omitempty"`
	SQLNumber int    `json:"sqlNumber,omitempty"`
}

// SQLErrorCodes lists every code.
func SQLErrorCodes() []string {
	return []string{SQLCodeLoginFailed, SQLCodeDatabaseUnavailable, SQLCodeTLS, SQLCodeTimeout, SQLCodeNetwork,
		SQLCodeBrowserUnreachable, SQLCodeInstanceNotFound, SQLCodePermissionDenied, SQLCodeNot1CDatabase,
		SQLCodeConfig, SQLCodeOther}
}

var sqlCodeDiag = map[string]string{
	SQLCodeLoginFailed: "sql.diag_login", SQLCodeDatabaseUnavailable: "sql.diag_database", SQLCodeTLS: "sql.diag_tls",
	SQLCodeTimeout: "sql.diag_timeout", SQLCodeNetwork: "sql.diag_network", SQLCodeBrowserUnreachable: "sql.diag_browser",
	SQLCodeInstanceNotFound: "sql.diag_instance", SQLCodePermissionDenied: "sql.diag_permission",
	SQLCodeNot1CDatabase: "sql.diag_not_1c", SQLCodeConfig: "sql.diag_config", SQLCodeOther: "sql.diag_other",
}

// SQLDiagKeyForCode returns the diagnosis key of a code (tlsKey refines
// tls); unknown → sql.diag_other.
func SQLDiagKeyForCode(code, tlsKey string) string {
	if code == SQLCodeTLS {
		switch tlsKey {
		case MsgSQLTLSEnableTLS10, MsgSQLTLSLegacyFailed, MsgSQLTLSCertificate:
			return "sql.diag_" + tlsKey[len("sql."):]
		}
	}
	if k, ok := sqlCodeDiag[code]; ok {
		return k
	}
	return "sql.diag_other"
}

// SQLDiagnoseCode returns the advice for an error code.
func SQLDiagnoseCode(code, tlsKey string) SQLDiagnosis {
	key := SQLDiagKeyForCode(code, tlsKey)
	for _, d := range sqlDiagnoses {
		if d.Key == key {
			return copyDiagnosis(d)
		}
	}
	return copyDiagnosis(sqlDiagnoses[len(sqlDiagnoses)-1])
}

func init() {
	sqlDiagnoses = append(sqlDiagnoses[:len(sqlDiagnoses)-1:len(sqlDiagnoses)-1], append(sqlCodeDiagnoses, sqlDiagnoses[len(sqlDiagnoses)-1])...)
	for _, d := range sqlDiagnoses {
		sqlMessages[d.Key] = d.Title
	}
}

// SQLDiagnosisKeys lists the sql.diag_* keys (all in SQLMessage).
func SQLDiagnosisKeys() []string {
	out := make([]string, len(sqlDiagnoses))
	for i, d := range sqlDiagnoses {
		out[i] = d.Key
	}
	return out
}

var sqlCodeDiagnoses = []SQLDiagnosis{
	{Key: "sql.diag_browser", Category: SQLConnNetwork,
		Title: "Служба SQL Server Browser не ответила",
		Steps: []string{
			"Запустите на сервере службу «SQL Server Browser» (тип запуска — автоматически).",
			"Откройте в брандмауэре сервера UDP-порт 1434; широковещательный поиск не проходит через маршрутизаторы — укажите имя сервера.",
			"Либо укажите TCP-порт экземпляра явно — тогда Browser не нужен.",
		}},
	{Key: "sql.diag_instance", Category: SQLConnNetwork,
		Title: "Экземпляр SQL Server не найден",
		Steps: []string{
			"Проверьте имя экземпляра после «\\» (кнопка «Найти серверы» показывает существующие).",
			"Экземпляр по умолчанию указывается без «\\имя»; скрытый экземпляр — только с явным TCP-портом.",
		}},
	{Key: "sql.diag_permission", Category: SQLConnOther,
		Title: "Недостаточно прав",
		Steps: []string{
			"Логину нужна роль db_datareader в базе: выполните скрипт «Выдать права».",
			"Если права выдавались на отдельные таблицы, добавьте нужные таблицы в скрипт.",
		}},
	{Key: "sql.diag_not_1c", Category: SQLConnOther,
		Title: "База не похожа на базу 1С",
		Steps: []string{
			"Проверьте, что выбрана база 1С:Предприятия (в ней есть таблицы Config, Params и _Node…).",
			"Для другой базы задайте таблицы сопоставления вручную.",
		}},
}
