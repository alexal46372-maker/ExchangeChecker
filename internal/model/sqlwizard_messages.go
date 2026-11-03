package model

// Phase 9 texts (connection wizard): auto-pick reasons (sql.autopick_*)
// and validation reasons of the wizard requests. They join the SQL text
// table (SQLMessage, SQLMessages, FormatMessage) at init.

// Validation keys of the phase 9 requests ({0} = field).
const (
	// ValSQLBrowseHost: SQLBrowseRequest.Host is not a host name / IP.
	ValSQLBrowseHost = "validation.sql_browse_host"
	// ValSQLInstanceName: {2} = maximum length (16).
	ValSQLInstanceName = "validation.sql_instance_name"
	// ValSQLPrincipal: {2} = reason (SQLPrincipal* constants).
	ValSQLPrincipal = "validation.sql_principal"
	// ValSQLGrantPassword: {2} = reason (too_long / control_char).
	ValSQLGrantPassword = "validation.sql_grant_password"
)

var sqlWizardMessages = map[string]string{
	MsgSQLAutoPickOK: "Найден один подходящий вариант — он применён автоматически. Проверьте пример строк.",

	MsgSQLAutoPickNone:          "Подходящие таблицы не найдены — укажите таблицы вручную.",
	MsgSQLAutoPickMultiplePlans: "В базе несколько планов обмена — выберите нужный.",
	MsgSQLAutoPickCounters:      "Времена обмена в базе не найдены, предложены счётчики сообщений плана обмена — подтвердите выбор.",

	MsgSQLAutoPickCountersOK: "Времена обмена в базе не найдены (1С 8.1): применены счётчики сообщений планов обмена — время фиксируется службой при их изменении.",

	MsgSQLAutoPickIncomplete: "Лучший вариант неполный (нет времени отправки или приёма, нет точек или поиск прерван) — выберите вручную.",

	MsgSQLAutoPickLowScore:  "Уверенности недостаточно — выберите вариант из списка.",
	MsgSQLAutoPickAmbiguous: "Несколько похожих вариантов — выберите нужный.",
	MsgSQLCandExchangePlan:  "Таблица плана обмена 1С",
	MsgSQLCandSendRecv:      "Есть время отправки и приёма",
	MsgSQLCandCounters:      "Только счётчики сообщений, без времени",
	MsgSQLCandIncomplete:    "Неполный вариант (нет времени отправки/приёма или точек)",
	MsgSQLCandLowScore:      "Низкая уверенность",
	ValSQLGrantPassword:     "Поле «{0}»: недопустимый пароль ({2}): до 128 символов, без управляющих символов.",

	ValSQLBrowseHost:   "Поле «{0}»: укажите имя компьютера или IP-адрес.",
	ValSQLInstanceName: "Поле «{0}»: имя экземпляра — до {2} символов, начинается с буквы, _ или #.",

	ValSQLPrincipal: "Поле «{0}»: недопустимое имя ({2}). Windows: ДОМЕН\\ИМЯ или ДОМЕН\\КОМПЬЮТЕР$; SQL-логин без «\\»; до 128 символов, без управляющих символов и пробелов по краям.",
}

func init() {
	for k, v := range sqlWizardMessages {
		sqlMessages[k] = v
	}
}

// SQLWizardMessageKeys lists the phase 9 sql.* keys (auto-pick reasons,
// candidate reasons, diagnosis titles sql.diag_*).
func SQLWizardMessageKeys() []string {
	return append(append(append(SQLDiagnosisKeys(), SQLCandidateReasonKeys()...), SQLBrowseAdviceKeys()...), MsgSQLAutoPickOK, MsgSQLAutoPickNone, MsgSQLAutoPickMultiplePlans, MsgSQLAutoPickCounters, MsgSQLAutoPickCountersOK,
		MsgSQLAutoPickIncomplete, MsgSQLAutoPickLowScore, MsgSQLAutoPickAmbiguous)
}

// SQLWizardValidationKeys lists the phase 9 validation.sql_* keys.
func SQLWizardValidationKeys() []string {
	return []string{ValSQLBrowseHost, ValSQLInstanceName, ValSQLPrincipal, ValSQLGrantPassword, ValSQLBrowseCIDR}
}
