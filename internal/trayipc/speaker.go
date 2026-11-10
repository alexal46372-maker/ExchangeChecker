package trayipc

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Phase 14 (feat/tray-speaker, CHK-TRAY-98): the client side of the PC
// speaker of the service (CHK-IPC-20: capabilities speaker /
// speaker_status; speaker_status → {type:"speaker_status", speaker:{…}};
// speaker_tone → ok, result accepted / stopped; errors
// notify.speaker_*). The owner of a tone is the connection: the alarm and
// «Прослушать» of one tray share it; another connection gets
// notify.speaker_busy. The service clips durMs to 60 s, the tray sends
// the profile duration as is. Only this file and the two
// commandCapability entries below are added to trayipc.

func init() {
	commandCapability[model.CmdSpeakerTone] = model.CapSpeaker
	commandCapability[model.CmdSpeakerStatus] = model.CapSpeakerStatus
}

// Reasons of a speaker that is not available on the tray side (i18n
// catalog keys, CHK-TRAY-99).
const (
	SpeakerNoService = "speaker.no_service" // not connected to the service
	SpeakerNoCap     = "speaker.no_cap"     // the service does not announce speaker / speaker_status
	SpeakerTimeout   = "speaker.timeout"    // no reply in time
)

// transientSpeaker: refusals that depend on the moment (another
// connection plays — notify.speaker_busy; the service allows a tone only
// during its warning/error alarm or in listen mode, one window per 10 s
// for the whole service, not per connection: a reconnect or another
// client gets no new window — notify.speaker_denied, CHK-IPC-20) — the
// tone goes through the speakers, but the speaker is NOT marked
// unavailable, so the next tone asks again.
func transientSpeaker(key string) bool {
	return key == model.MsgSpeakerBusy || key == model.MsgSpeakerDenied
}

// DefaultSpeakerTTL: how long a speaker_status reply is trusted.
const DefaultSpeakerTTL = 30 * time.Second

// DefaultSpeakerTimeout: the wait for a speaker_status / speaker_tone reply.
const DefaultSpeakerTimeout = 2 * time.Second

// SpeakerClient is the part of the Session the speaker uses (the settings
// window's session interface satisfies it too).
type SpeakerClient interface {
	Do(ctx context.Context, req model.IPCRequest) (model.IPCMessage, error)
	Status() Status
}

// Speaker caches speaker_status of one session and sends speaker_tone.
// Safe for concurrent use (the sound player goroutine and the UI).
type Speaker struct {
	C       SpeakerClient
	TTL     time.Duration // 0 = DefaultSpeakerTTL
	Timeout time.Duration // 0 = DefaultSpeakerTimeout

	mu   sync.Mutex
	st   model.SpeakerStatus
	have bool
	at   time.Time
	conn time.Time // ServiceStartedAt of the cached reply
	gen  int       // Restarts of the cached reply
}

// gate: the status without a request when the session cannot ask
// (not connected, no capability); ok = ask the service.
func (sp *Speaker) gate() (model.SpeakerStatus, Status, bool) {
	if sp == nil || sp.C == nil {
		return model.SpeakerStatus{Reason: SpeakerNoService}, Status{}, false
	}
	st := sp.C.Status()
	if st.State != StateConnected {
		return model.SpeakerStatus{Reason: SpeakerNoService}, st, false
	}
	if !st.Has(model.CapSpeaker) || !st.Has(model.CapSpeakerStatus) {
		return model.SpeakerStatus{Reason: SpeakerNoCap}, st, false
	}
	return model.SpeakerStatus{}, st, true
}

// Cached returns the speaker state without any request; fresh = false
// when the service should be asked (Status).
func (sp *Speaker) Cached() (st model.SpeakerStatus, fresh bool) {
	g, s, ok := sp.gate()
	if !ok {
		return g, true
	}
	sp.mu.Lock()
	defer sp.mu.Unlock()
	if !sp.have || !sp.conn.Equal(s.ServiceStartedAt) || sp.gen != s.Restarts {
		return model.SpeakerStatus{}, false
	}
	return sp.st, time.Since(sp.at) < sp.ttl()
}

func (sp *Speaker) ttl() time.Duration {
	if sp.TTL > 0 {
		return sp.TTL
	}
	return DefaultSpeakerTTL
}

func (sp *Speaker) timeout() time.Duration {
	if sp.Timeout > 0 {
		return sp.Timeout
	}
	return DefaultSpeakerTimeout
}

// Status returns the cached state or asks speaker_status (at most
// Timeout). A failed request counts as «not available» with its error.
func (sp *Speaker) Status(ctx context.Context) model.SpeakerStatus {
	if st, fresh := sp.Cached(); fresh {
		return st
	}
	_, s, ok := sp.gate()
	if !ok {
		st, _ := sp.Cached()
		return st
	}
	ctx, cancel := context.WithTimeout(ctx, sp.timeout())
	defer cancel()
	m, err := sp.C.Do(ctx, model.IPCRequest{Cmd: model.CmdSpeakerStatus})
	var st model.SpeakerStatus
	switch {
	case err != nil:
		st = model.SpeakerStatus{Reason: reasonOf(err)}
	case m.Type == model.MsgError:
		st = model.SpeakerStatus{Reason: replyErr(m).Error()}
	case m.Speaker == nil:
		st = model.SpeakerStatus{Reason: "speaker_status: no speaker in the reply"}
	default:
		st = *m.Speaker
	}
	sp.store(st, s)
	return st
}

func (sp *Speaker) store(st model.SpeakerStatus, s Status) {
	sp.mu.Lock()
	sp.st, sp.have, sp.at = st, true, time.Now()
	sp.conn, sp.gen = s.ServiceStartedAt, s.Restarts
	sp.mu.Unlock()
}

// Invalidate forgets the cached reply (e.g. on reconnection).
func (sp *Speaker) Invalidate() {
	if sp == nil {
		return
	}
	sp.mu.Lock()
	sp.have = false
	sp.mu.Unlock()
}

// Tone sends speaker_tone {freqHz, durMs} (durMs 0 = stop). A failure
// (except notify.speaker_busy / notify.speaker_denied: transient) marks the
// speaker not available with the error until the cache expires or the
// connection changes, so the next tones go straight to the fallback.
func (sp *Speaker) Tone(ctx context.Context, freqHz, durMs int) error {
	if g, _, ok := sp.gate(); !ok {
		return errors.New(g.Reason)
	}
	ctx, cancel := context.WithTimeout(ctx, sp.timeout())
	defer cancel()
	d := durMs // a pointer: the stop request carries "durMs":0
	req := model.IPCRequest{Cmd: model.CmdSpeakerTone, FreqHz: freqHz, DurMs: &d}
	m, err := sp.C.Do(ctx, req)
	if err == nil && m.Type == model.MsgError {
		err = replyErr(m)
	}
	if err != nil {
		err = errors.New(reasonOf(err))
		if durMs > 0 && !transientSpeaker(err.Error()) {
			sp.store(model.SpeakerStatus{Reason: err.Error()}, sp.C.Status())
		}
	}
	return err
}

// reasonOf: a catalog key for the session errors, else the error text
// (a service message key from replyErr).
func reasonOf(err error) string {
	switch {
	case errors.Is(err, ErrNotConnected):
		return SpeakerNoService
	case errors.Is(err, ErrUnsupported):
		return SpeakerNoCap
	case errors.Is(err, context.DeadlineExceeded):
		return SpeakerTimeout
	}
	return err.Error()
}

// Stop sends speaker_tone {durMs:0}.
func (sp *Speaker) Stop(ctx context.Context) error { return sp.Tone(ctx, 0, 0) }

func replyErr(m model.IPCMessage) error {
	switch {
	case m.ErrorKey != "":
		return errors.New(m.ErrorKey)
	case m.Error != "":
		return errors.New(m.Error)
	case m.Code != "":
		return errors.New(m.Code)
	}
	return errors.New("error")
}
