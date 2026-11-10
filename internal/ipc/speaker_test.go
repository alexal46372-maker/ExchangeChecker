package ipc

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/pcspeaker"
)

type recPort struct {
	pcspeaker.Port
	mu     sync.Mutex
	writes [][]pcspeaker.PortWrite
	r61    byte
}

func newRec(r61 byte) *recPort { return &recPort{r61: r61} }

func (p *recPort) Read(port uint16) (byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if port == pcspeaker.PortPPI {
		return p.r61, nil
	}
	return 0, nil
}
func (p *recPort) Write(batch []pcspeaker.PortWrite) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	cp := append([]pcspeaker.PortWrite(nil), batch...)
	p.writes = append(p.writes, cp)
	for _, w := range batch {
		if w.Port == pcspeaker.PortPPI {
			p.r61 = w.Val
		}
	}
	return nil
}

// seq is a snapshot of recorded writes (tests; recPort is also written
// from Server.drop / Release).
func (p *recPort) seq() [][]pcspeaker.PortWrite {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([][]pcspeaker.PortWrite, len(p.writes))
	for i, w := range p.writes {
		out[i] = append([]pcspeaker.PortWrite(nil), w...)
	}
	return out
}

func speakerSrv(t *testing.T, port pcspeaker.Port) (*Server, *MemListener, *pcspeaker.Device) {
	t.Helper()
	h := &fakeHandler{snap: testSnapshot()}
	s := NewServer(h, &testLog{})
	dev := pcspeaker.New(pcspeaker.Options{Port: port, DriverVersion: "test-0"})
	s.Speaker = dev
	l := NewMemListener()
	go s.Serve(l)
	t.Cleanup(func() { s.Close() })
	return s, l, dev
}

func dial(t *testing.T, l *MemListener) *Client {
	t.Helper()
	rw, err := l.Dial()
	if err != nil {
		t.Fatal(err)
	}
	c := NewClient(rw, nil)
	t.Cleanup(func() { c.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	// wait for hello by pinging (hello is a push; Call ping also works after)
	if _, err := c.Call(ctx, model.CmdPing); err != nil {
		t.Fatal(err)
	}
	return c
}

// TestCHK_IPC_20_SpeakerCapsStatusAndValidation — caps advertised; any
// client (no admin) may call speaker_status / speaker_tone; not_installed
// without a Port; arg validation; busy / owner-only stop; connection
// close silences that connection's tone (exact port writes).
func TestCHK_IPC_20_SpeakerCapsStatusAndValidation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// production-like: no Port
	s0, l0, _ := speakerSrv(t, nil)
	_ = s0
	rw, err := l0.Dial()
	if err != nil {
		t.Fatal(err)
	}
	helloCh := make(chan model.IPCMessage, 4)
	c0 := NewClient(rw, func(m model.IPCMessage) {
		if m.Type == model.MsgHello {
			helloCh <- m
		}
	})
	defer c0.Close()
	var hello model.IPCMessage
	select {
	case hello = <-helloCh:
	case <-ctx.Done():
		t.Fatal("no hello")
	}
	hasS, hasST := false, false
	for _, c := range hello.Capabilities {
		if c == model.CapSpeaker {
			hasS = true
		}
		if c == model.CapSpeakerStatus {
			hasST = true
		}
	}
	if !hasS || !hasST {
		t.Fatalf("caps %v", hello.Capabilities)
	}
	st, err := c0.Do(ctx, model.IPCRequest{Cmd: model.CmdSpeakerStatus})
	if err != nil || st.Type != model.MsgSpeakerStatus || st.Speaker == nil ||
		st.Speaker.Available || st.Speaker.Reason != model.SpeakerNotInstalled {
		t.Fatalf("status %+v %v", st, err)
	}
	tone, err := c0.Do(ctx, model.IPCRequest{Cmd: model.CmdSpeakerTone, FreqHz: 440, DurMs: 100})
	if err != nil || tone.ErrorKey != model.MsgSpeakerNotInstalled || tone.Code != model.ErrCodeUnavailable {
		t.Fatalf("tone not_installed %+v %v", tone, err)
	}
	// stop is a no-op even when not installed
	stop, err := c0.Do(ctx, model.IPCRequest{Cmd: model.CmdSpeakerTone, DurMs: 0})
	if err != nil || stop.Type != model.MsgOK || stop.Result != model.ResultToneStopped {
		t.Fatalf("stop %+v %v", stop, err)
	}

	// with a fake Port: exact writes, validation, two clients
	p := newRec(0x00)
	_, l, _ := speakerSrv(t, p)
	c1 := dial(t, l)
	c2 := dial(t, l)
	st, err = c1.Do(ctx, model.IPCRequest{Cmd: model.CmdSpeakerStatus})
	if err != nil || st.Speaker == nil || !st.Speaker.Available || st.Speaker.DriverVersion != "test-0" {
		t.Fatalf("available %+v %v", st, err)
	}
	// bad freq / negative dur
	for _, req := range []model.IPCRequest{
		{Cmd: model.CmdSpeakerTone, FreqHz: 36, DurMs: 100},
		{Cmd: model.CmdSpeakerTone, FreqHz: 32768, DurMs: 100},
		{Cmd: model.CmdSpeakerTone, FreqHz: 440, DurMs: -1},
	} {
		m, err := c1.Do(ctx, req)
		if err != nil || m.ErrorKey != model.MsgSpeakerInvalid || m.Code != model.ErrCodeInvalidRequest {
			t.Fatalf("validation %+v → %+v %v", req, m, err)
		}
	}
	if len(p.seq()) != 0 {
		t.Fatalf("invalid tone wrote %+v", p.seq())
	}
	ok, err := c1.Do(ctx, model.IPCRequest{Cmd: model.CmdSpeakerTone, FreqHz: 440, DurMs: 5000})
	if err != nil || ok.Type != model.MsgOK || ok.Result != model.ResultAccepted {
		t.Fatalf("start %+v %v", ok, err)
	}
	div := pcspeaker.Divisor(440)
	wantOn := []pcspeaker.PortWrite{
		{Port: pcspeaker.PortTimerCmd, Val: pcspeaker.PITMode3Ch2},
		{Port: pcspeaker.PortTimerData, Val: byte(div)},
		{Port: pcspeaker.PortTimerData, Val: byte(div >> 8)},
		{Port: pcspeaker.PortPPI, Val: pcspeaker.SpeakerBits},
	}
	if !reflect.DeepEqual(p.seq(), [][]pcspeaker.PortWrite{wantOn}) {
		t.Fatalf("writes %+v", p.seq())
	}
	busy, err := c2.Do(ctx, model.IPCRequest{Cmd: model.CmdSpeakerTone, FreqHz: 880, DurMs: 100})
	if err != nil || busy.ErrorKey != model.MsgSpeakerBusy || busy.Code != model.ErrCodeBusy {
		t.Fatalf("busy %+v %v", busy, err)
	}
	denied, err := c2.Do(ctx, model.IPCRequest{Cmd: model.CmdSpeakerTone, DurMs: 0})
	if err != nil || denied.ErrorKey != model.MsgSpeakerNotOwner {
		t.Fatalf("foreign stop %+v %v", denied, err)
	}
	if len(p.seq()) != 1 {
		t.Fatalf("foreign cmds wrote %+v", p.seq())
	}
	// connection close of the owner silences
	c1.Close()
	// wait for drop → Release via a subsequent status from c2 (Release
	// takes the same mutex as Status/Tone, so after this Do the off write
	// has happened if drop ran; if drop is still in flight, retry by
	// starting a tone from c2 — but that could race. Use a deadline loop
	// on p.writes without Sleep: Tone/Status from c2 after Close.)
	deadline := time.After(3 * time.Second)
	for len(p.seq()) < 2 {
		select {
		case <-deadline:
			t.Fatalf("close did not silence: %+v", p.seq())
		default:
			_, _ = c2.Do(ctx, model.IPCRequest{Cmd: model.CmdSpeakerStatus})
		}
	}
	wantOff := []pcspeaker.PortWrite{{Port: pcspeaker.PortPPI, Val: 0}}
	if !reflect.DeepEqual(p.seq()[1], wantOff) {
		t.Fatalf("close off %+v", p.seq())
	}
	// the speaker is free: c2 can start a tone
	ok, err = c2.Do(ctx, model.IPCRequest{Cmd: model.CmdSpeakerTone, FreqHz: 1000, DurMs: 100})
	if err != nil || ok.Type != model.MsgOK {
		t.Fatalf("after close %+v %v", ok, err)
	}
}

func TestCHK_IPC_20_NotInstalledOnBareServer(t *testing.T) {
	// a Server with Speaker set to a nil-Port device (the production wiring)
	s, l, _ := speakerSrv(t, nil)
	_ = s
	c := dial(t, l)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m, err := c.Do(ctx, model.IPCRequest{Cmd: model.CmdSpeakerStatus})
	if err != nil || m.Speaker == nil || m.Speaker.Available || m.Speaker.Reason != model.SpeakerNotInstalled {
		t.Fatalf("%+v %v", m, err)
	}
}
