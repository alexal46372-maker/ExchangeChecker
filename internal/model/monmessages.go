package model

// Localizable message keys of the monitor (phase 3). Same convention as
// sqlcheck.Message (stable keys, RU/EN table) but in model, so the tray can
// show them without importing the SQL driver.
const (
	// MsgMonitorPollError: generic poll error (no more specific key).
	MsgMonitorPollError = "monitor.poll_error"
	// MsgMonitorConnError: cannot connect / log in to SQL Server.
	MsgMonitorConnError = "monitor.conn_error"
	// MsgMonitorTimeout: the poll of the profile exceeded its time budget.
	MsgMonitorTimeout = "monitor.timeout"
	// MsgMonitorQueryError: a query failed.
	MsgMonitorQueryError = "monitor.query_error"
	// MsgMonitorMapping: the profile has no usable mapping.
	MsgMonitorMapping = "monitor.mapping"
	// MsgMonitorConfig: profile configuration / password problem.
	MsgMonitorConfig = "monitor.config"
	// MsgMonitorPanic: internal error while polling (recovered).
	MsgMonitorPanic = "monitor.panic"
	// MsgMonitorBusy: the previous poll of the profile is still running.
	MsgMonitorBusy = "monitor.busy"
	// MsgMonitorBaseline: counters mode, "not earlier than service start".
	MsgMonitorBaseline = "monitor.baseline"
	// MsgMonitorHung: the reader stayed busy longer than HungFactor poll
	// timeouts and was closed and recreated (CHK-MON-13).
	MsgMonitorHung = "monitor.hung"
	// MsgMonitorConfigStale: profiles.json / secrets could not be read or
	// parsed; the last good configuration stays in use (CHK-MON-10).
	MsgMonitorConfigStale = "monitor.config_stale"
	// MsgMonitorConfigInvalid: a config file cannot be read or parsed and
	// there is no previously loaded valid configuration (first start):
	// default settings are used; profiles from an unreadable profiles.json
	// are not polled (CHK-MON-10).
	MsgMonitorConfigInvalid = "monitor.config_invalid"
	// MsgMonitorPollerHung: no full poll cycle completed within the hang
	// threshold (CHK-IPC-04).
	MsgMonitorPollerHung = "monitor.poller_hung"
	// MsgMonitorUnknownProfile: IPC command for a profile ID that is not
	// in the current configuration (deleted / never existed).
	MsgMonitorUnknownProfile = "monitor.unknown_profile"
	// MsgMonitorUnknownPoint: ack_alert for a point the profile does not
	// have.
	MsgMonitorUnknownPoint = "monitor.unknown_point"
	// MsgMonitorNoActiveAlert: ack_alert found no active alert.
	MsgMonitorNoActiveAlert = "monitor.no_active_alert"
	// MsgMonitorAlertAcked: informational text of an acknowledgement.
	MsgMonitorAlertAcked = "monitor.alert_acked"
	// MsgMonitorRateLimited: the command was refused by the rate limit.
	MsgMonitorRateLimited = "monitor.rate_limited"

	// test_profile (CHK-IPC-03): result categories and request errors.
	MsgTestOK       = "monitor.test_ok"
	MsgTestNetwork  = "monitor.test_network"
	MsgTestLogin    = "monitor.test_login"
	MsgTestDatabase = "monitor.test_database"
	MsgTestTLS      = "monitor.test_tls"
	MsgTestTimeout  = "monitor.test_timeout"
	MsgTestConfig   = "monitor.test_config"
	MsgTestOther    = "monitor.test_other"
	// MsgTestBusy: the maximum number of concurrent tests is running.
	MsgTestBusy = "monitor.test_busy"
	// MsgTestPasswordRequired: empty password and the saved one may not be
	// reused (other server / login / auth mode, weaker encryption, new
	// profile).
	MsgTestPasswordRequired = "monitor.test_password_required"
	// MsgTestWindowsUnsaved: Windows authentication is tested only for a
	// saved profile with its saved server parameters.
	MsgTestWindowsUnsaved = "monitor.test_windows_unsaved"
	// MsgTestInvalidProfile: the profile of the request is invalid.
	MsgTestInvalidProfile = "monitor.test_invalid_profile"
)

var monitorMessages = map[string][2]string{ // key -> {ru, en}
	MsgMonitorPollError:      {"Ошибка опроса базы.", "Database poll failed."},
	MsgMonitorConnError:      {"Нет соединения с SQL Server (сеть, имя сервера, логин или пароль).", "Cannot connect to SQL Server (network, server name, login or password)."},
	MsgMonitorTimeout:        {"Опрос базы не уложился в отведённое время.", "The database poll timed out."},
	MsgMonitorQueryError:     {"Ошибка запроса к базе.", "A database query failed."},
	MsgMonitorMapping:        {"Для базы не настроены таблицы обмена (выполните «Найти таблицы»).", "The exchange tables are not configured (run \"Find tables\")."},
	MsgMonitorConfig:         {"Ошибка настроек профиля или пароля.", "Profile settings or password error."},
	MsgMonitorPanic:          {"Внутренняя ошибка при опросе базы.", "Internal error while polling the database."},
	MsgMonitorBusy:           {"Предыдущий опрос базы ещё выполняется.", "The previous poll of the database is still running."},
	MsgMonitorBaseline:       {"Не раньше запуска наблюдения службой.", "Not earlier than the service started observing."},
	MsgMonitorHung:           {"Опрос базы завис; соединение закрыто и будет открыто заново.", "The database poll hung; the connection was closed and will be reopened."},
	MsgMonitorConfigInvalid:  {"Файл настроек не читается или повреждён, а ранее загруженных корректных настроек нет: используются значения по умолчанию; базы из нечитаемого списка профилей не опрашиваются.", "A configuration file cannot be read or is corrupt and no valid configuration was loaded before: default settings are used; databases from an unreadable profile list are not polled."},
	MsgMonitorConfigStale:    {"Файл настроек не читается или повреждён; используются последние корректные настройки.", "The configuration file cannot be read or is corrupt; the last valid configuration is in use."},
	MsgMonitorPollerHung:     {"Служба давно не завершала цикл опроса баз: опрос, возможно, завис.", "The service has not completed a poll cycle for too long: polling may be hung."},
	MsgMonitorUnknownProfile: {"Такой базы нет в настройках (удалена или не сохранена).", "No such database in the configuration (deleted or not saved)."},
	MsgMonitorUnknownPoint:   {"У базы нет такой точки обмена.", "The database has no such exchange point."},
	MsgMonitorNoActiveAlert:  {"Нет активной тревоги для подтверждения.", "There is no active alarm to acknowledge."},
	MsgMonitorAlertAcked:     {"Тревога подтверждена.", "Alarm acknowledged."},
	MsgMonitorRateLimited:    {"Слишком частые запросы, повторите через несколько секунд.", "Too many requests, try again in a few seconds."},
	MsgTestOK:                {"Соединение установлено.", "Connection succeeded."},
	MsgTestNetwork:           {"Сервер недоступен по сети (имя, порт, экземпляр, брандмауэр).", "The server is unreachable (name, port, instance, firewall)."},
	MsgTestLogin:             {"Ошибка входа: неверный логин или пароль, учётная запись заблокирована или пароль истёк.", "Login failed: wrong login or password, account locked or password expired."},
	MsgTestDatabase:          {"База данных не найдена или нет доступа к ней.", "The database does not exist or access is denied."},
	MsgTestTLS:               {"Ошибка шифрования (TLS) при подключении.", "Encryption (TLS) error while connecting."},
	MsgTestTimeout:           {"Сервер не ответил за отведённое время.", "The server did not answer in time."},
	MsgTestConfig:            {"Параметры подключения заданы неверно.", "The connection settings are invalid."},
	MsgTestOther:             {"Не удалось подключиться.", "The connection failed."},
	MsgTestBusy:              {"Уже выполняется проверка подключения, повторите позже.", "A connection test is already running, try again later."},
	MsgTestPasswordRequired:  {"Введите пароль.", "Enter the password."},
	MsgTestWindowsUnsaved:    {"Сохраните профиль, чтобы проверить Windows-аутентификацию.", "Save the profile to test Windows authentication."},
	MsgTestInvalidProfile:    {"Профиль заполнен неверно.", "The profile is invalid."},
}

// MonitorMessage returns the text of a monitor.* key in lang ("en",
// anything else = "ru"), or "" for an unknown key.
func MonitorMessage(key, lang string) string {
	m, ok := monitorMessages[key]
	if !ok {
		return ""
	}
	if lang == LangEN {
		return m[1]
	}
	return m[0]
}

// MonitorMessageKeys lists the monitor.* keys.
func MonitorMessageKeys() []string {
	return []string{MsgMonitorPollError, MsgMonitorConnError, MsgMonitorTimeout, MsgMonitorQueryError,
		MsgMonitorMapping, MsgMonitorConfig, MsgMonitorPanic, MsgMonitorBusy, MsgMonitorBaseline, MsgMonitorHung, MsgMonitorConfigStale, MsgMonitorConfigInvalid,
		MsgMonitorPollerHung, MsgMonitorUnknownProfile, MsgMonitorUnknownPoint, MsgMonitorNoActiveAlert, MsgMonitorAlertAcked, MsgMonitorRateLimited,
		MsgTestOK, MsgTestNetwork, MsgTestLogin, MsgTestDatabase, MsgTestTLS, MsgTestTimeout, MsgTestConfig, MsgTestOther,
		MsgTestBusy, MsgTestPasswordRequired, MsgTestWindowsUnsaved, MsgTestInvalidProfile}
}
