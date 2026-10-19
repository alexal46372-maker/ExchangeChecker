package monitor

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Phase 10 (EC Service, CHK-MON-16): one worker goroutine per enabled
// profile, with its own timer and context. A hung database holds only its
// own profile (its worker waits in pollOne, which is cut off at the
// profile timeout; the reader is abandoned after HungFactor timeouts);
// every other profile keeps its cadence and is published on its own. A
// worker is stopped when its profile is removed or disabled and replaced
// when the profile changes (a changed profile gets a new entry); a
// result of a stopped worker is discarded. All polls share the global
// semaphore pollSem (MaxSQLConnections = 8).

// worker is the poll goroutine of one profile entry.
type worker struct {
	e      *entry
	cancel context.CancelFunc
	kick   chan struct{} // poll now (poll_now, check_now, reload)
	polled int32         // 1 after the first poll (atomic)
}

// syncWorkersLocked starts the workers of enabled entries and stops the
// ones whose entry was replaced, removed or disabled (m.mu held). No-op
// outside Run.
func (m *Monitor) syncWorkersLocked() {
	if m.runCtx == nil {
		return
	}
	for id, w := range m.workers {
		if e := m.entries[id]; e != w.e || !e.profile.Enabled {
			w.cancel()
			delete(m.workers, id)
		}
	}
	for _, p := range m.cfg.Profiles {
		e := m.entries[p.ID]
		if e == nil || !p.Enabled || m.workers[p.ID] != nil {
			continue
		}
		ctx, cancel := context.WithCancel(m.runCtx)
		w := &worker{e: e, cancel: cancel, kick: make(chan struct{}, 1)}
		m.workers[p.ID] = w
		m.wwg.Add(1)
		go m.runWorker(ctx, w)
	}
}

// stopWorkers cancels every worker and waits for them (end of Run).
func (m *Monitor) stopWorkers() {
	m.mu.Lock()
	for id, w := range m.workers {
		w.cancel()
		delete(m.workers, id)
	}
	m.runCtx = nil
	m.mu.Unlock()
	m.wwg.Wait()
}

// kickWorkers asks the workers of only (nil = every worker, empty = none)
// to poll now and returns the number of running workers. reload: a
// configuration reload; a worker started by it, still in its first poll,
// is not kicked again (no double poll of a new / changed profile).
func (m *Monitor) kickWorkers(only map[string]bool, reload bool) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, w := range m.workers {
		if only != nil && !only[id] || reload && atomic.LoadInt32(&w.polled) == 0 {
			continue
		}
		select {
		case w.kick <- struct{}{}:
		default:
		}
	}
	return len(m.workers)
}

// Workers returns the number of running profile workers (tests).
func (m *Monitor) Workers() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.workers)
}

// runWorker polls its profile right away, then every poll interval
// (counted from the end of a poll) or when kicked, until ctx is canceled.
func (m *Monitor) runWorker(ctx context.Context, w *worker) {
	defer m.wwg.Done()
	for {
		select { // a kick queued before this poll starts is covered by it
		case <-w.kick:
		default:
		}
		m.pollWorker(ctx, w.e)
		atomic.StoreInt32(&w.polled, 1)
		if ctx.Err() != nil {
			return
		}
		t := time.NewTimer(m.interval())
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		case <-w.kick:
			t.Stop()
		}
	}
}

// pollWorker polls one profile (jitter, global semaphore, pollOne) and
// publishes its result alone; nothing is published when ctx was canceled
// or the entry is no longer current.
func (m *Monitor) pollWorker(ctx context.Context, e *entry) {
	m.reapRetired()
	id := e.profile.ID
	m.mu.Lock()
	if m.entries[id] != e {
		m.mu.Unlock()
		return
	}
	settings := m.cfg.Settings
	m.mu.Unlock()
	if d := m.jitterFor(m.interval()); d > 0 {
		t := time.NewTimer(d)
		select {
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			return
		}
	}
	select {
	case m.pollSem <- struct{}{}:
	case <-ctx.Done():
		return
	}
	if ctx.Err() != nil { // select may pick the semaphore after a stop
		<-m.pollSem
		return
	}
	// check_now requests this poll covers: taken right before the poll,
	// after the jitter and the semaphore (architect review)
	m.mu.Lock()
	startSeq := m.checkCounter
	m.mu.Unlock()
	t0 := time.Now()
	ps := m.pollOne(ctx, e, settings)
	ms := time.Since(t0).Milliseconds()
	<-m.pollSem
	if ctx.Err() != nil {
		return
	}
	m.publish(func(res map[string]model.ProfileSnapshot, cfg Config, now time.Time) map[string]bool {
		m.lastCycle = now // a poll completed (heartbeat, CHK-IPC-04 / CHK-MON-16)
		if m.entries[id] != e {
			m.settleChecksLocked()
			return nil
		}
		snap := model.NewProfileSnapshot(e.profile, ps, cfg.Settings, now)
		lat, q := ms, ms
		snap.LatencyMs, snap.LastQueryMs = &lat, &q
		snap.LastCheckSeq = startSeq
		res[id] = snap
		m.coverCheckLocked(id, startSeq)
		m.settleChecksLocked()
		return map[string]bool{id: true}
	})
}
