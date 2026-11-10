package ipc

import (
	"errors"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// SpeakerPort is the PC speaker of the service (phase 14). MERGE NOTE:
// the minimal server side of feat/tray-speaker so the tray can be tested
// against a real Server with a fake port; EC Service feat/svc-speaker
// owns the real one and its version wins on merge.
type SpeakerPort interface {
	// Tone starts freqHz for durMs; durMs 0 stops the current tone.
	Tone(freqHz, durMs int) error
	// Status is the current state of the speaker.
	Status() model.SpeakerStatus
}

// speaker serves speaker_tone / speaker_status (any client, like
// ack_alert).
func (s *Server) speaker(req model.IPCRequest) model.IPCMessage {
	if req.Cmd == model.CmdSpeakerStatus {
		st := s.Speaker.Status()
		return model.IPCMessage{ID: req.ID, Type: model.MsgOK, Result: model.CmdSpeakerStatus, Speaker: &st}
	}
	dur := 0
	if req.DurMs != nil {
		dur = *req.DurMs
	}
	if req.DurMs == nil || dur < 0 || dur > model.MaxBeepDurMs ||
		(dur > 0 && (req.FreqHz < model.MinBeepFreqHz || req.FreqHz > model.MaxBeepFreqHz)) {
		return errorReply(req.ID, &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgMonitorBadRequest,
			Message: "ipc: speaker_tone needs freqHz and durMs", Args: []string{"durMs", model.ValRequired}}, "")
	}
	if err := s.Speaker.Tone(req.FreqHz, dur); err != nil {
		var ie *model.IPCError
		if !errors.As(err, &ie) {
			err = &model.IPCError{Code: model.ErrCodeInternal, Message: "ipc: speaker_tone: " + err.Error()}
		}
		return errorReply(req.ID, err, "")
	}
	return model.IPCMessage{ID: req.ID, Type: model.MsgOK, Result: model.ResultSpeakerTone}
}
