package pcspeaker

// SCM / power-event codes used to silence the speaker (CHK-SVC-11).
// Values match winsvc.h / winuser.h; duplicated here so this package
// stays free of winsvc and of Windows build tags.

const (
	ControlStop        = 0x00000001
	ControlShutdown    = 0x00000005
	ControlPreshutdown = 0x0000000F
	ControlPowerEvent  = 0x0000000D
	// PBTAPMSuspend is PBT_APMSUSPEND (the machine is about to suspend).
	PBTAPMSuspend = 0x00000004
)

// ShouldSilence reports whether this SCM control must turn the speaker
// off: STOP, SHUTDOWN, PRESHUTDOWN, or POWEREVENT + PBT_APMSUSPEND.
func ShouldSilence(code, eventType uint32) bool {
	switch code {
	case ControlStop, ControlShutdown, ControlPreshutdown:
		return true
	case ControlPowerEvent:
		return eventType == PBTAPMSuspend
	}
	return false
}
