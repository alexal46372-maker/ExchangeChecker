package model

// Phase 14 (Алекс, 10.11.2026; feat/tray-speaker, client side): «Тон»
// through the PC speaker of the service (EC Service feat/svc-speaker).
// The service announces
//
//	capability speaker         — speaker_tone {freqHz, durMs}; durMs 0 stops
//	capability speaker_status  — speaker_status → {speaker:{available, reason, driverVersion}}
//
// MERGE NOTE: these are the minimal client-side definitions agreed in the
// contract; when feat/svc-speaker is merged its definitions win (this
// file, IPCRequest.FreqHz/DurMs and IPCMessage.Speaker are the expected
// conflict spots).
const (
	CapSpeaker       = "speaker"
	CapSpeakerStatus = "speaker_status"
	CmdSpeakerTone   = "speaker_tone"
	CmdSpeakerStatus = "speaker_status"
	// ResultSpeakerTone: speaker_tone reply result.
	ResultSpeakerTone = "speaker_tone"
)

// SpeakerStatus is the state of the PC speaker on the service side.
type SpeakerStatus struct {
	Available bool `json:"available"`
	// Reason: why it is not available (a message key or a text; "" when
	// available).
	Reason string `json:"reason,omitempty"`
	// DriverVersion of the speaker driver ("" = unknown / none).
	DriverVersion string `json:"driverVersion,omitempty"`
}
