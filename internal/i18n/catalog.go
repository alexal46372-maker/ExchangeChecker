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
	KeyMenuSettings         = "menu.settings"
	KeySetTitleRead         = "set.title_read"
	KeySetTitleAdmin        = "set.title_admin"
	KeySetProfiles          = "set.profiles"
	KeySetNewProfile        = "set.new_profile"
	KeySetName              = "set.name"
	KeySetHost              = "set.host"
	KeySetInstance          = "set.instance"
	KeySetPort              = "set.port"
	KeySetDatabase          = "set.database"
	KeySetAuth              = "set.auth"
	KeySetAuthSQL           = "set.auth_sql"
	KeySetAuthWindows       = "set.auth_windows"
	KeySetLogin             = "set.login"
	KeySetPassword          = "set.password"
	KeySetClearPassword     = "set.clear_password"
	KeySetEncrypt           = "set.encrypt"
	KeySetEncDisable        = "set.enc_disable"
	KeySetEncOptional       = "set.enc_optional"
	KeySetEncOn             = "set.enc_on"
	KeySetEncStrict         = "set.enc_strict"
	KeySetTLS10             = "set.tls10"
	KeySetTrust             = "set.trust"
	KeySetEnabled           = "set.enabled"
	KeySetMapYes            = "set.map_yes"
	KeySetMapNo             = "set.map_no"
	KeySetThresholds        = "set.thresholds"
	KeySetTest              = "set.test"
	KeySetCancelTest        = "set.cancel_test"
	KeySetSave              = "set.save"
	KeySetDelete            = "set.delete"
	KeySetAdd               = "set.add"
	KeySetPoll              = "set.poll"
	KeySetReload            = "set.reload"
	KeySetElevate           = "set.elevate"
	KeySetClose             = "set.close"
	KeySetHintPortInst      = "set.hint_port_instance"
	KeySetHintPlaintext     = "set.hint_plaintext"
	KeySetPwNew             = "set.pw_new"
	KeySetPwKeep            = "set.pw_keep"
	KeySetPwReenter         = "set.pw_reenter"
	KeySetPwNone            = "set.pw_none"
	KeySetPwReplace         = "set.pw_replace"
	KeySetPwClear           = "set.pw_clear"
	KeySetPwWindows         = "set.pw_windows"
	KeySetPwWindowsDrop     = "set.pw_windows_drop"
	KeySetPwSaved           = "set.pw_saved"
	KeySetPwNotSaved        = "set.pw_not_saved"
	KeySetReadOnlyNote      = "set.read_only_note"
	KeySetLoading           = "set.loading"
	KeySetNoProfile         = "set.no_profile"
	KeySetSaving            = "set.saving"
	KeySetSaved             = "set.saved"
	KeySetSavedPoll         = "set.saved_poll"
	KeySetDeleting          = "set.deleting"
	KeySetDeleted           = "set.deleted"
	KeySetDeleteConfirm     = "set.delete_confirm"
	KeySetDiscardConfirm    = "set.discard_confirm"
	KeyDiscTitle            = "disc.title"
	KeyDiscButton           = "disc.button"
	KeyDiscFind             = "disc.find"
	KeyDiscListPoints       = "disc.list_points"
	KeyDiscCancel           = "disc.cancel"
	KeyDiscRunning          = "disc.running"
	KeyDiscChecking         = "disc.checking"
	KeyDiscFound            = "disc.found"
	KeyDiscNone             = "disc.none"
	KeyDiscIncomplete       = "disc.incomplete"
	KeyDiscReasonBudget     = "disc.reason_budget"
	KeyDiscReasonSize       = "disc.reason_size"
	KeyDiscReasonLimit      = "disc.reason_limit"
	KeyDiscReasonPointsSize = "disc.reason_points_size"
	KeyDiscReasonCheckSize  = "disc.reason_check_size"
	KeyDiscPointsFound      = "disc.points_found"
	KeyDiscPoint            = "disc.point"
	KeyDiscTime             = "disc.time"
	KeyDiscTolerance        = "disc.tolerance"
	KeyDiscByValue          = "disc.by_value"
	KeyDiscBadTime          = "disc.bad_time"
	KeyDiscBadTolerance     = "disc.bad_tolerance"
	KeyDiscMode             = "disc.mode"
	KeyDiscModeAuto         = "disc.mode_auto"
	KeyDiscModeManual       = "disc.mode_manual"
	KeyDiscModeCounters     = "disc.mode_counters"
	KeyDiscSchema           = "disc.schema"
	KeyDiscTable            = "disc.table"
	KeyDiscPointName        = "disc.point_name"
	KeyDiscPointID          = "disc.point_id"
	KeyDiscPointKey         = "disc.point_key"
	KeyDiscTimesTable       = "disc.times_table"
	KeyDiscTimesLink        = "disc.times_link"
	KeyDiscSend             = "disc.send"
	KeyDiscRecv             = "disc.recv"
	KeyDiscPlanLabel        = "disc.plan_label"
	KeyDiscManualSQL        = "disc.manual_sql"
	KeyDiscYearOffset       = "disc.year_offset"
	KeyDiscKeepUnlinked     = "disc.keep_unlinked"
	KeyDiscAllPlans         = "disc.all_plans"
	KeyDiscTemplate         = "disc.template"
	KeyDiscCheck            = "disc.check"
	KeyDiscApply            = "disc.apply"
	KeyDiscClose            = "disc.close"
	KeyDiscCheckOK          = "disc.check_ok"
	KeyDiscCheckFailed      = "disc.check_failed"
	KeyDiscViolations       = "disc.violations"
	KeyDiscPoints           = "disc.points"
	KeyDiscColumns          = "disc.columns"
	KeyDiscMatched          = "disc.matched"
	KeyDiscCanceled         = "disc.canceled"
	KeyDiscApplied          = "disc.applied"
	KeyDiscProfileChanged   = "disc.profile_changed"
	KeyDiscExtraPlans       = "disc.extra_plans"
	KeyDiscFieldMain        = "disc.field_main"
	KeyDiscFieldExtra       = "disc.field_extra"
	KeyDiscLimit            = "disc.limit"
	KeyDiscTimesSchema      = "disc.times_schema"
	KeyDiscLinkType         = "disc.link_type"
	KeyDiscLinkRTRefCol     = "disc.link_rtref_col"
	KeyDiscLinkRTRef        = "disc.link_rtref"
	KeyDiscScore            = "disc.score"
	KeyDiscCandSingle       = "disc.cand_single"
	KeyDiscCandLinked       = "disc.cand_linked"
	KeyDiscCandCounters     = "disc.cand_counters"
	KeyDiscToleranceHint    = "disc.tolerance_hint"
	KeyDiscConfirmClose     = "disc.confirm_close"
	KeyDiscConfirmOverwrite = "disc.confirm_overwrite"
	KeyDiscConfirmRemove    = "disc.confirm_remove"
	KeyDiscSourceAuto       = "disc.source_auto"
	KeyDiscSourceManual     = "disc.source_manual"
	KeyDiscSourceCounters   = "disc.source_counters"
	KeyDiscSourceNone       = "disc.source_none"
	KeyDiscBusy             = "disc.busy"
	KeyDiscConfigChanged    = "disc.config_changed"
	KeyDiscListChanged      = "disc.list_changed"
	KeyAutostartErrAccess   = "autostart.err_access"
	KeyAutostartErrRegistry = "autostart.err_registry"
	KeyAutostartErrPath     = "autostart.err_path"
	KeyAutostartErrSettings = "autostart.err_settings"
	KeyAutostartErrUnknown  = "autostart.err_unknown"
	KeyGenPage              = "gen.page"
	KeyGenProfilesPage      = "gen.profiles_page"
	KeyGenPoll              = "gen.poll"
	KeyGenWarn              = "gen.warn"
	KeyGenError             = "gen.error"
	KeyGenBeepEnabled       = "gen.beep_enabled"
	KeyGenWarnFreq          = "gen.warn_freq"
	KeyGenWarnDur           = "gen.warn_dur"
	KeyGenErrorFreq         = "gen.error_freq"
	KeyGenErrorDur          = "gen.error_dur"
	KeyGenGap               = "gen.gap"
	KeyGenLanguage          = "gen.language"
	KeyGenLangAuto          = "gen.lang_auto"
	KeyGenLangRU            = "gen.lang_ru"
	KeyGenLangEN            = "gen.lang_en"
	KeyGenAutostart         = "gen.autostart"
	KeyGenNote              = "gen.note"
	KeyGenSaved             = "gen.saved"
	KeyMenuLanguage         = "menu.language"
	KeyMenuLangAuto         = "menu.lang_auto"
	KeyMenuLangRU           = "menu.lang_ru"
	KeyMenuLangEN           = "menu.lang_en"
	KeySetTesting           = "set.testing"
	KeySetTestCanceled      = "set.test_canceled"
	KeySetTestOK            = "set.test_ok"
	KeySetTestFailed        = "set.test_failed"
	KeySetTestVersion       = "set.test_version"
	KeySetTestCompat        = "set.test_compat"
	KeySetTestEncrypt       = "set.test_encrypt"
	KeySetTestNoReply       = "set.test_no_reply"
	KeySetPolled            = "set.polled"
	KeySetPollCoalesced     = "set.poll_coalesced"
	KeySetNotConnected      = "set.not_connected"
	KeySetChangedOffline    = "set.changed_offline"
	KeySetUnsupported       = "set.unsupported"
	KeySetTooLarge          = "set.too_large"
	KeySetNoRevision        = "set.no_revision"
	KeySetConflictHint      = "set.conflict_hint"
	KeySetOtherInvalid      = "set.other_invalid"
	KeySetAccessHint        = "set.access_hint"
	KeySetRateHint          = "set.rate_hint"
	KeySetTimeoutHint       = "set.timeout_hint"
	KeySetFailed            = "set.failed"
	KeySetElevateCanceled   = "set.elevate_canceled"
	KeySetElevateFailed     = "set.elevate_failed"

	KeyUnknownMessage = "msg.unknown"
)

// Phase 8 (tray UX): profile fields, the service menu and tooltips
// (CHK-TRAY-12 … CHK-TRAY-15).
const (
	KeySetPreset          = "set.preset"
	KeySetPreset2008      = "set.preset_2008"
	KeySetPresetNone      = "set.preset_none"
	KeySetWarnMin         = "set.warn_min"
	KeySetErrorMin        = "set.error_min"
	KeySetConnTimeout     = "set.conn_timeout"
	KeySetQueryTimeout    = "set.query_timeout"
	KeySetPortCue         = "set.port_cue"
	KeySetThreshCue       = "set.thresh_cue"
	KeySetTimeoutCue      = "set.timeout_cue"
	KeyMenuService        = "menu.service"
	KeyMenuSvcStart       = "menu.svc_start"
	KeyMenuSvcStop        = "menu.svc_stop"
	KeySvcState           = "svc.state"
	KeySvcRunning         = "svc.running"
	KeySvcStopped         = "svc.stopped"
	KeySvcStartPending    = "svc.start_pending"
	KeySvcStopPending     = "svc.stop_pending"
	KeySvcNotInstalled    = "svc.not_installed"
	KeySvcNoAccess        = "svc.no_access"
	KeySvcUnknown         = "svc.unknown"
	KeySvcStartSent       = "svc.start_sent"
	KeySvcStopSent        = "svc.stop_sent"
	KeySvcErrAccess       = "svc.err_access"
	KeySvcErrNotInstalled = "svc.err_not_installed"
	KeySvcErrRunning      = "svc.err_running"
	KeySvcErrNotActive    = "svc.err_not_active"
	KeySvcErrOther        = "svc.err_other"
	KeyTipSetList         = "tip.set.list"
	KeyTipSetAdd          = "tip.set.add"
	KeyTipSetDelete       = "tip.set.delete"
	KeyTipSetDiscover     = "tip.set.discover"
	KeyTipSetName         = "tip.set.name"
	KeyTipSetHost         = "tip.set.host"
	KeyTipSetInstance     = "tip.set.instance"
	KeyTipSetPort         = "tip.set.port"
	KeyTipSetDatabase     = "tip.set.database"
	KeyTipSetAuth         = "tip.set.auth"
	KeyTipSetLogin        = "tip.set.login"
	KeyTipSetPassword     = "tip.set.password"
	KeyTipSetClearPw      = "tip.set.clear_pw"
	KeyTipSetEncrypt      = "tip.set.encrypt"
	KeyTipSetPreset       = "tip.set.preset"
	KeyTipSetTLS10        = "tip.set.tls10"
	KeyTipSetTrust        = "tip.set.trust"
	KeyTipSetEnabled      = "tip.set.enabled"
	KeyTipSetWarn         = "tip.set.warn"
	KeyTipSetError        = "tip.set.error"
	KeyTipSetConnTimeout  = "tip.set.conn_timeout"
	KeyTipSetQueryTimeout = "tip.set.query_timeout"
	KeyTipSetPage         = "tip.set.page"
	KeyTipSetStatus       = "tip.set.status"
	KeyTipSetTest         = "tip.set.test"
	KeyTipSetCancelTest   = "tip.set.cancel_test"
	KeyTipSetSave         = "tip.set.save"
	KeyTipSetPoll         = "tip.set.poll"
	KeyTipSetReload       = "tip.set.reload"
	KeyTipSetElevate      = "tip.set.elevate"
	KeyTipSetClose        = "tip.set.close"
	KeyTipGenPoll         = "tip.gen.poll"
	KeyTipGenWarn         = "tip.gen.warn"
	KeyTipGenError        = "tip.gen.error"
	KeyTipGenBeep         = "tip.gen.beep"
	KeyTipGenWarnFreq     = "tip.gen.warn_freq"
	KeyTipGenWarnDur      = "tip.gen.warn_dur"
	KeyTipGenErrFreq      = "tip.gen.err_freq"
	KeyTipGenErrDur       = "tip.gen.err_dur"
	KeyTipGenGap          = "tip.gen.gap"
	KeyTipGenLang         = "tip.gen.lang"
	KeyTipGenAutostart    = "tip.gen.autostart"
	KeyTipDiscFind        = "tip.disc.find"
	KeyTipDiscPoints      = "tip.disc.points"
	KeyTipDiscCancel      = "tip.disc.cancel"
	KeyTipDiscPoint       = "tip.disc.point"
	KeyTipDiscTime        = "tip.disc.time"
	KeyTipDiscTol         = "tip.disc.tol"
	KeyTipDiscByValue     = "tip.disc.by_value"
	KeyTipDiscList        = "tip.disc.list"
	KeyTipDiscDetails     = "tip.disc.details"
	KeyTipDiscMode        = "tip.disc.mode"
	KeyTipDiscField       = "tip.disc.field"
	KeyTipDiscPlanLabel   = "tip.disc.plan_label"
	KeyTipDiscYear        = "tip.disc.year"
	KeyTipDiscKeep        = "tip.disc.keep"
	KeyTipDiscAllPlans    = "tip.disc.all_plans"
	KeyTipDiscTemplate    = "tip.disc.template"
	KeyTipDiscSQL         = "tip.disc.sql"
	KeyTipDiscResult      = "tip.disc.result"
	KeyTipDiscCheck       = "tip.disc.check"
	KeyTipDiscApply       = "tip.disc.apply"
	KeyTipDiscClose       = "tip.disc.close"
	KeyTipAlertAck        = "tip.alert.ack"
	KeyTipAlertClose      = "tip.alert.close"
	KeyTipAboutOK         = "tip.about.ok"
)

// Phase 9a (CHK-TRAY-18..21).
const (
	KeyBalloonOverdueTitle   = "balloon.overdue_title"
	KeyBalloonRecoveredTitle = "balloon.recovered_title"
	KeyBalloonOverdue        = "balloon.overdue"
	KeyBalloonOverdueNoLag   = "balloon.overdue_nolag"
	KeyBalloonRecovered      = "balloon.recovered"
	KeyBalloonSummary        = "balloon.summary"
	KeyMenuBalloons          = "menu.balloons"
	KeyMenuStatusWin         = "menu.status_window"
	KeyMenuCheckNow          = "menu.check_now"
	KeyStTitle               = "st.title"
	KeyStDatabase            = "st.database"
	KeyStPoint               = "st.point"
	KeyStPlan                = "st.plan"
	KeyStFresh               = "st.fresh"
	KeyStAgo                 = "st.ago"
	KeyStState               = "st.state"
	KeyStSend                = "st.send"
	KeyStRecv                = "st.recv"
	KeyStNote                = "st.note"
	KeyStDisabled            = "st.disabled"
	KeyStNotPolled           = "st.not_polled"
	KeyStExportedAt          = "st.exported_at"
	KeyStExportCSV           = "st.export_csv"
	KeyStExportHTML          = "st.export_html"
	KeyStClose               = "st.close"
	KeyStSaved               = "st.saved"
	KeyStSaveFailed          = "st.save_failed"
	KeyStCheckQueued         = "st.check_queued"
	KeyStCheckFailed         = "st.check_failed"
	KeyStNoData              = "st.no_data"
	KeyStFilterCSV           = "st.filter_csv"
	KeyStFilterHTML          = "st.filter_html"
	KeyTipStList             = "tip.st_list"
	KeyTipCheckNow           = "tip.check_now"
	KeyTipExportCSV          = "tip.export_csv"
	KeyTipExportHTML         = "tip.export_html"
	KeyTipStInfo             = "tip.st_info"
	KeyTipClose              = "tip.st_close"
	KeyAgoNow                = "ago.now"
	KeyAgoMin                = "ago.min"
	KeyAgoHours              = "ago.hours"
	KeyAgoDays               = "ago.days"
)

var catalog = map[string][2]string{ // key -> {ru, en}
	KeyBalloonOverdueTitle:   {"Просрочка обмена", "Exchange overdue"},
	KeyBalloonRecoveredTitle: {"Обмен восстановлен", "Exchange recovered"},
	KeyBalloonOverdue:        {"%s: нет обмена %d мин (%s)", "%s: no exchange for %d min (%s)"},
	KeyBalloonOverdueNoLag:   {"%s: %s", "%s: %s"},
	KeyBalloonRecovered:      {"%s: обмен снова в норме", "%s: exchange is OK again"},
	KeyBalloonSummary:        {"Просрочено точек: %d, восстановлено: %d", "Points overdue: %d, recovered: %d"},
	KeyMenuBalloons:          {"Уведомления о просрочке", "Overdue notifications"},
	KeyMenuStatusWin:         {"Состояние…", "Status…"},
	KeyMenuCheckNow:          {"Проверить сейчас", "Check now"},
	KeyStTitle:               {"Состояние", "Status"},
	KeyStDatabase:            {"База", "Database"},
	KeyStPoint:               {"Точка", "Point"},
	KeyStPlan:                {"План обмена", "Exchange plan"},
	KeyStFresh:               {"Последний обмен", "Last exchange"},
	KeyStAgo:                 {"Давность", "Age"},
	KeyStState:               {"Состояние", "State"},
	KeyStSend:                {"Отправка", "Sent"},
	KeyStRecv:                {"Приём", "Received"},
	KeyStNote:                {"Примечание", "Note"},
	KeyStDisabled:            {"отключена", "disabled"},
	KeyStNotPolled:           {"ещё не опрошена", "not polled yet"},
	KeyStExportedAt:          {"Выгружено", "Exported"},
	KeyStExportCSV:           {"Экспорт CSV…", "Export CSV…"},
	KeyStExportHTML:          {"Экспорт HTML…", "Export HTML…"},
	KeyStClose:               {"Закрыть", "Close"},
	KeyStSaved:               {"Сохранено: %s", "Saved: %s"},
	KeyStSaveFailed:          {"Не удалось сохранить: %s", "Could not save: %s"},
	KeyStCheckQueued:         {"Проверка запрошена", "Check requested"},
	KeyStCheckFailed:         {"Проверка не запрошена: %s", "Check not requested: %s"},
	KeyStNoData:              {"Нет данных от службы", "No data from the service"},
	KeyStFilterCSV:           {"CSV (;)", "CSV (;)"},
	KeyStFilterHTML:          {"Страница HTML", "HTML page"},
	KeyTipStList:             {"Все базы и точки обмена: цвет — состояние, давность — сколько минут назад был последний обмен", "All databases and exchange points: colour is the state, age is how long ago the last exchange was"},
	KeyTipCheckNow:           {"Опросить все базы сейчас, не дожидаясь интервала (не чаще раза в 10 с)", "Poll all databases now without waiting for the interval (at most once per 10 s)"},
	KeyTipExportCSV:          {"Сохранить таблицу в CSV для Excel (UTF-8, разделитель «;»)", "Save the table as CSV for Excel (UTF-8, separator \";\")"},
	KeyTipExportHTML:         {"Сохранить таблицу как страницу HTML с цветами", "Save the table as an HTML page with colours"},
	KeyTipStInfo:             {"Результат последнего действия: проверка, экспорт", "Result of the last action: check, export"},
	KeyTipClose:              {"Закрыть окно", "Close the window"},
	KeyAgoNow:                {"только что", "just now"},
	KeyAgoMin:                {"%d мин назад", "%d min ago"},
	KeyAgoHours:              {"%d ч %d мин назад", "%d h %d min ago"},
	KeyAgoDays:               {"%d дн. назад", "%d days ago"},
	KeyAppName:               {"Exchange Checker", "Exchange Checker"},

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

	KeyMenuSettings:           {"Настройки…", "Settings…"},
	KeySetTitleRead:           {"Exchange Checker — настройки баз (только чтение)", "Exchange Checker — database settings (read only)"},
	KeySetTitleAdmin:          {"Exchange Checker — настройки баз (администратор)", "Exchange Checker — database settings (administrator)"},
	KeySetProfiles:            {"Базы:", "Databases:"},
	KeySetNewProfile:          {"(новая база)", "(new database)"},
	KeySetName:                {"Название:", "Name:"},
	KeySetHost:                {"Сервер (хост):", "Server (host):"},
	KeySetInstance:            {"Экземпляр:", "Instance:"},
	KeySetPort:                {"Порт:", "Port:"},
	KeySetDatabase:            {"База данных:", "Database:"},
	KeySetAuth:                {"Аутентификация:", "Authentication:"},
	KeySetAuthSQL:             {"SQL Server (логин и пароль)", "SQL Server (login and password)"},
	KeySetAuthWindows:         {"Windows (учётная запись службы)", "Windows (service account)"},
	KeySetLogin:               {"Логин:", "Login:"},
	KeySetPassword:            {"Пароль:", "Password:"},
	KeySetClearPassword:       {"Удалить сохранённый пароль", "Delete the saved password"},
	KeySetEncrypt:             {"Шифрование:", "Encryption:"},
	KeySetEncDisable:          {"Без шифрования", "No encryption"},
	KeySetEncOptional:         {"Только вход, если сервер умеет TLS", "Login only, if the server supports TLS"},
	KeySetEncOn:               {"Весь сеанс (TLS)", "Whole session (TLS)"},
	KeySetEncStrict:           {"Strict (TDS 8.0, SQL Server 2022+)", "Strict (TDS 8.0, SQL Server 2022+)"},
	KeySetTLS10:               {"Разрешить TLS 1.0 (SQL Server 2008)", "Allow TLS 1.0 (SQL Server 2008)"},
	KeySetTrust:               {"Доверять сертификату сервера без проверки", "Trust the server certificate without validation"},
	KeySetEnabled:             {"Опрашивать эту базу", "Monitor this database"},
	KeySetMapYes:              {"Таблицы обмена: настроены", "Exchange tables: configured"},
	KeySetMapNo:               {"Таблицы обмена: не настроены — найдите их кнопкой «Таблицы обмена…»", "Exchange tables: not configured — find them with «Exchange tables…»"},
	KeySetThresholds:          {"Свои пороги: предупреждение %d мин, ошибка %d мин", "Own thresholds: warning %d min, error %d min"},
	KeySetTest:                {"Проверить подключение", "Test connection"},
	KeySetCancelTest:          {"Отменить проверку", "Cancel test"},
	KeySetSave:                {"Сохранить", "Save"},
	KeySetDelete:              {"Удалить", "Delete"},
	KeySetAdd:                 {"Добавить", "Add"},
	KeySetPoll:                {"Опросить сейчас", "Poll now"},
	KeySetReload:              {"Перечитать", "Reload"},
	KeySetElevate:             {"Изменить (администратор)", "Edit (administrator)"},
	KeySetClose:               {"Закрыть", "Close"},
	KeySetHintPortInst:        {"Указан порт: подключение идёт прямо на порт, экземпляр через SQL Browser не ищется.", "A port is set: the connection goes straight to the port, SQL Browser is not asked for the instance."},
	KeySetHintPlaintext:       {"Без шифрования пароль SQL-логина передаётся почти открытым текстом. Лучше Windows-аутентификация или отдельный логин только с ролью db_datareader.", "Without encryption the SQL login password travels almost in clear text. Prefer Windows authentication or a separate login with the db_datareader role only."},
	KeySetPwNew:               {"Новая база: введите пароль SQL-логина.", "New database: enter the SQL login password."},
	KeySetPwKeep:              {"Пусто — оставить сохранённый пароль.", "Empty — keep the saved password."},
	KeySetPwReenter:           {"Сервер, логин или шифрование изменены: сохранённый пароль не будет использован, введите пароль заново.", "Server, login or encryption changed: the saved password will not be used, enter the password again."},
	KeySetPwNone:              {"Пароль не сохранён — введите пароль.", "No password saved — enter the password."},
	KeySetPwReplace:           {"Сохранённый пароль будет заменён.", "The saved password will be replaced."},
	KeySetPwClear:             {"Сохранённый пароль будет удалён.", "The saved password will be deleted."},
	KeySetPwWindows:           {"Windows-аутентификация: пароль не нужен.", "Windows authentication: no password needed."},
	KeySetPwWindowsDrop:       {"Windows-аутентификация: сохранённый пароль будет удалён при сохранении.", "Windows authentication: the saved password will be deleted on save."},
	KeySetPwSaved:             {"Пароль сохранён.", "A password is saved."},
	KeySetPwNotSaved:          {"Пароль не сохранён.", "No password saved."},
	KeySetReadOnlyNote:        {"Только чтение: изменять настройки может администратор.", "Read only: an administrator can change the settings."},
	KeySetLoading:             {"Загрузка настроек…", "Loading the settings…"},
	KeySetNoProfile:           {"Выберите базу или нажмите «Добавить».", "Select a database or press Add."},
	KeySetSaving:              {"Сохранение…", "Saving…"},
	KeySetSaved:               {"Сохранено.", "Saved."},
	KeySetSavedPoll:           {"Сохранено, опрос базы запланирован.", "Saved, the database poll is scheduled."},
	KeySetDeleting:            {"Удаление…", "Deleting…"},
	KeySetDeleted:             {"База удалена из настроек.", "The database was removed from the settings."},
	KeySetDeleteConfirm:       {"Удалить базу «%s» из настроек? Пароль, состояние и тревоги этой базы тоже будут удалены.", "Remove the database \"%s\" from the settings? Its password, state and alarms are removed too."},
	KeySetDiscardConfirm:      {"Отменить изменения?", "Discard changes?"},
	KeyDiscTitle:              {"Таблицы обмена", "Exchange tables"},
	KeyDiscButton:             {"Таблицы обмена…", "Exchange tables…"},
	KeyDiscFind:               {"Найти таблицы", "Find tables"},
	KeyDiscListPoints:         {"Точки обмена", "Exchange points"},
	KeyDiscCancel:             {"Отменить", "Cancel"},
	KeyDiscRunning:            {"Поиск… %d с (служба отвечает не дольше 100 с)", "Searching… %d s (the service answers within 100 s)"},
	KeyDiscChecking:           {"Проверка… %d с", "Checking… %d s"},
	KeyDiscFound:              {"Найдено вариантов: %d.", "Candidates found: %d."},
	KeyDiscNone:               {"Подходящих таблиц не найдено.", "No suitable tables found."},
	KeyDiscIncomplete:         {"Результат неполный.", "The result is incomplete."},
	KeyDiscReasonBudget:       {"Результат неполный: истекло время поиска (90 с). Повторите поиск или сузьте его (поиск по точке и времени).", "The result is incomplete: the search time (90 s) ran out. Retry or narrow the search (search by point and time)."},
	KeyDiscReasonSize:         {"Результат неполный: ответ сокращён до 64 КиБ. Уточните запрос (поиск по точке и времени).", "The result is incomplete: the reply was cut at 64 KiB. Refine the query (search by point and time)."},
	KeyDiscReasonLimit:        {"Список неполный: точек больше, чем запрошено (1000). Введите имя точки вручную.", "The list is incomplete: there are more points than requested (1000). Type the point name."},
	KeyDiscReasonPointsSize:   {"Список неполный: ответ сокращён до 64 КиБ. Введите имя точки вручную.", "The list is incomplete: the reply was cut at 64 KiB. Type the point name."},
	KeyDiscReasonCheckSize:    {"Предпросмотр неполный: ответ сокращён до 64 КиБ.", "The preview is incomplete: the reply was cut at 64 KiB."},
	KeyDiscPointsFound:        {"Точек обмена: %d.", "Exchange points: %d."},
	KeyDiscPoint:              {"Точка:", "Point:"},
	KeyDiscTime:               {"Время обмена (UTC+3):", "Exchange time (UTC+3):"},
	KeyDiscTolerance:          {"Допуск, мин:", "Tolerance, min:"},
	KeyDiscByValue:            {"Найти по точке", "Find by point"},
	KeyDiscBadTime:            {"Введите время в виде ДД.ММ.ГГГГ ЧЧ:ММ (UTC+3).", "Enter the time as DD.MM.YYYY HH:MM (UTC+3)."},
	KeyDiscBadTolerance:       {"Допуск — целое число минут, не меньше 0.", "The tolerance is a whole number of minutes, at least 0."},
	KeyDiscMode:               {"Режим:", "Mode:"},
	KeyDiscModeAuto:           {"Таблицы", "Tables"},
	KeyDiscModeManual:         {"Ручной SQL", "Manual SQL"},
	KeyDiscModeCounters:       {"Счётчики плана обмена", "Exchange plan counters"},
	KeyDiscSchema:             {"Схема:", "Schema:"},
	KeyDiscTable:              {"Таблица точек:", "Points table:"},
	KeyDiscPointName:          {"Колонка имени:", "Name column:"},
	KeyDiscPointID:            {"Колонка кода:", "Code column:"},
	KeyDiscPointKey:           {"Колонка ключа:", "Key column:"},
	KeyDiscTimesTable:         {"Таблица времён:", "Times table:"},
	KeyDiscTimesLink:          {"Колонка связи:", "Link column:"},
	KeyDiscSend:               {"Колонка отправки:", "Send column:"},
	KeyDiscRecv:               {"Колонка приёма:", "Receive column:"},
	KeyDiscPlanLabel:          {"Подпись плана:", "Plan label:"},
	KeyDiscManualSQL:          {"Ручной SQL:", "Manual SQL:"},
	KeyDiscYearOffset:         {"Сдвиг лет 1С (+2000)", "1C year offset (+2000)"},
	KeyDiscKeepUnlinked:       {"Показывать точки без записей времени", "Show points without time records"},
	KeyDiscAllPlans:           {"Добавить все найденные планы обмена", "Add all found exchange plans"},
	KeyDiscTemplate:           {"Шаблон:", "Template:"},
	KeyDiscCheck:              {"Проверить", "Check"},
	KeyDiscApply:              {"Применить", "Apply"},
	KeyDiscClose:              {"Закрыть", "Close"},
	KeyDiscCheckOK:            {"Проверка пройдена. Примеры (отправка / приём, UTC+3):", "Check passed. Examples (send / receive, UTC+3):"},
	KeyDiscCheckFailed:        {"Проверка не пройдена.", "The check failed."},
	KeyDiscViolations:         {"Конструкции SQL Server 2012+ (в SQL Server 2008 нет):", "SQL Server 2012+ constructs (not in SQL Server 2008):"},
	KeyDiscPoints:             {"точек: %d", "points: %d"},
	KeyDiscColumns:            {"Колонки:", "Columns:"},
	KeyDiscMatched:            {"Найденная точка:", "Found point:"},
	KeyDiscCanceled:           {"Операция отменена.", "The operation was canceled."},
	KeyDiscApplied:            {"Таблицы обмена перенесены в профиль. Нажмите «Сохранить».", "The exchange tables were taken into the profile. Click Save."},
	KeyDiscProfileChanged:     {"В окне настроек выбран другой профиль — откройте «Таблицы обмена» заново.", "Another profile is selected in the settings window; open Exchange tables again."},
	KeyDiscExtraPlans:         {"Доп. планы обмена", "Additional exchange plans"},
	KeyDiscFieldMain:          {"Таблицы обмена — %s", "Exchange tables — %s"},
	KeyDiscFieldExtra:         {"Доп. план обмена %d — %s", "Additional exchange plan %d — %s"},
	KeyDiscLimit:              {"Число строк (limit)", "Number of rows (limit)"},
	KeyDiscTimesSchema:        {"Схема таблицы времён", "Times table schema"},
	KeyDiscLinkType:           {"Колонка типа ссылки (_TYPE)", "Reference type column (_TYPE)"},
	KeyDiscLinkRTRefCol:       {"Колонка номера таблицы (_RTRef)", "Table number column (_RTRef)"},
	KeyDiscLinkRTRef:          {"Номер таблицы плана обмена", "Exchange plan table number"},
	KeyDiscScore:              {"оценка %d", "score %d"},
	KeyDiscCandSingle:         {"одна таблица", "single table"},
	KeyDiscCandLinked:         {"связанные таблицы", "linked tables"},
	KeyDiscCandCounters:       {"счётчики", "counters"},
	KeyDiscToleranceHint:      {"0 или пусто — 1 сутки (1440 мин)", "0 or empty = 1 day (1440 min)"},
	KeyDiscConfirmClose:       {"Изменения таблиц обмена не применены. Закрыть без применения?", "The exchange table changes were not applied. Close without applying?"},
	KeyDiscConfirmOverwrite:   {"Поля таблиц обмена изменены вручную и будут заменены. Продолжить?", "The exchange table fields were edited by hand and will be replaced. Continue?"},
	KeyDiscConfirmRemove:      {"При применении из профиля будут убраны: %s. Продолжить?", "Applying removes from the profile: %s. Continue?"},
	KeyDiscSourceAuto:         {"Источник: найдено поиском (auto)", "Source: found by the search (auto)"},
	KeyDiscSourceManual:       {"Источник: задано вручную (manual)", "Source: set by hand (manual)"},
	KeyDiscSourceCounters:     {"Источник: счётчики (counters)", "Source: counters"},
	KeyDiscConfigChanged:      {"Настройки профиля изменились, пока был открыт вопрос; ничего не применено — проверьте и нажмите «Применить» ещё раз.", "The profile settings changed while the question was open; nothing was applied. Check and press Apply again."},
	KeyDiscListChanged:        {"Список кандидатов обновился, пока был открыт вопрос — выберите кандидата ещё раз.", "The candidate list was updated while the question was open; select the candidate again."},
	KeyDiscBusy:               {"Окно настроек ждёт ответа сервиса — нажмите «Применить» ещё раз через несколько секунд.", "The settings window is waiting for the service; press Apply again in a few seconds."},
	KeyDiscSourceNone:         {"Таблицы обмена не заданы", "Exchange tables not set"},
	"hint.linked":             {"Точки и времена в разных таблицах, связаны ключом", "Points and times are in linked tables"},
	"hint.same_table":         {"Времена — колонки таблицы точек", "Times are columns of the points table"},
	"hint.node_table":         {"Таблица точек — план обмена 1С (_Node)", "The points table is a 1C exchange plan (_Node)"},
	"hint.generic":            {"Найдено по названиям колонок (не 1С)", "Found by column names (not 1C)"},
	"hint.by_value":           {"Найдено по указанной точке", "Found by the given point"},
	"hint.exact_match":        {"Имя точки совпало точно", "The point name matched exactly"},
	"hint.near_approx_time":   {"Время близко к указанному", "A time is close to the given one"},
	"hint.all_points_fresh":   {"Все точки обновлялись недавно", "All points were updated recently"},
	"hint.year_offset_2000":   {"Обнаружен сдвиг лет 1С +2000", "1C +2000 year offset detected"},
	"hint.send_recv_by_name":  {"Отправка/приём выбраны по названиям колонок", "Send/receive chosen by column names"},
	"hint.send_recv_by_order": {"Отправка/приём выбраны по порядку колонок — проверьте", "Send/receive chosen by column order — check them"},
	"hint.single_series":      {"Только одна колонка времени", "Only one time column"},
	"hint.no_timestamps":      {"Заполненных времён пока нет", "No filled times yet"},
	"hint.future_dates":       {"Последнее время — в будущем", "The latest time is in the future"},
	"hint.sampled":            {"Статистика по выборке строк", "Statistics from a row sample"},
	"hint.sample_failed":      {"Примеры строк прочитать не удалось", "Sample rows could not be read"},
	"hint.truncated":          {"Время поиска истекло, результат неполный", "The search time ran out; the result is partial"},
	"hint.counters_fallback":  {"Времена будут по изменениям счётчиков (с запуска службы)", "Times will come from counter changes (since service start)"},
	"hint.composite_ref":      {"Составная ссылка 1С (_TYPE/_RTRef/_RRRef)", "1C composite reference (_TYPE/_RTRef/_RRRef)"},
	"hint.by_time":            {"Найдено по времени выбранной точки", "Found by the time of the chosen point"},
	KeyAutostartErrAccess:     {"Служба не смогла изменить автозапуск трея: нет доступа к реестру (HKLM Run).", "The service could not change the tray autostart: registry access denied (HKLM Run)."},
	KeyAutostartErrRegistry:   {"Служба не смогла изменить автозапуск трея: ошибка реестра (HKLM Run).", "The service could not change the tray autostart: registry error (HKLM Run)."},
	KeyAutostartErrPath:       {"Служба не смогла определить путь к программе трея; автозапуск не изменён.", "The service could not determine the tray program path; autostart was not changed."},
	KeyAutostartErrSettings:   {"При запуске служба не смогла прочитать settings.json; автозапуск трея не изменён.", "At startup the service could not read settings.json; the tray autostart was not changed."},
	KeyAutostartErrUnknown:    {"Служба не смогла изменить автозапуск трея.", "The service could not change the tray autostart."},
	KeyGenPage:                {"Общие настройки…", "General settings…"},
	KeyGenProfilesPage:        {"Базы данных…", "Databases…"},
	KeyGenPoll:                {"Интервал опроса, с:", "Poll interval, s:"},
	KeyGenWarn:                {"Предупреждение через, мин:", "Warning after, min:"},
	KeyGenError:               {"Ошибка через, мин:", "Error after, min:"},
	KeyGenBeepEnabled:         {"Звуковой сигнал тревоги", "Alert sound"},
	KeyGenWarnFreq:            {"Предупреждение: частота, Гц:", "Warning: frequency, Hz:"},
	KeyGenWarnDur:             {"Предупреждение: длительность, мс:", "Warning: duration, ms:"},
	KeyGenErrorFreq:           {"Ошибка: частота, Гц:", "Error: frequency, Hz:"},
	KeyGenErrorDur:            {"Ошибка: длительность, мс:", "Error: duration, ms:"},
	KeyGenGap:                 {"Пауза между сигналами, мс:", "Pause between beeps, ms:"},
	KeyGenLanguage:            {"Язык журнала службы:", "Service log language:"},
	KeyGenLangAuto:            {"Как в Windows", "Windows default"},
	KeyGenLangRU:              {"Русский", "Русский (Russian)"},
	KeyGenLangEN:              {"English (английский)", "English"},
	KeyGenAutostart:           {"Запускать значок в трее при входе в Windows (для всех пользователей)", "Start the tray icon at Windows logon (all users)"},
	KeyGenNote:                {"Общие настройки действуют на весь компьютер и применяются службой. Звук выключает только сигнал: окно тревоги показывается всегда. Язык значка в трее выбирается в его меню у каждого пользователя.", "General settings apply to the whole computer and are used by the service. Turning the sound off mutes only the beep: the alert window is always shown. The tray icon language is chosen in its menu by each user."},
	KeyGenSaved:               {"Общие настройки сохранены.", "General settings saved."},
	KeyMenuLanguage:           {"Язык (Language)", "Language (Язык)"},
	KeyMenuLangAuto:           {"Как в Windows", "Windows default"},
	KeyMenuLangRU:             {"Русский", "Русский"},
	KeyMenuLangEN:             {"English", "English"},
	KeySetTesting:             {"Проверка подключения… (до 15 с)", "Testing the connection… (up to 15 s)"},
	KeySetTestCanceled:        {"Проверка отменена.", "The test was canceled."},
	KeySetTestOK:              {"Подключение успешно (%d мс).", "Connected successfully (%d ms)."},
	KeySetTestFailed:          {"Подключение не удалось: %s", "Connection failed: %s"},
	KeySetTestVersion:         {"Сервер: %s", "Server: %s"},
	KeySetTestCompat:          {"Уровень совместимости базы: %d", "Database compatibility level: %d"},
	KeySetTestEncrypt:         {"Шифрование: %s", "Encryption: %s"},
	KeySetTestNoReply:         {"Служба не ответила за %d с.", "The service did not answer within %d s."},
	KeySetPolled:              {"Опрос запущен.", "Poll started."},
	KeySetPollCoalesced:       {"Опрос уже запланирован.", "A poll is already scheduled."},
	KeySetNotConnected:        {"Нет связи со службой Exchange Checker.", "No connection to the Exchange Checker service."},
	KeySetChangedOffline:      {"Настройки изменились, пока не было связи со службой. Нажмите «Перечитать» (несохранённые правки будут потеряны).", "The settings were changed while the service was not connected. Click Reload (unsaved changes will be lost)."},
	KeySetUnsupported:         {"Эта версия службы не поддерживает эту команду.", "This service version does not support this command."},
	KeySetTooLarge:            {"Запрос больше 64 КиБ — сократите данные профиля.", "The request is larger than 64 KiB — shorten the profile data."},
	KeySetNoRevision:          {"Неизвестна версия настроек — нажмите «Перечитать».", "The settings version is unknown — press Reload."},
	KeySetConflictHint:        {"Нажмите «Перечитать»: несохранённые правки будут потеряны.", "Press Reload: unsaved changes will be lost."},
	KeySetOtherInvalid:        {"Сохранить нельзя: в файле настроек неверно заполнена другая база «%s» (%s). Исправьте или удалите её.", "Cannot save: another database \"%s\" in the settings file is invalid (%s). Fix or remove it."},
	KeySetRateHint:            {"Проверку подключения и опрос служба выполняет не чаще раза в 10 секунд.", "The service runs connection tests and polls at most once every 10 seconds."},
	KeySetTimeoutHint:         {"Проверка ограничена 15 секундами: проверьте сеть, порт и брандмауэр.", "The test is limited to 15 seconds: check the network, port and firewall."},
	KeySetAccessHint:          {"Нажмите «Изменить (администратор)».", "Press Edit (administrator)."},
	KeySetFailed:              {"Ошибка: %s", "Error: %s"},
	KeySetElevateCanceled:     {"Запуск от имени администратора отменён.", "Running as administrator was canceled."},
	KeySetElevateFailed:       {"Не удалось запустить настройки от имени администратора: %s", "Could not open the settings as administrator: %s"},

	KeyUnknownMessage:     {"Ошибка", "Error"},
	KeySetPreset:          {"Тип сервера:", "Server type:"},
	KeySetPreset2008:      {"SQL Server 2008 / 2008 R2", "SQL Server 2008 / 2008 R2"},
	KeySetPresetNone:      {"SQL Server 2012 и новее", "SQL Server 2012 or newer"},
	KeySetWarnMin:         {"Предупреждение, мин:", "Warning after, min:"},
	KeySetErrorMin:        {"Ошибка, мин:", "Error after, min:"},
	KeySetConnTimeout:     {"Тайм-аут подключения, с:", "Connect timeout, s:"},
	KeySetQueryTimeout:    {"Тайм-аут запроса, с:", "Query timeout, s:"},
	KeySetPortCue:         {"1433 или пусто для SQL Browser", "1433 or empty for SQL Browser"},
	KeySetThreshCue:       {"как в общих настройках", "as in the general settings"},
	KeySetTimeoutCue:      {"по умолчанию %d", "default %d"},
	KeyMenuService:        {"Служба", "Service"},
	KeyMenuSvcStart:       {"Запустить", "Start"},
	KeyMenuSvcStop:        {"Остановить", "Stop"},
	KeySvcState:           {"Состояние: %s", "State: %s"},
	KeySvcRunning:         {"работает", "running"},
	KeySvcStopped:         {"остановлена", "stopped"},
	KeySvcStartPending:    {"запускается", "starting"},
	KeySvcStopPending:     {"останавливается", "stopping"},
	KeySvcNotInstalled:    {"не установлена", "not installed"},
	KeySvcNoAccess:        {"нет доступа к состоянию", "no access to the state"},
	KeySvcUnknown:         {"неизвестно", "unknown"},
	KeySvcStartSent:       {"Служба «%s» запускается.", "The service \"%s\" is starting."},
	KeySvcStopSent:        {"Служба «%s» останавливается.", "The service \"%s\" is stopping."},
	KeySvcErrAccess:       {"Недостаточно прав, чтобы запустить или остановить службу «%s». Это может сделать администратор компьютера, например в оснастке «Службы» (services.msc).", "Not enough rights to start or stop the service \"%s\". A computer administrator can do it, for example in the Services console (services.msc)."},
	KeySvcErrNotInstalled: {"Служба «%s» не установлена на этом компьютере. Запустите установщик Exchange Checker.", "The service \"%s\" is not installed on this computer. Run the Exchange Checker setup."},
	KeySvcErrRunning:      {"Служба «%s» уже работает.", "The service \"%s\" is already running."},
	KeySvcErrNotActive:    {"Служба «%s» уже остановлена.", "The service \"%s\" is already stopped."},
	KeySvcErrOther:        {"Не удалось выполнить действие со службой «%s»: %s", "The service \"%s\" action failed: %s"},
	KeyTipSetList:         {"Базы, которые проверяет служба. Выберите базу, чтобы увидеть и изменить её настройки.", "Databases checked by the service. Select one to view and edit its settings."},
	KeyTipSetAdd:          {"Добавить новую базу данных для проверки.", "Add a new database to check."},
	KeyTipSetDelete:       {"Удалить выбранную базу из настроек (с подтверждением).", "Remove the selected database from the settings (asks first)."},
	KeyTipSetDiscover:     {"Найти в базе 1С таблицы с временем обмена и настроить их.", "Find the tables with exchange times in the 1C database and set them up."},
	KeyTipSetName:         {"Название базы в меню трея и в тревогах. Можно оставить пустым — тогда «сервер/база».", "Name of the database in the tray menu and alerts. May be empty: \"server/database\" is shown then."},
	KeyTipSetHost:         {"Имя компьютера или IP-адрес SQL Server, например SRV-SQL или 10.0.0.5. Без имени экземпляра.", "Computer name or IP address of SQL Server, e.g. SRV-SQL or 10.0.0.5. Without the instance name."},
	KeyTipSetInstance:     {"Только имя именованного экземпляра, например SQLEXPRESS. Не сервер и не IP. Пусто — экземпляр по умолчанию.", "Only the name of a named instance, e.g. SQLEXPRESS. Not the server and not an IP. Empty: the default instance."},
	KeyTipSetPort:         {"TCP-порт SQL Server. Пусто — 1433, а для именованного экземпляра порт узнаётся у SQL Browser (UDP 1434).", "TCP port of SQL Server. Empty: 1433; for a named instance the port is asked from SQL Browser (UDP 1434)."},
	KeyTipSetDatabase:     {"Имя базы данных 1С на сервере, как в SQL Server Management Studio.", "Name of the 1C database on the server, as in SQL Server Management Studio."},
	KeyTipSetAuth:         {"SQL — логин и пароль SQL Server. Windows — служба входит учётной записью компьютера (ДОМЕН\\ИМЯ$).", "SQL: a SQL Server login and password. Windows: the service logs in with the computer account (DOMAIN\\NAME$)."},
	KeyTipSetLogin:        {"Логин SQL Server только с ролью db_datareader. Не sa.", "SQL Server login with the db_datareader role only. Not sa."},
	KeyTipSetPassword:     {"Пароль SQL-логина. Хранится службой в зашифрованном виде; пусто — оставить сохранённый.", "Password of the SQL login. The service keeps it encrypted; empty keeps the saved one."},
	KeyTipSetClearPw:      {"Удалить сохранённый пароль этой базы при сохранении.", "Delete the saved password of this database on save."},
	KeyTipSetEncrypt:      {"Шифрование подключения. Для SQL Server 2008 без обновлений обычно «Без шифрования».", "Connection encryption. For SQL Server 2008 without updates usually \"No encryption\"."},
	KeyTipSetPreset:       {"Версия SQL Server. «2008 / 2008 R2» разрешает TLS 1.0 и запрещает режим Strict.", "SQL Server version. \"2008 / 2008 R2\" allows TLS 1.0 and forbids the Strict mode."},
	KeyTipSetTLS10:        {"Разрешить старый протокол TLS 1.0 — нужен SQL Server 2005/2008 без обновлений TLS 1.2.", "Allow the old TLS 1.0 protocol, needed by SQL Server 2005/2008 without the TLS 1.2 updates."},
	KeyTipSetTrust:        {"Не проверять сертификат сервера (самоподписанный). Действует только в режиме «Весь сеанс (TLS)».", "Do not validate the server certificate (self-signed). Used only with \"Whole session (TLS)\"."},
	KeyTipSetEnabled:      {"Снимите флажок, чтобы временно не проверять эту базу.", "Clear to stop checking this database for a while."},
	KeyTipSetWarn:         {"Через сколько минут без обмена показать предупреждение. Значение, равное унаследованному из общих настроек, не фиксируется: профиль следует общим настройкам.", "Minutes without exchange before a warning. A value equal to the one inherited from the general settings is not fixed: the profile follows the general settings."},
	KeyTipSetError:        {"Через сколько минут без обмена показать ошибку. Должно быть больше предупреждения. Значение, равное унаследованному из общих настроек, не фиксируется: профиль следует общим настройкам.", "Minutes without exchange before an error. Must be greater than the warning. A value equal to the one inherited from the general settings is not fixed: the profile follows the general settings."},
	KeyTipSetConnTimeout:  {"Сколько секунд ждать подключения к SQL Server (1–600). Значение, равное унаследованному (по умолчанию), не фиксируется: профиль следует общим настройкам.", "Seconds to wait for the connection to SQL Server (1–600). A value equal to the inherited (default) one is not fixed: the profile follows the general settings."},
	KeyTipSetQueryTimeout: {"Сколько секунд ждать ответа на запрос (1–600). Значение, равное унаследованному (по умолчанию), не фиксируется: профиль следует общим настройкам.", "Seconds to wait for a query result (1–600). A value equal to the inherited (default) one is not fixed: the profile follows the general settings."},
	KeyTipSetPage:         {"Переключиться между базами и общими настройками.", "Switch between the databases and the general settings."},
	KeyTipSetStatus:       {"Результат последнего действия и ошибки.", "Result of the last action and errors."},
	KeyTipSetTest:         {"Проверить подключение и чтение таблиц с текущими значениями формы.", "Test the connection and table reading with the current form values."},
	KeyTipSetCancelTest:   {"Прервать идущую проверку подключения.", "Stop the running connection test."},
	KeyTipSetSave:         {"Сохранить изменения в настройки службы.", "Save the changes to the service settings."},
	KeyTipSetPoll:         {"Попросить службу опросить эту базу сейчас.", "Ask the service to poll this database now."},
	KeyTipSetReload:       {"Перечитать настройки из службы (несохранённые правки пропадут).", "Reload the settings from the service (unsaved edits are lost)."},
	KeyTipSetElevate:      {"Открыть настройки с правами администратора, чтобы их можно было менять.", "Open the settings as administrator to be able to change them."},
	KeyTipSetClose:        {"Закрыть окно настроек.", "Close the settings window."},
	KeyTipGenPoll:         {"Как часто служба опрашивает базы, в секундах.", "How often the service polls the databases, in seconds."},
	KeyTipGenWarn:         {"Общий порог предупреждения в минутах для всех баз без своего порога.", "General warning threshold in minutes for databases without their own."},
	KeyTipGenError:        {"Общий порог ошибки в минутах; больше порога предупреждения.", "General error threshold in minutes; greater than the warning threshold."},
	KeyTipGenBeep:         {"Пищать системным динамиком при тревоге.", "Beep with the system speaker on an alert."},
	KeyTipGenWarnFreq:     {"Частота звука предупреждения, Гц.", "Pitch of the warning sound, Hz."},
	KeyTipGenWarnDur:      {"Длительность звука предупреждения, мс.", "Length of the warning sound, ms."},
	KeyTipGenErrFreq:      {"Частота звука ошибки, Гц.", "Pitch of the error sound, Hz."},
	KeyTipGenErrDur:       {"Длительность звука ошибки, мс.", "Length of the error sound, ms."},
	KeyTipGenGap:          {"Пауза между сигналами, мс.", "Pause between the beeps, ms."},
	KeyTipGenLang:         {"Язык сообщений службы в журнале событий Windows.", "Language of the service messages in the Windows Event Log."},
	KeyTipGenAutostart:    {"Запускать значок в трее при входе любого пользователя.", "Start the tray icon when any user logs on."},
	KeyTipDiscFind:        {"Найти в базе таблицы, похожие на время обмена.", "Find tables that look like exchange times."},
	KeyTipDiscPoints:      {"Загрузить список точек обмена (узлов планов обмена).", "Load the list of exchange points (exchange plan nodes)."},
	KeyTipDiscCancel:      {"Прервать идущий поиск или проверку.", "Stop the running search or check."},
	KeyTipDiscPoint:       {"Имя точки обмена, время которой вы знаете.", "Name of an exchange point whose time you know."},
	KeyTipDiscTime:        {"Известное время последнего обмена этой точки: ДД.ММ.ГГГГ ЧЧ:ММ (UTC+3).", "Known time of the point's last exchange: DD.MM.YYYY HH:MM (UTC+3)."},
	KeyTipDiscTol:         {"Допуск поиска по времени, минут. 0 или пусто — 1 сутки.", "Time tolerance of the search, minutes. 0 or empty: 1 day."},
	KeyTipDiscByValue:     {"Найти колонки, где у точки записано это время.", "Find the columns where the point has this time."},
	KeyTipDiscList:        {"Найденные варианты. Выберите вариант, чтобы перенести его в поля ниже.", "Found candidates. Select one to copy it to the fields below."},
	KeyTipDiscDetails:     {"Подробности выбранного варианта.", "Details of the selected candidate."},
	KeyTipDiscMode:        {"Как служба читает время: из таблиц, ручным SQL или по счётчикам сообщений.", "How the service reads the times: from tables, with manual SQL or by message counters."},
	KeyTipDiscField:       {"Имя таблицы или колонки SQL Server, обычно вида _Node123 или _Fld456.", "SQL Server table or column name, usually like _Node123 or _Fld456."},
	KeyTipDiscPlanLabel:   {"Своё название плана обмена для меню (необязательно).", "Own name of the exchange plan for the menu (optional)."},
	KeyTipDiscYear:        {"1С хранит даты со сдвигом +2000 лет — обычно флажок включён.", "1C stores dates shifted by +2000 years: usually checked."},
	KeyTipDiscKeep:        {"Показывать точки, для которых ещё нет записи времени.", "Show points that have no time record yet."},
	KeyTipDiscAllPlans:    {"Добавить в профиль все найденные планы обмена, а не только выбранный.", "Add every found exchange plan to the profile, not only the selected one."},
	KeyTipDiscTemplate:    {"Готовый шаблон ручного SQL для SQL Server 2008.", "Ready manual SQL template for SQL Server 2008."},
	KeyTipDiscSQL:         {"Запрос должен вернуть имя точки, время отправки и время приёма.", "The query must return the point name, send time and receive time."},
	KeyTipDiscResult:      {"Результат проверки и ошибки.", "Result of the check and errors."},
	KeyTipDiscCheck:       {"Проверить настройку на сервере и показать примеры.", "Check the setup on the server and show examples."},
	KeyTipDiscApply:       {"Перенести настройку в профиль базы (затем «Сохранить» в окне настроек).", "Copy the setup to the database profile (then \"Save\" in the settings window)."},
	KeyTipDiscClose:       {"Закрыть окно без применения.", "Close the window without applying."},
	KeyTipAlertAck:        {"Подтвердить тревогу: звук смолкнет до следующего изменения.", "Acknowledge the alert: silent until the next change."},
	KeyTipAlertClose:      {"Закрыть окно и заглушить звук до следующего изменения статуса.", "Close the window and mute until the next status change."},
	KeyTipAboutOK:         {"Закрыть окно «О программе».", "Close the About window."},
}
