package model

// Phase 13 item 7 (EC Service, CHK-MON-22 / CHK-IPC-19) and phase 17
// (CHK-MON-24 / CHK-IPC-22): flashing of an active alert. On every check
// that still fails the service pushes
//
//	{type:"flash", profileId, pointKey, eventId, level}
//
// to all clients; ack_alert does not itself stop it. dismiss_flash
// {eventId} (any connected client like ack_alert) is a suspend: the
// service pushes flash_stop and records flashDismissed. resume_flash
// {eventId} clears that mark; if the event is still active and not
// acknowledged, the service immediately pushes flash with the same
// eventId and replies ok with resumed:true. After ack, after the alert
// clears, or for an unknown / other eventId it replies ok with
// resumed:false and has no effect. On recovery (or when the alert ends
// otherwise: profile disabled / removed / muted) the service also pushes
//
//	{type:"flash_stop", profileId, pointKey}
//
// and forgets the suppression. A later error of the same point flashes
// with a new eventId.
const (
	CapFlash        = "flash"
	CapResumeFlash  = "resume_flash" // phase 17 (CHK-IPC-22)
	CmdDismissFlash = "dismiss_flash"
	CmdResumeFlash  = "resume_flash"
	MsgFlash        = "flash"
	MsgFlashStop    = "flash_stop"
	// ResultFlashDismissed: dismiss_flash reply result.
	ResultFlashDismissed = "flash_dismissed"
	// MsgMonitorUnknownEvent: dismiss_flash for an eventId that is not an
	// active alert (code not_found).
	MsgMonitorUnknownEvent = "monitor.unknown_event"
)

// FlashEvent is one flash / flash_stop push (Stop = flash_stop).
type FlashEvent struct {
	ProfileID string
	PointKey  string
	EventID   uint64
	Level     AlertLevel
	Stop      bool
}
