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
)

var sqlWizardMessages = map[string]LocalizedText{
	MsgSQLAutoPickOK: {"Найден один подходящий вариант — он применён автоматически. Проверьте пример строк.",
		"One matching option was found and applied automatically. Check the sample rows."},
	MsgSQLAutoPickNone:          {"Подходящие таблицы не найдены — укажите таблицы вручную.", "No matching tables were found — set the tables manually."},
	MsgSQLAutoPickMultiplePlans: {"В базе несколько планов обмена — выберите нужный.", "The database has several exchange plans — choose one."},
	MsgSQLAutoPickCounters: {"Времена обмена в базе не найдены, предложены счётчики сообщений плана обмена — подтвердите выбор.",
		"No exchange times were found; the exchange plan message counters are proposed — confirm the choice."},
	MsgSQLAutoPickIncomplete: {"Лучший вариант неполный (нет времени отправки или приёма, нет точек или поиск прерван) — выберите вручную.",
		"The best option is incomplete (no send or recv time, no points, or the search was cut short) — choose manually."},
	MsgSQLAutoPickLowScore:  {"Уверенности недостаточно — выберите вариант из списка.", "Not confident enough — choose an option from the list."},
	MsgSQLAutoPickAmbiguous: {"Несколько похожих вариантов — выберите нужный.", "Several similar options — choose one."},
	ValSQLBrowseHost:        {"Поле «{0}»: укажите имя компьютера или IP-адрес.", "Field \"{0}\": enter a computer name or an IP address."},
	ValSQLInstanceName: {"Поле «{0}»: имя экземпляра — до {2} символов, начинается с буквы, _ или #.",
		"Field \"{0}\": the instance name has up to {2} characters and starts with a letter, _ or #."},
	ValSQLPrincipal: {"Поле «{0}»: недопустимое имя ({2}). Windows: ДОМЕН\\ИМЯ или ДОМЕН\\КОМПЬЮТЕР$; SQL-логин без «\\»; до 128 символов, без управляющих символов и пробелов по краям.",
		"Field \"{0}\": invalid name ({2}). Windows: DOMAIN\\NAME or DOMAIN\\COMPUTER$; a SQL login without '\\'; up to 128 characters, no control characters, no leading or trailing spaces."},
}

func init() {
	for k, v := range sqlWizardMessages {
		sqlMessages[k] = v
	}
}

// SQLWizardMessageKeys lists the phase 9 sql.* keys (auto-pick reasons).
func SQLWizardMessageKeys() []string {
	return []string{MsgSQLAutoPickOK, MsgSQLAutoPickNone, MsgSQLAutoPickMultiplePlans, MsgSQLAutoPickCounters,
		MsgSQLAutoPickIncomplete, MsgSQLAutoPickLowScore, MsgSQLAutoPickAmbiguous}
}

// SQLWizardValidationKeys lists the phase 9 validation.sql_* keys.
func SQLWizardValidationKeys() []string {
	return []string{ValSQLBrowseHost, ValSQLInstanceName, ValSQLPrincipal}
}
