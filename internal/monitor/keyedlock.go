package monitor

import (
	"sort"
	"sync"
)

// keyedLocks is a table of per-key (per-profile) mutexes with reference
// counts (phase 4b, CHK-MON-13). An entry lives exactly as long as
// somebody holds or waits for its mutex and is removed by the last
// unlock. It is never deleted "from outside" while in use, so a waiter
// on the old mutex and a new locker of the same key (profile removed and
// re-added quickly) always share one mutex: the critical sections of one
// key can never run concurrently.
type keyedLocks struct {
	mu sync.Mutex
	m  map[string]*keyedLock
	// queued is called after a locker registered itself, before it
	// blocks on the mutex, with the key's reference count (tests).
	queued func(key string, refs int)
}

type keyedLock struct {
	mu   sync.Mutex
	refs int
}

// lock locks key and returns its unlock function (call exactly once).
func (k *keyedLocks) lock(key string) (unlock func()) {
	k.mu.Lock()
	if k.m == nil {
		k.m = map[string]*keyedLock{}
	}
	l := k.m[key]
	if l == nil {
		l = &keyedLock{}
		k.m[key] = l
	}
	l.refs++
	refs, hook := l.refs, k.queued
	k.mu.Unlock()
	if hook != nil {
		hook(key, refs)
	}
	l.mu.Lock()
	return func() {
		l.mu.Unlock()
		k.mu.Lock()
		l.refs--
		if l.refs == 0 && k.m[key] == l {
			delete(k.m, key)
		}
		k.mu.Unlock()
	}
}

// keys lists the keys that are locked or waited for (sorted).
func (k *keyedLocks) keys() []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	out := make([]string, 0, len(k.m))
	for key := range k.m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// refs returns the holders + waiters of key (tests).
func (k *keyedLocks) refs(key string) int {
	k.mu.Lock()
	defer k.mu.Unlock()
	if l := k.m[key]; l != nil {
		return l.refs
	}
	return 0
}
