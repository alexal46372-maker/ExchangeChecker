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

	// Phase 6 (CHK-IPC-06, CHK-CFG-03, CHK-IPC-07).
	// MsgMonitorAccessDenied: admin command from a non-elevated client.
	MsgMonitorAccessDenied = "monitor.access_denied_admin"
	// MsgMonitorConfigSaveFailed: the configuration could not be read or
	// written (nothing changed).
	MsgMonitorConfigSaveFailed = "monitor.config_save_failed"
	// MsgMonitorConfigConflict: expectedRevision mismatch.
	MsgMonitorConfigConflict = "monitor.config_conflict"
	// MsgMonitorProfileInvalid / MsgMonitorSettingsInvalid: validation
	// failed; ErrorArgs = [field, validation.* reason, params...].
	MsgMonitorProfileInvalid  = "monitor.profile_invalid"
	MsgMonitorSettingsInvalid = "monitor.settings_invalid"
	// MsgMonitorCanceled: the request was canceled.
	MsgMonitorCanceled = "monitor.canceled"
	// MsgMonitorUnknownRequest: cancel for no running request of the
	// connection.
	MsgMonitorUnknownRequest = "monitor.unknown_request"
	// MsgMonitorUnknownCommand: the service does not know the command.
	MsgMonitorUnknownCommand = "monitor.unknown_command"
	// MsgMonitorBadRequest: malformed request (missing field, duplicate
	// request id).
	MsgMonitorBadRequest = "monitor.bad_request"
	// MsgMonitorConfigChanged: event 105 text.
	MsgMonitorConfigChanged = "monitor.config_changed"

	// Phase 6, G2/G3 (CHK-IPC-08/09): discover, discover_by_value,
	// list_node_points, check_mapping.
	// MsgDiscoverBusy: the service runs its one discovery (discover /
	// discover_by_value) or two lighter operations (list_node_points /
	// check_mapping) already, or abandoned operations are still pending.
	MsgDiscoverBusy = "monitor.discover_busy"
	// MsgDiscoverProfileBusy: an interactive operation of the same
	// profile is running (one per profile).
	MsgDiscoverProfileBusy = "monitor.discover_profile_busy"
	// MsgDiscoverTimeout: the operation did not finish in time (the
	// service limit or the profile's time budget).
	MsgDiscoverTimeout = "monitor.discover_timeout"
	// MsgDiscoverFailed: the operation failed (no more specific key).
	MsgDiscoverFailed = "monitor.discover_failed"
	// MsgDiscoverPointNameEmpty: discover_by_value without a point name
	// (and without a point picked from list_node_points).
	MsgDiscoverPointNameEmpty = "monitor.discover_point_name_empty"
	// MsgDiscoverMappingInvalid: check_mapping found a problem;
	// ErrorArgs = [mapping.<field>, validation.* reason, params...].
	MsgDiscoverMappingInvalid = "monitor.discover_mapping_invalid"
	// MsgMonitorInternal: internal error of the service while handling a
	// request (a recovered panic; code internal, CHK-IPC-08).
	MsgMonitorInternal = "monitor.internal"
)

var monitorMessages = map[string][2]string{ // key -> {ru, en}
	MsgMonitorPollError:        {"Ошибка опроса базы.", "Database poll failed."},
	MsgMonitorConnError:        {"Нет соединения с SQL Server (сеть, имя сервера, логин или пароль).", "Cannot connect to SQL Server (network, server name, login or password)."},
	MsgMonitorTimeout:          {"Опрос базы не уложился в отведённое время.", "The database poll timed out."},
	MsgMonitorQueryError:       {"Ошибка запроса к базе.", "A database query failed."},
	MsgMonitorMapping:          {"Для базы не настроены таблицы обмена (выполните «Найти таблицы»).", "The exchange tables are not configured (run \"Find tables\")."},
	MsgMonitorConfig:           {"Ошибка настроек профиля или пароля.", "Profile settings or password error."},
	MsgMonitorPanic:            {"Внутренняя ошибка при опросе базы.", "Internal error while polling the database."},
	MsgMonitorBusy:             {"Предыдущий опрос базы ещё выполняется.", "The previous poll of the database is still running."},
	MsgMonitorBaseline:         {"Не раньше запуска наблюдения службой.", "Not earlier than the service started observing."},
	MsgMonitorHung:             {"Опрос базы завис; соединение закрыто и будет открыто заново.", "The database poll hung; the connection was closed and will be reopened."},
	MsgMonitorConfigInvalid:    {"Файл настроек не читается или повреждён, а ранее загруженных корректных настроек нет: используются значения по умолчанию; базы из нечитаемого списка профилей не опрашиваются.", "A configuration file cannot be read or is corrupt and no valid configuration was loaded before: default settings are used; databases from an unreadable profile list are not polled."},
	MsgMonitorConfigStale:      {"Файл настроек не читается или повреждён; используются последние корректные настройки.", "The configuration file cannot be read or is corrupt; the last valid configuration is in use."},
	MsgMonitorPollerHung:       {"Служба давно не завершала цикл опроса баз: опрос, возможно, завис.", "The service has not completed a poll cycle for too long: polling may be hung."},
	MsgMonitorUnknownProfile:   {"Такой базы нет в настройках (удалена или не сохранена).", "No such database in the configuration (deleted or not saved)."},
	MsgMonitorUnknownPoint:     {"У базы нет такой точки обмена.", "The database has no such exchange point."},
	MsgMonitorNoActiveAlert:    {"Нет активной тревоги для подтверждения.", "There is no active alarm to acknowledge."},
	MsgMonitorAlertAcked:       {"Тревога подтверждена.", "Alarm acknowledged."},
	MsgMonitorRateLimited:      {"Слишком частые запросы, повторите через несколько секунд.", "Too many requests, try again in a few seconds."},
	MsgTestOK:                  {"Соединение установлено.", "Connection succeeded."},
	MsgTestNetwork:             {"Сервер недоступен по сети (имя, порт, экземпляр, брандмауэр).", "The server is unreachable (name, port, instance, firewall)."},
	MsgTestLogin:               {"Ошибка входа: неверный логин или пароль, учётная запись заблокирована или пароль истёк.", "Login failed: wrong login or password, account locked or password expired."},
	MsgTestDatabase:            {"База данных не найдена или нет доступа к ней.", "The database does not exist or access is denied."},
	MsgTestTLS:                 {"Ошибка шифрования (TLS) при подключении.", "Encryption (TLS) error while connecting."},
	MsgTestTimeout:             {"Сервер не ответил за отведённое время.", "The server did not answer in time."},
	MsgTestConfig:              {"Параметры подключения заданы неверно.", "The connection settings are invalid."},
	MsgTestOther:               {"Не удалось подключиться.", "The connection failed."},
	MsgTestBusy:                {"Уже выполняется проверка подключения, повторите позже.", "A connection test is already running, try again later."},
	MsgTestPasswordRequired:    {"Введите пароль.", "Enter the password."},
	MsgTestWindowsUnsaved:      {"Сохраните профиль, чтобы проверить Windows-аутентификацию.", "Save the profile to test Windows authentication."},
	MsgTestInvalidProfile:      {"Профиль заполнен неверно.", "The profile is invalid."},
	MsgMonitorAccessDenied:     {"Нужны права администратора: запустите настройки от имени администратора.", "Administrator rights required: open the settings as administrator."},
	MsgMonitorConfigSaveFailed: {"Не удалось прочитать или записать настройки; ничего не изменено.", "The configuration could not be read or written; nothing was changed."},
	MsgMonitorConfigConflict:   {"Настройки изменились с момента открытия окна; обновите и повторите.", "The configuration changed since the window was opened; reload and try again."},
	MsgMonitorProfileInvalid:   {"Профиль заполнен неверно.", "The profile is invalid."},
	MsgMonitorSettingsInvalid:  {"Настройки заполнены неверно.", "The settings are invalid."},
	MsgMonitorCanceled:         {"Операция отменена.", "The operation was canceled."},
	MsgMonitorUnknownRequest:   {"Нет такой выполняющейся операции.", "No such running operation."},
	MsgMonitorUnknownCommand:   {"Служба не знает этой команды (обновите службу).", "The service does not know this command (update the service)."},
	MsgMonitorBadRequest:       {"Неверный запрос.", "Malformed request."},
	MsgMonitorConfigChanged:    {"Конфигурация изменена.", "Configuration changed."},
	MsgDiscoverBusy:            {"Служба уже ищет таблицы обмена или проверяет настройки, повторите позже.", "The service is already searching for exchange tables or checking a mapping, try again later."},
	MsgDiscoverProfileBusy:     {"Для этой базы уже выполняется операция, дождитесь её окончания.", "An operation for this database is already running, wait for it to finish."},
	MsgDiscoverTimeout:         {"Операция с базой не уложилась в отведённое время.", "The database operation timed out."},
	MsgDiscoverFailed:          {"Операция с базой не выполнена.", "The database operation failed."},
	MsgDiscoverPointNameEmpty:  {"Укажите имя или код точки обмена.", "Enter the exchange point name or code."},
	MsgDiscoverMappingInvalid:  {"Таблицы обмена не прошли проверку.", "The exchange tables did not pass the check."},
	MsgMonitorInternal:         {"Внутренняя ошибка службы при выполнении запроса. Подробности — в журнале службы.", "Internal service error while handling the request. See the service log for details."},
	ValRequired:                {"Поле «{0}» обязательно.", "Field \"{0}\" is required."},
	ValRange:                   {"Поле «{0}»: значение {4} вне диапазона {2}…{3}.", "Field \"{0}\": value {4} is out of range {2}..{3}."},
	ValEnum:                    {"Поле «{0}»: недопустимое значение «{2}».", "Field \"{0}\": unsupported value \"{2}\"."},
	ValGreaterThan:             {"Поле «{0}» должно быть больше «{2}» ({3}).", "Field \"{0}\" must be greater than \"{2}\" ({3})."},
	ValTooLong:                 {"Поле «{0}» длиннее {2} байт.", "Field \"{0}\" is longer than {2} bytes."},
	ValTooMany:                 {"Поле «{0}»: больше {2} элементов.", "Field \"{0}\": more than {2} items."},
	ValPresetConflict:          {"Поле «{0}»: значение «{2}» несовместимо с пресетом сервера «{3}».", "Field \"{0}\": value \"{2}\" conflicts with the server preset \"{3}\"."},
	ValServerFormat:            {"Сервер: укажите «хост» или «хост\\экземпляр».", "Server: enter \"host\" or \"host\\instance\"."},
	ValMapping:                 {"Таблицы обмена: несогласованные поля («{0}»).", "Exchange tables: inconsistent fields (\"{0}\")."},
	ValDuplicate:               {"Профиль с таким идентификатором уже есть.", "A profile with this id already exists."},
	ValServerForbidden:         {"Сервер: именованные каналы (np:), LPC (lpc:) и UNC-пути не допускаются, укажите «хост» или «хост\\экземпляр».", "Server: named pipes (np:), LPC (lpc:) and UNC paths are not allowed; enter \"host\" or \"host\\instance\"."},
	MsgPasswordTargetChange:    {"Сервер или порт изменён: введите пароль заново.", "The server or port changed: enter the password again."},
	MsgCheckRateLimited:        {"Проверка уже запрашивалась недавно, повторите позже.", "A check was requested recently, try again later."},
	MsgMonitorProfileOff:       {"Профиль отключён.", "The profile is disabled."},
	MsgHistoryDisabled:         {"История опросов недоступна.", "The poll history is not available."},
	MsgHistoryBadRange:         {"Неверный интервал истории.", "Invalid history interval."},
	MsgHistoryRangeTooLong:     {"Интервал истории больше 31 дня.", "The history interval is longer than 31 days."},
	MsgHistoryBadCursor:        {"Неверная позиция продолжения истории, запросите заново.", "Invalid history continuation, request it again."},
	MsgHistoryBadFilter:        {"Неверный фильтр истории.", "Invalid history filter."},
	MsgHistoryBusy:             {"История уже запрашивается, повторите позже.", "The history is already being read, try again later."},
	MsgHistoryFailed:           {"Не удалось прочитать историю.", "The history could not be read."},
	ValConflict:                {"Поле «{0}»: нельзя одновременно задать пароль и удалить его.", "Field \"{0}\": a password cannot be set and cleared at once."},
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
		MsgTestBusy, MsgTestPasswordRequired, MsgTestWindowsUnsaved, MsgTestInvalidProfile,
		MsgMonitorAccessDenied, MsgMonitorConfigSaveFailed, MsgMonitorConfigConflict, MsgMonitorProfileInvalid,
		MsgMonitorSettingsInvalid, MsgMonitorCanceled, MsgMonitorUnknownRequest, MsgMonitorUnknownCommand,
		MsgMonitorBadRequest, MsgMonitorConfigChanged,
		MsgDiscoverBusy, MsgDiscoverProfileBusy, MsgDiscoverTimeout, MsgDiscoverFailed, MsgDiscoverPointNameEmpty,
		MsgDiscoverMappingInvalid, MsgMonitorInternal,
		MsgPasswordTargetChange, MsgCheckRateLimited, MsgMonitorProfileOff, MsgHistoryDisabled, MsgHistoryBadRange,
		MsgHistoryRangeTooLong, MsgHistoryBadCursor, MsgHistoryBadFilter, MsgHistoryBusy, MsgHistoryFailed}
}
