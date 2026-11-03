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
	// start_pending | stop_pending | paused | disabled (start type
	// SERVICE_DISABLED, wins over the state) | not_installed | unknown.
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
	// SQLBrowserDisabled: the service start type is SERVICE_DISABLED (4),
	// whatever its state (it cannot be started until re-enabled).
	SQLBrowserDisabled = "disabled"
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
	// CIDR: primary /24 reported for the scan (explicit request, or the
	// first local /24 when CIDR was empty). Subnets: the other local /24s
	// included in the same empty-CIDR Browse (CHK-SQL-15); empty when an
	// explicit CIDR was requested.
	CIDR    string   `json:"cidr,omitempty"`
	Subnets []string `json:"subnets,omitempty"`
	// Narrowed: at least one scanned /24 belongs to an interface network
	// wider than /24 (only that interface's own /24 is scanned, not the
	// whole wide net; advice sql.browse_advice_subnet_narrowed).
	Narrowed bool `json:"narrowed,omitempty"`
}

// Subnet probe limits.
const (
	// MaxSQLSubnetHosts: up to 8 local /24s (interfaceIPv4s cap) × 254.
	MaxSQLSubnetHosts    = 2032
	SQLSubnetConcurrency = 32
	// SQLProbeTimeoutMs: base dial/PRELOGIN budget (host mode uses 4×).
	SQLProbeTimeoutMs = 300
	// SQLSubnetProbeTimeoutMs: thorough per-host budget for subnet mode
	// (CHK-SQL-15). Aligns with host's 4×SQLProbeTimeoutMs so a cold ARP
	// that misses at 300 ms still succeeds in one Browse.
	SQLSubnetProbeTimeoutMs = 1200
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

// Advice keys of SQLBrowseResult.Advice (texts in SQLMessage).
const (
	MsgSQLBrowseAdviceEnableBrowser = "sql.browse_advice_enable_browser"
	MsgSQLBrowseAdviceOpenUDP1434   = "sql.browse_advice_open_udp1434"
	MsgSQLBrowseAdviceEnterServer   = "sql.browse_advice_enter_server"
	MsgSQLBrowseAdvicePort1433      = "sql.browse_advice_port_1433"
	MsgSQLBrowseAdviceSendFailed    = "sql.browse_advice_send_failed"
	MsgSQLBrowseAdviceTCPDisabled   = "sql.browse_advice_tcp_disabled"
	// MsgSQLBrowseAdviceBrowserDisabled: SQLBrowser start type Disabled.
	MsgSQLBrowseAdviceBrowserDisabled = "sql.browse_advice_browser_disabled"
	// MsgSQLBrowseAdviceSubnetNarrowed: the interface network is wider
	// than /24; only its own /24 was scanned.
	MsgSQLBrowseAdviceSubnetNarrowed = "sql.browse_advice_subnet_narrowed"
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

var sqlBrowseDiagMessages = map[string]string{
	MsgSQLBrowseAdviceEnableBrowser: "Запустите на сервере службу «SQL Server Browser» (тип запуска — автоматически): без неё именованные экземпляры не находятся.",

	MsgSQLBrowseAdviceOpenUDP1434: "Откройте в брандмауэре Windows сервера входящий UDP-порт 1434 (SQL Server Browser); широковещательный поиск не проходит через маршрутизаторы.",

	MsgSQLBrowseAdviceEnterServer: "Введите имя или IP-адрес сервера вручную и нажмите «Проверить».",

	MsgSQLBrowseAdvicePort1433: "Экземпляр по умолчанию слушает TCP 1433: откройте этот порт в брандмауэре сервера или укажите порт явно («сервер,порт»).",

	MsgSQLBrowseAdviceSendFailed: "Запрос поиска не удалось отправить по сети (см. ошибки отправки): проверьте сетевое подключение и брандмауэр этого компьютера.",

	MsgSQLCheckAdviceParse:            "Укажите сервер как «имя», «имя\\экземпляр» или «имя,порт» (без np:, lpc:, UNC).",
	MsgSQLCheckAdviceDNS:              "Имя сервера не найдено в DNS: проверьте написание или укажите IP-адрес.",
	MsgSQLCheckAdviceBrowser:          "Порт экземпляра не получен от SQL Server Browser: запустите службу «SQL Server Browser», откройте UDP 1434 или укажите порт явно («имя,порт»).",
	MsgSQLCheckAdviceTCP:              "TCP-порт сервера недоступен: проверьте, что служба SQL Server запущена, протокол TCP/IP включён и порт открыт в брандмауэре сервера.",
	MsgSQLCheckAdvicePrelogin:         "По этому порту отвечает не SQL Server (нет ответа TDS PRELOGIN): проверьте номер порта.",
	MsgSQLCheckAdviceTLS:              "Шифрование профиля не совпадает с сервером: выберите «Шифрование: Без шифрования» или включите TLS на сервере; для SQL Server 2008 — «Разрешить TLS 1.0».",
	MsgSQLCheckAdviceLogin:            "Вход отклонён: проверьте логин и пароль, смешанный режим проверки подлинности и права имени входа (скрипт «Выдать права»).",
	MsgSQLCheckAdviceDB:               "База недоступна этому логину: выберите базу из списка или выполните скрипт «Выдать права».",
	MsgSQLCheckAdviceTables:           "В базе не видны таблицы планов обмена 1С (_Node…): проверьте, что выбрана база 1С, и права db_datareader.",
	ValSQLBrowseCIDR:                  "Поле «{0}»: укажите сеть IPv4 вида 192.168.1.0/24 (от /24 до /30, не более 254 адресов) одного из сетевых интерфейсов этого компьютера.",
	MsgSQLBrowseAdviceBrowserDisabled: "Служба «SQL Server Browser» отключена (тип запуска «Отключена»): в «Службах» или SQL Server Configuration Manager задайте тип запуска «Автоматически» и запустите службу.",

	MsgSQLBrowseAdviceSubnetNarrowed: "Сеть интерфейса шире /24: в одном сканировании проверяются /24 каждого локального интерфейса (до 254 адресов на сеть), но не вся широкая сеть. Для другой части той же сети укажите её /24 вручную или введите имя сервера.",

	MsgSQLBrowseAdviceTCPDisabled: "У локального экземпляра не задан TCP-порт: включите протокол TCP/IP в SQL Server Configuration Manager и перезапустите службу SQL Server.",
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
		MsgSQLBrowseAdviceBrowserDisabled, MsgSQLBrowseAdviceSubnetNarrowed,
		MsgSQLCheckAdviceParse, MsgSQLCheckAdviceDNS, MsgSQLCheckAdviceBrowser, MsgSQLCheckAdviceTCP,
		MsgSQLCheckAdvicePrelogin, MsgSQLCheckAdviceTLS, MsgSQLCheckAdviceLogin, MsgSQLCheckAdviceDB, MsgSQLCheckAdviceTables}
}
