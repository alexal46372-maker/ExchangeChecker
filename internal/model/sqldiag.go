package model

// Phase 9 (PLAN §12 p. 108, CHK-SQL-09): "what to do" texts for a failed
// connection test, per sqlcheck.ConnCategory and per sql.tls_* key
// (IPC sql_diagnose; driver-independent). Control names follow the tray
// settings window (phase 8, CHK-TRAY-14).

// Connection error categories (= sqlcheck.ConnCategory* values).
const (
	SQLConnLogin    = "login"
	SQLConnDatabase = "database"
	SQLConnTLS      = "tls"
	SQLConnTimeout  = "timeout"
	SQLConnNetwork  = "network"
	SQLConnConfig   = "config"
	SQLConnOther    = "other"
)

// SQLDiagnosis is the advice for one kind of connection failure.
type SQLDiagnosis struct {
	// Key: sql.diag_<category> or sql.diag_<tls key suffix>.
	Key      string   `json:"key"`
	Category string   `json:"category"`
	Title    string   `json:"title"`
	Steps    []string `json:"steps"`
}

// SQLConnCategories lists the categories in display order.
func SQLConnCategories() []string {
	return []string{SQLConnLogin, SQLConnDatabase, SQLConnTLS, SQLConnTimeout, SQLConnNetwork, SQLConnConfig, SQLConnOther}
}

var sqlDiagnoses = []SQLDiagnosis{
	{Key: "sql.diag_login", Category: SQLConnLogin,
		Title: "Сервер отклонил вход",
		Steps: []string{
			"Проверьте логин и пароль (для SQL-аутентификации пароль вводится заново после каждого запуска службы).",
			"Для SQL-логина на сервере должен быть включён смешанный режим проверки подлинности (SQL Server and Windows Authentication mode).",
			"Логин может быть отключён или заблокирован политикой паролей — проверьте его в SQL Server Management Studio (Безопасность → Имена входа).",
			"Для Windows-аутентификации служба входит под своей учётной записью (LocalSystem → компьютер ДОМЕН\\ИМЯ$ на удалённом сервере, NT AUTHORITY\\SYSTEM на этом же компьютере); создайте для неё имя входа скриптом «Выдать права».",
		}},
	{Key: "sql.diag_database", Category: SQLConnDatabase,
		Title: "База недоступна",
		Steps: []string{
			"Проверьте имя базы (выберите её из списка баз сервера).",
			"У логина нет пользователя в этой базе: выполните скрипт «Выдать права» (роль db_datareader).",
			"База может быть отключена, в режиме восстановления или однопользовательском режиме — проверьте её состояние на сервере.",
		}},
	{Key: "sql.diag_tls", Category: SQLConnTLS,
		Title: "Ошибка шифрования (TLS)",
		Steps: []string{
			"Для SQL Server 2005/2008 без обновлений TLS 1.2 выберите «Тип сервера: SQL Server 2008 / 2008 R2» или включите «Разрешить TLS 1.0 (SQL Server 2008)».",
			"Если сертификат сервера самоподписанный, при «Шифрование: Весь сеанс (TLS)» включите «Доверять сертификату сервера без проверки».",
			"Если шифрование не требуется, выберите «Шифрование: Без шифрования» (пароль SQL идёт по сети почти открытым текстом).",
		}},
	{Key: "sql.diag_tls_enable_tls10", Category: SQLConnTLS,
		Title: "Серверу нужен TLS 1.0",
		Steps: []string{
			"Включите «Разрешить TLS 1.0 (SQL Server 2008)» или выберите «Тип сервера: SQL Server 2008 / 2008 R2».",
			"Либо установите на сервер обновление TLS 1.2 для SQL Server 2008 / 2008 R2.",
		}},
	{Key: "sql.diag_tls_legacy_failed", Category: SQLConnTLS,
		Title: "TLS не согласован даже с TLS 1.0",
		Steps: []string{
			"Сервер предлагает только устаревшие шифры (например RC4): установите на сервер обновление TLS 1.2.",
			"Либо выберите «Шифрование: Без шифрования».",
		}},
	{Key: "sql.diag_tls_certificate", Category: SQLConnTLS,
		Title: "Сертификат сервера не прошёл проверку",
		Steps: []string{
			"Включите «Доверять сертификату сервера без проверки» (при «Шифрование: Весь сеанс (TLS)»).",
			"Либо выберите «Шифрование: Только вход, если сервер умеет TLS» или установите на сервер доверенный сертификат с именем сервера.",
		}},
	{Key: "sql.diag_timeout", Category: SQLConnTimeout,
		Title: "Сервер не ответил вовремя",
		Steps: []string{
			"Проверьте, что служба SQL Server запущена и компьютер доступен по сети.",
			"Брандмауэр мог отбросить пакеты: откройте TCP-порт экземпляра (по умолчанию 1433) и UDP 1434 для именованного экземпляра (SQL Server Browser).",
			"Для медленной сети увеличьте тайм-аут подключения в профиле.",
		}},
	{Key: "sql.diag_network", Category: SQLConnNetwork,
		Title: "Сервер не найден или отказал в соединении",
		Steps: []string{
			"Проверьте имя сервера и экземпляра (кнопка «Найти серверы» показывает серверы, отвечающие SQL Server Browser).",
			"Для именованного экземпляра должна работать служба SQL Server Browser, либо укажите TCP-порт явно.",
			"В SQL Server Configuration Manager включите протокол TCP/IP для экземпляра и перезапустите его.",
			"Откройте порт в брандмауэре Windows на сервере.",
		}},
	{Key: "sql.diag_config", Category: SQLConnConfig,
		Title: "Неверные параметры подключения",
		Steps: []string{
			"Проверьте поля профиля: сервер («хост» или «хост\\экземпляр»), порт, база, способ входа, шифрование.",
		}},
	{Key: "sql.diag_other", Category: SQLConnOther,
		Title: "Не удалось подключиться",
		Steps: []string{
			"Повторите проверку; текст ошибки сервера показан ниже — передайте его администратору SQL Server.",
		}},
}

func copyDiagnosis(d SQLDiagnosis) SQLDiagnosis {
	d.Steps = append([]string(nil), d.Steps...)
	return d
}

// SQLDiagnoses returns every diagnosis (a copy).
func SQLDiagnoses() []SQLDiagnosis {
	out := make([]SQLDiagnosis, len(sqlDiagnoses))
	for i, d := range sqlDiagnoses {
		out[i] = copyDiagnosis(d)
	}
	return out
}

// SQLDiagnose returns the advice for a connection test error: errorKey
// (sqlcheck.Error.Key: sql.tls_enable_tls10 / sql.tls_legacy_failed /
// sql.tls_certificate) refines the tls category; an unknown category is
// "other".
func SQLDiagnose(category, errorKey string) SQLDiagnosis {
	want := "sql.diag_" + category
	switch errorKey {
	case MsgSQLTLSEnableTLS10, MsgSQLTLSLegacyFailed, MsgSQLTLSCertificate:
		want = "sql.diag_" + errorKey[len("sql."):]
	}
	for _, d := range sqlDiagnoses {
		if d.Key == want {
			return copyDiagnosis(d)
		}
	}
	return copyDiagnosis(sqlDiagnoses[len(sqlDiagnoses)-1])
}
