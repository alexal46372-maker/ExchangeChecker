package model

import (
	"net"
	"strconv"
	"strings"
	"unicode"
)

// Phase 9 (connection wizard, EC SQL part; PLAN §12 p. 105, CHK-SQL-06):
// SQL Server instances found through the SQL Server Browser service
// (SSRP over UDP 1434). Driver-independent request / result for the IPC
// command sql_browse (EC Service -> EC Tray).

// SQL Browser limits.
const (
	// DefaultSQLBrowseTimeoutMs: how long responses are collected (phase
	// 11: 3 s, the SSRP request is sent 3 times 300 ms apart).
	DefaultSQLBrowseTimeoutMs = 3000
	// MinSQLBrowseTimeoutMs / MaxSQLBrowseTimeoutMs bound TimeoutMs.
	MinSQLBrowseTimeoutMs = 500
	MaxSQLBrowseTimeoutMs = 5000
	// MaxSQLSubnetTimeoutMs / DefaultSQLSubnetTimeoutMs: subnet mode (TCP
	// scan of up to 254 addresses) may run up to 15 s (architect, phase 11);
	// subnet runs only when requested explicitly, never for mode "".
	MaxSQLSubnetTimeoutMs     = 15000
	DefaultSQLSubnetTimeoutMs = 15000
	// MaxSQLBrowseInstances caps the instances of one result (Truncated).
	MaxSQLBrowseInstances = 256
	// MaxSQLBrowseHostLen: host name / IP literal length.
	MaxSQLBrowseHostLen = 255
	// MaxSQLInstanceNameLen: SQL Server instance names have <= 16 characters.
	MaxSQLInstanceNameLen = 16
)

// SQLBrowseRequest asks the SQL Server Browser for instances: Host empty
// = broadcast on the local network (SSRP CLNT_BCAST_EX); Host set =
// unicast to that host (CLNT_UCAST_EX), Host + Instance = only that
// instance (CLNT_UCAST_INST, e.g. to learn its TCP port).
type SQLBrowseRequest struct {
	Host      string `json:"host,omitempty"`
	Instance  string `json:"instance,omitempty"`
	TimeoutMs int    `json:"timeoutMs,omitempty"` // 0 = DefaultSQLBrowseTimeoutMs
	// Limit caps the instances (0 = MaxSQLBrowseInstances; 1..256).
	Limit int `json:"limit,omitempty"`
	// Mode (phase 11, PLAN §12 p. 160): local | broadcast | subnet | host;
	// "" = host when Host is set, else broadcast + local.
	Mode string `json:"mode,omitempty"`
	// Port: TCP port probed in host / subnet mode (0 = 1433).
	Port int `json:"port,omitempty"`
	// CIDR: subnet mode network ("" = the /24 of the first IPv4
	// interface); IPv4 /24../30 only; sqlcheck also requires it to
	// overlap a local interface network.
	CIDR string `json:"cidr,omitempty"`
}

// ValidSQLBrowseCIDR reports whether c is an IPv4 network /24../30.
func ValidSQLBrowseCIDR(c string) bool {
	ip, n, err := net.ParseCIDR(c)
	if err != nil || ip.To4() == nil {
		return false
	}
	ones, bits := n.Mask.Size()
	return bits == 32 && ones >= 24 && ones <= 30
}

// Browse modes (SQLBrowseRequest.Mode).
const (
	SQLBrowseAuto      = ""
	SQLBrowseLocal     = "local"     // this computer: registry, SQLBrowser service, 127.0.0.1:1434
	SQLBrowseBroadcast = "broadcast" // SSRP broadcast on every IPv4 interface + local
	SQLBrowseSubnet    = "subnet"    // TDS PRELOGIN to TCP port on every /24 address of each interface
	SQLBrowseHost      = "host"      // SSRP unicast + TCP PRELOGIN probe of Host
)

// EffectiveMode resolves SQLBrowseAuto.
func (r SQLBrowseRequest) EffectiveMode() string {
	if r.Mode != SQLBrowseAuto {
		return r.Mode
	}
	if r.Host != "" {
		return SQLBrowseHost
	}
	return SQLBrowseBroadcast
}

// SQLServerInstance is one instance announced by a SQL Server Browser.
type SQLServerInstance struct {
	// Host is the ServerName the browser announced (NetBIOS name).
	Host string `json:"host"`
	// Address is the IP address the response came from.
	Address string `json:"address"`
	// Instance is the instance name (MSSQLSERVER = default instance).
	Instance string `json:"instance"`
	Version  string `json:"version,omitempty"`
	// Source: browser (SSRP) | local (registry) | tcp (PRELOGIN probe).
	Source string `json:"source,omitempty"`
	// TCPPort is the TCP port (0: TCP disabled or not announced).
	TCPPort     int    `json:"tcpPort,omitempty"`
	IsClustered bool   `json:"isClustered,omitempty"`
	NamedPipe   string `json:"namedPipe,omitempty"`
}

// DefaultSQLInstance is the instance name of a default instance.
const DefaultSQLInstance = "MSSQLSERVER"

// Server returns the Profile.Server value for the instance: "host" for
// the default instance, else "host\instance" (Address when Host is
// empty).
func (i SQLServerInstance) Server() string {
	h := i.Host
	if h == "" {
		h = i.Address
	}
	if i.Instance == "" || strings.EqualFold(i.Instance, DefaultSQLInstance) {
		return h
	}
	return h + `\` + i.Instance
}

// SQLBrowseResult is the answer of sql_browse.
type SQLBrowseResult struct {
	Instances []SQLServerInstance `json:"instances"`
	// Responders is the number of distinct ip:port sources that answered.
	Responders int `json:"responders"`
	// Truncated: more than MaxSQLBrowseInstances instances answered.
	Truncated bool `json:"truncated"`
	// Partial: the list may be incomplete — Truncated, malformed answers
	// were dropped (Malformed), or a unicast host did not answer on all
	// its addresses before the timeout. A broadcast always runs to the
	// timeout; that alone is not partial.
	Partial   bool `json:"partial"`
	Malformed int  `json:"malformed,omitempty"`
	// Ignored: answers from addresses no request went to (addressed mode).
	Ignored int `json:"ignored,omitempty"`
	// Phase 11 diagnostics (PLAN §12 p. 162): SSRP datagrams sent, send
	// errors (count + at most MaxSQLBrowseSendErrors details), answers
	// accepted, local computer, TCP probe and advice keys (sql.browse_*).
	Mode           string              `json:"mode,omitempty"`
	Sent           int                 `json:"sent"`
	SendErrorCount int                 `json:"sendErrorCount"`
	SendErrors     []SQLBrowseSendErr  `json:"sendErrors,omitempty"`
	Responded      int                 `json:"responded"`
	Local          *SQLBrowseLocalInfo `json:"local,omitempty"`
	TCPProbe       *SQLBrowseTCPInfo   `json:"tcpProbe,omitempty"`
	Advice         []string            `json:"advice,omitempty"`
}

// Timeout returns TimeoutMs or the default.
func (r SQLBrowseRequest) Timeout() int {
	if r.TimeoutMs == 0 {
		if r.EffectiveMode() == SQLBrowseSubnet {
			return DefaultSQLSubnetTimeoutMs
		}
		return DefaultSQLBrowseTimeoutMs
	}
	return r.TimeoutMs
}

// MaxTimeoutMs is the upper TimeoutMs bound of the request's mode.
func (r SQLBrowseRequest) MaxTimeoutMs() int {
	if r.EffectiveMode() == SQLBrowseSubnet {
		return MaxSQLSubnetTimeoutMs
	}
	return MaxSQLBrowseTimeoutMs
}

// ClampSQLBrowseTimeoutMsFor is ClampSQLBrowseTimeoutMs with the subnet
// bound (15 s) for mode subnet (proposal for the IPC layer).
func ClampSQLBrowseTimeoutMsFor(mode string, ms int) int {
	if mode != SQLBrowseSubnet {
		return ClampSQLBrowseTimeoutMs(ms)
	}
	switch {
	case ms <= 0:
		return DefaultSQLSubnetTimeoutMs
	case ms < IPCSQLBrowseMinTimeoutMs:
		return IPCSQLBrowseMinTimeoutMs
	case ms > MaxSQLSubnetTimeoutMs:
		return MaxSQLSubnetTimeoutMs
	}
	return ms
}

// Validate checks the request; errors are *FieldError (host, instance,
// timeoutMs).
func (r SQLBrowseRequest) Validate() error {
	if r.TimeoutMs != 0 && (r.TimeoutMs < MinSQLBrowseTimeoutMs || r.TimeoutMs > r.MaxTimeoutMs()) {
		return fieldErr("timeoutMs", ValRange, "timeoutMs out of range", strconv.Itoa(MinSQLBrowseTimeoutMs),
			strconv.Itoa(r.MaxTimeoutMs()), strconv.Itoa(r.TimeoutMs))
	}
	if r.Limit < 0 || r.Limit > MaxSQLBrowseInstances {
		return fieldErr("limit", ValRange, "limit out of range", "0", strconv.Itoa(MaxSQLBrowseInstances), strconv.Itoa(r.Limit))
	}
	switch r.Mode {
	case SQLBrowseAuto, SQLBrowseLocal, SQLBrowseBroadcast, SQLBrowseSubnet:
	case SQLBrowseHost:
		if r.Host == "" {
			return fieldErr("host", ValRequired, "host is required in host mode")
		}
	default:
		return fieldErr("mode", ValEnum, "mode: local, broadcast, subnet or host", "local, broadcast, subnet, host")
	}
	if r.CIDR != "" && (r.EffectiveMode() != SQLBrowseSubnet || !ValidSQLBrowseCIDR(r.CIDR)) {
		return fieldErr("cidr", ValSQLBrowseCIDR, "cidr: IPv4 /24../30 network, subnet mode only")
	}
	if r.Port < 0 || r.Port > 65535 {
		return fieldErr("port", ValRange, "port out of range", "0", "65535", strconv.Itoa(r.Port))
	}
	if r.Host != "" && !ValidSQLBrowseHost(r.Host) {
		return fieldErr("host", ValSQLBrowseHost, "host must be a host name or an IPv4/IPv6 address")
	}
	if r.Instance != "" {
		if r.Host == "" {
			return fieldErr("host", ValRequired, "host is required with an instance")
		}
		if !ValidSQLInstanceName(r.Instance) {
			return fieldErr("instance", ValSQLInstanceName, "invalid instance name", strconv.Itoa(MaxSQLInstanceNameLen))
		}
	}
	return nil
}

// ValidSQLBrowseHost: an IP literal or a DNS / NetBIOS host name
// (letters, digits, '-', '_', '.'; at most MaxSQLBrowseHostLen bytes).
func ValidSQLBrowseHost(h string) bool {
	if h == "" || len(h) > MaxSQLBrowseHostLen {
		return false
	}
	if net.ParseIP(h) != nil {
		return true
	}
	for _, r := range h {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return h[0] != '.' && h[0] != '-'
}

// ValidSQLInstanceName: 1..16 characters, first a letter, '_' or '#',
// then letters, digits, '_', '#', '$' (SQL Server instance name rules).
func ValidSQLInstanceName(s string) bool {
	n := 0
	for i, r := range s {
		n++
		letter := unicode.IsLetter(r) || r == '_' || r == '#'
		if i == 0 && !letter || i > 0 && !(letter || unicode.IsDigit(r) || r == '$') {
			return false
		}
	}
	return n >= 1 && n <= MaxSQLInstanceNameLen
}
