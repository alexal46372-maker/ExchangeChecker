package pcspeaker

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Defaults for the InpOutx64 Port come from config.InpOutServiceName,
// config.InpOutDevicePath and config.InpOutSHA256 (owned by EC Setup in
// internal/config/install.go). Official signed inpoutx64.sys is version
// "1.2 x64" (Highresolution Enterprises); the sha256 is of the Highrez
// 1.5.0.1 distribution (see third_party/inpout and docs/PHASE14.md).
const (
	inpOutDriverVersion = "1.2 x64"

	idleStopDelay = 2 * time.Second
)

type InpOutOptions struct {
	ServiceName string
	DeviceName  string
	SHA256      string // lowercase hex of the official sys; tests may override
	Kernel      Kernel
	ReadFile    func(path string) ([]byte, error)
	Getenv      func(string) string
	Supported   func() bool
	After       func(d time.Duration) <-chan time.Time
	IdleDelay   time.Duration
	// IdleDone is called after an idle-stop goroutine has decided whether
	// to unload (tests). Production leaves it nil.
	IdleDone func()
}

// Kernel is the injectable SCM + CreateFile/DeviceIoControl seam (CHK-SVC-12).
// Tests supply a fake; production uses the Windows wrappers in driver_windows.go.
type Kernel interface {
	// Open looks up the named kernel-driver service.
	// missing=true if the service is not installed (ERROR_SERVICE_DOES_NOT_EXIST).
	Open(name string) (missing bool, err error)
	ImagePath() (string, error)
	Start() error
	Stop() error
	// Running is true when the opened service is SERVICE_RUNNING.
	Running() (bool, error)
	Close()
	OpenDevice(name string) (DeviceHandle, error)
}

// DeviceHandle is an open \\.\inpoutx64 handle.
type DeviceHandle interface {
	DeviceIoControl(code uint32, in, out []byte) error
	Close() error
}

// InpOut is a Port that talks to the official signed InpOutx64 driver
// for the duration of a tone only (StartService before I/O; ControlService
// STOP after silence + IdleDelay). The driver file is hashed before load.
type InpOut struct {
	o         InpOutOptions
	mu        sync.Mutex
	dev       DeviceHandle
	loaded    bool
	last      model.SpeakerStatus
	unloadGen uint64
}

// NewInpOut returns the production (or test-injected) InpOutx64 Port.
func NewInpOut(o InpOutOptions) *InpOut {
	if o.ServiceName == "" {
		o.ServiceName = config.InpOutServiceName
	}
	if o.DeviceName == "" {
		o.DeviceName = config.InpOutDevicePath
	}
	if o.SHA256 == "" {
		o.SHA256 = config.InpOutSHA256
	}
	if o.ReadFile == nil {
		o.ReadFile = os.ReadFile
	}
	if o.Getenv == nil {
		o.Getenv = os.Getenv
	}
	if o.Supported == nil {
		o.Supported = func() bool { return runtime.GOOS == "windows" && runtime.GOARCH == "amd64" }
	}
	if o.After == nil {
		o.After = time.After
	}
	if o.IdleDelay == 0 {
		o.IdleDelay = idleStopDelay
	}
	if o.Kernel == nil {
		o.Kernel = defaultKernel()
	}
	return &InpOut{o: o}
}

// Status implements Device's optional Status-on-Port hook.
func (p *InpOut) Status() model.SpeakerStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.probeLocked()
}

func (p *InpOut) probeLocked() model.SpeakerStatus {
	// A failed start / IOCTL is sticky until a later successful load or I/O:
	// re-hashing the official file would otherwise look "available" again.
	if p.loaded && p.last.Reason == model.SpeakerModuleFailed {
		return p.last
	}
	st := p.checkLocked(false)
	if st.Available && !p.loaded &&
		(p.last.Reason == model.SpeakerOpenFailed || p.last.Reason == model.SpeakerModuleFailed) {
		return p.last
	}
	p.last = st
	return st
}

// checkLocked returns the current speaker_status reason.
// When keep is true the SCM handle is left open after a successful hash
// so the caller can StartService (ensureLocked). Status passes keep=false.
func (p *InpOut) checkLocked(keep bool) model.SpeakerStatus {
	if p.o.Supported == nil || !p.o.Supported() {
		if !keep {
			// nothing opened
		}
		return model.SpeakerStatus{Reason: model.SpeakerUnsupportedOS}
	}
	if p.loaded {
		return model.SpeakerStatus{Available: true, DriverVersion: inpOutDriverVersion}
	}
	if p.o.Kernel == nil {
		return model.SpeakerStatus{Reason: model.SpeakerOpenFailed}
	}
	missing, err := p.o.Kernel.Open(p.o.ServiceName)
	if err != nil {
		p.o.Kernel.Close()
		return model.SpeakerStatus{Reason: model.SpeakerOpenFailed}
	}
	if missing {
		p.o.Kernel.Close()
		return model.SpeakerStatus{Reason: model.SpeakerNotInstalled}
	}
	if !keep {
		defer p.o.Kernel.Close()
	}
	path, err := p.o.Kernel.ImagePath()
	if err != nil || strings.TrimSpace(path) == "" {
		if keep {
			p.o.Kernel.Close()
		}
		return model.SpeakerStatus{Reason: model.SpeakerOpenFailed}
	}
	path = expandImagePath(path, p.o.Getenv)
	raw, err := p.o.ReadFile(path)
	if err != nil {
		if keep {
			p.o.Kernel.Close()
		}
		// Cannot verify the official signed binary — refuse to load.
		return model.SpeakerStatus{Reason: model.SpeakerBlocked}
	}
	sum := hex.EncodeToString(sha256Sum(raw))
	if sum != strings.ToLower(p.o.SHA256) {
		if keep {
			p.o.Kernel.Close()
		}
		return model.SpeakerStatus{Reason: model.SpeakerBlocked}
	}
	return model.SpeakerStatus{Available: true, DriverVersion: inpOutDriverVersion}
}

func sha256Sum(raw []byte) []byte {
	s := sha256.Sum256(raw)
	return s[:]
}

func (p *InpOut) ensureLocked() error {
	p.unloadGen++ // a new tone cancels a pending idle stop
	if p.loaded {
		return nil
	}
	st := p.checkLocked(true)
	p.last = st
	if !st.Available {
		return statusErr(st.Reason)
	}
	if err := p.o.Kernel.Start(); err != nil {
		p.o.Kernel.Close()
		p.last = model.SpeakerStatus{Reason: model.SpeakerOpenFailed}
		return err
	}
	dev, err := p.o.Kernel.OpenDevice(p.o.DeviceName)
	if err != nil {
		_ = p.o.Kernel.Stop()
		p.o.Kernel.Close()
		p.last = model.SpeakerStatus{Reason: model.SpeakerOpenFailed}
		return err
	}
	p.dev = dev
	p.loaded = true
	p.last = model.SpeakerStatus{Available: true, DriverVersion: inpOutDriverVersion}
	return nil
}

func (p *InpOut) Read(port uint16) (b byte, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	defer func() {
		if rec := recover(); rec != nil {
			p.unloadLocked()
			err = errModuleFailed()
		}
	}()
	if err = p.ensureLocked(); err != nil {
		return 0, err
	}
	in := readUcharIn(port)
	out := []byte{0}
	if err = p.dev.DeviceIoControl(ioctlReadPortUchar, in, out); err != nil {
		p.last = model.SpeakerStatus{Reason: model.SpeakerModuleFailed}
		return 0, err
	}
	return out[0], nil
}

func (p *InpOut) Write(batch []PortWrite) (err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	defer func() {
		if rec := recover(); rec != nil {
			p.unloadLocked()
			err = errModuleFailed()
		}
	}()
	if err = p.ensureLocked(); err != nil {
		return err
	}
	for _, w := range batch {
		buf := writeUcharBuf(w.Port, w.Val)
		if err = p.dev.DeviceIoControl(ioctlWritePortUchar, buf, nil); err != nil {
			p.last = model.SpeakerStatus{Reason: model.SpeakerModuleFailed}
			return err
		}
	}
	return nil
}

// ScheduleUnload stops the kernel driver after IdleDelay if no new tone starts.
func (p *InpOut) ScheduleUnload() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.unloadGen++
	g := p.unloadGen
	ch := p.o.After(p.o.IdleDelay)
	go func() {
		<-ch
		p.mu.Lock()
		if p.unloadGen == g {
			p.unloadLocked()
		}
		done := p.o.IdleDone
		p.mu.Unlock()
		if done != nil {
			done()
		}
	}()
}

// Unload closes the device and stops the service immediately (service
// stop / shutdown / suspend / panic recover).
func (p *InpOut) Unload() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.unloadLocked()
}

// StopLeftover stops a leftover running kernel driver at Exchange Checker
// service start. It does not start the driver or open the device, and it
// does not fail the service if the driver is absent or already stopped.
func (p *InpOut) StopLeftover() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.loaded {
		p.unloadLocked()
		return nil
	}
	if p.o.Supported != nil && !p.o.Supported() {
		return nil
	}
	if p.o.Kernel == nil {
		return nil
	}
	missing, err := p.o.Kernel.Open(p.o.ServiceName)
	if err != nil {
		p.o.Kernel.Close()
		return err
	}
	if missing {
		p.o.Kernel.Close()
		return nil
	}
	defer p.o.Kernel.Close()
	run, err := p.o.Kernel.Running()
	if err != nil || !run {
		return err
	}
	return p.o.Kernel.Stop()
}

func (p *InpOut) unloadLocked() {
	p.unloadGen++ // cancel a pending idle stop
	if p.dev != nil {
		_ = p.dev.Close()
		p.dev = nil
	}
	if p.o.Kernel != nil {
		_ = p.o.Kernel.Stop()
		p.o.Kernel.Close()
	}
	p.loaded = false
}

func statusErr(reason string) error {
	switch reason {
	case model.SpeakerNotInstalled:
		return unavailable()
	default:
		return &model.IPCError{Code: model.ErrCodeUnavailable, Key: model.MsgSpeakerFailed, Message: "speaker: " + reason}
	}
}

func errModuleFailed() error {
	return &model.IPCError{Code: model.ErrCodeFailed, Key: model.MsgSpeakerFailed, Message: "speaker: module failed"}
}
