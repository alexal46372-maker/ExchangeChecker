package main

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/ipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_IPC_20_ServiceNotInstalled — the real service (no driver)
// advertises speaker / speaker_status and reports not_installed; starting
// a tone fails with notify.speaker_not_installed; any client may call it.
func TestCHK_IPC_20_ServiceNotInstalled(t *testing.T) {
	s := startSvcWith(t, t.TempDir(), 0, nil, &fakeEvents{}, func(ac *appConfig) {
		ac.Authorize = func(io.ReadWriteCloser) (ipc.Peer, error) { return ipc.Peer{Account: `TEST\user`}, nil }
	})
	defer s.stop()
	hello := s.waitPush(t, func(m model.IPCMessage) bool { return m.Type == model.MsgHello })
	hasS, hasST := false, false
	for _, c := range hello.Capabilities {
		hasS = hasS || c == model.CapSpeaker
		hasST = hasST || c == model.CapSpeakerStatus
	}
	if !hasS || !hasST {
		t.Fatalf("caps %v", hello.Capabilities)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	st, err := s.c.Do(ctx, model.IPCRequest{Cmd: model.CmdSpeakerStatus})
	if err != nil || st.Type != model.MsgSpeakerStatus || st.Speaker == nil ||
		st.Speaker.Available || st.Speaker.Reason != model.SpeakerNotInstalled {
		t.Fatalf("status %+v %v", st, err)
	}
	tone, err := s.c.Do(ctx, model.IPCRequest{Cmd: model.CmdSpeakerTone, FreqHz: 440, DurMs: 100})
	if err != nil || tone.ErrorKey != model.MsgSpeakerNotInstalled || tone.Code != model.ErrCodeUnavailable {
		t.Fatalf("tone %+v %v", tone, err)
	}
}
