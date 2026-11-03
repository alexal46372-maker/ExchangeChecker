package model

// Phase 13 item 7 (EC Service, CHK-MON-22 / CHK-IPC-19): flashing of an
// active alert. On every check that still fails the service pushes
//
//	{type:"flash", profileId, pointKey, eventId, level}
//
// to all clients; ack_alert does not stop it. dismiss_flash {eventId}
// («Закрыть», any connected client like ack_alert) suppresses it for that
// alert until recovery; on recovery (or when the alert ends otherwise:
// profile disabled / removed / muted) the service pushes
//
//	{type:"flash_stop", profileId, pointKey}
//
// and forgets the suppression.
const (
	CapFlash        = "flash"
	CmdDismissFlash = "dismiss_flash"
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
