package model

// Phase 14 (CHK-SVC-11 / CHK-IPC-20): PC-speaker IPC.
//
//	caps: "speaker", "speaker_status"
//	{cmd:"speaker_tone", freqHz, durMs}  durMs=0 stops the tone
//	{cmd:"speaker_status"} → {type:"speaker_status", speaker:{available, reason?, driverVersion?}}
//
// Any connected client (like ack_alert). The owner of a tone is the
// connection that started it: only that connection may stop it (or
// close, which is an implicit stop). A second connection that starts a
// tone while one is playing is rejected (busy). No driver is shipped
// yet: the service reports available=false, reason=not_installed, and
// starting a tone fails with notify.speaker_not_installed.
const (
	CapSpeaker        = "speaker"
	CapSpeakerStatus  = "speaker_status"
	CmdSpeakerTone    = "speaker_tone"
	CmdSpeakerStatus  = "speaker_status"
	MsgSpeakerStatus  = "speaker_status"
	ResultToneStopped = "stopped"

	// Status.Reason when Available is false.
	SpeakerUnsupportedOS = "unsupported_os"
	SpeakerNotInstalled  = "not_installed"
	SpeakerBlocked       = "blocked"
	SpeakerOpenFailed    = "open_failed"
	SpeakerModuleFailed  = "module_failed"

	MsgSpeakerNotInstalled = "notify.speaker_not_installed"
	MsgSpeakerBusy         = "notify.speaker_busy"
	MsgSpeakerNotOwner     = "notify.speaker_not_owner"
	MsgSpeakerInvalid      = "notify.speaker_invalid"
	MsgSpeakerFailed       = "notify.speaker_failed"
)

// SpeakerStatus is the speaker_status payload.
type SpeakerStatus struct {
	Available     bool   `json:"available"`
	Reason        string `json:"reason,omitempty"`
	DriverVersion string `json:"driverVersion,omitempty"`
}
