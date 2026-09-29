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
)

var monitorMessages = map[string][2]string{ // key -> {ru, en}
	MsgMonitorPollError:     {"Ошибка опроса базы.", "Database poll failed."},
	MsgMonitorConnError:     {"Нет соединения с SQL Server (сеть, имя сервера, логин или пароль).", "Cannot connect to SQL Server (network, server name, login or password)."},
	MsgMonitorTimeout:       {"Опрос базы не уложился в отведённое время.", "The database poll timed out."},
	MsgMonitorQueryError:    {"Ошибка запроса к базе.", "A database query failed."},
	MsgMonitorMapping:       {"Для базы не настроены таблицы обмена (выполните «Найти таблицы»).", "The exchange tables are not configured (run \"Find tables\")."},
	MsgMonitorConfig:        {"Ошибка настроек профиля или пароля.", "Profile settings or password error."},
	MsgMonitorPanic:         {"Внутренняя ошибка при опросе базы.", "Internal error while polling the database."},
	MsgMonitorBusy:          {"Предыдущий опрос базы ещё выполняется.", "The previous poll of the database is still running."},
	MsgMonitorBaseline:      {"Не раньше запуска наблюдения службой.", "Not earlier than the service started observing."},
	MsgMonitorHung:          {"Опрос базы завис; соединение закрыто и будет открыто заново.", "The database poll hung; the connection was closed and will be reopened."},
	MsgMonitorConfigInvalid: {"Файл настроек не читается или повреждён, а ранее загруженных корректных настроек нет: используются значения по умолчанию; базы из нечитаемого списка профилей не опрашиваются.", "A configuration file cannot be read or is corrupt and no valid configuration was loaded before: default settings are used; databases from an unreadable profile list are not polled."},
	MsgMonitorConfigStale:   {"Файл настроек не читается или повреждён; используются последние корректные настройки.", "The configuration file cannot be read or is corrupt; the last valid configuration is in use."},
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
		MsgMonitorMapping, MsgMonitorConfig, MsgMonitorPanic, MsgMonitorBusy, MsgMonitorBaseline, MsgMonitorHung, MsgMonitorConfigStale, MsgMonitorConfigInvalid}
}
