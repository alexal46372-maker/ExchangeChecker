package pcspeaker

import (
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

type fakePort struct {
	mu     sync.Mutex
	r61    byte
	writes [][]PortWrite
	reads  []uint16
	wrote  chan struct{}
	errR   error
	errW   error
}

func (f *fakePort) Read(port uint16) (byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads = append(f.reads, port)
	if f.errR != nil {
		return 0, f.errR
	}
	if port == PortPPI {
		return f.r61, nil
	}
	return 0, nil
}

func (f *fakePort) Write(batch []PortWrite) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.errW != nil {
		return f.errW
	}
	cp := append([]PortWrite(nil), batch...)
	f.writes = append(f.writes, cp)
	for _, w := range batch {
		if w.Port == PortPPI {
			f.r61 = w.Val
		}
	}
	if f.wrote != nil {
		select {
		case f.wrote <- struct{}{}:
		default:
		}
	}
	return nil
}

func (f *fakePort) seq() [][]PortWrite {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]PortWrite, len(f.writes))
	copy(out, f.writes)
	return out
}

func onSeq(freq int, r61 byte) []PortWrite {
	d := Divisor(freq)
	return []PortWrite{
		{PortTimerCmd, PITMode3Ch2},
		{PortTimerData, byte(d)},
		{PortTimerData, byte(d >> 8)},
		{PortPPI, r61 | SpeakerBits},
	}
}

func offSeq(r61 byte) []PortWrite {
	return []PortWrite{{PortPPI, r61 &^ SpeakerBits}}
}

func key(err error) string {
	ie, ok := err.(*model.IPCError)
	if !ok {
		if err == nil {
			return ""
		}
		return err.Error()
	}
	return ie.Key
}

func code(err error) string {
	ie, ok := err.(*model.IPCError)
	if !ok {
		return ""
	}
	return ie.Code
}

// TestCHK_SVC_11_DivisorAndPortWrites — exact PIT divisor and the
// enable / disable port batches (mode 3, ch2, RMW of 0x61).
func TestCHK_SVC_11_DivisorAndPortWrites(t *testing.T) {
	if Divisor(440) != uint16(PITHz/440) || Divisor(440) != 2711 {
		t.Fatalf("440 Hz divisor %d, want 2711 (1193182/440)", Divisor(440))
	}
	if Divisor(37) != uint16(PITHz/37) || Divisor(37) != 32248 {
		t.Fatalf("37 Hz divisor %d", Divisor(37))
	}
	if Divisor(32767) != uint16(PITHz/32767) || Divisor(32767) != 36 {
		t.Fatalf("32767 Hz divisor %d", Divisor(32767))
	}
	if Divisor(1000) != uint16(PITHz/1000) {
		t.Fatalf("1000 Hz divisor %d != %d", Divisor(1000), PITHz/1000)
	}
	p := &fakePort{r61: 0x20} // refresh bit already set, must be preserved
	d := New(Options{Port: p})
	if err := d.Tone("a", 440, 1000); err != nil {
		t.Fatal(err)
	}
	got := p.seq()
	want := [][]PortWrite{onSeq(440, 0x20)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("on writes %+v, want %+v", got, want)
	}
	if err := d.Stop("a"); err != nil {
		t.Fatal(err)
	}
	got = p.seq()
	want = [][]PortWrite{onSeq(440, 0x20), offSeq(0x20 | SpeakerBits)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("on+off %+v, want %+v", got, want)
	}
}

// TestCHK_SVC_11_RangeOwnerBusyAutoOff — frequency limits, cap of
// duration, owner-only stop, busy from another owner, same-owner
// replace, auto-off via an injectable timer (no sleep).
func TestCHK_SVC_11_RangeOwnerBusyAutoOff(t *testing.T) {
	p := &fakePort{r61: 0}
	after := make(chan time.Time)
	var last time.Duration
	d := New(Options{Port: p, After: func(dur time.Duration) <-chan time.Time {
		last = dur
		return after
	}})
	if st := d.Status(); !st.Available || st.Reason != "" {
		t.Fatalf("status %+v", st)
	}
	if key(d.Tone("a", 36, 100)) != model.MsgSpeakerInvalid ||
		key(d.Tone("a", 32768, 100)) != model.MsgSpeakerInvalid {
		t.Fatal("out of range accepted")
	}
	if len(p.seq()) != 0 {
		t.Fatal("rejected tone wrote ports")
	}
	if err := d.Tone("a", 1000, MaxBeepDurMs+5000); err != nil {
		t.Fatal(err)
	}
	if last != time.Duration(MaxBeepDurMs)*time.Millisecond {
		t.Fatalf("duration not capped: %s", last)
	}
	if key(d.Tone("b", 880, 100)) != model.MsgSpeakerBusy || code(d.Tone("b", 880, 100)) != model.ErrCodeBusy {
		t.Fatal("second owner not busy")
	}
	if key(d.Stop("b")) != model.MsgSpeakerNotOwner {
		t.Fatal("foreign stop")
	}
	if len(p.seq()) != 1 {
		t.Fatalf("busy/foreign stop must not write: %+v", p.seq())
	}
	if err := d.Stop("a"); err != nil {
		t.Fatal(err)
	}
}

func TestCHK_SVC_11_AutoOffTimer(t *testing.T) {
	p := &fakePort{r61: 0, wrote: make(chan struct{}, 8)}
	oldCh := make(chan time.Time)
	newCh := make(chan time.Time)
	n := 0
	d := New(Options{Port: p, After: func(time.Duration) <-chan time.Time {
		n++
		if n == 1 {
			return oldCh
		}
		return newCh
	}})
	if err := d.Tone("a", 440, 50); err != nil {
		t.Fatal(err)
	}
	<-p.wrote
	if err := d.Tone("a", 880, 50); err != nil {
		t.Fatal(err)
	}
	<-p.wrote
	oldCh <- time.Time{} // stale auto-off must not write
	// if it wrote, the next receive would see it; give the goroutine a
	// chance by checking seq under a status call (autoOff takes the same mutex)
	d.mu.Lock()
	on := d.on
	d.mu.Unlock()
	if !on {
		t.Fatal("stale auto-off turned the replacement off")
	}
	if len(p.seq()) != 2 {
		t.Fatalf("stale auto-off wrote: %+v", p.seq())
	}
	p.wrote = make(chan struct{}, 1)
	newCh <- time.Time{}
	<-p.wrote
	want := offSeq(SpeakerBits)
	got := p.seq()
	if !reflect.DeepEqual(got[len(got)-1], want) {
		t.Fatalf("auto-off %+v", got)
	}
}

func TestCHK_SVC_11_NotInstalledAndSilence(t *testing.T) {
	d := New(Options{}) // no Port
	st := d.Status()
	if st.Available || st.Reason != model.SpeakerNotInstalled {
		t.Fatalf("status %+v", st)
	}
	if key(d.Tone("a", 440, 100)) != model.MsgSpeakerNotInstalled || code(d.Tone("a", 440, 100)) != model.ErrCodeUnavailable {
		t.Fatal("tone without port")
	}
	if err := d.Stop("a"); err != nil { // stop is a no-op
		t.Fatal(err)
	}
	p := &fakePort{r61: 0x04}
	d = New(Options{Port: p})
	if err := d.Tone("a", 440, 10); err != nil {
		t.Fatal(err)
	}
	d.Silence()
	got := p.seq()
	want := [][]PortWrite{onSeq(440, 0x04), offSeq(0x04 | SpeakerBits)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("silence %+v want %+v", got, want)
	}
	// Release of a non-owner is a no-op
	if err := d.Tone("a", 1000, 10); err != nil {
		t.Fatal(err)
	}
	n := len(p.seq())
	d.Release("b")
	if len(p.seq()) != n {
		t.Fatal("release of a foreign owner wrote")
	}
	d.Release("a")
	if len(p.seq()) == n {
		t.Fatal("release of the owner did not stop")
	}
}
