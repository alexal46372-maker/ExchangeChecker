package model

import (
	"encoding/json"
	"fmt"
	"net/mail"
	"strings"
	"time"
)

// Phase 13b (EC Service, CHK-CFG-09 / CHK-IPC-16 / CHK-SVC-08): email
// notifications. settings.email mirrors settings.viber (schedule, events);
// the SMTP password is in email.dpapi (DPAPI LocalMachine, DACL SY+BA),
// write-only via save_settings smtpPassword / clearSmtpPassword.

const (
	CmdEmailTest = "email_test"
	CapEmail     = "email"
	MsgEmailTest = "email_test"

	EmailTestInterval = 10 * time.Second

	EmailMaxRecipients = 20
	EmailMaxIDLen      = 64
	EmailMaxNameLen    = 64
	EmailMaxAddrLen    = 254
	EmailMaxHostLen    = 253
	EmailMaxFromLen    = 254
	EmailMinPort       = 1
	EmailMaxPort       = 65535
)

// TLS modes of settings.email.tlsMode.
const (
	EmailTLSNone     = "none"
	EmailTLSStartTLS = "starttls"
	EmailTLSImplicit = "tls" // implicit TLS (usually port 465)
)

const (
	MsgEmailNoPassword   = "notify.email_no_password"
	MsgEmailNoRecipients = "notify.email_no_recipients"
	MsgEmailNoHost       = "notify.email_no_host"
	MsgEmailNoFrom       = "notify.email_no_from"
	MsgEmailAuthFailed   = "notify.email_auth_failed"
	MsgEmailInvalidAddr  = "notify.email_invalid_addr"
	MsgEmailNetwork      = "notify.email_network"
	MsgEmailTLS          = "notify.email_tls"
	MsgEmailFailed       = "notify.email_failed"
	MsgEmailCanceled     = "notify.email_canceled"
	MsgEmailTimeout      = "notify.email_timeout"
	MsgEmailInvalid      = "notify.email_invalid"
	MsgEmailRateLimited  = "notify.email_rate_limited"
)

// EmailRecipient is one mailbox with a stable id (email_test by
// recipientId) and display name.
type EmailRecipient struct {
	ID   string `json:"id"`
	Addr string `json:"addr"`
	Name string `json:"name"`
}

// EmailSettings is settings.email.
type EmailSettings struct {
	Enabled bool   `json:"enabled"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	// TLSMode: none | starttls | tls (implicit).
	TLSMode string `json:"tlsMode"`
	From    string `json:"from"`
	// HasPassword is read-only (views): email.dpapi holds a password.
	HasPassword     bool             `json:"hasPassword"`
	Recipients      []EmailRecipient `json:"recipients"`
	Schedule        ViberSchedule    `json:"schedule"` // same semantics as Viber
	Events          []string         `json:"events"`   // ViberEvent* kinds
	Status          *ViberStatus     `json:"status,omitempty"`
	exportNoSecrets bool             `json:"-"`
}

// DefaultEmailSettings: disabled, port 587 starttls, Mon-Fri 08:30-17:00,
// all events.
func DefaultEmailSettings() EmailSettings {
	return EmailSettings{
		Port: 587, TLSMode: EmailTLSStartTLS,
		Recipients: []EmailRecipient{},
		Schedule:   ViberSchedule{Days: []int{1, 2, 3, 4, 5}, From: "08:30", To: "17:00"},
		Events:     ViberEventKinds(),
	}
}

// Stored drops read-only fields for settings.json.
func (e EmailSettings) Stored() EmailSettings {
	e.HasPassword, e.Status = false, nil
	if e.Recipients == nil {
		e.Recipients = []EmailRecipient{}
	}
	if e.Events == nil {
		e.Events = []string{}
	}
	if e.Schedule.Days == nil {
		e.Schedule.Days = []int{}
	}
	return e
}

// ForExport omits hasPassword / status (CHK-CFG-09 export).
func (e EmailSettings) ForExport() EmailSettings {
	e = e.Stored()
	e.exportNoSecrets = true
	return e
}

// MarshalJSON: views keep hasPassword; ForExport omits it and status.
func (e EmailSettings) MarshalJSON() ([]byte, error) {
	if e.exportNoSecrets {
		return json.Marshal(struct {
			Enabled    bool             `json:"enabled"`
			Host       string           `json:"host"`
			Port       int              `json:"port"`
			TLSMode    string           `json:"tlsMode"`
			From       string           `json:"from"`
			Recipients []EmailRecipient `json:"recipients"`
			Schedule   ViberSchedule    `json:"schedule"`
			Events     []string         `json:"events"`
		}{e.Enabled, e.Host, e.Port, e.TLSMode, e.From, e.Recipients, e.Schedule, e.Events})
	}
	type wire struct {
		Enabled     bool             `json:"enabled"`
		Host        string           `json:"host"`
		Port        int              `json:"port"`
		TLSMode     string           `json:"tlsMode"`
		From        string           `json:"from"`
		HasPassword bool             `json:"hasPassword"`
		Recipients  []EmailRecipient `json:"recipients"`
		Schedule    ViberSchedule    `json:"schedule"`
		Events      []string         `json:"events"`
		Status      *ViberStatus     `json:"status,omitempty"`
	}
	return json.Marshal(wire{e.Enabled, e.Host, e.Port, e.TLSMode, e.From, e.HasPassword, e.Recipients, e.Schedule, e.Events, e.Status})
}

// HasEvent reports whether kind is selected.
func (e EmailSettings) HasEvent(kind string) bool {
	for _, k := range e.Events {
		if k == kind {
			return true
		}
	}
	return false
}

// InWindow uses the same schedule rules as Viber.
func (e EmailSettings) InWindow(t time.Time) bool {
	return (ViberSettings{Schedule: e.Schedule}).InWindow(t)
}

// ValidEmailID: 1..64 printable ASCII without spaces (like Viber id).
func ValidEmailID(s string) bool { return ValidViberID(s) }

// ValidEmailAddr: RFC 5322 mailbox (net/mail), length ≤ 254.
func ValidEmailAddr(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > EmailMaxAddrLen {
		return false
	}
	_, err := mail.ParseAddress(s)
	return err == nil
}

// ValidSMTPPassword: non-empty, ≤ 256 bytes, no NULs.
func ValidSMTPPassword(p string) bool {
	return p != "" && len(p) <= 256 && !strings.ContainsRune(p, 0)
}

// Validate checks settings.email (FieldError paths under "email.").
func (e EmailSettings) Validate() error {
	const p = "email."
	switch e.TLSMode {
	case "", EmailTLSNone, EmailTLSStartTLS, EmailTLSImplicit:
	default:
		return fieldErr(p+"tlsMode", ValEnum, "email: tlsMode must be none, starttls or tls", e.TLSMode)
	}
	if e.Port != 0 && (e.Port < EmailMinPort || e.Port > EmailMaxPort) {
		return fieldErr(p+"port", ValRange, "email: port must be 1..65535", itoa(EmailMinPort), itoa(EmailMaxPort), itoa(e.Port))
	}
	if len(e.Host) > EmailMaxHostLen {
		return fieldErr(p+"host", ValTooLong, "email: host too long", itoa(EmailMaxHostLen))
	}
	if e.From != "" && !ValidEmailAddr(e.From) {
		return fieldErr(p+"from", ValRequired, "email: from must be a valid email address")
	}
	if len(e.From) > EmailMaxFromLen {
		return fieldErr(p+"from", ValTooLong, "email: from too long", itoa(EmailMaxFromLen))
	}
	if len(e.Recipients) > EmailMaxRecipients {
		return fieldErr(p+"recipients", ValTooMany, fmt.Sprintf("email: at most %d recipients", EmailMaxRecipients), itoa(EmailMaxRecipients))
	}
	seen := map[string]bool{}
	for i, r := range e.Recipients {
		f := p + "recipients[" + itoa(i) + "]"
		if !ValidEmailID(r.ID) {
			return fieldErr(f+".id", ValRequired, "email: recipient id: 1..64 printable ASCII without spaces")
		}
		if seen[r.ID] {
			return fieldErr(f+".id", ValDuplicate, "email: duplicate recipient id")
		}
		seen[r.ID] = true
		if !ValidEmailAddr(r.Addr) {
			return fieldErr(f+".addr", ValRequired, "email: recipient addr must be a valid email address")
		}
		if len([]rune(r.Name)) > EmailMaxNameLen {
			return fieldErr(f+".name", ValTooLong, "email: recipient name: at most 64 characters", itoa(EmailMaxNameLen))
		}
	}
	if e.Enabled {
		if strings.TrimSpace(e.Host) == "" {
			return fieldErr(p+"host", ValRequired, "email: enabled without host")
		}
		if e.Port == 0 {
			return fieldErr(p+"port", ValRequired, "email: enabled without port")
		}
		if strings.TrimSpace(e.From) == "" || !ValidEmailAddr(e.From) {
			return fieldErr(p+"from", ValRequired, "email: enabled without a valid from address")
		}
		if len(e.Recipients) == 0 {
			return fieldErr(p+"recipients", ValRequired, "email: enabled without recipients")
		}
		if e.TLSMode == "" {
			return fieldErr(p+"tlsMode", ValRequired, "email: enabled without tlsMode")
		}
	}
	// schedule + events: same rules as Viber
	vs := ViberSettings{Schedule: e.Schedule, Events: e.Events, Recipients: []ViberRecipient{{ID: "x"}}, Enabled: false}
	if err := vs.Validate(); err != nil {
		if fe := AsFieldError(err); fe != nil {
			fe.Field = strings.Replace(fe.Field, "viber.", "email.", 1)
			fe.Msg = strings.Replace(fe.Msg, "viber:", "email:", 1)
			return fe
		}
		return err
	}
	return nil
}

// EmailTestItem is one recipient/addr result of email_test.
type EmailTestItem struct {
	ID       string `json:"id,omitempty"`
	Addr     string `json:"addr,omitempty"`
	OK       bool   `json:"ok"`
	ErrorKey string `json:"errorKey,omitempty"`
}

// EmailTestResult is the email_test reply.
type EmailTestResult struct {
	OK      bool            `json:"ok"`
	Results []EmailTestItem `json:"results"`
}
