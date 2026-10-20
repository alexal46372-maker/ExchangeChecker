package model

// Phase 11 (PLAN §12 p. 160-164, CHK-SQL-10..13): diagnostics of server
// discovery ("the wizard finds no servers"), local instances, TCP PRELOGIN
// probe, step-by-step server check, progress events.

// MaxSQLBrowseSendErrors caps SQLBrowseResult.SendErrors (the count is
// SendErrorCount).
const MaxSQLBrowseSendErrors = 16

// SQLBrowseSendErr is one failed SSRP send (or socket bind).
type SQLBrowseSendErr struct {
	// Local: local address the socket was bound to ("" = unbound).
	Local string `json:"local,omitempty"`
	// Target: destination ip:port.
	Target string `json:"target"`
	// Error: the OS error text (no credentials ever pass here).
	Error string `json:"error"`
}

// SQLBrowseLocalInfo describes this computer (Windows only).
type SQLBrowseLocalInfo struct {
	// Supported: false off Windows (nothing below is filled).
	Supported bool `json:"supported"`
	// Installed: instance names from the registry (InstalledInstances,
	// 64- and 32-bit views).
	Installed []string `json:"installed"`
	// BrowserState: SQLBrowser service state: running | stopped |
	// start_pending | stop_pending | paused | not_installed | unknown.
	BrowserState string `json:"browserState"`
	// BrowserAnswered: 127.0.0.1:1434 answered.
	BrowserAnswered bool `json:"browserAnswered"`
	// Error: registry / SCM read error text, if any.
	Error string `json:"error,omitempty"`
}

// SQLBrowser service states (SQLBrowseLocalInfo.BrowserState).
const (
	SQLBrowserRunning      = "running"
	SQLBrowserStopped      = "stopped"
	SQLBrowserStartPending = "start_pending"
	SQLBrowserStopPending  = "stop_pending"
	SQLBrowserPaused       = "paused"
	SQLBrowserNotInstalled = "not_installed"
	SQLBrowserUnknown      = "unknown"
)

// SQLBrowseTCPInfo summarizes the TCP PRELOGIN probe.
type SQLBrowseTCPInfo struct {
	Port int `json:"port"`
	// Probed addresses, Open (TCP connect ok), Prelogin (TDS answer ok).
	Probed   int `json:"probed"`
	Open     int `json:"open"`
	Prelogin int `json:"prelogin"`
	// Truncated: the scan was cut (ctx ended).
	Truncated bool `json:"truncated,omitempty"`
	// CIDR scanned (subnet mode) and the other local /24 networks the
	// wizard may scan next (one /24 per request, <= 254 addresses).
	CIDR    string   `json:"cidr,omitempty"`
	Subnets []string `json:"subnets,omitempty"`
}

// Subnet probe limits.
const (
	MaxSQLSubnetHosts       = 254 // one /24 per scan (architect)
	SQLSubnetConcurrency    = 32
	SQLProbeTimeoutMs       = 300
	SQLBrowseSends          = 3
	SQLBrowseSendIntervalMs = 300
)

// SQLBrowseProgress (json done/total/found) is one progress event of a browse (callback, called
// synchronously from the browsing goroutine; it must not block).
type SQLBrowseProgress struct {
	// Phase: local | ssrp | subnet | host | done.
	Phase string `json:"phase"`
	Done  int    `json:"done"`
	Total int    `json:"total"`
	// Found: instances found so far (deduplicated).
	Found int `json:"found"`
}

// Advice keys of SQLBrowseResult.Advice (RU/EN in SQLMessage).
const (
	MsgSQLBrowseAdviceEnableBrowser = "sql.browse_advice_enable_browser"
	MsgSQLBrowseAdviceOpenUDP1434   = "sql.browse_advice_open_udp1434"
	MsgSQLBrowseAdviceEnterServer   = "sql.browse_advice_enter_server"
	MsgSQLBrowseAdvicePort1433      = "sql.browse_advice_port_1433"
	MsgSQLBrowseAdviceSendFailed    = "sql.browse_advice_send_failed"
	MsgSQLBrowseAdviceTCPDisabled   = "sql.browse_advice_tcp_disabled"
)

// Server check stages (SQLCheckStep.ID), in order; the check stops at the
// first failure. browser runs only for host\instance without a port;
// login/db/tables only with credentials.
const (
	SQLStepParse    = "parse"
	SQLStepDNS      = "dns"
	SQLStepBrowser  = "browser"
	SQLStepTCP      = "tcp"
	SQLStepPrelogin = "prelogin"
	SQLStepTLS      = "tls" // encryption negotiated in PRELOGIN vs the profile
	SQLStepLogin    = "login"
	SQLStepDB       = "db"     // the profile database opens (HAS_DBACCESS)
	SQLStepTables   = "tables" // 1C exchange plan tables (_Node*) visible
)

// SQLCheckSteps lists the stage ids in order.
func SQLCheckSteps() []string {
	return []string{SQLStepParse, SQLStepDNS, SQLStepBrowser, SQLStepTCP, SQLStepPrelogin, SQLStepTLS, SQLStepLogin, SQLStepDB, SQLStepTables}
}

// SQLCheckStep is the result of one stage of a server check.
type SQLCheckStep struct {
	ID string `json:"id"`
	OK bool   `json:"ok"`
	Ms int64  `json:"ms"`
	// Code: model.SQLCode* of a failure; ErrorKey: its sql.diag_* text;
	// AdviceKey: the sql.check_advice_* text of the stage.
	Code      string `json:"code,omitempty"`
	ErrorKey  string `json:"errorKey,omitempty"`
	AdviceKey string `json:"adviceKey,omitempty"`
	// Detail: addresses, port, version, encryption, or the error text
	// (never the login, the password or a connection string).
	Detail string `json:"detail,omitempty"`
}

// SQLServerCheckResult is the answer of a step-by-step server check.
type SQLServerCheckResult struct {
	OK       bool   `json:"ok"`
	Host     string `json:"host"`
	Instance string `json:"instance,omitempty"`
	Port     int    `json:"port"`
	Address  string `json:"address,omitempty"`
	Version  string `json:"version,omitempty"`
	// Encryption: PRELOGIN answer of the server: off | on | not_supported | required.
	Encryption string         `json:"encryption,omitempty"`
	Steps      []SQLCheckStep `json:"steps"`
	// Diagnosis of the failed stage (nil when OK).
	Diagnosis *SQLDiagnosis `json:"diagnosis,omitempty"`
}

// Advice keys of the check stages.
const (
	MsgSQLCheckAdviceParse    = "sql.check_advice_parse"
	MsgSQLCheckAdviceDNS      = "sql.check_advice_dns"
	MsgSQLCheckAdviceBrowser  = "sql.check_advice_browser"
	MsgSQLCheckAdviceTCP      = "sql.check_advice_tcp"
	MsgSQLCheckAdvicePrelogin = "sql.check_advice_prelogin"
	MsgSQLCheckAdviceTLS      = "sql.check_advice_tls"
	MsgSQLCheckAdviceLogin    = "sql.check_advice_login"
	MsgSQLCheckAdviceDB       = "sql.check_advice_db"
	MsgSQLCheckAdviceTables   = "sql.check_advice_tables"
)

// SQLCheckAdviceKey returns the advice key of a stage.
func SQLCheckAdviceKey(step string) string { return "sql.check_advice_" + step }

// ValSQLBrowseCIDR: SQLBrowseRequest.CIDR is not an IPv4 /24../30 network.
const ValSQLBrowseCIDR = "validation.sql_browse_cidr"

var sqlBrowseDiagMessages = map[string]LocalizedText{
	MsgSQLBrowseAdviceEnableBrowser: {"Запустите на сервере службу «SQL Server Browser» (тип запуска — автоматически): без неё именованные экземпляры не находятся.",
		"Start the \"SQL Server Browser\" service on the server (startup type automatic): named instances are not found without it."},
	MsgSQLBrowseAdviceOpenUDP1434: {"Откройте в брандмауэре Windows сервера входящий UDP-порт 1434 (SQL Server Browser); широковещательный поиск не проходит через маршрутизаторы.",
		"Allow inbound UDP port 1434 (SQL Server Browser) in the server's Windows Firewall; broadcast discovery does not cross routers."},
	MsgSQLBrowseAdviceEnterServer: {"Введите имя или IP-адрес сервера вручную и нажмите «Проверить».",
		"Enter the server name or IP address manually and press \"Check\"."},
	MsgSQLBrowseAdvicePort1433: {"Экземпляр по умолчанию слушает TCP 1433: откройте этот порт в брандмауэре сервера или укажите порт явно («сервер,порт»).",
		"A default instance listens on TCP 1433: open this port in the server firewall or enter the port explicitly (\"server,port\")."},
	MsgSQLBrowseAdviceSendFailed: {"Запрос поиска не удалось отправить по сети (см. ошибки отправки): проверьте сетевое подключение и брандмауэр этого компьютера.",
		"The discovery request could not be sent (see the send errors): check this computer's network connection and firewall."},
	MsgSQLCheckAdviceParse:    {"Укажите сервер как «имя», «имя\\экземпляр» или «имя,порт» (без np:, lpc:, UNC).", "Enter the server as \"name\", \"name\\instance\" or \"name,port\" (no np:, lpc:, UNC)."},
	MsgSQLCheckAdviceDNS:      {"Имя сервера не найдено в DNS: проверьте написание или укажите IP-адрес.", "The server name is not found in DNS: check the spelling or enter the IP address."},
	MsgSQLCheckAdviceBrowser:  {"Порт экземпляра не получен от SQL Server Browser: запустите службу «SQL Server Browser», откройте UDP 1434 или укажите порт явно («имя,порт»).", "The instance port was not received from the SQL Server Browser: start the \"SQL Server Browser\" service, open UDP 1434 or enter the port (\"name,port\")."},
	MsgSQLCheckAdviceTCP:      {"TCP-порт сервера недоступен: проверьте, что служба SQL Server запущена, протокол TCP/IP включён и порт открыт в брандмауэре сервера.", "The server TCP port is not reachable: check that SQL Server runs, TCP/IP is enabled and the port is open in the server firewall."},
	MsgSQLCheckAdvicePrelogin: {"По этому порту отвечает не SQL Server (нет ответа TDS PRELOGIN): проверьте номер порта.", "Something other than SQL Server answers on this port (no TDS PRELOGIN answer): check the port number."},
	MsgSQLCheckAdviceTLS:      {"Шифрование профиля не совпадает с сервером: выберите «Шифрование: Без шифрования» или включите TLS на сервере; для SQL Server 2008 — «Разрешить TLS 1.0».", "The profile encryption does not match the server: choose \"Encryption: No encryption\" or enable TLS on the server; for SQL Server 2008 \"Allow TLS 1.0\"."},
	MsgSQLCheckAdviceLogin:    {"Вход отклонён: проверьте логин и пароль, смешанный режим проверки подлинности и права имени входа (скрипт «Выдать права»).", "The login was rejected: check the login and password, mixed mode authentication and the login's rights (\"Grant access\" script)."},
	MsgSQLCheckAdviceDB:       {"База недоступна этому логину: выберите базу из списка или выполните скрипт «Выдать права».", "The database is not available to this login: pick it from the list or run the \"Grant access\" script."},
	MsgSQLCheckAdviceTables:   {"В базе не видны таблицы планов обмена 1С (_Node…): проверьте, что выбрана база 1С, и права db_datareader.", "No 1C exchange plan tables (_Node…) are visible in the database: check that a 1C database is selected and the db_datareader rights."},
	ValSQLBrowseCIDR:          {"Поле «{0}»: укажите сеть IPv4 вида 192.168.1.0/24 (от /24 до /30, не более 254 адресов) одного из сетевых интерфейсов этого компьютера.", "Field \"{0}\": enter an IPv4 network like 192.168.1.0/24 (/24 to /30, at most 254 addresses) of one of this computer's network interfaces."},
	MsgSQLBrowseAdviceTCPDisabled: {"У локального экземпляра не задан TCP-порт: включите протокол TCP/IP в SQL Server Configuration Manager и перезапустите службу SQL Server.",
		"The local instance has no TCP port: enable the TCP/IP protocol in SQL Server Configuration Manager and restart the SQL Server service."},
}

func init() {
	for k, v := range sqlBrowseDiagMessages {
		sqlMessages[k] = v
	}
}

// SQLBrowseAdviceKeys lists the phase 11 advice keys.
func SQLBrowseAdviceKeys() []string {
	return []string{MsgSQLBrowseAdviceEnableBrowser, MsgSQLBrowseAdviceOpenUDP1434, MsgSQLBrowseAdviceEnterServer,
		MsgSQLBrowseAdvicePort1433, MsgSQLBrowseAdviceSendFailed, MsgSQLBrowseAdviceTCPDisabled,
		MsgSQLCheckAdviceParse, MsgSQLCheckAdviceDNS, MsgSQLCheckAdviceBrowser, MsgSQLCheckAdviceTCP,
		MsgSQLCheckAdvicePrelogin, MsgSQLCheckAdviceTLS, MsgSQLCheckAdviceLogin, MsgSQLCheckAdviceDB, MsgSQLCheckAdviceTables}
}
