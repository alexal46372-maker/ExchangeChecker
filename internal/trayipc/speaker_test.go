package trayipc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/ipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/pcspeaker"
)

// ioPort is the I/O port of the service's pcspeaker.Device in tests; it
// also reports speaker_status itself (like the InpOutx64 port).
type ioPort struct {
	mu     sync.Mutex
	st     model.SpeakerStatus
	fail   error
	r61    byte
	on     bool
	div    uint16
	starts []uint16 // divisors of the started tones
	offs   int
	asked  int
}

func (p *ioPort) Read(port uint16) (byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.r61, nil
}

func (p *ioPort) Write(b []pcspeaker.PortWrite) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.fail != nil {
		return p.fail
	}
	var lo, hi byte
	n := 0
	for _, w := range b {
		switch w.Port {
		case pcspeaker.PortTimerData:
			if n == 0 {
				lo = w.Val
			} else {
				hi = w.Val
			}
			n++
		case pcspeaker.PortPPI:
			p.r61 = w.Val
			on := w.Val&pcspeaker.SpeakerBits == pcspeaker.SpeakerBits
			if on {
				p.starts = append(p.starts, uint16(lo)|uint16(hi)<<8)
			} else if p.on {
				p.offs++
			}
			p.on = on
		}
	}
	return nil
}

func (p *ioPort) Status() model.SpeakerStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.asked++
	return p.st
}

func (p *ioPort) set(st model.SpeakerStatus, fail error) {
	p.mu.Lock()
	p.st, p.fail = st, fail
	p.mu.Unlock()
}

func (p *ioPort) snap() (starts []uint16, offs, asked int, on bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]uint16(nil), p.starts...), p.offs, p.asked, p.on
}

// TestCHK_TRAY_98_SpeakerClient — the client of speaker_status /
// speaker_tone against the real ipc.Server of the service with its
// pcspeaker.Device on a fake I/O port: the cached status (one
// speaker_status until Invalidate), tone and stop (durMs 0) reach the
// port, the service clips durMs (the tray does not refuse > 10 s), the
// owner is the connection (another one gets notify.speaker_busy, which
// does not mark the speaker unavailable), a failing port gives
// notify.speaker_failed and marks it unavailable, and a service without
// the capabilities gets no request at all.
func TestCHK_TRAY_98_SpeakerClient(t *testing.T) {
	stopReq, _ := json.Marshal(model.IPCRequest{Cmd: model.CmdSpeakerTone, DurMs: ms(0)})
	if !strings.Contains(string(stopReq), `"durMs":0`) {
		t.Fatalf("stop request %s", stopReq)
	}
	port := &ioPort{st: model.SpeakerStatus{Available: true, DriverVersion: "1.5.0.1"}}
	var dmu sync.Mutex
	var durs []time.Duration
	dev := pcspeaker.New(pcspeaker.Options{Port: port, After: func(d time.Duration) <-chan time.Time {
		dmu.Lock()
		durs = append(durs, d)
		dmu.Unlock()
		return time.After(d)
	}})
	l := ipc.NewMemListener()
	srv := ipc.NewServer(&fakeHandler{}, nil)
	srv.Speaker = dev
	go srv.Serve(l)
	t.Cleanup(func() { srv.Close() })
	dial := func(context.Context) (io.ReadWriteCloser, error) { return l.Dial() }
	newSess := func(d Dialer) *Session {
		s := New(testOptions(newFakeClock(), d))
		runSession(t, s)
		waitFor(t, "connected", connected(s))
		return s
	}
	s := newSess(dial)
	sp := &Speaker{C: s}
	if _, fresh := sp.Cached(); fresh {
		t.Fatal("fresh before the first speaker_status")
	}
	// the reply is {type:"speaker_status", speaker:{…}}
	if m, err := s.Do(context.Background(), model.IPCRequest{Cmd: model.CmdSpeakerStatus}); err != nil || m.Type != model.MsgSpeakerStatus || m.Speaker == nil || !m.Speaker.Available {
		t.Fatalf("speaker_status reply %+v %v", m, err)
	}
	_, _, asked0, _ := port.snap()
	for i := 0; i < 3; i++ {
		if st := sp.Status(context.Background()); !st.Available || st.DriverVersion != "1.5.0.1" {
			t.Fatalf("status %+v", st)
		}
	}
	if _, _, asked, _ := port.snap(); asked-asked0 != 1 {
		t.Fatalf("speaker_status asked %d times, want 1 (cache)", asked-asked0)
	}
	// tone → accepted, on the port; stop → stopped, off
	if m, err := s.Do(context.Background(), model.IPCRequest{Cmd: model.CmdSpeakerTone, FreqHz: 440, DurMs: ms(5000)}); err != nil || m.Result != model.ResultAccepted {
		t.Fatalf("speaker_tone %+v %v", m, err)
	}
	if m, err := s.Do(context.Background(), model.IPCRequest{Cmd: model.CmdSpeakerTone}); err != nil || m.Type != model.MsgError || m.ErrorKey != model.MsgSpeakerInvalid {
		t.Fatalf("speaker_tone without durMs: %+v %v", m, err)
	}
	if m, err := s.Do(context.Background(), model.IPCRequest{Cmd: model.CmdSpeakerTone, DurMs: ms(0)}); err != nil || m.Result != model.ResultToneStopped {
		t.Fatalf("stop %+v %v", m, err)
	}
	if err := sp.Tone(context.Background(), 880, 120000); err != nil { // > 60 s: the service clips it
		t.Fatal(err)
	}
	starts, offs, _, on := port.snap()
	if len(starts) != 2 || starts[0] != pcspeaker.Divisor(440) || starts[1] != pcspeaker.Divisor(880) || offs != 1 || !on {
		t.Fatalf("port: starts %v offs %d on %v", starts, offs, on)
	}
	dmu.Lock()
	if len(durs) != 2 || durs[0] != 5*time.Second || durs[1] != time.Duration(pcspeaker.MaxBeepDurMs)*time.Millisecond {
		t.Errorf("durations %v (want 5 s and the service cap 60 s)", durs)
	}
	dmu.Unlock()

	// another connection while this one plays → busy, not cached
	s2 := newSess(dial)
	sp2 := &Speaker{C: s2}
	if err := sp2.Tone(context.Background(), 1000, 300); err == nil || err.Error() != model.MsgSpeakerBusy {
		t.Fatalf("busy: %v", err)
	}
	if st := sp2.Status(context.Background()); !st.Available {
		t.Fatalf("busy must not mark the speaker unavailable: %+v", st)
	}
	// only the owner may stop it (the other one gets not_owner)
	if err := sp2.Stop(context.Background()); err == nil || err.Error() != model.MsgSpeakerNotOwner {
		t.Fatalf("not owner: %v", err)
	}
	if err := sp.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, offs, _, on := port.snap(); offs != 2 || on {
		t.Fatalf("owner stop: offs %d on %v", offs, on)
	}

	// not available → reason from the service, cached
	port.set(model.SpeakerStatus{Reason: model.SpeakerNotInstalled}, nil)
	sp.Invalidate()
	if st := sp.Status(context.Background()); st.Available || st.Reason != model.SpeakerNotInstalled {
		t.Fatalf("after Invalidate: %+v", st)
	}
	if err := sp.Tone(context.Background(), 880, 300); err == nil || err.Error() != model.MsgSpeakerNotInstalled {
		t.Fatalf("tone while not installed: %v", err)
	}
	// a failing port → notify.speaker_failed, marks it unavailable
	port.set(model.SpeakerStatus{Available: true}, errors.New("ioctl"))
	sp.Invalidate()
	if err := sp.Tone(context.Background(), 880, 300); err == nil || err.Error() != model.MsgSpeakerFailed {
		t.Fatalf("refused tone: %v", err)
	}
	if st, fresh := sp.Cached(); !fresh || st.Available || st.Reason != model.MsgSpeakerFailed {
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
	s3 := newSess(dial2)
	sp3 := &Speaker{C: s3}
	if st := sp3.Status(context.Background()); st.Available || st.Reason != SpeakerNoCap {
		t.Fatalf("no cap: %+v", st)
	}
	if err := sp3.Tone(context.Background(), 880, 300); err == nil || err.Error() != SpeakerNoCap {
		t.Fatalf("no cap tone: %v", err)
	}
	if _, err := s3.Do(context.Background(), model.IPCRequest{Cmd: model.CmdSpeakerTone, DurMs: ms(0)}); !errors.Is(err, ErrUnsupported) {
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

func ms(v int) *int { return &v }
