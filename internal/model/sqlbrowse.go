package model

import (
	"net"
	"strconv"
	"strings"
	"unicode"
)

// Phase 9 (connection wizard, EC SQL part; PLAN §12 p. 96, CHK-SQL-06):
// SQL Server instances found through the SQL Server Browser service
// (SSRP over UDP 1434). Driver-independent request / result for the IPC
// command sql_browse (EC Service -> EC Tray).

// SQL Browser limits.
const (
	// DefaultSQLBrowseTimeoutMs: how long responses are collected.
	DefaultSQLBrowseTimeoutMs = 2000
	// MinSQLBrowseTimeoutMs / MaxSQLBrowseTimeoutMs bound TimeoutMs.
	MinSQLBrowseTimeoutMs = 200
	MaxSQLBrowseTimeoutMs = 15000
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
	// Responders is the number of distinct addresses that answered.
	Responders int `json:"responders"`
	// Truncated: more than MaxSQLBrowseInstances instances answered.
	Truncated bool `json:"truncated"`
	// Partial: the list may be incomplete — Truncated, malformed answers
	// were dropped (Malformed), or a unicast host did not answer on all
	// its addresses before the timeout. A broadcast always runs to the
	// timeout; that alone is not partial.
	Partial   bool `json:"partial"`
	Malformed int  `json:"malformed,omitempty"`
}

// Timeout returns TimeoutMs or the default.
func (r SQLBrowseRequest) Timeout() int {
	if r.TimeoutMs == 0 {
		return DefaultSQLBrowseTimeoutMs
	}
	return r.TimeoutMs
}

// Validate checks the request; errors are *FieldError (host, instance,
// timeoutMs).
func (r SQLBrowseRequest) Validate() error {
	if r.TimeoutMs != 0 && (r.TimeoutMs < MinSQLBrowseTimeoutMs || r.TimeoutMs > MaxSQLBrowseTimeoutMs) {
		return fieldErr("timeoutMs", ValRange, "timeoutMs out of range", strconv.Itoa(MinSQLBrowseTimeoutMs),
			strconv.Itoa(MaxSQLBrowseTimeoutMs), strconv.Itoa(r.TimeoutMs))
	}
	if r.Limit < 0 || r.Limit > MaxSQLBrowseInstances {
		return fieldErr("limit", ValRange, "limit out of range", "0", strconv.Itoa(MaxSQLBrowseInstances), strconv.Itoa(r.Limit))
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
