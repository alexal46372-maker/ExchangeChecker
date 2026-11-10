package model

// Phase 14 (CHK-SVC-11 / CHK-IPC-20): PC-speaker IPC.
//
//	caps: "speaker", "speaker_status"
//	{cmd:"speaker_tone", freqHz, durMs}  durMs=0 stops the tone
//	{cmd:"speaker_status"} → {type:"speaker_status", speaker:{available, reason?, driverVersion?}}
//
// Any connected client (like ack_alert), subject to the tone-access
// rule: an active warning/error alarm, or listen mode. The owner of a
// tone is the connection that started it: only that connection may
// stop it (or close, which is an implicit stop); durMs=0 of the owner's
// tone is always allowed. A second connection that starts a tone while
// one is playing is rejected (busy).
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
	MsgSpeakerDenied       = "notify.speaker_denied"

	// SpeakerListenIntervalMs / SpeakerListenMaxMs: when there is no
	// active warning/error alarm, speaker_tone is listen mode — at most
	// once per interval for the whole service, duration clipped to MaxMs.
	SpeakerListenIntervalMs = 10_000
	SpeakerListenMaxMs      = 5_000
)

// SpeakerStatus is the speaker_status payload.
type SpeakerStatus struct {
	Available     bool   `json:"available"`
	Reason        string `json:"reason,omitempty"`
	DriverVersion string `json:"driverVersion,omitempty"`
}
