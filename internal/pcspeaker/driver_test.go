package pcspeaker

import (
	"bytes"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// --- fake SCM / DeviceIoControl seam ------------------------------------------------

type ioctlRec struct {
	Code uint32
	In   []byte
	OutN int
}

type fakeDev struct {
	ppi      byte
	calls    []ioctlRec
	ioctlErr error
	panicOn  bool
	closed   int
}

func (d *fakeDev) DeviceIoControl(code uint32, in, out []byte) error {
	incopy := append([]byte(nil), in...)
	d.calls = append(d.calls, ioctlRec{Code: code, In: incopy, OutN: len(out)})
	if d.panicOn {
		panic("ioctl panic")
	}
	if d.ioctlErr != nil {
		return d.ioctlErr
	}
	port := uint16(0)
	if len(in) >= 2 {
		port = uint16(in[0]) | uint16(in[1])<<8
	}
	switch code {
	case ioctlReadPortUchar:
		if len(out) < 1 {
			return errors.New("short out")
		}
		if port == PortPPI {
			out[0] = d.ppi
		} else {
			out[0] = 0
		}
	case ioctlWritePortUchar:
		if len(in) != 3 {
			return errors.New("write buffer must be 3 bytes")
		}
		if port == PortPPI {
			d.ppi = in[2]
		}
	}
	return nil
}

func (d *fakeDev) Close() error { d.closed++; return nil }

type fakeKernel struct {
	missing                                     bool
	openErr, pathErr, startErr, stopErr, devErr error
	path                                        string
	opens, starts, stops, closes, devices       int
	dev                                         *fakeDev
	running                                     bool
}

func (k *fakeKernel) Open(name string) (bool, error) {
	k.opens++
	if name == "" {
		return false, errors.New("empty service name")
	}
	if k.openErr != nil {
		return false, k.openErr
	}
	return k.missing, nil
}
func (k *fakeKernel) ImagePath() (string, error) {
	if k.pathErr != nil {
		return "", k.pathErr
	}
	return k.path, nil
}
func (k *fakeKernel) Start() error {
	k.starts++
	if k.startErr == nil {
		k.running = true
	}
	return k.startErr
}
func (k *fakeKernel) Stop() error {
	k.stops++
	if k.stopErr == nil {
		k.running = false
	}
	return k.stopErr
}
func (k *fakeKernel) Running() (bool, error) { return k.running, nil }
func (k *fakeKernel) Close()                 { k.closes++ }
func (k *fakeKernel) OpenDevice(name string) (DeviceHandle, error) {
	k.devices++
	if k.devErr != nil {
		return nil, k.devErr
	}
	if k.dev == nil {
		k.dev = &fakeDev{}
	}
	return k.dev, nil
}

func readyKernel(t *testing.T, body []byte) (*fakeKernel, string, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "inpoutx64.sys")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := hex.EncodeToString(sha256Sum(body))
	k := &fakeKernel{path: path, dev: &fakeDev{ppi: 0x04}}
	return k, path, sum
}

func newReady(t *testing.T) (*InpOut, *fakeKernel, chan time.Time, chan struct{}) {
	t.Helper()
	k, _, sum := readyKernel(t, []byte("official-inpoutx64-sys-body"))
	idle := make(chan time.Time)
	done := make(chan struct{}, 4)
	p := NewInpOut(InpOutOptions{
		Kernel:    k,
		SHA256:    sum,
		Supported: func() bool { return true },
		After:     func(time.Duration) <-chan time.Time { return idle },
		IdleDone:  func() { done <- struct{}{} },
	})
	return p, k, idle, done
}

// --- buffer / path tests -------------------------------------------------------------

func TestCHK_SVC_12_IOCTLCodesAndBuffers(t *testing.T) {
	if ioctlReadPortUchar != 0x9C402004 || ioctlWritePortUchar != 0x9C402008 {
		t.Fatalf("ioctl codes 0x%X 0x%X", ioctlReadPortUchar, ioctlWritePortUchar)
	}
	// signed values from hwinterfacedrv.h lines 2–3 (cast via a variable:
	// a uint32 constant above MaxInt32 cannot be converted to int32 directly).
	r, w := ioctlReadPortUchar, ioctlWritePortUchar
	if int32(r) != -1673519100 || int32(w) != -1673519096 {
		t.Fatalf("signed ioctl %d %d", int32(r), int32(w))
	}
	if got := writeUcharBuf(0x43, 0xB6); !bytes.Equal(got, []byte{0x43, 0x00, 0xB6}) {
		t.Fatalf("write 0x43: %x", got)
	}
	if got := writeUcharBuf(0x42, 0x97); !bytes.Equal(got, []byte{0x42, 0x00, 0x97}) {
		t.Fatalf("write 0x42 lo: %x", got)
	}
	if got := writeUcharBuf(0x61, 0x03); !bytes.Equal(got, []byte{0x61, 0x00, 0x03}) {
		t.Fatalf("write 0x61: %x", got)
	}
	if got := readUcharIn(0x61); !bytes.Equal(got, []byte{0x61, 0x00}) {
		t.Fatalf("read in: %x", got)
	}
}

func TestCHK_SVC_12_ExpandImagePath(t *testing.T) {
	env := func(k string) string {
		switch k {
		case "SystemRoot", "WINDIR":
			return `C:\Windows`
		case "ProgramFiles":
			return `C:\Program Files`
		}
		return ""
	}
	cases := map[string]string{
		`\??\C:\Windows\System32\drivers\inpoutx64.sys`:    `C:\Windows\System32\drivers\inpoutx64.sys`,
		`\\?\C:\Windows\System32\drivers\inpoutx64.sys`:    `C:\Windows\System32\drivers\inpoutx64.sys`,
		`\SystemRoot\System32\drivers\inpoutx64.sys`:       `C:\Windows\System32\drivers\inpoutx64.sys`,
		`\systemroot\System32\drivers\inpoutx64.sys`:       `C:\Windows\System32\drivers\inpoutx64.sys`,
		`%SystemRoot%\System32\drivers\inpoutx64.sys`:      `C:\Windows\System32\drivers\inpoutx64.sys`,
		`"C:\Program Files\ExchangeChecker\inpoutx64.sys"`: `C:\Program Files\ExchangeChecker\inpoutx64.sys`,
		`"%ProgramFiles%\ExchangeChecker\inpoutx64.sys"`:   `C:\Program Files\ExchangeChecker\inpoutx64.sys`,
		`C:\Windows\System32\drivers\inpoutx64.sys`:        `C:\Windows\System32\drivers\inpoutx64.sys`,
		`"\??\C:\Windows\System32\drivers\inpoutx64.sys"`:  `C:\Windows\System32\drivers\inpoutx64.sys`,
	}
	for in, want := range cases {
		if got := expandImagePath(in, env); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}

func TestCHK_SVC_12_OfficialHashConstant(t *testing.T) {
	if config.InpOutSHA256 != "f8965fdce668692c3785afa3559159f9a18287bc0d53abb21902895a8ecf221b" {
		t.Fatalf("config.InpOutSHA256 %q", config.InpOutSHA256)
	}
	if config.InpOutServiceName != "ExchangeCheckerInpOut" || config.InpOutDevicePath != `\\.\inpoutx64` {
		t.Fatalf("config constants %q %q", config.InpOutServiceName, config.InpOutDevicePath)
	}
	p := NewInpOut(InpOutOptions{Supported: func() bool { return false }})
	if p.o.ServiceName != config.InpOutServiceName || p.o.DeviceName != config.InpOutDevicePath || p.o.SHA256 != config.InpOutSHA256 {
		t.Fatalf("NewInpOut defaults %q %q %q", p.o.ServiceName, p.o.DeviceName, p.o.SHA256)
	}
}

func TestCHK_SVC_12_UnsupportedOS(t *testing.T) {
	p := NewInpOut(InpOutOptions{Supported: func() bool { return false }, Kernel: &fakeKernel{}})
	st := p.Status()
	if st.Available || st.Reason != model.SpeakerUnsupportedOS {
		t.Fatalf("status %+v", st)
	}
	if _, err := p.Read(PortPPI); err == nil {
		t.Fatal("read on unsupported OS")
	}
}

func TestCHK_SVC_12_NotInstalled(t *testing.T) {
	p := NewInpOut(InpOutOptions{Supported: func() bool { return true }, Kernel: &fakeKernel{missing: true}})
	st := p.Status()
	if st.Available || st.Reason != model.SpeakerNotInstalled {
		t.Fatalf("status %+v", st)
	}
	_, err := p.Read(PortPPI)
	if key(err) != model.MsgSpeakerNotInstalled {
		t.Fatalf("read err %v", err)
	}
}

func TestCHK_SVC_12_HashMismatchDoesNotStart(t *testing.T) {
	k, _, _ := readyKernel(t, []byte("official"))
	p := NewInpOut(InpOutOptions{
		Kernel:    k,
		SHA256:    hex.EncodeToString(sha256Sum([]byte("other"))),
		Supported: func() bool { return true },
	})
	st := p.Status()
	if st.Available || st.Reason != model.SpeakerBlocked {
		t.Fatalf("status %+v", st)
	}
	if _, err := p.Read(PortPPI); err == nil {
		t.Fatal("read of blocked driver")
	}
	if k.starts != 0 || k.devices != 0 {
		t.Fatalf("started or opened a blocked driver: starts=%d devices=%d", k.starts, k.devices)
	}
}

func TestCHK_SVC_12_UnreadableSysIsBlocked(t *testing.T) {
	k := &fakeKernel{path: `\??\C:\missing\inpoutx64.sys`}
	p := NewInpOut(InpOutOptions{
		Kernel:    k,
		SHA256:    config.InpOutSHA256,
		Supported: func() bool { return true },
		Getenv:    func(string) string { return `C:\Windows` },
	})
	st := p.Status()
	if st.Available || st.Reason != model.SpeakerBlocked {
		t.Fatalf("status %+v", st)
	}
	if k.starts != 0 {
		t.Fatal("started without a readable sys")
	}
}

func TestCHK_SVC_12_StartFailOpenFailed(t *testing.T) {
	k, _, sum := readyKernel(t, []byte("sys"))
	k.startErr = errors.New("start failed")
	p := NewInpOut(InpOutOptions{Kernel: k, SHA256: sum, Supported: func() bool { return true }})
	if st := p.Status(); !st.Available { // hash is ok, not yet started
		t.Fatalf("pre-start status %+v", st)
	}
	if _, err := p.Read(PortPPI); err == nil {
		t.Fatal("expected start error")
	}
	if p.Status().Reason != model.SpeakerOpenFailed {
		t.Fatalf("after start fail %+v", p.Status())
	}
}

func TestCHK_SVC_12_OpenDeviceFail(t *testing.T) {
	k, _, sum := readyKernel(t, []byte("sys"))
	k.devErr = errors.New("createfile failed")
	p := NewInpOut(InpOutOptions{Kernel: k, SHA256: sum, Supported: func() bool { return true }})
	if _, err := p.Read(PortPPI); err == nil {
		t.Fatal("expected open error")
	}
	if k.starts != 1 || k.stops != 1 {
		t.Fatalf("start/stop on open fail starts=%d stops=%d", k.starts, k.stops)
	}
	if p.Status().Reason != model.SpeakerOpenFailed {
		t.Fatalf("status %+v", p.Status())
	}
}

func TestCHK_SVC_12_IOCTLFailModuleFailed(t *testing.T) {
	p, k, _, _ := newReady(t)
	k.dev.ioctlErr = errors.New("ioctl failed")
	if _, err := p.Read(PortPPI); err == nil {
		t.Fatal("expected ioctl error")
	}
	if p.Status().Reason != model.SpeakerModuleFailed {
		t.Fatalf("status %+v", p.Status())
	}
}

// --- exact port-write IOCTL sequence + idle stop ------------------------------------

func TestCHK_SVC_12_ToneIOCTLSequenceAndIdleStop(t *testing.T) {
	p, k, _, _ := newReady(t)
	d := New(Options{Port: p, After: func(time.Duration) <-chan time.Time { return make(chan time.Time) }})
	if st := d.Status(); !st.Available || st.Reason != "" || st.DriverVersion != inpOutDriverVersion {
		t.Fatalf("device status %+v", st)
	}
	if err := d.Tone("a", 440, 1000); err != nil {
		t.Fatal(err)
	}
	if k.starts != 1 || k.devices != 1 {
		t.Fatalf("load starts=%d devices=%d", k.starts, k.devices)
	}
	div := Divisor(440) // 2711 = 0x0A97
	want := []ioctlRec{
		{ioctlReadPortUchar, []byte{0x61, 0x00}, 1},
		{ioctlWritePortUchar, []byte{0x43, 0x00, PITMode3Ch2}, 0},
		{ioctlWritePortUchar, []byte{0x42, 0x00, byte(div)}, 0},
		{ioctlWritePortUchar, []byte{0x42, 0x00, byte(div >> 8)}, 0},
		{ioctlWritePortUchar, []byte{0x61, 0x00, 0x04 | SpeakerBits}, 0},
	}
	if !reflect.DeepEqual(k.dev.calls, want) {
		t.Fatalf("on sequence\n got %#v\nwant %#v", k.dev.calls, want)
	}
	d.Silence() // immediate Unload (stop / shutdown / suspend)
	if k.stops < 1 || k.dev.closed < 1 {
		t.Fatalf("Silence must unload immediately: stops=%d closed=%d", k.stops, k.dev.closed)
	}

	// A later tone, then Stop (not Silence): idle timer stops the service.
	p2, k2, idle2, done2 := newReady(t)
	d2 := New(Options{Port: p2, After: func(time.Duration) <-chan time.Time { return make(chan time.Time) }})
	if err := d2.Tone("a", 440, 1000); err != nil {
		t.Fatal(err)
	}
	if err := d2.Stop("a"); err != nil {
		t.Fatal(err)
	}
	if k2.stops != 0 {
		t.Fatalf("Stop must not unload immediately, stops=%d", k2.stops)
	}
	offWant := []ioctlRec{
		{ioctlReadPortUchar, []byte{0x61, 0x00}, 1},
		{ioctlWritePortUchar, []byte{0x61, 0x00, 0x04}, 0},
	}
	gotOff := k2.dev.calls[len(k2.dev.calls)-2:]
	if !reflect.DeepEqual(gotOff, offWant) {
		t.Fatalf("off sequence %#v want %#v", gotOff, offWant)
	}
	idle2 <- time.Time{}
	<-done2
	if k2.stops != 1 || k2.dev.closed != 1 {
		t.Fatalf("idle stop stops=%d closed=%d", k2.stops, k2.dev.closed)
	}
}

func TestCHK_SVC_12_IdleCancelledByNewTone(t *testing.T) {
	p, k, idle, done := newReady(t)
	hold := make(chan time.Time)
	d := New(Options{Port: p, After: func(time.Duration) <-chan time.Time { return hold }})
	if err := d.Tone("a", 440, 1000); err != nil {
		t.Fatal(err)
	}
	if err := d.Stop("a"); err != nil {
		t.Fatal(err)
	}
	if err := d.Tone("a", 880, 1000); err != nil {
		t.Fatal(err)
	}
	idle <- time.Time{} // stale idle must be ignored
	<-done
	if k.stops != 0 {
		t.Fatalf("stale idle stopped the driver during a new tone: %d", k.stops)
	}
	d.Silence()
}

func TestCHK_SVC_12_PanicRecoversAndUnloads(t *testing.T) {
	p, k, _, _ := newReady(t)
	k.dev.panicOn = true
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				t.Fatalf("panic escaped InpOut: %v", rec)
			}
		}()
		if _, err := p.Read(PortPPI); err == nil {
			t.Fatal("expected recovered error")
		}
	}()
	if k.stops != 1 {
		t.Fatalf("panic must unload, stops=%d", k.stops)
	}
}

func TestCHK_SVC_12_SystemRootImagePath(t *testing.T) {
	body := []byte("sys-body")
	dir := t.TempDir()
	k := &fakeKernel{path: `\SystemRoot\System32\drivers\inpoutx64.sys`, dev: &fakeDev{}}
	p := NewInpOut(InpOutOptions{
		Kernel:    k,
		SHA256:    hex.EncodeToString(sha256Sum(body)),
		Supported: func() bool { return true },
		Getenv: func(n string) string {
			if n == "SystemRoot" {
				return dir
			}
			return ""
		},
		ReadFile: func(got string) ([]byte, error) {
			want := dir + `\System32\drivers\inpoutx64.sys`
			if got != want {
				t.Fatalf("ReadFile path %q want %q", got, want)
			}
			return body, nil
		},
	})
	st := p.Status()
	if !st.Available {
		t.Fatalf("status %+v", st)
	}
}

func TestCHK_SVC_12_StopLeftoverDoesNotStart(t *testing.T) {
	k := &fakeKernel{running: true, missing: false}
	p := NewInpOut(InpOutOptions{Kernel: k, Supported: func() bool { return true }})
	if err := p.StopLeftover(); err != nil {
		t.Fatal(err)
	}
	if k.starts != 0 || k.stops != 1 || k.running {
		t.Fatalf("leftover: starts=%d stops=%d running=%v", k.starts, k.stops, k.running)
	}
	// missing service: no-op
	k2 := &fakeKernel{missing: true, running: true}
	p2 := NewInpOut(InpOutOptions{Kernel: k2, Supported: func() bool { return true }})
	if err := p2.StopLeftover(); err != nil {
		t.Fatal(err)
	}
	if k2.stops != 0 {
		t.Fatalf("stopped a missing service: %d", k2.stops)
	}
}

func TestCHK_SVC_12_DefaultStatusOnThisHost(t *testing.T) {
	p := NewInpOut(InpOutOptions{})
	st := p.Status()
	if st.Available {
		t.Fatalf("available on this host: %+v", st)
	}
	switch st.Reason {
	case model.SpeakerUnsupportedOS, model.SpeakerNotInstalled, model.SpeakerOpenFailed:
	default:
		t.Fatalf("unexpected reason %+v", st)
	}
}
