package model

// Localizable message keys of the monitor (phase 3). Same convention as
// sqlcheck.Message (stable keys, Russian texts) but in model, so the tray can
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
	// MsgMonitorNoActiveMute: unmute_profile when the profile is not muted.
	MsgMonitorNoActiveMute = "monitor.no_active_mute"
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

var monitorMessages = map[string]string{ // key -> text (RU)
	MsgMonitorPollError:        "Ошибка опроса базы.",
	MsgMonitorConnError:        "Нет соединения с SQL Server (сеть, имя сервера, логин или пароль).",
	MsgMonitorTimeout:          "Опрос базы не уложился в отведённое время.",
	MsgMonitorQueryError:       "Ошибка запроса к базе.",
	MsgMonitorMapping:          "Для базы не настроены таблицы обмена (выполните «Найти таблицы»).",
	MsgMonitorConfig:           "Ошибка настроек профиля или пароля.",
	MsgMonitorPanic:            "Внутренняя ошибка при опросе базы.",
	MsgTestStepsServerMismatch: "Сервер не совпадает с сервером профиля: сохранённый пароль профиля используется только для его сервера (имя, экземпляр, порт). Проверьте сервер или выполните проверку без профиля.",

	MsgMonitorStalled:          "Обмен идёт, но номера сообщений узла не меняются дольше порога «застоя».",
	MsgMonitorBusy:             "Предыдущий опрос базы ещё выполняется.",
	MsgMonitorBaseline:         "Не раньше запуска наблюдения службой.",
	MsgMonitorHung:             "Опрос базы завис; соединение закрыто и будет открыто заново.",
	MsgMonitorConfigInvalid:    "Файл настроек не читается или повреждён, а ранее загруженных корректных настроек нет: используются значения по умолчанию; базы из нечитаемого списка профилей не опрашиваются.",
	MsgMonitorConfigStale:      "Файл настроек не читается или повреждён; используются последние корректные настройки.",
	MsgMonitorPollerHung:       "Служба давно не завершала цикл опроса баз: опрос, возможно, завис.",
	MsgMonitorUnknownProfile:   "Такой базы нет в настройках (удалена или не сохранена).",
	MsgMonitorUnknownPoint:     "У базы нет такой точки обмена.",
	MsgMonitorNoActiveAlert:    "Нет активной тревоги для подтверждения.",
	MsgMonitorNoActiveMute:     "Профиль не отключён (нет активного mute).",
	MsgMonitorAlertAcked:       "Тревога подтверждена.",
	MsgMonitorRateLimited:      "Слишком частые запросы, повторите через несколько секунд.",
	MsgTestOK:                  "Соединение установлено.",
	MsgTestNetwork:             "Сервер недоступен по сети (имя, порт, экземпляр, брандмауэр).",
	MsgTestLogin:               "Ошибка входа: неверный логин или пароль, учётная запись заблокирована или пароль истёк.",
	MsgTestDatabase:            "База данных не найдена или нет доступа к ней.",
	MsgTestTLS:                 "Ошибка шифрования (TLS) при подключении.",
	MsgTestTimeout:             "Сервер не ответил за отведённое время.",
	MsgTestConfig:              "Параметры подключения заданы неверно.",
	MsgTestOther:               "Не удалось подключиться.",
	MsgTestBusy:                "Уже выполняется проверка подключения, повторите позже.",
	MsgTestPasswordRequired:    "Введите пароль.",
	MsgTestWindowsUnsaved:      "Сохраните профиль, чтобы проверить Windows-аутентификацию.",
	MsgTestInvalidProfile:      "Профиль заполнен неверно.",
	MsgMonitorAccessDenied:     "Нужны права администратора: запустите настройки от имени администратора.",
	MsgMonitorConfigSaveFailed: "Не удалось прочитать или записать настройки; ничего не изменено.",
	MsgMonitorConfigConflict:   "Настройки изменились с момента открытия окна; обновите и повторите.",
	MsgMonitorProfileInvalid:   "Профиль заполнен неверно.",
	MsgMonitorSettingsInvalid:  "Настройки заполнены неверно.",
	MsgMonitorCanceled:         "Операция отменена.",
	MsgMonitorUnknownRequest:   "Нет такой выполняющейся операции.",
	MsgMonitorUnknownCommand:   "Служба не знает этой команды (обновите службу).",
	MsgMonitorBadRequest:       "Неверный запрос.",
	MsgImportFormat:            "Неподдерживаемый формат файла конфигурации (ожидается ec-config/1).",
	MsgImportMode:              "Неверный режим импорта (merge или replace).",
	MsgImportDuplicateID:       "В файле конфигурации повторяется идентификатор профиля.",
	MsgImportNoConfig:          "Не передана конфигурация для импорта.",
	MsgViberNoToken:            "Не задан токен бота Viber.",
	MsgViberNoRecipients:       "Не заданы получатели Viber.",
	MsgViberCanceled:           "Отправка в Viber отменена.",
	MsgViberTimeout:            "Время отправки в Viber истекло.",
	MsgViberAuthFailed:         "Viber отклонил токен бота.",
	MsgViberReceiverInvalid:    "Получатель Viber не найден или не подписан на бота.",
	MsgViberRateLimited:        "Слишком много запросов к Viber; повторите позже.",
	MsgViberNetwork:            "Нет связи с сервером Viber (сеть, прокси).",
	MsgViberTLS:                "Ошибка TLS при подключении к Viber: обновите корневые сертификаты Windows.",
	MsgViberFailed:             "Viber не принял сообщение.",
	MsgAlarmTestLevel:          "Уровень тестовой тревоги: warning или error.",
	MsgViberNotChecked:         "ещё не проверена",
	MsgViberErrorUnknown:       "ошибка подключения или запроса",
	MsgViberInvalid:            "Неверные настройки Viber.",
	MsgEmailNoPassword:         "Не задан пароль SMTP.",
	MsgEmailNoRecipients:       "Нет сохранённых получателей e-mail: добавьте получателя и сохраните настройки e-mail, затем повторите тест.",
	MsgEmailRecipientNotFound:  "Получатель e-mail с таким ID не найден среди сохранённых: сохраните настройки e-mail и повторите тест.",
	MsgMonitorUnknownEvent:     "Тревога не найдена или уже снята.",
	MsgTelegramNoToken:         "Не задан токен бота Telegram.",
	MsgTelegramNoChat:          "Не задан ID чата Telegram.",
	MsgTelegramAuthFailed:      "Telegram отклонил токен бота.",
	MsgTelegramChatInvalid:     "Чат Telegram не найден или бот не добавлен в чат.",
	MsgTelegramRateLimited:     "Слишком много запросов к Telegram; повторите позже.",
	MsgTelegramNetwork:         "Нет связи с сервером Telegram (сеть, прокси).",
	MsgTelegramTLS:             "Не удалось установить защищённое соединение с Telegram (TLS 1.2, корневые сертификаты).",
	MsgTelegramFailed:          "Telegram не принял сообщение.",
	MsgTelegramTimeout:         "Время отправки в Telegram истекло.",
	MsgTelegramCanceled:        "Отправка в Telegram отменена.",
	MsgTelegramInvalid:         "Неверные настройки Telegram.",
	MsgEmailNoHost:             "Не задан SMTP-сервер.",
	MsgEmailNoFrom:             "Не задан адрес отправителя.",
	MsgEmailAuthFailed:         "SMTP-сервер отклонил логин или пароль.",
	MsgEmailInvalidAddr:        "Неверный адрес email получателя.",
	MsgEmailNetwork:            "Нет связи с SMTP-сервером (сеть, прокси).",
	MsgEmailTLS:                "Ошибка TLS при подключении к SMTP: обновите корневые сертификаты Windows.",
	MsgEmailFailed:             "SMTP-сервер не принял сообщение.",
	MsgEmailCanceled:           "Отправка email отменена.",
	MsgEmailTimeout:            "Время отправки email истекло.",
	MsgEmailInvalid:            "Неверные настройки email.",
	MsgEmailRateLimited:        "Слишком частый тест email; повторите позже.",
	MsgImportTrayTooLarge:      "Блок настроек трея в файле конфигурации слишком большой.",
	MsgImportRollbackFailed:    "Импорт не удался, и прежнюю конфигурацию восстановить не удалось: состояние не восстановлено, проверьте настройки.",
	MsgMonitorConfigChanged:    "Конфигурация изменена.",
	MsgDiscoverBusy:            "Служба уже ищет таблицы обмена или проверяет настройки, повторите позже.",
	MsgDiscoverProfileBusy:     "Для этой базы уже выполняется операция, дождитесь её окончания.",
	MsgDiscoverTimeout:         "Операция с базой не уложилась в отведённое время.",
	MsgDiscoverFailed:          "Операция с базой не выполнена.",
	MsgDiscoverPointNameEmpty:  "Укажите имя или код точки обмена.",
	MsgDiscoverMappingInvalid:  "Таблицы обмена не прошли проверку.",
	MsgMonitorInternal:         "Внутренняя ошибка службы при выполнении запроса. Подробности — в журнале службы.",
	ValRequired:                "Поле «{0}» обязательно.",
	ValRange:                   "Поле «{0}»: значение {4} вне диапазона {2}…{3}.",
	ValEnum:                    "Поле «{0}»: недопустимое значение «{2}».",
	ValGreaterThan:             "Поле «{0}» должно быть больше «{2}» ({3}).",
	ValTooLong:                 "Поле «{0}» длиннее {2} байт.",
	ValTooMany:                 "Поле «{0}»: больше {2} элементов.",
	ValPresetConflict:          "Поле «{0}»: значение «{2}» несовместимо с пресетом сервера «{3}».",
	ValServerFormat:            "Сервер: укажите «хост» или «хост\\экземпляр».",
	ValMapping:                 "Таблицы обмена: несогласованные поля («{0}»).",
	ValDuplicate:               "Профиль с таким идентификатором уже есть.",
	ValSQLServerForbidden:      "Сервер: префиксы протокола (tcp:, np:, lpc:, sm:, via:, admin:) и UNC-пути не допускаются, укажите «хост» или «хост\\экземпляр».",
	MsgPasswordTargetChange:    "Сервер или порт изменён: введите пароль заново.",
	MsgBrowseRateLimited:       "Поиск серверов SQL уже выполнялся недавно, повторите позже.",
	MsgCheckRateLimited:        "Проверка уже запрашивалась недавно, повторите позже.",
	MsgMonitorProfileOff:       "Профиль отключён.",
	MsgHistoryDisabled:         "История опросов недоступна.",
	MsgHistoryBadRange:         "Неверный интервал истории.",
	MsgHistoryRangeTooLong:     "Интервал истории больше 31 дня.",
	MsgHistoryBadCursor:        "Неверная позиция продолжения истории, запросите заново.",
	MsgHistoryBadFilter:        "Неверный фильтр истории.",
	MsgHistoryBusy:             "История уже запрашивается, повторите позже.",
	MsgHistoryFailed:           "Не удалось прочитать историю.",
	ValConflict:                "Поле «{0}»: нельзя одновременно задать пароль и удалить его.",
}

// MonitorMessage returns the (Russian) text of a monitor.* key, or ""
// for an unknown key.
func MonitorMessage(key string) string {
	return monitorMessages[key]
}

// MonitorMessageKeys lists the monitor.* keys.
func MonitorMessageKeys() []string {
	return []string{MsgMonitorPollError, MsgMonitorConnError, MsgMonitorTimeout, MsgMonitorQueryError,
		MsgMonitorMapping, MsgMonitorConfig, MsgMonitorPanic, MsgMonitorBusy, MsgMonitorBaseline, MsgMonitorHung, MsgMonitorConfigStale, MsgMonitorConfigInvalid,
		MsgMonitorPollerHung, MsgMonitorUnknownProfile, MsgMonitorUnknownPoint, MsgMonitorNoActiveAlert, MsgMonitorNoActiveMute, MsgMonitorAlertAcked, MsgMonitorRateLimited,
		MsgTestOK, MsgTestNetwork, MsgTestLogin, MsgTestDatabase, MsgTestTLS, MsgTestTimeout, MsgTestConfig, MsgTestOther,
		MsgTestBusy, MsgTestPasswordRequired, MsgTestWindowsUnsaved, MsgTestInvalidProfile,
		MsgMonitorAccessDenied, MsgMonitorConfigSaveFailed, MsgMonitorConfigConflict, MsgMonitorProfileInvalid,
		MsgMonitorSettingsInvalid, MsgMonitorCanceled, MsgMonitorUnknownRequest, MsgMonitorUnknownCommand,
		MsgMonitorBadRequest, MsgMonitorConfigChanged,
		MsgDiscoverBusy, MsgDiscoverProfileBusy, MsgDiscoverTimeout, MsgDiscoverFailed, MsgDiscoverPointNameEmpty,
		MsgDiscoverMappingInvalid, MsgMonitorInternal,
		MsgPasswordTargetChange, MsgCheckRateLimited, MsgBrowseRateLimited, MsgMonitorProfileOff, MsgHistoryDisabled, MsgHistoryBadRange,
		MsgHistoryRangeTooLong, MsgHistoryBadCursor, MsgHistoryBadFilter, MsgHistoryBusy, MsgHistoryFailed,
		MsgImportFormat, MsgImportMode, MsgImportDuplicateID, MsgImportNoConfig, MsgImportTrayTooLarge, MsgImportRollbackFailed,
		MsgViberNoToken, MsgViberNoRecipients, MsgViberAuthFailed, MsgViberReceiverInvalid, MsgViberRateLimited,
		MsgViberNetwork, MsgViberTLS, MsgViberFailed, MsgViberInvalid, MsgViberNotChecked, MsgViberErrorUnknown, MsgAlarmTestLevel, MsgViberCanceled, MsgViberTimeout, MsgMonitorStalled, MsgTestStepsServerMismatch, MsgEmailNoPassword, MsgEmailNoRecipients, MsgEmailRecipientNotFound, MsgMonitorUnknownEvent, MsgTelegramNoToken, MsgTelegramNoChat, MsgTelegramAuthFailed, MsgTelegramChatInvalid, MsgTelegramRateLimited, MsgTelegramNetwork, MsgTelegramTLS, MsgTelegramFailed, MsgTelegramTimeout, MsgTelegramCanceled, MsgTelegramInvalid, MsgEmailNoHost, MsgEmailNoFrom, MsgEmailAuthFailed, MsgEmailInvalidAddr, MsgEmailNetwork, MsgEmailTLS, MsgEmailFailed, MsgEmailCanceled, MsgEmailTimeout, MsgEmailInvalid, MsgEmailRateLimited}
}
