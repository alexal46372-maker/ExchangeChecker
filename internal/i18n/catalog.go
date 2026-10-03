package i18n

// Catalog keys of the tray UI (phase 6a). Format verbs (%s, %d) are the
// same in both languages.
const (
	KeyAppName = "app.name"

	KeySvcConnecting    = "svc.connecting"
	KeySvcNotRunning    = "svc.not_running"
	KeySvcUnavailable   = "svc.unavailable"
	KeySvcNotResponding = "svc.not_responding"
	KeySvcUntrusted     = "svc.untrusted"
	KeySvcPollerHung    = "svc.poller_hung"
	KeySvcOK            = "svc.ok"
	KeySvcOldProtocol   = "svc.old_protocol"

	KeyLevelOK      = "level.ok"
	KeyLevelWarning = "level.warning"
	KeyLevelError   = "level.error"

	KeyDBDisabled = "db.disabled"
	KeyDBPending  = "db.pending"
	KeyDBStale    = "db.stale"
	KeyDBNone     = "db.none"
	KeyDBNoPoints = "db.no_points"
	KeyDBNoData   = "db.no_data"

	KeyPtSend     = "pt.send"
	KeyPtRecv     = "pt.recv"
	KeyPtNever    = "pt.never"
	KeyPtLag      = "pt.lag"
	KeyPtBaseline = "pt.baseline"
	KeyPtAcked    = "pt.acked"
	KeyPtPlan     = "pt.plan"

	KeyMenuAbout = "menu.about"
	KeyMenuExit  = "menu.exit"

	KeyAboutTitle     = "about.title"
	KeyAboutVersion   = "about.version"
	KeyAboutProtocol  = "about.protocol"
	KeyAboutNoService = "about.no_service"
	KeyAboutText      = "about.text"
	KeyAboutOK        = "about.ok"

	// alert window (phase 6b, CHK-TRAY-03)
	KeyAlertTitleError     = "alert.title_error"
	KeyAlertTitleWarning   = "alert.title_warning"
	KeyAlertHeadError      = "alert.head_error"
	KeyAlertHeadWarning    = "alert.head_warning"
	KeyAlertPoint          = "alert.point"
	KeyAlertPointNever     = "alert.point_never"
	KeyAlertProfile        = "alert.profile"
	KeyAlertMore           = "alert.more"
	KeyAlertAck            = "alert.ack"
	KeyAlertClose          = "alert.close"
	KeyAlertAckPending     = "alert.ack_pending"
	KeyAlertAckGone        = "alert.ack_gone"
	KeyAlertAckUnsupported = "alert.ack_unsupported"
	KeyAlertAckFailed      = "alert.ack_failed"
	KeyAlertCloseHint      = "alert.close_hint"

	// settings window (phase 6c, CHK-TRAY-04/05/06)
	KeyMenuSettings       = "menu.settings"
	KeySetTitleRead       = "set.title_read"
	KeySetTitleAdmin      = "set.title_admin"
	KeySetProfiles        = "set.profiles"
	KeySetNewProfile      = "set.new_profile"
	KeySetName            = "set.name"
	KeySetHost            = "set.host"
	KeySetInstance        = "set.instance"
	KeySetPort            = "set.port"
	KeySetDatabase        = "set.database"
	KeySetAuth            = "set.auth"
	KeySetAuthSQL         = "set.auth_sql"
	KeySetAuthWindows     = "set.auth_windows"
	KeySetLogin           = "set.login"
	KeySetPassword        = "set.password"
	KeySetClearPassword   = "set.clear_password"
	KeySetEncrypt         = "set.encrypt"
	KeySetEncDisable      = "set.enc_disable"
	KeySetEncOptional     = "set.enc_optional"
	KeySetEncOn           = "set.enc_on"
	KeySetEncStrict       = "set.enc_strict"
	KeySetTLS10           = "set.tls10"
	KeySetTrust           = "set.trust"
	KeySetEnabled         = "set.enabled"
	KeySetMapYes          = "set.map_yes"
	KeySetMapNo           = "set.map_no"
	KeySetThresholds      = "set.thresholds"
	KeySetTest            = "set.test"
	KeySetCancelTest      = "set.cancel_test"
	KeySetSave            = "set.save"
	KeySetDelete          = "set.delete"
	KeySetAdd             = "set.add"
	KeySetPoll            = "set.poll"
	KeySetReload          = "set.reload"
	KeySetElevate         = "set.elevate"
	KeySetClose           = "set.close"
	KeySetHintPortInst    = "set.hint_port_instance"
	KeySetHintPlaintext   = "set.hint_plaintext"
	KeySetPwNew           = "set.pw_new"
	KeySetPwKeep          = "set.pw_keep"
	KeySetPwReenter       = "set.pw_reenter"
	KeySetPwNone          = "set.pw_none"
	KeySetPwReplace       = "set.pw_replace"
	KeySetPwClear         = "set.pw_clear"
	KeySetPwWindows       = "set.pw_windows"
	KeySetPwWindowsDrop   = "set.pw_windows_drop"
	KeySetPwSaved         = "set.pw_saved"
	KeySetPwNotSaved      = "set.pw_not_saved"
	KeySetReadOnlyNote    = "set.read_only_note"
	KeySetLoading         = "set.loading"
	KeySetNoProfile       = "set.no_profile"
	KeySetSaving          = "set.saving"
	KeySetSaved           = "set.saved"
	KeySetSavedPoll       = "set.saved_poll"
	KeySetDeleting        = "set.deleting"
	KeySetDeleted         = "set.deleted"
	KeySetDeleteConfirm   = "set.delete_confirm"
	KeySetTesting         = "set.testing"
	KeySetTestCanceled    = "set.test_canceled"
	KeySetTestOK          = "set.test_ok"
	KeySetTestFailed      = "set.test_failed"
	KeySetTestVersion     = "set.test_version"
	KeySetTestCompat      = "set.test_compat"
	KeySetTestEncrypt     = "set.test_encrypt"
	KeySetTestNoReply     = "set.test_no_reply"
	KeySetPolled          = "set.polled"
	KeySetPollCoalesced   = "set.poll_coalesced"
	KeySetNotConnected    = "set.not_connected"
	KeySetUnsupported     = "set.unsupported"
	KeySetTooLarge        = "set.too_large"
	KeySetNoRevision      = "set.no_revision"
	KeySetConflictHint    = "set.conflict_hint"
	KeySetOtherInvalid    = "set.other_invalid"
	KeySetAccessHint      = "set.access_hint"
	KeySetRateHint        = "set.rate_hint"
	KeySetTimeoutHint     = "set.timeout_hint"
	KeySetFailed          = "set.failed"
	KeySetElevateCanceled = "set.elevate_canceled"
	KeySetElevateFailed   = "set.elevate_failed"

	KeyUnknownMessage = "msg.unknown"
)

var catalog = map[string][2]string{ // key -> {ru, en}
	KeyAppName: {"Exchange Checker", "Exchange Checker"},

	KeySvcConnecting:    {"Подключение к службе…", "Connecting to the service…"},
	KeySvcNotRunning:    {"Служба Exchange Checker не запущена", "The Exchange Checker service is not running"},
	KeySvcUnavailable:   {"Нет связи со службой Exchange Checker", "No connection to the Exchange Checker service"},
	KeySvcNotResponding: {"Служба не отвечает", "The service is not responding"},
	KeySvcUntrusted:     {"Канал службы открыт посторонним процессом — команды не отправляются", "The service pipe is owned by another process — no commands are sent"},
	KeySvcPollerHung:    {"Служба работает, но опрос баз завис", "The service is running, but polling is hung"},
	KeySvcOK:            {"Служба работает", "The service is running"},
	KeySvcOldProtocol:   {"Старая версия службы (протокол %d): часть функций недоступна", "Old service version (protocol %d): some features are unavailable"},

	KeyLevelOK:      {"норма", "OK"},
	KeyLevelWarning: {"предупреждение", "warning"},
	KeyLevelError:   {"ошибка", "error"},

	KeyDBDisabled: {"выключена", "disabled"},
	KeyDBPending:  {"ожидание опроса", "waiting for the first poll"},
	KeyDBStale:    {"последние известные данные", "last known data"},
	KeyDBNone:     {"Нет настроенных баз", "No databases configured"},
	KeyDBNoPoints: {"Нет точек обмена", "No exchange points"},
	KeyDBNoData:   {"Нет данных от службы", "No data from the service"},

	KeyPtSend:     {"отпр.", "sent"},
	KeyPtRecv:     {"приём", "recv"},
	KeyPtNever:    {"—", "—"},
	KeyPtLag:      {"%d мин назад", "%d min ago"},
	KeyPtBaseline: {"не раньше запуска службы", "not earlier than the service start"},
	KeyPtAcked:    {"подтверждено", "acknowledged"},
	KeyPtPlan:     {"План обмена", "Exchange plan"},

	KeyMenuAbout: {"О программе", "About"},
	KeyMenuExit:  {"Выход", "Exit"},

	KeyAboutTitle:     {"О программе Exchange Checker", "About Exchange Checker"},
	KeyAboutVersion:   {"Версия: %s", "Version: %s"},
	KeyAboutProtocol:  {"Служба: протокол %d", "Service: protocol %d"},
	KeyAboutNoService: {"Служба: не подключена", "Service: not connected"},
	KeyAboutText:      {"Контроль обменов 1С 8.1 в базах Microsoft SQL Server.", "Monitoring of 1C 8.1 exchanges in Microsoft SQL Server databases."},
	KeyAboutOK:        {"OK", "OK"},

	KeyAlertTitleError:     {"Exchange Checker — ошибка обмена", "Exchange Checker — exchange error"},
	KeyAlertTitleWarning:   {"Exchange Checker — предупреждение", "Exchange Checker — warning"},
	KeyAlertHeadError:      {"Обмен данными остановлен или база недоступна:", "Data exchange has stopped or a database is unavailable:"},
	KeyAlertHeadWarning:    {"Обмен данными задерживается:", "Data exchange is delayed:"},
	KeyAlertPoint:          {"%s — %s: %s, нет обмена %d мин", "%s — %s: %s, no exchange for %d min"},
	KeyAlertPointNever:     {"%s — %s: %s, обмена ещё не было", "%s — %s: %s, no exchange yet"},
	KeyAlertProfile:        {"%s: %s — %s", "%s: %s — %s"},
	KeyAlertMore:           {"…и ещё %d", "…and %d more"},
	KeyAlertAck:            {"Подтвердить", "Acknowledge"},
	KeyAlertClose:          {"Закрыть", "Close"},
	KeyAlertAckPending:     {"Подтверждение…", "Acknowledging…"},
	KeyAlertAckGone:        {"Тревога уже снята или подтверждена.", "The alarm has already cleared or been acknowledged."},
	KeyAlertAckUnsupported: {"Эта версия службы не поддерживает подтверждение тревог.", "This service version cannot acknowledge alarms."},
	KeyAlertAckFailed:      {"Не удалось подтвердить: %s", "Could not acknowledge: %s"},
	KeyAlertCloseHint:      {"«Закрыть» выключает звук до следующего изменения; «Подтвердить» снимает тревогу.", "Close mutes the sound until the next change; Acknowledge clears the alarm."},

	KeyMenuSettings:       {"Настройки…", "Settings…"},
	KeySetTitleRead:       {"Exchange Checker — настройки баз (только чтение)", "Exchange Checker — database settings (read only)"},
	KeySetTitleAdmin:      {"Exchange Checker — настройки баз (администратор)", "Exchange Checker — database settings (administrator)"},
	KeySetProfiles:        {"Базы:", "Databases:"},
	KeySetNewProfile:      {"(новая база)", "(new database)"},
	KeySetName:            {"Название:", "Name:"},
	KeySetHost:            {"Сервер (хост):", "Server (host):"},
	KeySetInstance:        {"Экземпляр:", "Instance:"},
	KeySetPort:            {"Порт:", "Port:"},
	KeySetDatabase:        {"База данных:", "Database:"},
	KeySetAuth:            {"Аутентификация:", "Authentication:"},
	KeySetAuthSQL:         {"SQL Server (логин и пароль)", "SQL Server (login and password)"},
	KeySetAuthWindows:     {"Windows (учётная запись службы)", "Windows (service account)"},
	KeySetLogin:           {"Логин:", "Login:"},
	KeySetPassword:        {"Пароль:", "Password:"},
	KeySetClearPassword:   {"Удалить сохранённый пароль", "Delete the saved password"},
	KeySetEncrypt:         {"Шифрование:", "Encryption:"},
	KeySetEncDisable:      {"Без шифрования", "No encryption"},
	KeySetEncOptional:     {"Только вход, если сервер умеет TLS", "Login only, if the server supports TLS"},
	KeySetEncOn:           {"Весь сеанс (TLS)", "Whole session (TLS)"},
	KeySetEncStrict:       {"Strict (TDS 8.0, SQL Server 2022+)", "Strict (TDS 8.0, SQL Server 2022+)"},
	KeySetTLS10:           {"Разрешить TLS 1.0 (SQL Server 2008)", "Allow TLS 1.0 (SQL Server 2008)"},
	KeySetTrust:           {"Доверять сертификату сервера без проверки", "Trust the server certificate without validation"},
	KeySetEnabled:         {"Опрашивать эту базу", "Monitor this database"},
	KeySetMapYes:          {"Таблицы обмена: настроены", "Exchange tables: configured"},
	KeySetMapNo:           {"Таблицы обмена: не настроены (поиск таблиц — в следующем обновлении)", "Exchange tables: not configured (table discovery comes in the next update)"},
	KeySetThresholds:      {"Свои пороги: предупреждение %d мин, ошибка %d мин", "Own thresholds: warning %d min, error %d min"},
	KeySetTest:            {"Проверить подключение", "Test connection"},
	KeySetCancelTest:      {"Отменить проверку", "Cancel test"},
	KeySetSave:            {"Сохранить", "Save"},
	KeySetDelete:          {"Удалить", "Delete"},
	KeySetAdd:             {"Добавить", "Add"},
	KeySetPoll:            {"Опросить сейчас", "Poll now"},
	KeySetReload:          {"Перечитать", "Reload"},
	KeySetElevate:         {"Изменить (администратор)", "Edit (administrator)"},
	KeySetClose:           {"Закрыть", "Close"},
	KeySetHintPortInst:    {"Указан порт: подключение идёт прямо на порт, экземпляр через SQL Browser не ищется.", "A port is set: the connection goes straight to the port, SQL Browser is not asked for the instance."},
	KeySetHintPlaintext:   {"Без шифрования пароль SQL-логина передаётся почти открытым текстом. Лучше Windows-аутентификация или отдельный логин только с ролью db_datareader.", "Without encryption the SQL login password travels almost in clear text. Prefer Windows authentication or a separate login with the db_datareader role only."},
	KeySetPwNew:           {"Новая база: введите пароль SQL-логина.", "New database: enter the SQL login password."},
	KeySetPwKeep:          {"Пусто — оставить сохранённый пароль.", "Empty — keep the saved password."},
	KeySetPwReenter:       {"Сервер, логин или шифрование изменены: сохранённый пароль не будет использован, введите пароль заново.", "Server, login or encryption changed: the saved password will not be used, enter the password again."},
	KeySetPwNone:          {"Пароль не сохранён — введите пароль.", "No password saved — enter the password."},
	KeySetPwReplace:       {"Сохранённый пароль будет заменён.", "The saved password will be replaced."},
	KeySetPwClear:         {"Сохранённый пароль будет удалён.", "The saved password will be deleted."},
	KeySetPwWindows:       {"Windows-аутентификация: пароль не нужен.", "Windows authentication: no password needed."},
	KeySetPwWindowsDrop:   {"Windows-аутентификация: сохранённый пароль будет удалён при сохранении.", "Windows authentication: the saved password will be deleted on save."},
	KeySetPwSaved:         {"Пароль сохранён.", "A password is saved."},
	KeySetPwNotSaved:      {"Пароль не сохранён.", "No password saved."},
	KeySetReadOnlyNote:    {"Только чтение: изменять настройки может администратор.", "Read only: an administrator can change the settings."},
	KeySetLoading:         {"Загрузка настроек…", "Loading the settings…"},
	KeySetNoProfile:       {"Выберите базу или нажмите «Добавить».", "Select a database or press Add."},
	KeySetSaving:          {"Сохранение…", "Saving…"},
	KeySetSaved:           {"Сохранено.", "Saved."},
	KeySetSavedPoll:       {"Сохранено, опрос базы запланирован.", "Saved, the database poll is scheduled."},
	KeySetDeleting:        {"Удаление…", "Deleting…"},
	KeySetDeleted:         {"База удалена из настроек.", "The database was removed from the settings."},
	KeySetDeleteConfirm:   {"Удалить базу «%s» из настроек? Пароль, состояние и тревоги этой базы тоже будут удалены.", "Remove the database \"%s\" from the settings? Its password, state and alarms are removed too."},
	KeySetTesting:         {"Проверка подключения… (до 15 с)", "Testing the connection… (up to 15 s)"},
	KeySetTestCanceled:    {"Проверка отменена.", "The test was canceled."},
	KeySetTestOK:          {"Подключение успешно (%d мс).", "Connected successfully (%d ms)."},
	KeySetTestFailed:      {"Подключение не удалось: %s", "Connection failed: %s"},
	KeySetTestVersion:     {"Сервер: %s", "Server: %s"},
	KeySetTestCompat:      {"Уровень совместимости базы: %d", "Database compatibility level: %d"},
	KeySetTestEncrypt:     {"Шифрование: %s", "Encryption: %s"},
	KeySetTestNoReply:     {"Служба не ответила за %d с.", "The service did not answer within %d s."},
	KeySetPolled:          {"Опрос запущен.", "Poll started."},
	KeySetPollCoalesced:   {"Опрос уже запланирован.", "A poll is already scheduled."},
	KeySetNotConnected:    {"Нет связи со службой Exchange Checker.", "No connection to the Exchange Checker service."},
	KeySetUnsupported:     {"Эта версия службы не поддерживает эту команду.", "This service version does not support this command."},
	KeySetTooLarge:        {"Запрос больше 64 КиБ — сократите данные профиля.", "The request is larger than 64 KiB — shorten the profile data."},
	KeySetNoRevision:      {"Неизвестна версия настроек — нажмите «Перечитать».", "The settings version is unknown — press Reload."},
	KeySetConflictHint:    {"Нажмите «Перечитать»: несохранённые правки будут потеряны.", "Press Reload: unsaved changes will be lost."},
	KeySetOtherInvalid:    {"Сохранить нельзя: в файле настроек неверно заполнена другая база «%s» (%s). Исправьте или удалите её.", "Cannot save: another database \"%s\" in the settings file is invalid (%s). Fix or remove it."},
	KeySetRateHint:        {"Проверку подключения и опрос служба выполняет не чаще раза в 10 секунд.", "The service runs connection tests and polls at most once every 10 seconds."},
	KeySetTimeoutHint:     {"Проверка ограничена 15 секундами: проверьте сеть, порт и брандмауэр.", "The test is limited to 15 seconds: check the network, port and firewall."},
	KeySetAccessHint:      {"Нажмите «Изменить (администратор)».", "Press Edit (administrator)."},
	KeySetFailed:          {"Ошибка: %s", "Error: %s"},
	KeySetElevateCanceled: {"Запуск от имени администратора отменён.", "Running as administrator was canceled."},
	KeySetElevateFailed:   {"Не удалось запустить настройки от имени администратора: %s", "Could not open the settings as administrator: %s"},

	KeyUnknownMessage: {"Ошибка", "Error"},
}
