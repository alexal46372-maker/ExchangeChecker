package model

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Phase 10 (EC Service, CHK-SVC-06): Viber notifications. The settings
// live in viber.json (not settings.json: never part of export_config,
// own admin commands); the bot token in viber.dpapi (DPAPI LocalMachine,
// DACL SYSTEM+Administrators), write-only: no reply, log or event ever
// contains it (HasToken only).

// Commands, capability, reply types.
const (
	// CmdGetViber (admin): reply MsgViber with Viber.
	CmdGetViber = "get_viber"
	// CmdSaveViber (admin): IPCRequest.Viber (settings), ViberToken (set),
	// ClearViberToken; reply MsgViber with Viber (the saved view).
	CmdSaveViber = "save_viber"
	// CmdViberTest (admin, async, cancel {ref}): a test message to
	// IPCRequest.Receiver (or every saved receiver) with ViberToken (or
	// the saved token; a token sent here is not saved); ignores enabled
	// and the schedule. Reply MsgViberTest with ViberTest.
	CmdViberTest = "viber_test"
	CapViber     = "viber"
	MsgViber     = "viber"
	MsgViberTest = "viber_test"

	// ViberTestInterval: one viber_test per service per 10 s.
	ViberTestInterval = 10 * time.Second
	// ViberMaxReceivers, ViberMaxIDLen, ViberMaxTokenLen,
	// ViberMaxSenderName (Viber limit 28).
	ViberMaxReceivers  = 20
	ViberMaxIDLen      = 64
	ViberMaxTokenLen   = 256
	ViberMaxSenderName = 28
)

// Error keys (texts in monitorMessages).
const (
	MsgViberNoToken         = "notify.viber_no_token"
	MsgViberNoReceivers     = "notify.viber_no_receivers"
	MsgViberAuthFailed      = "notify.viber_auth_failed"
	MsgViberReceiverInvalid = "notify.viber_receiver_invalid"
	MsgViberRateLimited     = "notify.viber_rate_limited"
	MsgViberNetwork         = "notify.viber_network"
	MsgViberTLS             = "notify.viber_tls"
	MsgViberFailed          = "notify.viber_failed"
	MsgViberInvalid         = "notify.viber_invalid"
)

// ViberSchedule: days ISO 1 = Monday .. 7 = Sunday; From / To "HH:MM"
// local time of the service machine, From < To (no window across
// midnight in v1).
type ViberSchedule struct {
	Days []int  `json:"days"`
	From string `json:"from"`
	To   string `json:"to"`
}

// ViberEvents selects the sent event kinds.
type ViberEvents struct {
	Overdue   bool `json:"overdue"`   // a point / profile got worse (warning, error, no connection)
	Recovered bool `json:"recovered"` // back to ok
}

// ViberSettings (viber.json).
type ViberSettings struct {
	Enabled   bool          `json:"enabled"`
	Receivers []string      `json:"receivers"`
	Schedule  ViberSchedule `json:"schedule"`
	Events    ViberEvents   `json:"events"`
	// ReminderMinutes: summary of the unresolved problems every N minutes
	// inside the window (0 = none, else 15..1440).
	ReminderMinutes int    `json:"reminderMinutes"`
	SenderName      string `json:"senderName"`
}

// DefaultViberSettings: disabled, Mon-Fri 08:30-17:00, both events,
// reminder 60 min.
func DefaultViberSettings() ViberSettings {
	return ViberSettings{Receivers: []string{}, Schedule: ViberSchedule{Days: []int{1, 2, 3, 4, 5}, From: "08:30", To: "17:00"},
		Events: ViberEvents{Overdue: true, Recovered: true}, ReminderMinutes: 60, SenderName: "ExchangeChecker"}
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

func viberIDOK(s string) bool {
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
	if t == "" || len(t) > ViberMaxTokenLen || strings.ContainsAny(t, " ") {
		return false
	}
	for i := 0; i < len(t); i++ {
		if t[i] <= ' ' || t[i] > '~' {
			return false
		}
	}
	return true
}

// Validate checks the settings (FieldError, validation.* keys).
func (v ViberSettings) Validate() error {
	if len(v.Receivers) > ViberMaxReceivers {
		return fieldErr("receivers", ValTooMany, fmt.Sprintf("at most %d receivers", ViberMaxReceivers), itoa(ViberMaxReceivers))
	}
	seen := map[string]bool{}
	for i, r := range v.Receivers {
		f := "receivers[" + itoa(i) + "]"
		if !viberIDOK(r) {
			return fieldErr(f, ValRequired, "receiver id: 1..64 printable ASCII characters without spaces")
		}
		if seen[r] {
			return fieldErr(f, ValDuplicate, "duplicate receiver")
		}
		seen[r] = true
	}
	if v.Enabled && len(v.Receivers) == 0 {
		return fieldErr("receivers", ValRequired, "enabled without receivers")
	}
	if len(v.Schedule.Days) == 0 || len(v.Schedule.Days) > 7 {
		return fieldErr("schedule.days", ValRequired, "schedule.days: 1..7 days")
	}
	days := map[int]bool{}
	for i, d := range v.Schedule.Days {
		if d < 1 || d > 7 || days[d] {
			return fieldErr("schedule.days["+itoa(i)+"]", ValRange, "day must be 1 (Monday) .. 7 (Sunday), unique", "1", "7")
		}
		days[d] = true
	}
	from, ok1 := ParseHHMM(v.Schedule.From)
	if !ok1 {
		return fieldErr("schedule.from", ValRequired, "schedule.from must be HH:MM")
	}
	to, ok2 := ParseHHMM(v.Schedule.To)
	if !ok2 {
		return fieldErr("schedule.to", ValRequired, "schedule.to must be HH:MM")
	}
	if to <= from {
		return fieldErr("schedule.to", ValGreaterThan, "schedule.to must be after schedule.from", "schedule.from")
	}
	if v.ReminderMinutes != 0 && (v.ReminderMinutes < 15 || v.ReminderMinutes > 1440) {
		return fieldErr("reminderMinutes", ValRange, "reminderMinutes: 0 or 15..1440", "15", "1440")
	}
	if n := len([]rune(v.SenderName)); n == 0 || n > ViberMaxSenderName {
		return fieldErr("senderName", ValTooLong, "senderName: 1..28 characters", itoa(ViberMaxSenderName))
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

// ViberStatus is the delivery state (get_viber / save_viber).
type ViberStatus struct {
	LastOKAt     *time.Time `json:"lastOkAt,omitempty"`
	LastErrorAt  *time.Time `json:"lastErrorAt,omitempty"`
	LastErrorKey string     `json:"lastErrorKey,omitempty"`
	Queued       int        `json:"queued"`
	Sent         uint64     `json:"sent"`
	Dropped      uint64     `json:"dropped"`
}

// ViberView is the reply of get_viber / save_viber: the settings and
// HasToken, never the token.
type ViberView struct {
	Settings ViberSettings `json:"settings"`
	HasToken bool          `json:"hasToken"`
	InWindow bool          `json:"inWindow"`
	Status   ViberStatus   `json:"status"`
}

// ViberTestItem is the result for one receiver.
type ViberTestItem struct {
	Receiver    string `json:"receiver"`
	OK          bool   `json:"ok"`
	ErrorKey    string `json:"errorKey,omitempty"`
	ViberStatus *int   `json:"viberStatus,omitempty"`
}

// ViberTestResult is the reply of viber_test.
type ViberTestResult struct {
	Results []ViberTestItem `json:"results"`
}

// ValidReceivers checks only the receiver ids (viber_test).
func (v ViberSettings) ValidReceivers() bool {
	for _, r := range v.Receivers {
		if !viberIDOK(r) {
			return false
		}
	}
	return true
}
