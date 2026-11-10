package ipc

import (
	"errors"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Speaker runs speaker_tone / speaker_status (phase 14, CHK-IPC-20).
// Any connected client, like ack_alert, subject to the tone-access rule.
// The owner of a tone is the connection that started it.
type Speaker interface {
	Tone(owner any, freqHz, durMs int) error
	Status() model.SpeakerStatus
	Release(owner any)
	Silence()
}

func speakerCommand(cmd string) bool {
	return cmd == model.CmdSpeakerTone || cmd == model.CmdSpeakerStatus
}

// startSpeaker handles speaker_tone / speaker_status synchronously
// (they do not block). false: the client is too slow.
func (s *Server) startSpeaker(c *conn, req model.IPCRequest) bool {
	switch req.Cmd {
	case model.CmdSpeakerStatus:
		st := s.Speaker.Status()
		return c.send(model.IPCMessage{ID: req.ID, Type: model.MsgSpeakerStatus, Speaker: &st})
	case model.CmdSpeakerTone:
		// nil durMs is invalid (omitted); 0 stops. Pointer so "durMs":0
		// survives omitempty (same shape as the tray client).
		if req.DurMs == nil || *req.DurMs < 0 {
			return c.send(errorReply(req.ID, &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgSpeakerInvalid,
				Message: "ipc: speaker_tone durMs must be >= 0", Args: []string{"durMs", model.ValRange}}, ""))
		}
		dur := *req.DurMs
		// durMs=0 stops the caller's own tone and is always allowed
		// (even with no alarm and no listen slot). Foreign stop is still
		// notify.speaker_not_owner from the device.
		if dur > 0 {
			var err error
			dur, err = s.authorizeSpeakerTone(c, dur)
			if err != nil {
				return c.send(errorReply(req.ID, err, ""))
			}
		}
		err := s.Speaker.Tone(c, req.FreqHz, dur)
		if err != nil {
			return c.send(errorReply(req.ID, err, ""))
		}
		if dur > 0 && !s.hasSpeakerAlarm() {
			now := s.now
			if now == nil {
				now = time.Now
			}
			c.lastListen = now()
		}
		res := model.ResultAccepted
		if dur == 0 {
			res = model.ResultToneStopped
		}
		return c.send(model.IPCMessage{ID: req.ID, Type: model.MsgOK, Result: res})
	}
	return c.send(errorReply(req.ID, errors.New("unknown command: "+req.Cmd), model.ErrCodeUnknownCommand))
}

func (s *Server) hasSpeakerAlarm() bool {
	if s == nil || s.H == nil {
		return false
	}
	for _, a := range s.H.Snapshot().Alerts {
		if a.To == model.LevelWarning || a.To == model.LevelError {
			return true
		}
	}
	return false
}

// authorizeSpeakerTone allows a start when the service has an active
// warning/error alarm, or in listen mode (once per 10s per connection,
// duration clipped to 5s). Otherwise notify.speaker_denied.
func (s *Server) authorizeSpeakerTone(c *conn, dur int) (int, error) {
	if s.hasSpeakerAlarm() {
		return dur, nil
	}
	now := s.now
	if now == nil {
		now = time.Now
	}
	t := now()
	if !c.lastListen.IsZero() && t.Sub(c.lastListen) < time.Duration(model.SpeakerListenIntervalMs)*time.Millisecond {
		return 0, speakerDenied()
	}
	if dur > model.SpeakerListenMaxMs {
		dur = model.SpeakerListenMaxMs
	}
	return dur, nil
}

func speakerDenied() error {
	return &model.IPCError{
		Code:    model.ErrCodeDenied,
		Key:     model.MsgSpeakerDenied,
		Message: "ipc: speaker_tone denied",
	}
}
