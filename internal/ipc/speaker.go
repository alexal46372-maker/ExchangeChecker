package ipc

import (
	"errors"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Speaker runs speaker_tone / speaker_status (phase 14, CHK-IPC-20).
// Any connected client, like ack_alert. The owner of a tone is the
// connection that started it.
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
		if req.DurMs < 0 {
			return c.send(errorReply(req.ID, &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgSpeakerInvalid,
				Message: "ipc: speaker_tone durMs must be >= 0", Args: []string{"durMs", model.ValRange}}, ""))
		}
		err := s.Speaker.Tone(c, req.FreqHz, req.DurMs)
		if err != nil {
			return c.send(errorReply(req.ID, err, ""))
		}
		res := model.ResultAccepted
		if req.DurMs <= 0 {
			res = model.ResultToneStopped
		}
		return c.send(model.IPCMessage{ID: req.ID, Type: model.MsgOK, Result: res})
	}
	return c.send(errorReply(req.ID, errors.New("unknown command: "+req.Cmd), model.ErrCodeUnknownCommand))
}
