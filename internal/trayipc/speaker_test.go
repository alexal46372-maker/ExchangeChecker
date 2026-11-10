package trayipc

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_TRAY_98_SpeakerClient — the client of speaker_status /
// speaker_tone: the request on the wire ("durMs":0 for the stop), the
// cached status (one speaker_status until Invalidate), a refused tone
// marks the speaker not available, and a service without the
// capabilities gets no request at all.
func TestCHK_TRAY_98_SpeakerClient(t *testing.T) {
	stop, _ := json.Marshal(model.IPCRequest{Cmd: model.CmdSpeakerTone, DurMs: new(int)})
	if !strings.Contains(string(stop), `"durMs":0`) {
		t.Fatalf("stop request %s", stop)
	}
	ms := &manualServer{}
	var mu sync.Mutex
	var tones []model.IPCRequest
	avail, fail := true, false
	dial := ms.dialer(model.IPCMessage{Type: model.MsgHello, Version: 2, Heartbeat: &model.Heartbeat{Protocol: 2},
		Capabilities: []string{model.CapSpeaker, model.CapSpeakerStatus}}, func(r model.IPCRequest) *model.IPCMessage {
		mu.Lock()
		defer mu.Unlock()
		switch r.Cmd {
		case model.CmdGetStatus:
			return &model.IPCMessage{Type: model.MsgStatus, Status: &model.StatusSnapshot{}}
		case model.CmdSpeakerStatus:
			return &model.IPCMessage{Type: model.MsgOK, Speaker: &model.SpeakerStatus{Available: avail, Reason: map[bool]string{false: "нет динамика"}[avail]}}
		case model.CmdSpeakerTone:
			tones = append(tones, r)
			if fail && r.DurMs != nil && *r.DurMs > 0 {
				return &model.IPCMessage{Type: model.MsgError, Code: model.ErrCodeInternal, ErrorKey: model.MsgMonitorInternal}
			}
			return &model.IPCMessage{Type: model.MsgOK, Result: model.ResultSpeakerTone}
		}
		return &model.IPCMessage{Type: model.MsgError, Code: model.ErrCodeUnknownCommand}
	})
	s := New(testOptions(newFakeClock(), dial))
	runSession(t, s)
	waitFor(t, "connected", connected(s))
	sp := &Speaker{C: s}
	count := func(cmd string) int {
		n := 0
		for _, c := range ms.got() {
			if c == cmd {
				n++
			}
		}
		return n
	}
	if _, fresh := sp.Cached(); fresh {
		t.Fatal("fresh before the first speaker_status")
	}
	for i := 0; i < 3; i++ {
		if st := sp.Status(context.Background()); !st.Available {
			t.Fatalf("status %+v", st)
		}
	}
	if n := count(model.CmdSpeakerStatus); n != 1 {
		t.Fatalf("speaker_status sent %d times, want 1 (cache)", n)
	}
	if err := sp.Tone(context.Background(), 880, 300); err != nil {
		t.Fatal(err)
	}
	if err := sp.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if len(tones) != 2 || tones[0].FreqHz != 880 || tones[0].DurMs == nil || *tones[0].DurMs != 300 ||
		tones[1].DurMs == nil || *tones[1].DurMs != 0 {
		t.Fatalf("tones %+v", tones)
	}
	avail = false
	mu.Unlock()
	sp.Invalidate()
	if st := sp.Status(context.Background()); st.Available || st.Reason != "нет динамика" || count(model.CmdSpeakerStatus) != 2 {
		t.Fatalf("after Invalidate: %+v", st)
	}
	// a refused tone marks it not available until the cache is renewed
	mu.Lock()
	avail, fail = true, true
	mu.Unlock()
	sp.Invalidate()
	if err := sp.Tone(context.Background(), 880, 300); err == nil || err.Error() != model.MsgMonitorInternal {
		t.Fatalf("refused tone: %v", err)
	}
	if st, fresh := sp.Cached(); !fresh || st.Available || st.Reason != model.MsgMonitorInternal {
		t.Fatalf("after refusal: %+v %v", st, fresh)
	}

	// no capabilities → no request, reason speaker.no_cap
	ms2 := &manualServer{}
	dial2 := ms2.dialer(model.IPCMessage{Type: model.MsgHello, Version: 2, Heartbeat: &model.Heartbeat{Protocol: 2}}, func(r model.IPCRequest) *model.IPCMessage {
		if r.Cmd == model.CmdGetStatus {
			return &model.IPCMessage{Type: model.MsgStatus, Status: &model.StatusSnapshot{}}
		}
		return &model.IPCMessage{Type: model.MsgError, Code: model.ErrCodeUnknownCommand}
	})
	s2 := New(testOptions(newFakeClock(), dial2))
	runSession(t, s2)
	waitFor(t, "connected", connected(s2))
	sp2 := &Speaker{C: s2}
	if st := sp2.Status(context.Background()); st.Available || st.Reason != SpeakerNoCap {
		t.Fatalf("no cap: %+v", st)
	}
	if err := sp2.Tone(context.Background(), 880, 300); err == nil || err.Error() != SpeakerNoCap {
		t.Fatalf("no cap tone: %v", err)
	}
	if _, err := s2.Do(context.Background(), model.IPCRequest{Cmd: model.CmdSpeakerTone}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("speaker_tone without the capability: %v", err)
	}
	for _, c := range ms2.got() {
		if strings.HasPrefix(c, "speaker") {
			t.Fatalf("speaker request to a service without the capability: %v", ms2.got())
		}
	}
	// not connected
	if st := (&Speaker{C: New(Options{})}).Status(context.Background()); st.Available || st.Reason != SpeakerNoService {
		t.Fatalf("not connected: %+v", st)
	}
	if st := (*Speaker)(nil).Status(context.Background()); st.Reason != SpeakerNoService {
		t.Fatalf("nil speaker: %+v", st)
	}
}
