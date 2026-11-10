package main

import (
	"context"
	"io"
	"runtime"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/ipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_IPC_20_ServiceDefaultDriverStatus — the real service advertises
// speaker / speaker_status; production Port is InpOutx64. On this host
// (no Win7 amd64 + official driver) Status is unavailable: Windows/amd64
// without the service → not_installed; any other GOOS/GOARCH → unsupported_os.
// Starting a tone fails with the matching notify key; any client may call it.
func TestCHK_IPC_20_ServiceDefaultDriverStatus(t *testing.T) {
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
	wantReason, wantKey := model.SpeakerUnsupportedOS, model.MsgSpeakerFailed
	if runtime.GOOS == "windows" && runtime.GOARCH == "amd64" {
		wantReason, wantKey = model.SpeakerNotInstalled, model.MsgSpeakerNotInstalled
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	st, err := s.c.Do(ctx, model.IPCRequest{Cmd: model.CmdSpeakerStatus})
	if err != nil || st.Type != model.MsgSpeakerStatus || st.Speaker == nil ||
		st.Speaker.Available || st.Speaker.Reason != wantReason {
		t.Fatalf("status %+v %v (want reason %s)", st, err, wantReason)
	}
	tone, err := s.c.Do(ctx, model.IPCRequest{Cmd: model.CmdSpeakerTone, FreqHz: 440, DurMs: 100})
	if err != nil || tone.ErrorKey != wantKey || tone.Code != model.ErrCodeUnavailable {
		t.Fatalf("tone %+v %v (want key %s)", tone, err, wantKey)
	}
}
