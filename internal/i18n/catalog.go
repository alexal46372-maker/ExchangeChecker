package i18n

// Catalog keys of the tray UI (phase 6a); Russian only (CHK-TRAY-86).
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
	// KeyDBMuted: the database is muted until recovery by the service
	// (mute_until_recovered; shown in the left menu, CHK-TRAY-91).
	KeyDBMuted = "db.muted"
	KeyDBNone     = "db.none"
	KeyDBNoPoints = "db.no_points"
	// KeyDBToggleFailed: balloon after a failed set_enabled from the
	// database check mark (CHK-TRAY-91).
	KeyDBToggleFailed = "db.toggle_failed"
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

	KeyPanelStalled        = "panel.stalled"
	KeyPtStalled           = "pt.stalled"
	KeyAlertTestBadge      = "alert.test_badge"
	KeyMenuSummary         = "menu.summary"
	KeySumTitle            = "sum.title"
	KeySumHours            = "sum.hours"
	KeySumTruncated        = "sum.truncated"
	KeySumBusy             = "sum.busy"
	KeySumEmpty            = "sum.empty"
	KeySumClose            = "sum.close"
	KeyAlarmTest           = "alarm.test"
	KeyAlarmTestTip        = "alarm.test_tip"
	KeyAlarmTestTipDenied  = "alarm.test_tip_denied"
	KeyAlarmTestTitle      = "alarm.test_title"
	KeyAlarmTestWarning    = "alarm.test_warning"
	KeyAlarmTestError      = "alarm.test_error"
	KeyAlarmTestViber      = "alarm.test_viber"
	KeyAlarmTestRun        = "alarm.test_run"
	KeyAlarmTestClose      = "alarm.test_close"
	KeyAlarmTestNoViber    = "alarm.test_no_viber"
	KeyAlarmTestRetry      = "alarm.test_retry"
	KeyAlarmTestAccess     = "alarm.test_access"
	KeySetStalledMin       = "set.stalled_min"
	KeyTipSetStalled       = "tip.set_stalled"
	KeyGenStalled          = "gen.stalled"
	KeyCueStalled          = "cue.stalled"

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
	KeySetColPoll           = "set.col_poll" // CHK-TRAY-80 DB list column
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
	KeySetClone             = "set.clone"
	KeySetCloned            = "set.cloned"
	KeySetClonedNeedPw      = "set.cloned_need_pw"
	KeySetCloneConfirm      = "set.clone_confirm"
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
	// phase 11 (CHK-TRAY-47): left navigation of the settings window
	KeySetNavProfiles     = "set.nav_profiles"
	KeyDiscCandidates     = "disc.candidates"
	KeyDiscDetails        = "disc.details"
	KeyDiscResultLabel    = "disc.result_label"
	KeySetNavGeneral      = "set.nav_general"
	KeySetNavViber        = "set.nav_viber"

	KeySetNavEmail        = "set.nav_email"
	KeyEmGroup            = "set.em_group"
	KeyEmEnabled          = "set.em_enabled"
	KeyEmHost             = "set.em_host"
	KeyEmPort             = "set.em_port"
	KeyEmTLSMode          = "set.em_tls_mode"
	KeyEmTLSNone          = "set.em_tls_none"
	KeyEmTLSStartTLS      = "set.em_tls_starttls"
	KeyEmTLSImplicit      = "set.em_tls_tls"
	KeyEmFromAddr         = "set.em_from_addr"
	KeyEmPassword         = "set.em_password"
	KeyEmClearPassword    = "set.em_clear_password"
	KeyEmPasswordSaved    = "set.em_password_saved"
	KeyEmPasswordNone     = "set.em_password_none"
	KeyEmPasswordNew      = "set.em_password_new"
	KeyEmPasswordClear    = "set.em_password_clear"
	KeyEmPasswordNoneTLS  = "set.em_password_none_tls"
	KeyEmRecipients       = "set.em_recipients"
	KeyEmBadAddr          = "set.em_bad_addr"
	KeyEmDays             = "set.em_days"
	KeyEmFrom             = "set.em_from"
	KeyEmTo               = "set.em_to"
	KeyEmEvents           = "set.em_events"
	KeyEmTestTo           = "set.em_test_to"
	KeyEmTestAll          = "set.em_test_all"
	KeyEmTest             = "set.em_test"
	KeyEmTesting          = "set.em_testing"
	KeyEmTestOK           = "set.em_test_ok"
	KeyEmTestFailed       = "set.em_test_failed"
	KeyEmTestItemOK       = "set.em_test_item_ok"
	KeyEmTestNoResult     = "set.em_test_no_result"
	KeyEmStatus           = "set.em_status"
	KeyEmLastOK           = "set.em_last_ok"
	KeyEmLastError        = "set.em_last_error"
	KeyEmSaved            = "set.em_saved"
	KeyTipEmPage          = "tip.em_page"
	KeyTipEmEnabled       = "tip.em_enabled"
	KeyTipEmHost          = "tip.em_host"
	KeyTipEmPort          = "tip.em_port"
	KeyTipEmTLSMode       = "tip.em_tls_mode"
	KeyTipEmFromAddr      = "tip.em_from_addr"
	KeyTipEmPassword      = "tip.em_password"
	KeyTipEmClearPassword = "tip.em_clear_password"
	KeyTipEmRecipients    = "tip.em_recipients"
	KeyTipEmDay           = "tip.em_day"
	KeyTipEmFrom          = "tip.em_from"
	KeyTipEmTo            = "tip.em_to"
	KeyTipEmEvent         = "tip.em_event"
	KeyTipEmTestTo        = "tip.em_test_to"
	KeyTipEmTest          = "tip.em_test"
	KeyCueEmHost          = "cue.em_host"
	KeyCueEmPort          = "cue.em_port"
	KeyCueEmFrom          = "cue.em_from"
	KeyCueEmPassword      = "cue.em_password"
	KeyXferEmailCheck     = "set.xfer_email_check"
	KeyXferEmailWarnExport = "set.xfer_email_warn_export"
	KeyXferEmailWarnImport = "set.xfer_email_warn_import"
	KeyTipXferEmail       = "tip.xfer_email"
	KeyTipSetNavProfiles  = "tip.set.nav_profiles"
	KeyGenPoll            = "gen.poll"
	KeyGenWarn            = "gen.warn"
	KeyGenError           = "gen.error"
	KeyGenBeepEnabled     = "gen.beep_enabled"
	KeyGenWarnFreq        = "gen.warn_freq"
	KeyGenWarnDur         = "gen.warn_dur"
	KeyGenErrorFreq       = "gen.error_freq"
	KeyGenErrorDur        = "gen.error_dur"
	KeyGenGap             = "gen.gap"
	KeyGenAutostart       = "gen.autostart"
	KeyGenNote            = "gen.note"
	KeyGenSaved           = "gen.saved"
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
	KeySetChangedOffline  = "set.changed_offline"
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
	KeyCueName            = "cue.name"
	KeyCueHost            = "cue.host"
	KeyCueInstance        = "cue.instance"
	KeyCueDatabase        = "cue.database"
	KeyCueLogin           = "cue.login"
	KeyCuePassword        = "cue.password"
	KeyCueWizServer       = "cue.wiz_server"
	KeyCueDiscTime        = "cue.disc_time"
	KeyCueDiscSchema      = "cue.disc_schema"
	KeyCueDiscColumn      = "cue.disc_column"
	KeyCueVbToken         = "cue.vb_token"
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
	KeyTipSetClone         = "tip.set.clone"
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

// Phase 11: database cards of the «Состояние» window (CHK-TRAY-48).
const (
	KeyCardFresh      = "card.fresh"
	KeyCardNoData     = "card.no_data"
	KeyCardPoints     = "card.points"
	KeyCardTip        = "card.tip"
	KeyCardMore       = "card.more"
	KeyCardPollingOff = "card.polling_off"
	KeyCardMoreHint   = "card.more_hint"
	KeyTipCards       = "tip.cards"
)

// Phase 9b (CHK-TRAY-22..29).
const (
	KeyCheckRateLimited   = "check.rate_limited"
	KeyCheckRunning       = "check.running"
	KeyCheckCoalesced     = "check.coalesced"
	KeyCheckDone          = "check.done"
	KeyCheckTimeout       = "check.timeout"
	KeyStHistory          = "st.history"
	KeyTipStHistory       = "tip.st_history"
	KeyStHistoryNoPoint   = "st.history_no_point"
	KeyStHistoryOff       = "st.history_off"
	KeyHistTitle          = "hist.title"
	KeyHistDay            = "hist.day"
	KeyHistWeek           = "hist.week"
	KeyTipHistDay         = "tip.hist_day"
	KeyTipHistWeek        = "tip.hist_week"
	KeyTipHistChart       = "tip.hist_chart"
	KeyTipHistInfo        = "tip.hist_info"
	KeyTipHistExportCSV   = "tip.hist_export_csv"
	KeyTipHistExportHTML  = "tip.hist_export_html"
	KeyTipHistClose       = "tip.hist_close"
	KeyHistLoading        = "hist.loading"
	KeyHistLoaded         = "hist.loaded"
	KeyHistEmpty          = "hist.empty"
	KeyHistExporting      = "hist.exporting"
	KeyHistPartial        = "hist.partial"
	KeyHistFailed         = "hist.failed"
	KeyHistAxisMin        = "hist.axis_min"
	KeyHistColTime        = "hist.col_time"
	KeyHistColKind        = "hist.col_kind"
	KeyHistColLag         = "hist.col_lag"
	KeyHistColLatency     = "hist.col_latency"
	KeyHistColChange      = "hist.col_change"
	KeyHistColDuration    = "hist.col_duration"
	KeyHistKindPoint      = "hist.kind_point"
	KeyHistKindProfile    = "hist.kind_profile"
	KeyHistKindEvent      = "hist.kind_event"
	KeyHistKindRecovered  = "hist.kind_recovered"
	KeyValReasonNp        = "val.reason_np"
	KeyValReasonLpc       = "val.reason_lpc"
	KeyValReasonAdmin     = "val.reason_admin"
	KeyValReasonUnc       = "val.reason_unc"
	KeyValReasonBackslash = "val.reason_backslash"
	KeyValReasonTcp       = "val.reason_tcp"
	KeyValReasonSm        = "val.reason_sm"
	KeyValReasonVia       = "val.reason_via"
	KeyValReasonSlash     = "val.reason_slash"
	KeyValReasonPipe      = "val.reason_pipe"
)

// phase 9b part 2: «Добавить базу» wizard (CHK-TRAY-30..32)
const (
	KeyWizTitle            = "wiz.title"
	KeyWizStep             = "wiz.step"
	KeyWizStepServer       = "wiz.step_server"
	KeyWizStepDB           = "wiz.step_db"
	KeyWizStepFinish       = "wiz.step_finish"
	KeyWizServer           = "wiz.server"
	KeyWizFind             = "wiz.find"
	KeyWizBrowsing         = "wiz.browsing"
	KeyWizFound            = "wiz.found"
	KeyWizFoundPartial     = "wiz.found_partial"
	KeyWizNoneFound        = "wiz.none_found"
	KeyWizRateLimited      = "wiz.rate_limited"
	KeyWizBusy             = "wiz.busy"
	KeyWizPort             = "wiz.port"
	KeyWizAuth             = "wiz.auth"
	KeyWizAuthSQL          = "wiz.auth_sql"
	KeyWizAuthWindows      = "wiz.auth_windows"
	KeyWizLogin            = "wiz.login"
	KeyWizPassword         = "wiz.password"
	KeyWizEncrypt          = "wiz.encrypt"
	KeyWizDBs              = "wiz.dbs"
	KeyWizRefresh          = "wiz.refresh"
	KeyWizDBLoading        = "wiz.db_loading"
	KeyWizDBCount          = "wiz.db_count"
	KeyWizDBTruncated      = "wiz.db_truncated"
	KeyWizDB1C             = "wiz.db_1c"
	KeyWizDBNode           = "wiz.db_node"
	KeyWizDBSize           = "wiz.db_size"
	KeyWizDBNone           = "wiz.db_none"
	KeyWizTesting          = "wiz.testing"
	KeyWizTestOK           = "wiz.test_ok"
	KeyWizDiscovering      = "wiz.discovering"
	KeyWizApplied          = "wiz.applied"
	KeyWizChoose           = "wiz.choose"
	KeyWizChosen           = "wiz.chosen"
	// fix/tray-wizard-autofind (CHK-TRAY-83..85)
	KeyWizFoundPlan        = "wiz.found_plan"
	KeyWizFoundPlans       = "wiz.found_plans"
	KeyWizFoundFields      = "wiz.found_fields"
	KeyWizDetails          = "wiz.details"
	KeyWizErrNone          = "wiz.err_none"
	KeyWizErrAmbiguous     = "wiz.err_ambiguous"
	KeyWizErrWinUnsaved    = "wiz.err_windows_unsaved"
	KeyWizErrTruncated     = "wiz.err_truncated"
	KeyWizNoteTruncated    = "wiz.note_truncated"
	KeyTipWizDetails       = "tip.wiz_details"
	KeyWizErrLowScore      = "wiz.err_low_score"
	KeyWizErrIncomplete    = "wiz.err_incomplete"
	KeyWizConfidence       = "wiz.confidence"
	KeyWizCands            = "wiz.cands"
	KeyWizSaving           = "wiz.saving"
	KeyWizBack             = "wiz.back"
	KeyWizNext             = "wiz.next"
	KeyWizSave             = "wiz.save"
	KeyWizAdvanced         = "wiz.advanced"
	KeyWizGrant            = "wiz.grant"
	KeyWizCancel           = "wiz.cancel"
	KeyWizGrantCopied      = "wiz.grant_copied"
	KeyWizGrantPlaceholder = "wiz.grant_placeholder"
	KeyWizClipFailed       = "wiz.clip_failed"
	KeyWizAccessDenied     = "wiz.access_denied"
	KeyWizSubnet           = "wiz.subnet"
	KeyWizCancelBrowse     = "wiz.cancel_browse"
	KeyWizBrowseProgress   = "wiz.browse_progress"
	KeyWizBusyRetry        = "wiz.busy_retry"
	KeyWizPublicNetwork    = "wiz.public_network"
	KeyWizCheckSteps       = "wiz.check_steps"
	KeyWizCheckParse       = "wiz.check_parse"
	KeyWizCheckDNS         = "wiz.check_dns"
	KeyWizCheckBrowser     = "wiz.check_browser"
	KeyWizCheckTCP         = "wiz.check_tcp"
	KeyWizCheckPrelogin    = "wiz.check_prelogin"
	KeyWizCheckTLS         = "wiz.check_tls"
	KeyWizCheckLogin       = "wiz.check_login"
	KeyWizCheckDB          = "wiz.check_db"
	KeyWizCheckTables      = "wiz.check_tables"
	KeyWizChecking         = "wiz.checking"
	KeyWizErrCode          = "wiz.err_code"
	KeyWizWhatToDo         = "wiz.what_to_do"
	KeyWizFBLogin          = "wiz.fb_login"
	KeyWizFBDatabase       = "wiz.fb_database"
	KeyWizFBTLS            = "wiz.fb_tls"
	KeyWizFBTimeout        = "wiz.fb_timeout"
	KeyWizFBNetwork        = "wiz.fb_network"
	KeyWizFBBrowser        = "wiz.fb_browser"
	KeyWizFBInstance       = "wiz.fb_instance"
	KeyWizFBPermission     = "wiz.fb_permission"
	KeyWizFBNot1C          = "wiz.fb_not1c"
	KeyWizFBConfig         = "wiz.fb_config"
	KeyWizFBOther          = "wiz.fb_other"
	KeyTipWizServer        = "tip.wiz_server"
	KeyTipWizFind          = "tip.wiz_find"
	KeyTipWizSubnet        = "tip.wiz_subnet"
	KeyTipWizCancelBrowse  = "tip.wiz_cancel_browse"
	KeyTipWizInstances     = "tip.wiz_instances"
	KeyTipWizPort          = "tip.wiz_port"
	KeyTipWizAuth          = "tip.wiz_auth"
	KeyTipWizLogin         = "tip.wiz_login"
	KeyTipWizPassword      = "tip.wiz_password"
	KeyTipWizEncrypt       = "tip.wiz_encrypt"
	KeyTipWizDBs           = "tip.wiz_dbs"
	KeyTipWizRefresh       = "tip.wiz_refresh"
	KeyTipWizCands         = "tip.wiz_cands"
	KeyTipWizResult        = "tip.wiz_result"
	KeyTipWizBack          = "tip.wiz_back"
	KeyTipWizNext          = "tip.wiz_next"
	KeyTipWizSave          = "tip.wiz_save"
	KeyTipWizAdvanced      = "tip.wiz_advanced"
	KeyTipWizGrant         = "tip.wiz_grant"
	KeyTipWizCancel        = "tip.wiz_cancel"
)

// phase 10 A–C (CHK-TRAY-33..39)
const (
	KeyPanelNoService   = "panel.no_service"
	KeyPanelNoSQL       = "panel.no_sql"
	KeyPanelTitle       = "panel.title"
	KeyAlertMute        = "alert.mute"
	KeyAlertUnmute      = "alert.unmute"
	KeyTipAlertMute     = "tip.alert_mute"
	KeyMenuSound        = "menu.sound"
	KeyMenuVolume       = "menu.volume"
	KeyMenuPinHelp      = "menu.pin_help"
	KeyPinHelpTitle     = "pin.title"
	KeyPinHelpText      = "pin.text"
	KeySoundTitle       = "sound.title"
	KeySoundWarning     = "sound.warning"
	KeySoundError       = "sound.error"
	KeySoundBeep        = "sound.beep"
	KeySoundFile        = "sound.file"
	KeySoundFreq        = "sound.freq"
	KeySoundDur         = "sound.dur"
	KeySoundBrowse      = "sound.browse"
	KeySoundVolume      = "sound.volume"
	KeySoundLoop        = "sound.loop"
	KeySoundGap         = "sound.gap"
	KeySoundTest        = "sound.test"
	KeySoundStop        = "sound.stop"
	KeySoundSave        = "sound.save"
	KeySoundCancel      = "sound.cancel"
	KeySoundFilterAudio = "sound.filter_audio"
	KeySoundFilterAll   = "sound.filter_all"
	KeySoundBadFile     = "sound.bad_file"
	KeySoundBadValue    = "sound.bad_value"
	KeySoundSaved       = "sound.saved"
	KeySoundFallback    = "sound.fallback"
	// phase 14 (feat/tray-speaker, CHK-TRAY-98/99): «Тон» through the
	// PC speaker of the service
	KeySoundToneVolume    = "sound.tone_volume"
	KeySpeakerAvailable   = "speaker.available"
	KeySpeakerUnavailable = "speaker.unavailable"
	KeySpeakerNoService   = "speaker.no_service"
	KeySpeakerNoCap       = "speaker.no_cap"
	KeySpeakerNoReason    = "speaker.no_reason"
	KeySpeakerTimeout     = "speaker.timeout"
	KeySpeakerFallback    = "speaker.fallback"
	KeyTipSoundKind     = "tip.sound_kind"
	KeyTipSoundFreq     = "tip.sound_freq"
	KeyTipSoundDur      = "tip.sound_dur"
	KeyTipSoundFile     = "tip.sound_file"
	KeyTipSoundBrowse   = "tip.sound_browse"
	KeyTipSoundVolume   = "tip.sound_volume"
	KeyTipSoundLoop     = "tip.sound_loop"
	KeyTipSoundGap      = "tip.sound_gap"
	KeyTipSoundTest     = "tip.sound_test"
	KeyTipSoundSave     = "tip.sound_save"
	KeyVolumeTitle      = "volume.title"
	KeyTipVolume        = "tip.volume"
)

// phase 10 D
const (
	KeySetModeExpert = "set.mode_expert"
	KeySetModeSimple = "set.mode_simple"
	KeyTipSetUIMode  = "tip.set_ui_mode"
	// phase 10 D3: group boxes of the settings window
	KeySetGroupConn    = "set.group_conn"
	KeySetGroupLogin   = "set.group_login"
	KeySetGroupTLS     = "set.group_tls"
	KeySetGroupChecks  = "set.group_checks"
	KeySetGroupGeneral = "set.group_general"
	KeySetGroupSound   = "set.group_sound"
	// phase 10 D4: «Viber» page
	KeyVbPage             = "set.vb_page"
	KeyVbGroup            = "set.vb_group"
	KeyVbEnabled          = "set.vb_enabled"
	KeyVbToken            = "set.vb_token"
	KeyVbClearToken       = "set.vb_clear_token"
	KeyVbTokenSaved       = "set.vb_token_saved"
	KeyVbTokenNone        = "set.vb_token_none"
	KeyVbTokenNew         = "set.vb_token_new"
	KeyVbTokenClear       = "set.vb_token_clear"
	KeyVbRecipients       = "set.vb_recipients"
	KeyVbDays             = "set.vb_days"
	KeyVbDay1             = "set.vb_day1"
	KeyVbDay2             = "set.vb_day2"
	KeyVbDay3             = "set.vb_day3"
	KeyVbDay4             = "set.vb_day4"
	KeyVbDay5             = "set.vb_day5"
	KeyVbDay6             = "set.vb_day6"
	KeyVbDay7             = "set.vb_day7"
	KeyVbFrom             = "set.vb_from"
	KeyVbTo               = "set.vb_to"
	KeyVbEvents           = "set.vb_events"
	KeyVbEvError          = "set.vb_ev_error"
	KeyVbEvWarning        = "set.vb_ev_warning"
	KeyVbEvRecovered      = "set.vb_ev_recovered"
	KeyVbEvNoConn         = "set.vb_ev_no_connection"
	KeyVbReminder         = "set.vb_reminder"
	KeyVbReminderHint     = "set.vb_reminder_hint"
	KeyVbTestTo           = "set.vb_test_to"
	KeyVbTestAll          = "set.vb_test_all"
	KeyVbTest             = "set.vb_test"
	KeyVbTesting          = "set.vb_testing"
	KeyVbTestOK           = "set.vb_test_ok"
	KeyVbTestFailed       = "set.vb_test_failed"
	KeyVbTestItemOK       = "set.vb_test_item_ok"
	KeyVbTestNoResult     = "set.vb_test_no_result"
	KeyVbStatus           = "set.vb_status"
	KeyVbLastOK           = "set.vb_last_ok"
	KeyVbLastError        = "set.vb_last_error"
	KeyVbSaved            = "set.vb_saved"
	// Telegram page (phase 13, CHK-TRAY-94).
	KeyTgGroup           = "set.tg_group"
	// «Мигание экрана» on «Общие» (phase 13, CHK-TRAY-95).
	KeyGenScreenFlash       = "gen.screen_flash"
	KeyGenScreenFlashFailed = "gen.screen_flash_failed"
	KeyTipGenScreenFlash    = "tip.gen_screen_flash"
	KeyTgEnabled         = "set.tg_enabled"
	KeyTgToken           = "set.tg_token"
	KeyTgClearToken      = "set.tg_clear_token"
	KeyTgChat            = "set.tg_chat"
	KeyTgChatHint        = "set.tg_chat_hint"
	KeyTgTest            = "set.tg_test"
	KeyTgTesting         = "set.tg_testing"
	KeyTgTestOK          = "set.tg_test_ok"
	KeyTgTestFailed      = "set.tg_test_failed"
	KeyTgTestRateLimited = "set.tg_test_rate_limited"
	KeyTgSaved           = "set.tg_saved"
	KeySetNavTelegram    = "set.nav_telegram"
	KeyTipTgPage         = "tip.tg_page"
	KeyTipTgEnabled      = "tip.tg_enabled"
	KeyTipTgToken        = "tip.tg_token"
	KeyTipTgClearToken   = "tip.tg_clear_token"
	KeyTipTgChat         = "tip.tg_chat"
	KeyTipTgTest         = "tip.tg_test"
	KeyTipVbPage          = "tip.vb_page"
	KeyTipVbEnabled       = "tip.vb_enabled"
	KeyTipVbToken         = "tip.vb_token"
	KeyTipVbClearToken    = "tip.vb_clear_token"
	KeyTipVbRecipients    = "tip.vb_recipients"
	KeyTipVbDay           = "tip.vb_day"
	KeyTipVbFrom          = "tip.vb_from"
	KeyTipVbTo            = "tip.vb_to"
	KeyTipVbEvent         = "tip.vb_event"
	KeyTipVbReminder      = "tip.vb_reminder"
	KeyTipVbTestTo        = "tip.vb_test_to"
	KeyTipVbTest          = "tip.vb_test"
	KeyXferExport         = "set.xfer_export"
	KeyXferImport         = "set.xfer_import"
	KeyTipXferExport      = "tip.xfer_export"
	KeyTipXferImport      = "tip.xfer_import"
	KeyXferFilter         = "set.xfer_filter"
	KeyXferContainsLogins = "set.xfer_contains_logins"
	KeyXferConflict       = "set.xfer_conflict"
	KeyXferExported       = "set.xfer_exported"
	KeyXferWriteFailed    = "set.xfer_write_failed"
	KeyXferReadFailed     = "set.xfer_read_failed"
	KeyXferTooLarge       = "set.xfer_too_large"
	KeyXferBadFile        = "set.xfer_bad_file"
	KeyXferTitle          = "set.xfer_title"
	KeyXferMerge          = "set.xfer_merge"
	KeyXferReplace        = "set.xfer_replace"
	KeyXferTrayCheck      = "set.xfer_tray_check"
	KeyXferDo             = "set.xfer_do"
	KeyXferChecking       = "set.xfer_checking"
	KeyXferImporting      = "set.xfer_importing"
	KeyXferAdded          = "set.xfer_added"
	KeyXferUpdated        = "set.xfer_updated"
	KeyXferRemoved        = "set.xfer_removed"
	KeyXferNeedPw         = "set.xfer_need_pw"
	KeyXferNoChanges      = "set.xfer_no_changes"
	KeyXferNoTray         = "set.xfer_no_tray"
	KeyXferTrayKept       = "set.xfer_tray_kept"
	KeyXferTraySummary    = "set.xfer_tray_summary"
	KeyXferOn             = "set.xfer_on"
	KeyXferOff            = "set.xfer_off"
	KeyXferNoSoundFile    = "set.xfer_no_sound_file"
	KeyXferDone           = "set.xfer_done"
	KeyXferTrayApplied    = "set.xfer_tray_applied"
	KeyXferOtherUser      = "set.xfer_other_user"
	KeyXferEnterPw        = "set.xfer_enter_pw"
	KeyXferTrayFailed     = "set.xfer_tray_failed"
	KeyXferViberCheck     = "set.xfer_viber_check"
	KeyXferViberWarnExport = "set.xfer_viber_warn_export"
	KeyXferViberWarnImport = "set.xfer_viber_warn_import"
	KeyXferExportTitle    = "set.xfer_export_title"
	KeyTipXferMerge       = "tip.xfer_merge"
	KeyTipXferReplace     = "tip.xfer_replace"
	KeyTipXferTray        = "tip.xfer_tray"
	KeyTipXferViber       = "tip.xfer_viber"
	KeyTipXferDo          = "tip.xfer_do"
	KeyTipXferPreview     = "tip.xfer_preview"
)

var catalog = map[string]string{ // key -> text (RU)
	KeyCheckRateLimited:      "Проверка уже запрашивалась недавно, повторите через %d с",
	KeyCheckRunning:          "Проверка выполняется…",
	KeyCheckCoalesced:        "Опрос уже идёт, ждём его результат…",
	KeyCheckDone:             "Проверка выполнена",
	KeyCheckTimeout:          "Результат проверки не пришёл, данные обновятся при следующем опросе",
	KeyStHistory:             "История…",
	KeyTipStHistory:          "График отставания и события выбранной точки за сутки или неделю",
	KeyStHistoryNoPoint:      "Выберите в таблице строку точки обмена",
	KeyStHistoryOff:          "История недоступна: служба её не поддерживает",
	KeyHistTitle:             "История: %s",
	KeyHistDay:               "Сутки",
	KeyHistWeek:              "Неделя",
	KeyTipHistDay:            "Последние 24 часа, точки по 5 минут",
	KeyTipHistWeek:           "Последние 7 дней, точки по часу",
	KeyTipHistChart:          "Отставание обмена, мин: толстая линия — среднее, тонкие — мин. и макс.; жёлтая и красная — пороги; треугольник — просрочка, кружок — восстановление",
	KeyTipHistInfo:           "Состояние загрузки и выгрузки истории",
	KeyTipHistExportCSV:      "Сохранить все записи истории точки за период в CSV (UTF-8, «;»)",
	KeyTipHistExportHTML:     "Сохранить все записи истории точки за период как страницу HTML",
	KeyTipHistClose:          "Закрыть окно и прервать загрузку",
	KeyHistLoading:           "Загрузка… страниц: %d",
	KeyHistLoaded:            "Интервалов: %d, событий: %d",
	KeyHistEmpty:             "Нет данных за период",
	KeyHistExporting:         "Выгрузка… записей: %d",
	KeyHistPartial:           "Показаны не все записи: достигнут предел выгрузки",
	KeyHistFailed:            "Не удалось получить историю",
	KeyHistAxisMin:           "мин",
	KeyHistColTime:           "Время",
	KeyHistColKind:           "Запись",
	KeyHistColLag:            "Отставание, мин",
	KeyHistColLatency:        "Опрос, мс",
	KeyHistColChange:         "Переход",
	KeyHistColDuration:       "Длительность, мин",
	KeyHistKindPoint:         "опрос точки",
	KeyHistKindProfile:       "опрос базы",
	KeyHistKindEvent:         "тревога",
	KeyHistKindRecovered:     "восстановление",
	KeyValReasonNp:           "Причина: «np:» — именованный канал.",
	KeyValReasonLpc:          "Причина: «lpc:» — локальное подключение LPC.",
	KeyValReasonAdmin:        "Причина: «admin:» — выделенное подключение администратора (DAC).",
	KeyValReasonUnc:          "Причина: адрес вида «\\\\компьютер» — UNC-путь.",
	KeyValReasonBackslash:    "Причина: адрес начинается с обратной косой черты.",
	KeyValReasonTcp:          "Причина: префикс протокола «tcp:» не нужен — укажите «хост» или «хост\\экземпляр», порт — в поле «Порт».",
	KeyValReasonSm:           "Причина: «sm:» — общая память (только локальный сервер).",
	KeyValReasonVia:          "Причина: «via:» — устаревший протокол VIA.",
	KeyValReasonSlash:        "Причина: адрес начинается с косой черты «/».",
	KeyValReasonPipe:         "Причина: адрес содержит путь канала «\\pipe\\».",
	KeyBalloonOverdueTitle:   "Просрочка обмена",
	KeyBalloonRecoveredTitle: "Обмен восстановлен",
	KeyBalloonOverdue:        "%s: нет обмена %d мин (%s)",
	KeyBalloonOverdueNoLag:   "%s: %s",
	KeyBalloonRecovered:      "%s: обмен снова в норме",
	KeyBalloonSummary:        "Просрочено точек: %d, восстановлено: %d",
	KeyMenuBalloons:          "Уведомления о просрочке",
	KeyMenuStatusWin:         "Состояние…",
	KeyMenuCheckNow:          "Проверить сейчас",
	KeyStTitle:               "Состояние",
	KeyCardFresh:             "Свежесть: %s",
	KeyCardNoData:            "нет данных",
	KeyCardPoints:            "Точек: %d, с проблемами: %d",
	KeyCardTip:               "Цвет карточки — худшее состояние точек базы, свежесть — по самой давней точке. Подробности — в таблице ниже.",
	KeyCardPollingOff:        "Опрос выключен",
	KeyCardMore:              "Ещё баз: %d",
	KeyCardMoreHint:          "Все базы — в таблице ниже",
	KeyTipCards:              "Карточки баз: цвет — состояние, свежесть и число точек с проблемами. Наведите на карточку, чтобы увидеть точки.",
	KeyStDatabase:            "База",
	KeyStPoint:               "Точка",
	KeyStPlan:                "План обмена",
	KeyStFresh:               "Последний обмен",
	KeyStAgo:                 "Давность",
	KeyStState:               "Состояние",
	KeyStSend:                "Отправка",
	KeyStRecv:                "Приём",
	KeyStNote:                "Примечание",
	KeyStDisabled:            "отключена",
	KeyStNotPolled:           "ещё не опрошена",
	KeyStExportedAt:          "Выгружено",
	KeyStExportCSV:           "Экспорт CSV…",
	KeyStExportHTML:          "Экспорт HTML…",
	KeyStClose:               "Закрыть",
	KeyStSaved:               "Сохранено: %s",
	KeyStSaveFailed:          "Не удалось сохранить: %s",
	KeyStCheckQueued:         "Проверка запрошена",
	KeyStCheckFailed:         "Проверка не запрошена: %s",
	KeyStNoData:              "Нет данных от службы",
	KeyStFilterCSV:           "CSV (;)",
	KeyStFilterHTML:          "Страница HTML",
	KeyTipStList:             "Все базы и точки обмена: цвет — состояние, давность — сколько минут назад был последний обмен",
	KeyTipCheckNow:           "Опросить все базы сейчас, не дожидаясь интервала (не чаще раза в 10 с)",
	KeyTipExportCSV:          "Сохранить таблицу в CSV для Excel (UTF-8, разделитель «;»)",
	KeyTipExportHTML:         "Сохранить таблицу как страницу HTML с цветами",
	KeyTipStInfo:             "Результат последнего действия: проверка, экспорт",
	KeyTipClose:              "Закрыть окно",
	KeyAgoNow:                "только что",
	KeyAgoMin:                "%d мин назад",
	KeyAgoHours:              "%d ч %d мин назад",
	KeyAgoDays:               "%d дн. назад",
	KeyAppName:               "Exchange Checker",

	KeySvcConnecting:    "Подключение к службе…",
	KeySvcNotRunning:    "Служба Exchange Checker не запущена",
	KeySvcUnavailable:   "Нет связи со службой Exchange Checker",
	KeySvcNotResponding: "Служба не отвечает",
	KeySvcUntrusted:     "Канал службы открыт посторонним процессом — команды не отправляются",
	KeySvcPollerHung:    "Служба работает, но опрос баз завис",
	KeySvcOK:            "Служба работает",
	KeySvcOldProtocol:   "Старая версия службы (протокол %d): часть функций недоступна",

	KeyLevelOK:      "норма",
	KeyLevelWarning: "предупреждение",
	KeyLevelError:   "ошибка",

	KeyDBDisabled: "выключена",
	KeyDBPending:  "ожидание опроса",
	KeyDBStale:    "последние известные данные",
	KeyDBMuted:    "заглушено",
	KeyDBNone:     "Нет настроенных баз",
	KeyDBNoPoints: "Нет точек обмена",
	KeyDBToggleFailed: "Не удалось переключить опрос базы",
	KeyDBNoData:   "Нет данных от службы",

	KeyPtSend:     "отпр.",
	KeyPtRecv:     "приём",
	KeyPtNever:    "—",
	KeyPtLag:      "%d мин назад",
	KeyPtBaseline: "не раньше запуска службы",
	KeyPtAcked:    "подтверждено",
	KeyPtPlan:     "План обмена",

	KeyMenuAbout: "О программе",
	KeyMenuExit:  "Выход",

	KeyAboutTitle:     "О программе Exchange Checker",
	KeyAboutVersion:   "Версия: %s",
	KeyAboutProtocol:  "Служба: протокол %d",
	KeyAboutNoService: "Служба: не подключена",
	KeyAboutText:      "Контроль обменов 1С 8.1 в базах Microsoft SQL Server.",
	KeyAboutOK:        "OK",

	KeyAlertTitleError:     "Exchange Checker — ошибка обмена",
	KeyAlertTitleWarning:   "Exchange Checker — предупреждение",
	KeyAlertHeadError:      "Обмен данными остановлен или база недоступна:",
	KeyAlertHeadWarning:    "Обмен данными задерживается:",
	KeyAlertPoint:          "%s — %s: %s, нет обмена %d мин",
	KeyAlertPointNever:     "%s — %s: %s, обмена ещё не было",
	KeyAlertProfile:        "%s: %s — %s",
	KeyAlertMore:           "…и ещё %d",
	KeyAlertAck:            "Подтвердить",
	KeyAlertClose:          "Закрыть",
	KeyAlertAckPending:     "Подтверждение…",
	KeyAlertAckGone:        "Тревога уже снята или подтверждена.",
	KeyAlertAckUnsupported: "Эта версия службы не поддерживает подтверждение тревог.",
	KeyAlertAckFailed:      "Не удалось подтвердить: %s",
	KeyAlertCloseHint:      "«Закрыть» выключает звук до следующего изменения; «Подтвердить» снимает тревогу.",
	KeyPanelStalled:        "%s — %s: застой обмена (номера сообщений не меняются)",
	KeyPtStalled:           "застой обмена",
	KeyAlertTestBadge:      "ТЕСТ",
	KeyMenuSummary:         "Сводка…",
	KeySumTitle:            "Сводка обмена",
	KeySumHours:            "За %d ч",
	KeySumTruncated:        "Показаны не все базы",
	KeySumBusy:             "Служба занята, повтор через 5 с…",
	KeySumEmpty:            "Нет данных",
	KeySumClose:            "Закрыть",
	KeyAlarmTest:           "Тест тревоги…",
	KeyAlarmTestTip:        "Отправить тестовую тревогу (и при желании в Viber).",
	KeyAlarmTestTipDenied:  "Только администратор может отправить тестовую тревогу.",
	KeyAlarmTestTitle:      "Тест тревоги",
	KeyAlarmTestWarning:    "Предупреждение",
	KeyAlarmTestError:      "Ошибка",
	KeyAlarmTestViber:      "Также в Viber",
	KeyAlarmTestRun:        "Отправить",
	KeyAlarmTestClose:      "Закрыть",
	KeyAlarmTestNoViber:    "Viber не настроен",
	KeyAlarmTestRetry:      "Повтор через %d с",
	KeyAlarmTestAccess:     "Нет прав администратора для теста тревоги.",
	KeySetStalledMin:       "Застой, мин",
	KeyTipSetStalled:       "Порог застоя обмена (номера _SentNo/_ReceivedNo). 0 = как в общих настройках; пусто в общих = 120; 0 в общих = выкл.",
	KeyGenStalled:          "Застой обмена, мин",
	KeyCueStalled:          "0 = выкл., пусто = 120",

	KeyMenuSettings:           "Настройки…",
	KeySetTitleRead:           "Exchange Checker — настройки баз (только чтение)",
	KeySetTitleAdmin:          "Exchange Checker — настройки баз (администратор)",
	KeySetProfiles:            "Базы:",
	KeySetNewProfile:          "(новая база)",
	KeySetName:                "Название:",
	KeySetHost:                "Сервер (хост):",
	KeySetInstance:            "Экземпляр:",
	KeySetPort:                "Порт:",
	KeySetDatabase:            "База данных:",
	KeySetAuth:                "Аутентификация:",
	KeySetAuthSQL:             "SQL Server (логин и пароль)",
	KeySetAuthWindows:         "Windows (учётная запись службы)",
	KeySetLogin:               "Логин:",
	KeySetPassword:            "Пароль:",
	KeySetClearPassword:       "Удалить сохранённый пароль",
	KeySetEncrypt:             "Шифрование:",
	KeySetEncDisable:          "Без шифрования",
	KeySetEncOptional:         "Только вход, если сервер умеет TLS",
	KeySetEncOn:               "Весь сеанс (TLS)",
	KeySetEncStrict:           "Строгое (TDS 8.0, SQL Server 2022+)",
	KeySetTLS10:               "Разрешить TLS 1.0 (SQL Server 2008)",
	KeySetTrust:               "Доверять сертификату сервера без проверки",
	KeySetEnabled:             "Опрашивать эту базу",
	KeySetColPoll:             "Опрос",
	KeySetMapYes:              "Таблицы обмена: настроены",
	KeySetMapNo:               "Таблицы обмена: не настроены — найдите их кнопкой «Таблицы обмена…»",
	KeySetThresholds:          "Свои пороги: предупреждение %d мин, ошибка %d мин",
	KeySetTest:                "Проверить подключение",
	KeySetCancelTest:          "Отменить проверку",
	KeySetSave:                "Сохранить",
	KeySetDelete:              "Удалить",
	KeySetClone:               "Клонировать базу",
	KeySetAdd:                 "Добавить",
	KeySetPoll:                "Опросить сейчас",
	KeySetReload:              "Перечитать",
	KeySetElevate:             "Изменить (администратор)",
	KeySetClose:               "Закрыть",
	KeySetHintPortInst:        "Указан порт: подключение идёт прямо на порт, экземпляр через SQL Browser не ищется.",
	KeySetHintPlaintext:       "Без шифрования пароль SQL-логина передаётся почти открытым текстом. Лучше Windows-аутентификация или отдельный логин только с ролью db_datareader.",
	KeySetPwNew:               "Новая база: введите пароль SQL-логина.",
	KeySetPwKeep:              "Пусто — оставить сохранённый пароль.",
	KeySetPwReenter:           "Сервер, логин или шифрование изменены: сохранённый пароль не будет использован, введите пароль заново.",
	KeySetPwNone:              "Пароль не сохранён — введите пароль.",
	KeySetPwReplace:           "Сохранённый пароль будет заменён.",
	KeySetPwClear:             "Сохранённый пароль будет удалён.",
	KeySetPwWindows:           "Windows-аутентификация: пароль не нужен.",
	KeySetPwWindowsDrop:       "Windows-аутентификация: сохранённый пароль будет удалён при сохранении.",
	KeySetPwSaved:             "Пароль сохранён.",
	KeySetPwNotSaved:          "Пароль не сохранён.",
	KeySetReadOnlyNote:        "Только чтение: изменять настройки может администратор.",
	KeySetLoading:             "Загрузка настроек…",
	KeySetNoProfile:           "Выберите базу или нажмите «Добавить».",
	KeySetSaving:              "Сохранение…",
	KeySetSaved:               "Сохранено.",
	KeySetSavedPoll:           "Сохранено, опрос базы запланирован.",
	KeySetDeleting:            "Удаление…",
	KeySetDeleted:             "База удалена из настроек.",
	KeySetDeleteConfirm:       "Удалить базу «%s» из настроек? Пароль, состояние и тревоги этой базы тоже будут удалены.",
	KeySetCloned:              "База «%s» создана (копия). Введите пароль SQL.",
	KeySetClonedNeedPw:        "База «%s» создана. Пароль не копируется — введите его.",
	KeySetCloneConfirm:        "Клонировать базу «%s»? Пароль SQL не копируется — его нужно ввести заново.",
	KeySetDiscardConfirm:      "Отменить изменения?",
	KeyDiscTitle:              "Таблицы обмена",
	KeyDiscButton:             "Таблицы обмена…",
	KeyDiscFind:               "Найти таблицы",
	KeyDiscListPoints:         "Точки обмена",
	KeyDiscCancel:             "Отменить",
	KeyDiscRunning:            "Поиск… %d с (служба отвечает не дольше 100 с)",
	KeyDiscChecking:           "Проверка… %d с",
	KeyDiscFound:              "Найдено вариантов: %d.",
	KeyDiscNone:               "Подходящих таблиц не найдено.",
	KeyDiscIncomplete:         "Результат неполный.",
	KeyDiscReasonBudget:       "Результат неполный: истекло время поиска (90 с). Повторите поиск или сузьте его (поиск по точке и времени).",
	KeyDiscReasonSize:         "Результат неполный: ответ сокращён до 64 КиБ. Уточните запрос (поиск по точке и времени).",
	KeyDiscReasonLimit:        "Список неполный: точек больше, чем запрошено (1000). Введите имя точки вручную.",
	KeyDiscReasonPointsSize:   "Список неполный: ответ сокращён до 64 КиБ. Введите имя точки вручную.",
	KeyDiscReasonCheckSize:    "Предпросмотр неполный: ответ сокращён до 64 КиБ.",
	KeyDiscPointsFound:        "Точек обмена: %d.",
	KeyDiscPoint:              "Точка:",
	KeyDiscTime:               "Время обмена (UTC+3):",
	KeyDiscTolerance:          "Допуск, мин:",
	KeyDiscByValue:            "Найти по точке",
	KeyDiscBadTime:            "Введите время в виде ДД.ММ.ГГГГ ЧЧ:ММ (UTC+3).",
	KeyDiscBadTolerance:       "Допуск — целое число минут, не меньше 0.",
	KeyDiscMode:               "Режим:",
	KeyDiscModeAuto:           "Таблицы",
	KeyDiscModeManual:         "Ручной SQL",
	KeyDiscModeCounters:       "Счётчики плана обмена",
	KeyDiscSchema:             "Схема:",
	KeyDiscTable:              "Таблица точек:",
	KeyDiscPointName:          "Колонка имени:",
	KeyDiscPointID:            "Колонка кода:",
	KeyDiscPointKey:           "Колонка ключа:",
	KeyDiscTimesTable:         "Таблица времён:",
	KeyDiscTimesLink:          "Колонка связи:",
	KeyDiscSend:               "Колонка отправки:",
	KeyDiscRecv:               "Колонка приёма:",
	KeyDiscPlanLabel:          "Подпись плана:",
	KeyDiscManualSQL:          "Ручной SQL:",
	KeyDiscYearOffset:         "Сдвиг лет 1С (+2000)",
	KeyDiscKeepUnlinked:       "Показывать точки без записей времени",
	KeyDiscAllPlans:           "Добавить все найденные планы обмена",
	KeyDiscTemplate:           "Шаблон:",
	KeyDiscCheck:              "Проверить",
	KeyDiscApply:              "Применить",
	KeyDiscClose:              "Закрыть",
	KeyDiscCheckOK:            "Проверка пройдена. Примеры (отправка / приём, UTC+3):",
	KeyDiscCheckFailed:        "Проверка не пройдена.",
	KeyDiscViolations:         "Конструкции SQL Server 2012+ (в SQL Server 2008 нет):",
	KeyDiscPoints:             "точек: %d",
	KeyDiscColumns:            "Колонки:",
	KeyDiscMatched:            "Найденная точка:",
	KeyDiscCanceled:           "Операция отменена.",
	KeyDiscApplied:            "Таблицы обмена перенесены в профиль. Нажмите «Сохранить».",
	KeyDiscProfileChanged:     "В окне настроек выбран другой профиль — откройте «Таблицы обмена» заново.",
	KeyDiscExtraPlans:         "Доп. планы обмена",
	KeyDiscFieldMain:          "Таблицы обмена — %s",
	KeyDiscFieldExtra:         "Доп. план обмена %d — %s",
	KeyDiscLimit:              "Число строк (limit)",
	KeyDiscTimesSchema:        "Схема таблицы времён",
	KeyDiscLinkType:           "Колонка типа ссылки (_TYPE)",
	KeyDiscLinkRTRefCol:       "Колонка номера таблицы (_RTRef)",
	KeyDiscLinkRTRef:          "Номер таблицы плана обмена",
	KeyDiscScore:              "оценка %d",
	KeyDiscCandSingle:         "одна таблица",
	KeyDiscCandLinked:         "связанные таблицы",
	KeyDiscCandCounters:       "счётчики",
	KeyDiscToleranceHint:      "0 или пусто — 1 сутки (1440 мин)",
	KeyDiscConfirmClose:       "Изменения таблиц обмена не применены. Закрыть без применения?",
	KeyDiscConfirmOverwrite:   "Поля таблиц обмена изменены вручную и будут заменены. Продолжить?",
	KeyDiscConfirmRemove:      "При применении из профиля будут убраны: %s. Продолжить?",
	KeyDiscSourceAuto:         "Источник: найдено поиском (auto)",
	KeyDiscSourceManual:       "Источник: задано вручную (manual)",
	KeyDiscSourceCounters:     "Источник: счётчики (counters)",
	KeyDiscConfigChanged:      "Настройки профиля изменились, пока был открыт вопрос; ничего не применено — проверьте и нажмите «Применить» ещё раз.",
	KeyDiscListChanged:        "Список кандидатов обновился, пока был открыт вопрос — выберите кандидата ещё раз.",
	KeyDiscBusy:               "Окно настроек ждёт ответа сервиса — нажмите «Применить» ещё раз через несколько секунд.",
	KeyDiscSourceNone:         "Таблицы обмена не заданы",
	"hint.linked":             "Точки и времена в разных таблицах, связаны ключом",
	"hint.same_table":         "Времена — колонки таблицы точек",
	"hint.node_table":         "Таблица точек — план обмена 1С (_Node)",
	"hint.generic":            "Найдено по названиям колонок (не 1С)",
	"hint.by_value":           "Найдено по указанной точке",
	"hint.exact_match":        "Имя точки совпало точно",
	"hint.near_approx_time":   "Время близко к указанному",
	"hint.all_points_fresh":   "Все точки обновлялись недавно",
	"hint.year_offset_2000":   "Обнаружен сдвиг лет 1С +2000",
	"hint.send_recv_by_name":  "Отправка/приём выбраны по названиям колонок",
	"hint.send_recv_by_order": "Отправка/приём выбраны по порядку колонок — проверьте",
	"hint.single_series":      "Только одна колонка времени",
	"hint.no_timestamps":      "Заполненных времён пока нет",
	"hint.future_dates":       "Последнее время — в будущем",
	"hint.sampled":            "Статистика по выборке строк",
	"hint.sample_failed":      "Примеры строк прочитать не удалось",
	"hint.truncated":          "Время поиска истекло, результат неполный",
	"hint.counters_fallback":  "Времена будут по изменениям счётчиков (с запуска службы)",
	"hint.composite_ref":      "Составная ссылка 1С (_TYPE/_RTRef/_RRRef)",
	"hint.by_time":            "Найдено по времени выбранной точки",
	KeyAutostartErrAccess:     "Служба не смогла изменить автозапуск трея: нет доступа к реестру (HKLM Run).",
	KeyAutostartErrRegistry:   "Служба не смогла изменить автозапуск трея: ошибка реестра (HKLM Run).",
	KeyAutostartErrPath:       "Служба не смогла определить путь к программе трея; автозапуск не изменён.",
	KeyAutostartErrSettings:   "При запуске служба не смогла прочитать settings.json; автозапуск трея не изменён.",
	KeyAutostartErrUnknown:    "Служба не смогла изменить автозапуск трея.",
	KeyGenPage:                "Общие настройки…",
	KeyGenProfilesPage:        "Базы данных…",
	KeySetNavProfiles:         "Базы данных",
	KeyDiscCandidates:         "Найденные таблицы:",
	KeyDiscDetails:            "Подробности:",
	KeyDiscResultLabel:        "Результат проверки:",
	KeySetNavGeneral:          "Общие настройки",
	KeySetNavViber:            "Уведомления Viber",

	KeySetNavEmail:            "Уведомления e-mail",
	KeyEmGroup:                "Уведомления e-mail",
	KeyEmEnabled:              "Отправлять уведомления по e-mail",
	KeyEmHost:                 "SMTP-сервер:",
	KeyEmPort:                 "Порт:",
	KeyEmTLSMode:              "TLS:",
	KeyEmTLSNone:              "Без TLS (без AUTH)",
	KeyEmTLSStartTLS:          "STARTTLS",
	KeyEmTLSImplicit:          "TLS (порт 465)",
	KeyEmFromAddr:             "От кого (From):",
	KeyEmPassword:             "Пароль SMTP:",
	KeyEmClearPassword:        "Удалить сохранённый пароль",
	KeyEmPasswordSaved:        "Пароль сохранён (не показывается).",
	KeyEmPasswordNone:         "Пароль не задан.",
	KeyEmPasswordNew:          "Новый пароль будет сохранён.",
	KeyEmPasswordClear:        "Сохранённый пароль будет удалён.",
	KeyEmPasswordNoneTLS:      "Пароль не используется (tlsMode = none).",
	KeyEmRecipients:           "Получатели (адреса через запятую, точку с запятой, пробел или с новой строки):",
	KeyEmBadAddr:              "Неверный адрес e-mail получателя: «%s». Укажите адреса через запятую, точку с запятой, пробел или с новой строки, например: ivanov@mail.ru, petrov@mail.ru",
	KeyEmDays:                 "Дни:",
	KeyEmFrom:                 "Время с (ЧЧ:ММ):",
	KeyEmTo:                   "по:",
	KeyEmEvents:               "События:",
	KeyEmTestTo:               "Тест для:",
	KeyEmTestAll:              "Все получатели",
	KeyEmTest:                 "Тест",
	KeyEmTesting:              "Отправка теста…",
	KeyEmTestOK:               "Тест отправлен.",
	KeyEmTestFailed:           "Тест не удался.",
	KeyEmTestItemOK:           "доставлено",
	KeyEmTestNoResult:         "Нет результата теста.",
	KeyEmStatus:               "Отправлено: %d, в очереди: %d, отброшено: %d",
	KeyEmLastOK:               "Последняя успешная отправка: %s",
	KeyEmLastError:            "Последняя ошибка (%s): %s",
	KeyEmSaved:                "Настройки e-mail сохранены.",
	KeyTipEmPage:              "Уведомления по электронной почте",
	KeyTipEmEnabled:           "Включить отправку писем по событиям обмена",
	KeyTipEmHost:              "Имя или адрес SMTP-сервера",
	KeyTipEmPort:              "Порт SMTP (обычно 25, 465 или 587)",
	KeyTipEmTLSMode:           "none — без TLS и без AUTH; starttls / tls — AUTH с паролем, логин = From",
	KeyTipEmFromAddr:          "Адрес отправителя (и логин SMTP AUTH)",
	KeyTipEmPassword:          "Пароль SMTP (только запись; при tlsMode=none не используется)",
	KeyTipEmClearPassword:     "Удалить пароль, сохранённый службой",
	KeyTipEmRecipients:        "Адреса через запятую, точку с запятой, пробел или с новой строки (например: ivanov@mail.ru, petrov@mail.ru); строка «ID адрес имя» тоже допускается",
	KeyTipEmDay:               "День недели окна рассылки",
	KeyTipEmFrom:              "Начало окна (локальное время службы)",
	KeyTipEmTo:                "Конец окна (локальное время службы)",
	KeyTipEmEvent:             "Какие события слать письмом",
	KeyTipEmTestTo:            "Кому отправить тестовое письмо",
	KeyTipEmTest:              "Отправить тестовое письмо сейчас (игнорирует расписание)",
	KeyCueEmHost:              "smtp.example.com",
	KeyCueEmPort:              "587",
	KeyCueEmFrom:              "monitor@example.com",
	KeyCueEmPassword:          "пароль SMTP",
	KeyXferEmailCheck:         "Получатели e-mail",
	KeyXferEmailWarnExport:    "В файл попадут получатели e-mail (пароль SMTP не переносится). Храните файл в надёжном месте.",
	KeyXferEmailWarnImport:    "Получатели e-mail из файла будут применены (пароль SMTP не переносится).",
	KeyTipXferEmail:           "Несекретные получатели e-mail в файле (пароль SMTP никогда не переносится)",
	KeyTipSetNavProfiles:      "Базы данных: подключение, вход, шифрование, опрос и пороги каждой базы.",
	KeyGenPoll:                "Интервал опроса, с:",
	KeyGenWarn:                "Предупреждение через, мин:",
	KeyGenError:               "Ошибка через, мин:",
	KeyGenBeepEnabled:         "Звуковой сигнал тревоги",
	KeyGenWarnFreq:            "Предупреждение: частота, Гц:",
	KeyGenWarnDur:             "Предупреждение: длительность, мс:",
	KeyGenErrorFreq:           "Ошибка: частота, Гц:",
	KeyGenErrorDur:            "Ошибка: длительность, мс:",
	KeyGenGap:                 "Пауза между сигналами, мс:",
	KeyGenAutostart:           "Запускать значок в трее при входе в Windows (для всех пользователей)",
	KeyGenNote:                "Общие настройки действуют на весь компьютер и применяются службой. Звук выключает только сигнал: окно тревоги показывается всегда.",
	KeyGenSaved:               "Общие настройки сохранены.",
	KeySetTesting:             "Проверка подключения… (до 15 с)",
	KeySetTestCanceled:        "Проверка отменена.",
	KeySetTestOK:              "Подключение успешно (%d мс).",
	KeySetTestFailed:          "Подключение не удалось: %s",
	KeySetTestVersion:         "Сервер: %s",
	KeySetTestCompat:          "Уровень совместимости базы: %d",
	KeySetTestEncrypt:         "Шифрование: %s",
	KeySetTestNoReply:         "Служба не ответила за %d с.",
	KeySetPolled:              "Опрос запущен.",
	KeySetPollCoalesced:       "Опрос уже запланирован.",
	KeySetNotConnected:        "Нет связи со службой Exchange Checker.",
	KeySetChangedOffline:      "Настройки изменились, пока не было связи со службой. Нажмите «Перечитать» (несохранённые правки будут потеряны).",
	KeySetUnsupported:         "Эта версия службы не поддерживает эту команду.",
	KeySetTooLarge:            "Запрос больше 64 КиБ — сократите данные профиля.",
	KeySetNoRevision:          "Неизвестна версия настроек — нажмите «Перечитать».",
	KeySetConflictHint:        "Нажмите «Перечитать»: несохранённые правки будут потеряны.",
	KeySetOtherInvalid:        "Сохранить нельзя: в файле настроек неверно заполнена другая база «%s» (%s). Исправьте или удалите её.",
	KeySetRateHint:            "Проверку подключения и опрос служба выполняет не чаще раза в 10 секунд.",
	KeySetTimeoutHint:         "Проверка ограничена 15 секундами: проверьте сеть, порт и брандмауэр.",
	KeySetAccessHint:          "Нажмите «Изменить (администратор)».",
	KeySetFailed:              "Ошибка: %s",
	KeySetElevateCanceled:     "Запуск от имени администратора отменён.",
	KeySetElevateFailed:       "Не удалось запустить настройки от имени администратора: %s",

	KeyUnknownMessage:      "Ошибка",
	KeySetPreset:           "Тип сервера:",
	KeySetPreset2008:       "SQL Server 2008 / 2008 R2",
	KeySetPresetNone:       "SQL Server 2012 и новее",
	KeySetWarnMin:          "Предупреждение, мин:",
	KeySetErrorMin:         "Ошибка, мин:",
	KeySetConnTimeout:      "Тайм-аут подключения, с:",
	KeySetQueryTimeout:     "Тайм-аут запроса, с:",
	KeySetPortCue:          "1433 или пусто для SQL Browser",
	KeySetThreshCue:        "как в общих настройках",
	KeySetTimeoutCue:       "по умолчанию %d",
	KeyCueName:             "например, Склад",
	KeyCueHost:             "имя или IP-адрес сервера",
	KeyCueInstance:         "пусто — экземпляр по умолчанию",
	KeyCueDatabase:         "имя базы 1С на сервере",
	KeyCueLogin:            "SQL-логин, например ec_monitor",
	KeyCuePassword:         "пароль SQL-логина",
	KeyCueWizServer:        "имя, IP или сервер\\экземпляр",
	KeyCueDiscTime:         "ДД.ММ.ГГГГ ЧЧ:ММ",
	KeyCueDiscSchema:       "dbo",
	KeyCueDiscColumn:       "из кандидата или вручную",
	KeyCueVbToken:          "токен бота из Viber Admin Panel",
	KeyMenuService:         "Служба",
	KeyMenuSvcStart:        "Запустить",
	KeyMenuSvcStop:         "Остановить",
	KeySvcState:            "Состояние: %s",
	KeySvcRunning:          "работает",
	KeySvcStopped:          "остановлена",
	KeySvcStartPending:     "запускается",
	KeySvcStopPending:      "останавливается",
	KeySvcNotInstalled:     "не установлена",
	KeySvcNoAccess:         "нет доступа к состоянию",
	KeySvcUnknown:          "неизвестно",
	KeySvcStartSent:        "Служба «%s» запускается.",
	KeySvcStopSent:         "Служба «%s» останавливается.",
	KeySvcErrAccess:        "Недостаточно прав, чтобы запустить или остановить службу «%s». Это может сделать администратор компьютера, например в оснастке «Службы» (services.msc).",
	KeySvcErrNotInstalled:  "Служба «%s» не установлена на этом компьютере. Запустите установщик Exchange Checker.",
	KeySvcErrRunning:       "Служба «%s» уже работает.",
	KeySvcErrNotActive:     "Служба «%s» уже остановлена.",
	KeySvcErrOther:         "Не удалось выполнить действие со службой «%s»: %s",
	KeyTipSetList:          "Базы, которые проверяет служба. Выберите базу, чтобы увидеть и изменить её настройки.",
	KeyTipSetAdd:           "Добавить новую базу данных для проверки.",
	KeyTipSetDelete:        "Удалить выбранную базу из настроек (с подтверждением).",
	KeyTipSetClone:         "Создать копию выбранной базы без пароля (нужен clone_profile).",
	KeyTipSetDiscover:      "Найти в базе 1С таблицы с временем обмена и настроить их.",
	KeyTipSetName:          "Название базы в меню трея и в тревогах. Можно оставить пустым — тогда «сервер/база».",
	KeyTipSetHost:          "Имя компьютера или IP-адрес SQL Server, например SRV-SQL или 10.0.0.5. Без имени экземпляра.",
	KeyTipSetInstance:      "Только имя именованного экземпляра, например SQLEXPRESS. Не сервер и не IP. Пусто — экземпляр по умолчанию.",
	KeyTipSetPort:          "TCP-порт SQL Server. Пусто — 1433, а для именованного экземпляра порт узнаётся у SQL Browser (UDP 1434).",
	KeyTipSetDatabase:      "Имя базы данных 1С на сервере, как в SQL Server Management Studio.",
	KeyTipSetAuth:          "SQL — логин и пароль SQL Server. Windows — служба входит учётной записью компьютера (ДОМЕН\\ИМЯ$).",
	KeyTipSetLogin:         "Логин SQL Server только с ролью db_datareader. Не sa.",
	KeyTipSetPassword:      "Пароль SQL-логина. Хранится службой в зашифрованном виде; пусто — оставить сохранённый.",
	KeyTipSetClearPw:       "Удалить сохранённый пароль этой базы при сохранении.",
	KeyTipSetEncrypt:       "Шифрование подключения. Для SQL Server 2008 без обновлений обычно «Без шифрования».",
	KeyTipSetPreset:        "Версия SQL Server. «2008 / 2008 R2» разрешает TLS 1.0 и запрещает режим Strict.",
	KeyTipSetTLS10:         "Разрешить старый протокол TLS 1.0 — нужен SQL Server 2005/2008 без обновлений TLS 1.2.",
	KeyTipSetTrust:         "Не проверять сертификат сервера (самоподписанный). Действует только в режиме «Весь сеанс (TLS)».",
	KeyTipSetEnabled:       "Снимите флажок, чтобы временно не проверять эту базу.",
	KeyTipSetWarn:          "Через сколько минут без обмена показать предупреждение. Значение, равное унаследованному из общих настроек, не фиксируется: профиль следует общим настройкам.",
	KeyTipSetError:         "Через сколько минут без обмена показать ошибку. Должно быть больше предупреждения. Значение, равное унаследованному из общих настроек, не фиксируется: профиль следует общим настройкам.",
	KeyTipSetConnTimeout:   "Сколько секунд ждать подключения к SQL Server (1–600). Значение, равное унаследованному (по умолчанию), не фиксируется: профиль следует общим настройкам.",
	KeyTipSetQueryTimeout:  "Сколько секунд ждать ответа на запрос (1–600). Значение, равное унаследованному (по умолчанию), не фиксируется: профиль следует общим настройкам.",
	KeyTipSetPage:          "Общие настройки компьютера: опрос, пороги, звук, автозапуск.",
	KeyTipSetStatus:        "Результат последнего действия и ошибки.",
	KeyTipSetTest:          "Проверить подключение и чтение таблиц с текущими значениями формы.",
	KeyTipSetCancelTest:    "Прервать идущую проверку подключения.",
	KeyTipSetSave:          "Сохранить изменения в настройки службы.",
	KeyTipSetPoll:          "Попросить службу опросить эту базу сейчас.",
	KeyTipSetReload:        "Перечитать настройки из службы (несохранённые правки пропадут).",
	KeyTipSetElevate:       "Открыть настройки с правами администратора, чтобы их можно было менять.",
	KeyTipSetClose:         "Закрыть окно настроек.",
	KeyTipGenPoll:          "Как часто служба опрашивает базы, в секундах.",
	KeyTipGenWarn:          "Общий порог предупреждения в минутах для всех баз без своего порога.",
	KeyTipGenError:         "Общий порог ошибки в минутах; больше порога предупреждения.",
	KeyTipGenBeep:          "Пищать системным динамиком при тревоге.",
	KeyTipGenWarnFreq:      "Частота звука предупреждения, Гц.",
	KeyTipGenWarnDur:       "Длительность звука предупреждения, мс.",
	KeyTipGenErrFreq:       "Частота звука ошибки, Гц.",
	KeyTipGenErrDur:        "Длительность звука ошибки, мс.",
	KeyTipGenGap:           "Пауза между сигналами, мс.",
	KeyTipGenAutostart:     "Запускать значок в трее при входе любого пользователя.",
	KeyTipDiscFind:         "Найти в базе таблицы, похожие на время обмена.",
	KeyTipDiscPoints:       "Загрузить список точек обмена (узлов планов обмена).",
	KeyTipDiscCancel:       "Прервать идущий поиск или проверку.",
	KeyTipDiscPoint:        "Имя точки обмена, время которой вы знаете.",
	KeyTipDiscTime:         "Известное время последнего обмена этой точки: ДД.ММ.ГГГГ ЧЧ:ММ (UTC+3).",
	KeyTipDiscTol:          "Допуск поиска по времени, минут. 0 или пусто — 1 сутки.",
	KeyTipDiscByValue:      "Найти колонки, где у точки записано это время.",
	KeyTipDiscList:         "Найденные варианты. Выберите вариант, чтобы перенести его в поля ниже.",
	KeyTipDiscDetails:      "Подробности выбранного варианта.",
	KeyTipDiscMode:         "Как служба читает время: из таблиц, ручным SQL или по счётчикам сообщений.",
	KeyTipDiscField:        "Имя таблицы или колонки SQL Server, обычно вида _Node123 или _Fld456.",
	KeyTipDiscPlanLabel:    "Своё название плана обмена для меню (необязательно).",
	KeyTipDiscYear:         "1С хранит даты со сдвигом +2000 лет — обычно флажок включён.",
	KeyTipDiscKeep:         "Показывать точки, для которых ещё нет записи времени.",
	KeyTipDiscAllPlans:     "Добавить в профиль все найденные планы обмена, а не только выбранный.",
	KeyTipDiscTemplate:     "Готовый шаблон ручного SQL для SQL Server 2008.",
	KeyTipDiscSQL:          "Запрос должен вернуть имя точки, время отправки и время приёма.",
	KeyTipDiscResult:       "Результат проверки и ошибки.",
	KeyTipDiscCheck:        "Проверить настройку на сервере и показать примеры.",
	KeyTipDiscApply:        "Перенести настройку в профиль базы (затем «Сохранить» в окне настроек).",
	KeyTipDiscClose:        "Закрыть окно без применения.",
	KeyTipAlertAck:         "Подтвердить тревогу: звук смолкнет до следующего изменения.",
	KeyTipAlertClose:       "Закрыть окно и заглушить звук до следующего изменения статуса.",
	KeyTipAboutOK:          "Закрыть окно «О программе».",
	KeyWizTitle:            "Добавить базу",
	KeyWizStep:             "Шаг %d из 3: %s",
	KeyWizStepServer:       "сервер и вход",
	KeyWizStepDB:           "база данных",
	KeyWizStepFinish:       "проверка и сохранение",
	KeyWizServer:           "Сервер:",
	KeyWizFind:             "Найти",
	KeyWizBrowsing:         "Поиск серверов SQL…",
	KeyWizFound:            "Найдено экземпляров: %d (ответили компьютеров: %d).",
	KeyWizFoundPartial:     "Список может быть неполным.",
	KeyWizNoneFound:        "Серверы не найдены. Проверьте, что служба SQL Server Browser запущена и UDP 1434 не закрыт брандмауэром, или введите адрес вручную.",
	KeyWizRateLimited:      "Поиск выполнялся недавно — повторите через %d с.",
	KeyWizBusy:             "Служба уже выполняет поиск или проверку — повторите позже.",
	KeyWizPort:             "Порт:",
	KeyWizAuth:             "Вход:",
	KeyWizAuthSQL:          "Логин SQL Server",
	KeyWizAuthWindows:      "Учётная запись компьютера (служба EC)",
	KeyWizLogin:            "Логин:",
	KeyWizPassword:         "Пароль:",
	KeyWizEncrypt:          "Шифрование:",
	KeyWizDBs:              "Базы данных сервера:",
	KeyWizRefresh:          "Обновить список",
	KeyWizDBLoading:        "Получение списка баз…",
	KeyWizDBCount:          "Баз: %d.",
	KeyWizDBTruncated:      "Показаны не все базы.",
	KeyWizDB1C:             "база 1С",
	KeyWizDBNode:           "есть планы обмена (_Node)",
	KeyWizDBSize:           "%d МБ",
	KeyWizDBNone:           "Выберите базу в списке.",
	KeyWizTesting:          "Проверка подключения…",
	KeyWizTestOK:           "Подключение работает.",
	KeyWizDiscovering:      "Поиск таблиц обмена…",
	KeyWizApplied:          "Выбрано автоматически: %s",
	KeyWizChoose:           "Выберите вариант в списке или откройте «Расширенные настройки».",
	KeyWizChosen:           "Выбрано: %s",
	KeyWizFoundPlan:        "Найдено: план %s",
	KeyWizFoundPlans:       " (+%d)",
	KeyWizFoundFields:      "поля %s",
	KeyWizDetails:          "Подробнее",
	KeyWizErrNone: "Таблицы обмена не найдены: у учётной записи службы, вероятно, нет прав на чтение таблиц базы. Нажмите «Скопировать SQL-скрипт прав», выполните его в SQL Server Management Studio и повторите поиск.",
	KeyWizErrAmbiguous: "Найдены два равноценных варианта: %s и %s. Откройте «Подробнее» и выберите нужный.",
	KeyWizErrWinUnsaved: "Вход Windows для новой базы служба проверяет только после сохранения профиля. Выберите вход SQL Server или сохраните базу через «Расширенные настройки» и запустите поиск таблиц там.",
	KeyWizErrTruncated: "Поиск таблиц прерван по времени или объёму, ничего не найдено. Повторите поиск; если повторится — откройте «Расширенные настройки».",
	KeyWizErrLowScore: "Найденные таблицы не похожи на таблицы обмена с достаточной уверенностью — автоматически ничего не применено. Лучший вариант выделен в списке «Подробнее»: проверьте его и нажмите на него, чтобы выбрать.",
	KeyWizErrIncomplete: "Лучший найденный вариант неполный (нет времени отправки или приёма, нет точек или поиск прерван) — автоматически ничего не применено. Он выделен в списке «Подробнее»: выберите его или другой вариант.",
	KeyTipWizDetails:       "Показать все найденные варианты таблиц обмена и выбрать другой вручную.",
	KeyWizNoteTruncated:    "Поиск был прерван, результат может быть неполным.",
	KeyWizConfidence:       "[%d%%]",
	KeyWizCands:            "Найденные таблицы обмена:",
	KeyWizSaving:           "Сохранение…",
	KeyWizBack:             "< Назад",
	KeyWizNext:             "Далее >",
	KeyWizSave:             "Сохранить",
	KeyWizAdvanced:         "Расширенные настройки…",
	KeyWizGrant:            "Скопировать SQL-скрипт прав",
	KeyWizCancel:           "Отмена",
	KeyWizGrantCopied:      "SQL-скрипт прав скопирован в буфер обмена. EC его не выполняет: проверьте и выполните его сами (например, в SQL Server Management Studio) от имени администратора SQL Server.",
	KeyWizGrantPlaceholder: "Замените %s на пароль логина.",
	KeyWizClipFailed:       "Не удалось скопировать в буфер обмена: %s",
	KeyWizAccessDenied:     "Нет прав администратора для этой команды. Закройте окно и откройте настройки снова через «Изменить (администратор)» с подтверждением UAC.",
	KeyWizSubnet:           "Сканировать подсеть…",
	KeyWizCancelBrowse:     "Отменить поиск",
	KeyWizBrowseProgress:   "Поиск: %s %d/%d, найдено %d",
	KeyWizBusyRetry:        "Служба занята. Повтор через %d с…",
	KeyWizPublicNetwork:    "Активен профиль сети «Общедоступная»: поиск SQL Server в ней ограничен правилами брандмауэра. Переключите сеть на «Частная» (домашняя/рабочая) или «Домен».",
	KeyWizCheckSteps:       "Проверка подключения",
	KeyWizCheckParse:       "Разбор адреса",
	KeyWizCheckDNS:         "DNS",
	KeyWizCheckBrowser:     "SQL Server Browser",
	KeyWizCheckTCP:         "TCP",
	KeyWizCheckPrelogin:    "PRELOGIN",
	KeyWizCheckTLS:         "Шифрование",
	KeyWizCheckLogin:       "Вход",
	KeyWizCheckDB:          "База данных",
	KeyWizCheckTables:      "Таблицы обмена",
	KeyWizChecking:         "Проверка сервера…",
	KeyWizErrCode:          "Код ошибки: %s",
	KeyWizWhatToDo:         "Что делать:",
	KeyWizFBLogin:          "Проверьте логин и пароль; для учётной записи компьютера — что у неё есть логин на SQL Server (кнопка «Скопировать SQL-скрипт прав»).",
	KeyWizFBDatabase:       "База недоступна: проверьте имя, что она в состоянии ONLINE и что у логина есть к ней доступ.",
	KeyWizFBTLS:            "Ошибка TLS: попробуйте другой режим шифрования или включите TLS 1.0 для старого сервера; для проверки сертификата нужен доверенный сертификат.",
	KeyWizFBTimeout:        "Сервер не ответил вовремя: проверьте сеть и брандмауэр, увеличьте тайм-аут подключения.",
	KeyWizFBNetwork:        "Нет сетевого подключения: проверьте имя сервера, порт, что TCP/IP включён в SQL Server Configuration Manager и порт открыт в брандмауэре.",
	KeyWizFBBrowser:        "Служба SQL Server Browser не ответила: запустите её на сервере и откройте UDP 1434, или укажите порт экземпляра явно.",
	KeyWizFBInstance:       "Экземпляр не найден: проверьте имя экземпляра (кнопка «Найти»), что служба SQL Server Browser запущена на сервере и UDP 1434 открыт, или укажите порт экземпляра явно.",
	KeyWizFBPermission:     "Недостаточно прав на чтение: выдайте права скриптом (кнопка «Скопировать SQL-скрипт прав»).",
	KeyWizFBNot1C:          "Это не база 1С: выберите другую базу или задайте таблицы в «Расширенных настройках».",
	KeyWizFBConfig:         "Параметры подключения заданы неверно: проверьте поля шага «Сервер».",
	KeyWizFBOther:          "Проверьте параметры подключения и журнал службы EC.",
	KeyTipWizServer:        "Имя или IP-адрес SQL Server, для именованного экземпляра — «компьютер\\экземпляр». Без «tcp:», «np:» и UNC-путей.",
	KeyTipWizFind:          "Найти серверы через SQL Server Browser: по умолчанию — этот компьютер и локальная сеть; подсеть — отдельной кнопкой.",
	KeyTipWizSubnet:        "Сканировать TCP-порты SQL в подсети этого компьютера (дольше; только по явному запросу).",
	KeyTipWizCancelBrowse:  "Отменить текущий поиск серверов.",
	KeyTipWizInstances:     "Найденные экземпляры SQL Server; выбор подставляет адрес в поле «Сервер».",
	KeyTipWizPort:          "TCP-порт; пусто — порт по умолчанию или через SQL Server Browser.",
	KeyTipWizAuth:          "Логин SQL Server или учётная запись компьютера, от имени которой работает служба EC.",
	KeyTipWizLogin:         "Логин SQL Server только для чтения.",
	KeyTipWizPassword:      "Пароль логина; хранится службой в зашифрованном виде и не показывается.",
	KeyTipWizEncrypt:       "Режим шифрования (TLS) подключения.",
	KeyTipWizDBs:           "Базы данных сервера; отмечены базы 1С и базы с планами обмена (_Node).",
	KeyTipWizRefresh:       "Получить список баз заново.",
	KeyTipWizCands:         "Варианты таблиц обмена с уверенностью; выбор применяется к новой базе.",
	KeyTipWizResult:        "Ход проверки, ошибки и что делать.",
	KeyTipWizBack:          "Вернуться к предыдущему шагу.",
	KeyTipWizNext:          "Перейти к следующему шагу.",
	KeyTipWizSave:          "Сохранить новую базу в настройках службы.",
	KeyTipWizAdvanced:      "Продолжить в полной форме настроек с уже введёнными данными.",
	KeyTipWizGrant:         "Скопировать в буфер обмена T-SQL, выдающий права только на чтение; EC скрипт не выполняет.",
	KeyTipWizCancel:        "Закрыть мастер без сохранения.",
	// phase 10 A–C
	KeyPanelNoService:     "Нет связи со службой Exchange Checker",
	KeyPanelNoSQL:         "%s: нет связи с SQL Server",
	KeyPanelTitle:         "Exchange Checker — проблемы",
	KeyAlertMute:          "Звук выкл",
	KeyAlertUnmute:        "Звук вкл",
	KeyTipAlertMute:       "Выключить или включить звук этой тревоги. Следующая новая тревога снова будет со звуком",
	KeyMenuSound:          "Звук тревог…",
	KeyMenuVolume:         "Громкость",
	KeyMenuPinHelp:        "Как закрепить значок…",
	KeyPinHelpTitle:       "Значок Exchange Checker",
	KeyPinHelpText:        "Чтобы значок всегда был виден рядом с часами (Windows 7):\r\n1. Нажмите стрелку «Отображать скрытые значки» слева от часов.\r\n2. Выберите «Настроить…».\r\n3. Для Exchange Checker выберите «Показать значок и уведомления».\r\n\r\nВ Windows 10 и 11: Параметры → Персонализация → Панель задач → Значки в углу панели задач — включите Exchange Checker.",
	KeySoundTitle:         "Звук тревог",
	KeySoundWarning:       "Предупреждение",
	KeySoundError:         "Ошибка",
	KeySoundBeep:          "Тон (системный динамик)",
	KeySoundFile:          "Файл",
	KeySoundFreq:          "Частота, Гц:",
	KeySoundDur:           "Длительность, мс:",
	KeySoundBrowse:        "Обзор…",
	KeySoundVolume:        "Громкость, %:",
	KeySoundLoop:          "Повторять до подтверждения",
	KeySoundGap:           "Пауза между повторами, мс:",
	KeySoundTest:          "Прослушать",
	KeySoundStop:          "Остановить",
	KeySoundSave:          "Сохранить",
	KeySoundCancel:        "Отмена",
	KeySoundFilterAudio:   "Звуковые файлы (*.wav; *.mp3; *.wma; *.mid)",
	KeySoundFilterAll:     "Все файлы (*.*)",
	KeySoundBadFile:       "Файл не найден или не поддерживается: %s",
	KeySoundBadValue:      "Проверьте значение: %s",
	KeySoundSaved:         "Сохранено",
	KeySoundFallback:      "Не удалось воспроизвести «%s» (%s), звучит тон",
	KeySoundToneVolume:    "громкость не регулируется",
	KeySpeakerAvailable:   "Системный динамик: доступен",
	KeySpeakerUnavailable: "Системный динамик: недоступен (%s) — тон звучит через колонки",
	KeySpeakerNoService:   "нет связи со службой",
	KeySpeakerNoCap:       "служба не поддерживает системный динамик",
	KeySpeakerNoReason:    "причина не указана",
	KeySpeakerTimeout:     "служба не ответила вовремя",
	KeySpeakerFallback:    "Системный динамик недоступен (%s), тон звучит через колонки",
	KeyTipSoundKind:       "Тон — заданная частота через системный динамик компьютера (его включает служба); если он недоступен — синусоида через колонки или звуковую карту. Громкость тона не регулируется. Файл — WAV, MP3, WMA или MID",
	KeyTipSoundFreq:       "Частота тона, 37–32767 Гц",
	KeyTipSoundDur:        "Длительность одного сигнала, мс",
	KeyTipSoundFile:       "Путь к звуковому файлу. Если файл не воспроизводится, звучит тон",
	KeyTipSoundBrowse:     "Выбрать звуковой файл",
	KeyTipSoundVolume:     "Громкость этого профиля, 0–100%; умножается на общую громкость",
	KeyTipSoundLoop:       "Повторять звук, пока тревогу не подтвердят или не закроют окно; иначе — один раз",
	KeyTipSoundGap:        "Пауза между повторами звука, мс",
	KeyTipSoundTest:       "Проиграть звук с текущими настройками",
	KeyTipSoundSave:       "Сохранить настройки звука для текущего пользователя Windows",
	KeyVolumeTitle:        "Громкость тревог",
	KeyTipVolume:          "Общая громкость звуков тревог, 0–100%. Сохраняется сразу",
	KeySetModeExpert:      "Экспертный режим",
	KeySetModeSimple:      "Простой режим",
	KeyTipSetUIMode:       "Простой режим — только основные поля; экспертный — все настройки. Выбор запоминается для пользователя Windows",
	KeySetGroupConn:       "Подключение",
	KeySetGroupLogin:      "Вход",
	KeySetGroupTLS:        "Шифрование",
	KeySetGroupChecks:     "Опрос и пороги",
	KeySetGroupGeneral:    "Общие настройки компьютера",
	KeySetGroupSound:      "Звук",
	KeyVbPage:             "Уведомления Viber…",
	KeyVbGroup:            "Уведомления Viber",
	KeyVbEnabled:          "Отправлять уведомления в Viber",
	KeyVbToken:            "Токен бота:",
	KeyVbClearToken:       "Удалить сохранённый токен",
	KeyVbTokenSaved:       "Токен сохранён (не показывается).",
	KeyVbTokenNone:        "Токен не задан.",
	KeyVbTokenNew:         "Новый токен будет сохранён.",
	KeyVbTokenClear:       "Сохранённый токен будет удалён.",
	KeyVbRecipients:       "Получатели (по одному в строке: ID имя):",
	KeyVbDays:             "Дни:",
	KeyVbDay1:             "Пн",
	KeyVbDay2:             "Вт",
	KeyVbDay3:             "Ср",
	KeyVbDay4:             "Чт",
	KeyVbDay5:             "Пт",
	KeyVbDay6:             "Сб",
	KeyVbDay7:             "Вс",
	KeyVbFrom:             "Время с (ЧЧ:ММ):",
	KeyVbTo:               "до:",
	KeyVbEvents:           "События:",
	KeyVbEvError:          "Ошибка обмена",
	KeyVbEvWarning:        "Предупреждение",
	KeyVbEvRecovered:      "Восстановление",
	KeyVbEvNoConn:         "Нет подключения к базе",
	KeyVbReminder:         "Напоминание, мин:",
	KeyVbReminderHint:     "0 — без напоминаний, иначе 15–1440",
	KeyVbTestTo:           "Тест для:",
	KeyVbTestAll:          "Все получатели",
	KeyVbTest:             "Тест",
	KeyVbTesting:          "Отправка тестового сообщения…",
	KeyVbTestOK:           "Тест Viber: доставлено.",
	KeyVbTestFailed:       "Тест Viber: есть ошибки.",
	KeyVbTestItemOK:       "доставлено",
	KeyVbTestNoResult:     "Служба не вернула результат теста.",
	KeyVbStatus:           "Отправлено: %d, в очереди: %d, отброшено: %d",
	KeyVbLastOK:           "Последняя доставка: %s",
	KeyVbLastError:        "Последняя ошибка: %s — %s",
	KeyVbSaved:            "Настройки Viber сохранены.",
	KeyTgGroup:           "Уведомления Telegram",
	KeyGenScreenFlash:       "Мигание экрана при ошибке обмена",
	KeyGenScreenFlashFailed: "Не удалось сохранить «Мигание экрана»:",
	KeyTipGenScreenFlash:    "Пока есть ошибка обмена, весь экран (все мониторы) мигает чёрным; фокус не перехватывается. «Закрыть» в окне тревоги останавливает мигание до восстановления",
	KeyTgEnabled:         "Отправлять уведомления в Telegram",
	KeyTgToken:           "Токен бота:",
	KeyTgClearToken:      "Удалить сохранённый токен",
	KeyTgChat:            "ID чата:",
	KeyTgChatHint:        "Число (например, -1001234567890) или @имя_канала; бот должен быть добавлен в чат.",
	KeyTgTest:            "Тест",
	KeyTgTesting:         "Отправка тестового сообщения в Telegram…",
	KeyTgTestOK:          "Тест Telegram: доставлено.",
	KeyTgTestFailed:      "Тест Telegram: не доставлено.",
	KeyTgTestRateLimited: "Тест Telegram — не чаще раза в 10 с; повторите через %d с.",
	KeyTgSaved:           "Настройки Telegram сохранены.",
	KeySetNavTelegram:    "Telegram",
	KeyTipTgPage:         "Уведомления о тревогах в Telegram: токен бота, чат, расписание, события",
	KeyTipTgEnabled:      "Служба отправляет сообщения только в окне расписания",
	KeyTipTgToken:        "Токен бота Telegram (от @BotFather). Только запись: сохранённый токен не показывается; пусто — не менять",
	KeyTipTgClearToken:   "Удалить сохранённый токен при сохранении",
	KeyTipTgChat:         "Куда отправлять: числовой ID чата или @имя_канала",
	KeyTipTgTest:         "Тестовое сообщение сейчас (без учёта расписания и «Отправлять»), не чаще раза в 10 с",
	KeyTipVbPage:          "Уведомления о тревогах в Viber: токен бота, получатели, расписание, события",
	KeyTipVbEnabled:       "Служба отправляет сообщения только в окне расписания",
	KeyTipVbToken:         "Токен бота Viber. Только запись: сохранённый токен не показывается; пусто — не менять",
	KeyTipVbClearToken:    "Удалить сохранённый токен при сохранении",
	KeyTipVbRecipients:    "ID пользователя Viber (без пробелов, до 64 символов) и имя, до 20 получателей",
	KeyTipVbDay:           "День недели, в который отправляются уведомления",
	KeyTipVbFrom:          "Начало окна отправки, местное время компьютера службы",
	KeyTipVbTo:            "Конец окна отправки (позже начала)",
	KeyTipVbEvent:         "Отправлять сообщение об этом событии",
	KeyTipVbReminder:      "Сводка нерешённых проблем каждые N минут в окне расписания",
	KeyTipVbTestTo:        "Кому отправить тест: одному получателю или всем",
	KeyTipVbTest:          "Тестовое сообщение сейчас (без учёта расписания и «Отправлять»), не чаще раза в 10 с",
	KeyXferExport:         "Экспорт…",
	KeyXferImport:         "Импорт…",
	KeyTipXferExport:      "Сохранить базы и общие настройки службы, а также звук и вид трея в файл (без паролей; получатели Viber — по флажку)",
	KeyTipXferImport:      "Загрузить настройки из файла: сначала показывается, что изменится",
	KeyXferFilter:         "Настройки EC (*.json)",
	KeyXferContainsLogins: "Файл содержит адреса серверов, логины SQL (без паролей) и пути к звуковым файлам. Храните его в надёжном месте.",
	KeyXferConflict:       "Конфигурация изменилась, проверьте заново",
	KeyXferExported:       "Настройки сохранены в файл %s",
	KeyXferWriteFailed:    "Не удалось записать файл: %s",
	KeyXferReadFailed:     "Не удалось прочитать файл: %s",
	KeyXferTooLarge:       "Файл слишком большой (больше %d КБ).",
	KeyXferBadFile:        "Это не файл настроек EC: %s",
	KeyXferTitle:          "Импорт настроек",
	KeyXferMerge:          "Объединить: добавить новые базы и обновить существующие",
	KeyXferReplace:        "Заменить: базы, которых нет в файле, будут удалены",
	KeyXferTrayCheck:      "Также звук и вид трея",
	KeyXferDo:             "Импортировать",
	KeyXferChecking:       "Проверка файла службой…",
	KeyXferImporting:      "Импорт…",
	KeyXferAdded:          "Будут добавлены (%d): %s",
	KeyXferUpdated:        "Будут обновлены (%d): %s",
	KeyXferRemoved:        "Будут удалены (%d): %s",
	KeyXferNeedPw:         "Нужно будет заново ввести пароль (%d): %s",
	KeyXferNoChanges:      "Базы данных не изменятся.",
	KeyXferNoTray:         "В файле нет настроек трея.",
	KeyXferTrayKept:       "Звук и вид трея не меняются.",
	KeyXferTraySummary:    "Трей: всплывающие уведомления %s, громкость %s, %s",
	KeyXferOn:             "вкл.",
	KeyXferOff:            "выкл.",
	KeyXferNoSoundFile:    "Внимание: звуковой файл не найден на этом компьютере, будет звучать тон: %s",
	KeyXferDone:           "Импорт выполнен.",
	KeyXferTrayApplied:    "Звук и вид трея применены",
	KeyXferOtherUser:      "Окно настроек запущено от имени другого пользователя Windows, чем трей: звук и вид трея не экспортируются и не импортируются (они хранятся в профиле пользователя трея).",
	KeyXferEnterPw:        "Введите пароль заново для «%s» и сохраните (осталось: %d).",
	KeyXferTrayFailed:     "Не удалось сохранить настройки трея: %s",
	KeyXferViberCheck:     "Получатели Viber",
	KeyXferViberWarnExport: "В файл попадут получатели Viber (токен бота не переносится). Храните файл в надёжном месте.",
	KeyXferViberWarnImport: "Получатели Viber из файла будут применены (токен бота не переносится).",
	KeyXferExportTitle:    "Экспорт настроек",
	KeyTipXferMerge:       "Базы с тем же идентификатором обновляются, новые добавляются, остальные остаются",
	KeyTipXferReplace:     "Список баз становится таким, как в файле",
	KeyTipXferTray:        "Всплывающие уведомления, звук и режим окна этого пользователя",
	KeyTipXferViber:       "Несекретные получатели Viber в файле (токен бота никогда не переносится)",
	KeyTipXferDo:          "Выполнить импорт, как показано выше",
	KeyTipXferPreview:     "Что изменит импорт",
}
