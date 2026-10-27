package model

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Phase 10 (EC Service, CHK-SVC-06): Viber notifications. The settings
// are settings.viber (settings.json, get_config / get_status settings,
// save_settings; export_config only with includeViberRecipients, and then
// without hasToken / status / token — CHK-CFG-07). The bot token is in
// viber.dpapi (DPAPI LocalMachine, DACL SYSTEM+Administrators),
// write-only: save_settings viberToken / clearViberToken; no reply, log
// or event ever contains it (hasToken only).

// Command, capability, reply type.
const (
	// CmdViberTest (admin, async, cancel {ref}): a test message to
	// IPCRequest.RecipientID (or every saved recipient) with ViberToken
	// (or the saved token; a token sent here is not saved); ignores
	// enabled and the schedule. Reply MsgViberTest with ViberTest.
	CmdViberTest = "viber_test"
	CapViber     = "viber"
	MsgViberTest = "viber_test"

	// ViberTestInterval: one viber_test per service per 10 s.
	ViberTestInterval = 10 * time.Second
	// ViberMaxRecipients, ViberMaxIDLen, ViberMaxNameLen, ViberMaxTokenLen.
	ViberMaxRecipients = 20
	ViberMaxIDLen      = 64
	ViberMaxNameLen    = 64
	ViberMaxTokenLen   = 256
	// ViberSenderName is sender.name of every message (Viber limit 28).
	ViberSenderName = "ExchangeChecker"
)

// Event kinds of settings.viber.events.
const (
	ViberEventError        = "error"         // a point became overdue (error threshold)
	ViberEventWarning      = "warning"       // a point reached the warning threshold
	ViberEventNoConnection = "no_connection" // connection / query error of a profile
	ViberEventRecovered    = "recovered"     // back to ok
)

// ViberEventKinds lists the kinds (the default: all four).
func ViberEventKinds() []string {
	return []string{ViberEventError, ViberEventWarning, ViberEventRecovered, ViberEventNoConnection}
}

// ViberEventOf maps an alert transition to its kind ("" = not sent:
// unchanged level or an improvement that is not back to ok).
func ViberEventOf(e AlertEvent) string {
	switch {
	case e.To == e.From:
		return ""
	case e.To == LevelOK:
		return ViberEventRecovered
	case e.To < e.From:
		return ""
	case e.PointName == "" && e.PlanTable == "" && (e.Error != "" || e.ErrorKey != ""):
		return ViberEventNoConnection
	case e.To == LevelError:
		return ViberEventError
	case e.To == LevelWarning:
		return ViberEventWarning
	}
	return ""
}

// Error keys (texts in monitorMessages).
const (
	MsgViberNoToken         = "notify.viber_no_token"
	MsgViberNoRecipients    = "notify.viber_no_recipients"
	MsgViberAuthFailed      = "notify.viber_auth_failed"
	MsgViberReceiverInvalid = "notify.viber_receiver_invalid"
	MsgViberRateLimited     = "notify.viber_rate_limited"
	MsgViberNetwork         = "notify.viber_network"
	MsgViberTLS             = "notify.viber_tls"
	MsgViberFailed          = "notify.viber_failed"
	// MsgViberCanceled / MsgViberTimeout: the request ended (cancel /
	// deadline) before this recipient was sent to (phase 11, CHK-IPC-11).
	MsgViberCanceled = "notify.viber_canceled"
	MsgViberTimeout  = "notify.viber_timeout"
	MsgViberInvalid  = "notify.viber_invalid"
	// texts of the messages themselves
	MsgViberNotChecked   = "notify.not_checked"   // summary: profile not polled yet
	MsgViberErrorUnknown = "notify.error_unknown" // an error without a known key
)

// ViberSchedule: days ISO 1 = Monday .. 7 = Sunday; From / To "HH:MM"
// local time of the service machine, From < To (no window across
// midnight in v1).
type ViberSchedule struct {
	Days []int  `json:"days"`
	From string `json:"from"`
	To   string `json:"to"`
}

// ViberRecipient is a Viber user id with a display name.
type ViberRecipient struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ViberSettings is settings.viber.
type ViberSettings struct {
	Enabled bool `json:"enabled"`
	// HasToken is read-only (views): viber.dpapi holds a token. Ignored
	// on save, never stored.
	HasToken   bool             `json:"hasToken,omitempty"`
	Recipients []ViberRecipient `json:"recipients"`
	Schedule   ViberSchedule    `json:"schedule"`
	// Events: the sent kinds (ViberEvent*), default all four.
	Events []string `json:"events"`
	// ReminderMinutes: summary of the unresolved problems every N minutes
	// inside the window (0 = none, else 15..1440).
	ReminderMinutes int `json:"reminderMinutes"`
	// Status is read-only (get_config / get_status settings): delivery
	// state of the running service. Ignored on save, never stored.
	Status *ViberStatus `json:"status,omitempty"`
}

// DefaultViberSettings: disabled, Mon-Fri 08:30-17:00, all events,
// reminder 60 min.
func DefaultViberSettings() ViberSettings {
	return ViberSettings{Recipients: []ViberRecipient{}, Schedule: ViberSchedule{Days: []int{1, 2, 3, 4, 5}, From: "08:30", To: "17:00"},
		Events: ViberEventKinds(), ReminderMinutes: 60}
}

// Stored returns v without the read-only fields and with nil slices made
// empty (what settings.json holds; the comparison form).
func (v ViberSettings) Stored() ViberSettings {
	v.HasToken, v.Status = false, nil
	if v.Recipients == nil {
		v.Recipients = []ViberRecipient{}
	}
	if v.Events == nil {
		v.Events = []string{}
	}
	if v.Schedule.Days == nil {
		v.Schedule.Days = []int{}
	}
	return v
}

// HasEvent reports whether kind is selected.
func (v ViberSettings) HasEvent(kind string) bool {
	for _, k := range v.Events {
		if k == kind {
			return true
		}
	}
	return false
}

// ParseHHMM returns the minutes after midnight of "HH:MM".
func ParseHHMM(s string) (int, bool) {
	if len(s) != 5 || s[2] != ':' {
		return 0, false
	}
	h, err1 := strconv.Atoi(s[:2])
	m, err2 := strconv.Atoi(s[3:])
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

// ValidViberID: 1..64 printable ASCII characters without spaces.
func ValidViberID(s string) bool {
	if s == "" || len(s) > ViberMaxIDLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] <= ' ' || s[i] > '~' {
			return false
		}
	}
	return true
}

// ValidViberToken: printable ASCII without spaces, 1..256 bytes.
func ValidViberToken(t string) bool {
	return t != "" && len(t) <= ViberMaxTokenLen && !strings.ContainsAny(t, " \t\r\n") && printable(t)
}

func printable(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] <= ' ' || s[i] > '~' {
			return false
		}
	}
	return true
}

// Validate checks the settings (FieldError, field paths below "viber.").
func (v ViberSettings) Validate() error {
	const p = "viber."
	if len(v.Recipients) > ViberMaxRecipients {
		return fieldErr(p+"recipients", ValTooMany, fmt.Sprintf("viber: at most %d recipients", ViberMaxRecipients), itoa(ViberMaxRecipients))
	}
	seen := map[string]bool{}
	for i, r := range v.Recipients {
		f := p + "recipients[" + itoa(i) + "]"
		if !ValidViberID(r.ID) {
			return fieldErr(f+".id", ValRequired, "viber: recipient id: 1..64 printable ASCII characters without spaces")
		}
		if seen[r.ID] {
			return fieldErr(f+".id", ValDuplicate, "viber: duplicate recipient")
		}
		seen[r.ID] = true
		if len([]rune(r.Name)) > ViberMaxNameLen {
			return fieldErr(f+".name", ValTooLong, "viber: recipient name: at most 64 characters", itoa(ViberMaxNameLen))
		}
	}
	if v.Enabled && len(v.Recipients) == 0 {
		return fieldErr(p+"recipients", ValRequired, "viber: enabled without recipients")
	}
	if len(v.Schedule.Days) == 0 || len(v.Schedule.Days) > 7 {
		return fieldErr(p+"schedule.days", ValRequired, "viber: schedule.days: 1..7 days")
	}
	days := map[int]bool{}
	for i, d := range v.Schedule.Days {
		if d < 1 || d > 7 || days[d] {
			return fieldErr(p+"schedule.days["+itoa(i)+"]", ValRange, "viber: day must be 1 (Monday) .. 7 (Sunday), unique", "1", "7")
		}
		days[d] = true
	}
	from, ok1 := ParseHHMM(v.Schedule.From)
	if !ok1 {
		return fieldErr(p+"schedule.from", ValRequired, "viber: schedule.from must be HH:MM")
	}
	to, ok2 := ParseHHMM(v.Schedule.To)
	if !ok2 {
		return fieldErr(p+"schedule.to", ValRequired, "viber: schedule.to must be HH:MM")
	}
	if to <= from {
		return fieldErr(p+"schedule.to", ValGreaterThan, "viber: schedule.to must be after schedule.from", "schedule.from")
	}
	kinds := map[string]bool{}
	for i, k := range v.Events {
		ok := false
		for _, known := range ViberEventKinds() {
			ok = ok || k == known
		}
		if !ok || kinds[k] {
			return fieldErr(p+"events["+itoa(i)+"]", ValEnum, "viber: events: error, warning, recovered, no_connection (unique)", k)
		}
		kinds[k] = true
	}
	if v.ReminderMinutes != 0 && (v.ReminderMinutes < 15 || v.ReminderMinutes > 1440) {
		return fieldErr(p+"reminderMinutes", ValRange, "viber: reminderMinutes: 0 or 15..1440", "15", "1440")
	}
	return nil
}

// InWindow reports whether local time t is inside the schedule.
func (v ViberSettings) InWindow(t time.Time) bool {
	wd := int(t.Weekday())
	if wd == 0 {
		wd = 7
	}
	day := false
	for _, d := range v.Schedule.Days {
		if d == wd {
			day = true
		}
	}
	from, ok1 := ParseHHMM(v.Schedule.From)
	to, ok2 := ParseHHMM(v.Schedule.To)
	m := t.Hour()*60 + t.Minute()
	return day && ok1 && ok2 && m >= from && m < to
}

// ViberStatus is settings.viber.status (read-only view).
type ViberStatus struct {
	Queued       int        `json:"queued"`
	Sent         uint64     `json:"sent"`
	Dropped      uint64     `json:"dropped"`
	LastOKAt     *time.Time `json:"lastOkAt,omitempty"`
	LastErrorAt  *time.Time `json:"lastErrorAt,omitempty"`
	LastErrorKey string     `json:"lastErrorKey,omitempty"`
}

// ViberTestItem is the result for one recipient.
type ViberTestItem struct {
	ID       string `json:"id"`
	OK       bool   `json:"ok"`
	ErrorKey string `json:"errorKey,omitempty"`
}

// ViberTestResult is the reply of viber_test: OK = every recipient ok.
type ViberTestResult struct {
	OK      bool            `json:"ok"`
	Results []ViberTestItem `json:"results"`
}
