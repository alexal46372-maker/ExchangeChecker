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

	KeyMenuSettingsDisabled = "menu.settings_disabled"
	KeyMenuAbout            = "menu.about"
	KeyMenuExit             = "menu.exit"

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

	KeyMenuSettingsDisabled: {"Настройки (будут доступны в следующем обновлении)", "Settings (available in the next update)"},
	KeyMenuAbout:            {"О программе", "About"},
	KeyMenuExit:             {"Выход", "Exit"},

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

	KeyUnknownMessage: {"Ошибка", "Error"},
}
