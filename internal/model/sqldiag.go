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
	Key      string          `json:"key"`
	Category string          `json:"category"`
	Title    LocalizedText   `json:"title"`
	Steps    []LocalizedText `json:"steps"`
}

// SQLConnCategories lists the categories in display order.
func SQLConnCategories() []string {
	return []string{SQLConnLogin, SQLConnDatabase, SQLConnTLS, SQLConnTimeout, SQLConnNetwork, SQLConnConfig, SQLConnOther}
}

type lt = LocalizedText

var sqlDiagnoses = []SQLDiagnosis{
	{Key: "sql.diag_login", Category: SQLConnLogin,
		Title: lt{"Сервер отклонил вход", "The server rejected the login"},
		Steps: []LocalizedText{
			{"Проверьте логин и пароль (для SQL-аутентификации пароль вводится заново после каждого запуска службы).", "Check the login and password (with SQL authentication the password is entered again after every service start)."},
			{"Для SQL-логина на сервере должен быть включён смешанный режим проверки подлинности (SQL Server and Windows Authentication mode).", "For a SQL login the server must use mixed mode (SQL Server and Windows Authentication mode)."},
			{"Логин может быть отключён или заблокирован политикой паролей — проверьте его в SQL Server Management Studio (Безопасность → Имена входа).", "The login may be disabled or locked out by the password policy — check it in SQL Server Management Studio (Security → Logins)."},
			{"Для Windows-аутентификации служба входит под своей учётной записью (LocalSystem → компьютер ДОМЕН\\ИМЯ$ на удалённом сервере, NT AUTHORITY\\SYSTEM на этом же компьютере); создайте для неё имя входа скриптом «Выдать права».", "With Windows authentication the service logs in as its own account (LocalSystem → computer account DOMAIN\\NAME$ on a remote server, NT AUTHORITY\\SYSTEM on the same computer); create a login for it with the \"Grant access\" script."},
		}},
	{Key: "sql.diag_database", Category: SQLConnDatabase,
		Title: lt{"База недоступна", "The database is not available"},
		Steps: []LocalizedText{
			{"Проверьте имя базы (выберите её из списка баз сервера).", "Check the database name (pick it from the server's database list)."},
			{"У логина нет пользователя в этой базе: выполните скрипт «Выдать права» (роль db_datareader).", "The login has no user in this database: run the \"Grant access\" script (db_datareader role)."},
			{"База может быть отключена, в режиме восстановления или однопользовательском режиме — проверьте её состояние на сервере.", "The database may be offline, restoring or in single-user mode — check its state on the server."},
		}},
	{Key: "sql.diag_tls", Category: SQLConnTLS,
		Title: lt{"Ошибка шифрования (TLS)", "Encryption (TLS) error"},
		Steps: []LocalizedText{
			{"Для SQL Server 2005/2008 без обновлений TLS 1.2 выберите «Тип сервера: SQL Server 2008 / 2008 R2» или включите «Разрешить TLS 1.0 (SQL Server 2008)».", "For SQL Server 2005/2008 without the TLS 1.2 updates choose \"Server type: SQL Server 2008 / 2008 R2\" or enable \"Allow TLS 1.0 (SQL Server 2008)\"."},
			{"Если сертификат сервера самоподписанный, при «Шифрование: Весь сеанс (TLS)» включите «Доверять сертификату сервера без проверки».", "If the server certificate is self-signed, with \"Encryption: Whole session (TLS)\" enable \"Trust the server certificate without validation\"."},
			{"Если шифрование не требуется, выберите «Шифрование: Без шифрования» (пароль SQL идёт по сети почти открытым текстом).", "If encryption is not required choose \"Encryption: No encryption\" (the SQL password travels almost in clear text)."},
		}},
	{Key: "sql.diag_tls_enable_tls10", Category: SQLConnTLS,
		Title: lt{"Серверу нужен TLS 1.0", "The server needs TLS 1.0"},
		Steps: []LocalizedText{
			{"Включите «Разрешить TLS 1.0 (SQL Server 2008)» или выберите «Тип сервера: SQL Server 2008 / 2008 R2».", "Enable \"Allow TLS 1.0 (SQL Server 2008)\" or choose \"Server type: SQL Server 2008 / 2008 R2\"."},
			{"Либо установите на сервер обновление TLS 1.2 для SQL Server 2008 / 2008 R2.", "Or install the TLS 1.2 update for SQL Server 2008 / 2008 R2 on the server."},
		}},
	{Key: "sql.diag_tls_legacy_failed", Category: SQLConnTLS,
		Title: lt{"TLS не согласован даже с TLS 1.0", "TLS failed even with TLS 1.0"},
		Steps: []LocalizedText{
			{"Сервер предлагает только устаревшие шифры (например RC4): установите на сервер обновление TLS 1.2.", "The server offers only obsolete ciphers (e.g. RC4): install the TLS 1.2 update on the server."},
			{"Либо выберите «Шифрование: Без шифрования».", "Or choose \"Encryption: No encryption\"."},
		}},
	{Key: "sql.diag_tls_certificate", Category: SQLConnTLS,
		Title: lt{"Сертификат сервера не прошёл проверку", "The server certificate was rejected"},
		Steps: []LocalizedText{
			{"Включите «Доверять сертификату сервера без проверки» (при «Шифрование: Весь сеанс (TLS)»).", "Enable \"Trust the server certificate without validation\" (with \"Encryption: Whole session (TLS)\")."},
			{"Либо выберите «Шифрование: Только вход, если сервер умеет TLS» или установите на сервер доверенный сертификат с именем сервера.", "Or choose \"Encryption: Login only, if the server supports TLS\", or install a trusted certificate with the server name on the server."},
		}},
	{Key: "sql.diag_timeout", Category: SQLConnTimeout,
		Title: lt{"Сервер не ответил вовремя", "The server did not answer in time"},
		Steps: []LocalizedText{
			{"Проверьте, что служба SQL Server запущена и компьютер доступен по сети.", "Check that the SQL Server service is running and the computer is reachable."},
			{"Брандмауэр мог отбросить пакеты: откройте TCP-порт экземпляра (по умолчанию 1433) и UDP 1434 для именованного экземпляра (SQL Server Browser).", "A firewall may drop the packets: open the instance's TCP port (1433 by default) and UDP 1434 for a named instance (SQL Server Browser)."},
			{"Для медленной сети увеличьте тайм-аут подключения в профиле.", "On a slow network increase the connection timeout of the profile."},
		}},
	{Key: "sql.diag_network", Category: SQLConnNetwork,
		Title: lt{"Сервер не найден или отказал в соединении", "Server not found or connection refused"},
		Steps: []LocalizedText{
			{"Проверьте имя сервера и экземпляра (кнопка «Найти серверы» показывает серверы, отвечающие SQL Server Browser).", "Check the server and instance name (\"Find servers\" lists the servers that answer the SQL Server Browser)."},
			{"Для именованного экземпляра должна работать служба SQL Server Browser, либо укажите TCP-порт явно.", "A named instance needs the SQL Server Browser service, or enter the TCP port explicitly."},
			{"В SQL Server Configuration Manager включите протокол TCP/IP для экземпляра и перезапустите его.", "Enable the TCP/IP protocol for the instance in SQL Server Configuration Manager and restart it."},
			{"Откройте порт в брандмауэре Windows на сервере.", "Open the port in the Windows firewall on the server."},
		}},
	{Key: "sql.diag_config", Category: SQLConnConfig,
		Title: lt{"Неверные параметры подключения", "Invalid connection settings"},
		Steps: []LocalizedText{
			{"Проверьте поля профиля: сервер («хост» или «хост\\экземпляр»), порт, база, способ входа, шифрование.", "Check the profile fields: server (\"host\" or \"host\\instance\"), port, database, authentication, encryption."},
		}},
	{Key: "sql.diag_other", Category: SQLConnOther,
		Title: lt{"Не удалось подключиться", "Could not connect"},
		Steps: []LocalizedText{
			{"Повторите проверку; текст ошибки сервера показан ниже — передайте его администратору SQL Server.", "Try again; the server's error text is shown below — pass it to the SQL Server administrator."},
		}},
}

func copyDiagnosis(d SQLDiagnosis) SQLDiagnosis {
	d.Steps = append([]LocalizedText(nil), d.Steps...)
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
