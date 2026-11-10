package pcspeaker

import (
	"sync"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Phase 14 (CHK-SVC-11): PC-speaker tone, driver-independent. A Port
// writes I/O ports 0x42/0x43/0x61 in batches; no driver is shipped in
// this package; the InpOutx64 Port is wired by the service (CHK-SVC-12).
//
// PIT channel 2, mode 3, square wave: 0x43←0xB6, 0x42←lo, 0x42←hi of
// divisor 1193182/F (integer division); enable 0x61 |= 0x03; off
// 0x61 &= ^0x03 (read-modify-write). One tone at a time. A new tone from
// a different owner is rejected (busy); the same owner may replace its
// tone. Only the owner may Stop; Silence (service stop / shutdown /
// preshutdown / PBT_APMSUSPEND) always turns the speaker off.

const (
	// PITHz is the 8253/8254 channel-2 input clock (Hz).
	PITHz = 1193182
	// PortTimerData is the PIT channel-2 data port.
	PortTimerData uint16 = 0x42
	// PortTimerCmd is the PIT command port.
	PortTimerCmd uint16 = 0x43
	// PortPPI is the PPI port (speaker gate + data on bits 0 and 1).
	PortPPI uint16 = 0x61
	// PITMode3Ch2 is "channel 2, lobyte/hibyte, mode 3, binary".
	PITMode3Ch2 byte = 0xB6
	// SpeakerBits are the two PPI bits that enable the speaker.
	SpeakerBits byte = 0x03

	// MinFreqHz / MaxFreqHz: the same range as Win32 Beep. 37 Hz is the
	// lowest frequency whose divisor still fits in 16 bits with margin
	// under PITHz; 32767 Hz is the historical Beep maximum.
	MinFreqHz = 37
	MaxFreqHz = 32767
	// MaxBeepDurMs caps a single tone (durations above are clipped).
	MaxBeepDurMs = 60_000
)

// Port is the I/O-port backend (a future kernel driver, or a test fake).
// Write applies every operation of the batch in order; Read is used for
// the read-modify-write of port 0x61.
type Port interface {
	Read(port uint16) (byte, error)
	Write(batch []PortWrite) error
}

// PortWrite is one byte written to an I/O port.
type PortWrite struct {
	Port uint16
	Val  byte
}

// Options of New; zero = production defaults (no Port → not installed).
type Options struct {
	Port Port
	// Reason / DriverVersion are reported by Status when Port != nil
	// (tests). With a nil Port, Status is always not_installed.
	Reason        string
	DriverVersion string
	// After(d) fires after d (nil = time.After). Tests inject a channel.
	After func(d time.Duration) <-chan time.Time
	Now   func() time.Time
}

// Device is the process-wide PC speaker. Safe for concurrent use.
type Device struct {
	o     Options
	mu    sync.Mutex
	on    bool
	owner any
	gen   uint64
}

// New returns a Device. Call Silence when the service is stopping.
func New(o Options) *Device {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.After == nil {
		o.After = time.After
	}
	return &Device{o: o}
}

// statusPort is a Port that reports speaker_status itself (InpOutx64).
type statusPort interface {
	Status() model.SpeakerStatus
}

// Status is the speaker_status payload.
func (d *Device) Status() model.SpeakerStatus {
	if d == nil {
		return model.SpeakerStatus{Available: false, Reason: model.SpeakerNotInstalled}
	}
	if sp, ok := d.o.Port.(statusPort); ok {
		return sp.Status()
	}
	if d.o.Port == nil {
		return model.SpeakerStatus{Available: false, Reason: model.SpeakerNotInstalled}
	}
	st := model.SpeakerStatus{Available: true, DriverVersion: d.o.DriverVersion}
	if d.o.Reason != "" {
		st.Available = false
		st.Reason = d.o.Reason
	}
	return st
}

func (d *Device) available() bool {
	return d != nil && d.Status().Available
}

// Tone starts a tone of freqHz for durMs milliseconds owned by owner
// (the IPC connection). durMs <= 0 stops the tone if owner is the
// current owner (or if nothing is playing). Duration is capped at
// MaxBeepDurMs. A tone from another owner while one is playing is busy.
func (d *Device) Tone(owner any, freqHz, durMs int) error {
	if durMs <= 0 {
		return d.Stop(owner)
	}
	if !d.available() {
		return unavailableReason(d.Status().Reason)
	}
	if freqHz < MinFreqHz || freqHz > MaxFreqHz {
		return invalid()
	}
	if durMs > MaxBeepDurMs {
		durMs = MaxBeepDurMs
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.on && d.owner != owner {
		return busy()
	}
	if err := d.programLocked(freqHz); err != nil {
		return failed(err)
	}
	d.on, d.owner = true, owner
	d.gen++
	g := d.gen
	ch := d.o.After(time.Duration(durMs) * time.Millisecond)
	go d.autoOff(g, ch)
	return nil
}

// Stop turns the tone off if owner started it (or if nothing is on).
func (d *Device) Stop(owner any) error {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.on {
		return nil
	}
	if d.owner != owner {
		return notOwner()
	}
	return d.offLocked()
}

// Release stops the tone if owner started it; no error if not (connection close).
func (d *Device) Release(owner any) {
	if err := d.Stop(owner); err != nil && !isNotOwner(err) {
		return
	}
}

// unloader is a Port that can stop the kernel driver after silence.
type unloader interface {
	ScheduleUnload()
	Unload()
}

// Silence turns the speaker off regardless of owner (stop / shutdown /
// preshutdown / suspend) and unloads the kernel driver immediately.
// Safe on a nil Device.
func (d *Device) Silence() {
	if d == nil {
		return
	}
	d.mu.Lock()
	_ = d.offLocked()
	d.mu.Unlock()
	if u, ok := d.o.Port.(unloader); ok {
		u.Unload()
	}
}

func (d *Device) autoOff(gen uint64, ch <-chan time.Time) {
	<-ch
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.gen == gen && d.on {
		_ = d.offLocked()
	}
}

func (d *Device) programLocked(freqHz int) error {
	div := Divisor(freqHz)
	cur, err := d.o.Port.Read(PortPPI)
	if err != nil {
		return err
	}
	return d.o.Port.Write([]PortWrite{
		{PortTimerCmd, PITMode3Ch2},
		{PortTimerData, byte(div)},
		{PortTimerData, byte(div >> 8)},
		{PortPPI, cur | SpeakerBits},
	})
}

func (d *Device) offLocked() error {
	d.gen++ // invalidate an in-flight auto-off
	if !d.on {
		return nil
	}
	d.on, d.owner = false, nil
	if d.o.Port == nil {
		return nil
	}
	cur, err := d.o.Port.Read(PortPPI)
	if err != nil {
		return err
	}
	err = d.o.Port.Write([]PortWrite{{PortPPI, cur &^ SpeakerBits}})
	if err == nil {
		if u, ok := d.o.Port.(unloader); ok {
			u.ScheduleUnload()
		}
	}
	return err
}

// Divisor is PITHz/freqHz (integer division), the 16-bit PIT latch.
func Divisor(freqHz int) uint16 {
	if freqHz < 1 {
		freqHz = 1
	}
	n := PITHz / freqHz
	if n > 0xFFFF {
		n = 0xFFFF
	}
	return uint16(n)
}

func unavailable() error {
	return &model.IPCError{Code: model.ErrCodeUnavailable, Key: model.MsgSpeakerNotInstalled, Message: "speaker: not installed"}
}

func unavailableReason(reason string) error {
	if reason == model.SpeakerNotInstalled || reason == "" {
		return unavailable()
	}
	return &model.IPCError{Code: model.ErrCodeUnavailable, Key: model.MsgSpeakerFailed, Message: "speaker: " + reason}
}
func busy() error {
	return &model.IPCError{Code: model.ErrCodeBusy, Key: model.MsgSpeakerBusy, Message: "speaker: busy"}
}
func notOwner() error {
	return &model.IPCError{Code: model.ErrCodeFailed, Key: model.MsgSpeakerNotOwner, Message: "speaker: not owner"}
}
func invalid() error {
	return &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgSpeakerInvalid, Message: "speaker: invalid frequency or duration",
		Args: []string{"freqHz", model.ValRange}}
}
func failed(err error) error {
	return &model.IPCError{Code: model.ErrCodeFailed, Key: model.MsgSpeakerFailed, Message: "speaker: port i/o failed"}
}

func isNotOwner(err error) bool {
	ie, ok := err.(*model.IPCError)
	return ok && ie.Key == model.MsgSpeakerNotOwner
}
