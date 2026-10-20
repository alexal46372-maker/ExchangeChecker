package model

import "time"

// Phase 11 (CHK-IPC-11): alarm_test — a test alarm from the settings of
// EC Tray. Admin only, capability alarm_test.
//
// Request {"cmd":"alarm_test","level":"warning"|"error","viber":bool}.
// The service pushes {"type":"alert","alert":{…,"test":true}} to every
// connected tray (id / eventId 0: never acknowledged; not in
// events_since, the history, the Event Log, status.alerts / problems,
// Viber summaries; only the service log file records who sent it);
// viber:true also sends "Тест" / "Test" to every saved Viber recipient
// ignoring enabled and the schedule. Reply MsgAlarmTest with AlarmTest.
// One alarm_test per service per AlarmTestInterval (rate_limited +
// retryAfterMs).
const (
	CmdAlarmTest      = "alarm_test"
	CapAlarmTest      = "alarm_test"
	MsgAlarmTest      = "alarm_test"
	AlarmTestInterval = 10 * time.Second

	AlarmLevelWarning = "warning"
	AlarmLevelError   = "error"

	// AlarmTestProfileID / AlarmTestName identify the test alert.
	AlarmTestProfileID = "alarm-test"

	MsgAlarmTestLevel = "monitor.alarm_test_level" // level not warning / error
)

// client_hello (phase 11, CHK-IPC-11, architect): a client declares
// what it supports, {"cmd":"client_hello","capabilities":[...]} (any
// client; at most ClientCapsMax items of at most ClientCapMaxLen chars,
// else invalid_request; unknown ones ignored). Reply {"type":"client_hello",
// "capabilities":[accepted known ones]}. Stored per connection; the test
// alarm push (test:true) goes only to connections that declared
// ClientCapAlarmTest (clients without client_hello, e.g. tray 0.10, never
// get it).
const (
	CmdClientHello     = "client_hello"
	MsgClientHello     = "client_hello"
	CapClientHello     = "client_hello"
	ClientCapAlarmTest = "alarm_test"
	ClientCapsMax      = 64
	ClientCapMaxLen    = 64
)

// KnownClientCaps lists the client capabilities the service acts on.
var KnownClientCaps = map[string]bool{ClientCapAlarmTest: true}

// AlarmTestResult is the reply of alarm_test (contract with EC Tray):
// OK = the test alert was pushed; Viber (viber:true only) per recipient,
// {"ok":false,"results":[],"errorKey":"notify.viber_no_token" |
// "notify.viber_no_recipients"} when nothing could be sent.
type AlarmTestResult struct {
	OK    bool            `json:"ok"`
	Viber *AlarmTestViber `json:"viber,omitempty"`
}

// AlarmTestViber is the viber part of the reply: ViberTestResult plus
// errorKey when nothing could be sent (architect review of 1ecc54b).
type AlarmTestViber struct {
	ViberTestResult
	ErrorKey string `json:"errorKey,omitempty"`
}

// AlarmLevelOf maps the request level ("warning" / "error").
func AlarmLevelOf(s string) (AlertLevel, bool) {
	switch s {
	case AlarmLevelWarning:
		return LevelWarning, true
	case AlarmLevelError:
		return LevelError, true
	}
	return LevelOK, false
}
