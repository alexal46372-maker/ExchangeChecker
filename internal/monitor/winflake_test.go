package monitor

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

type countingSource struct {
	ConfigSource
	loads int32
}

func (c *countingSource) Load() Config {
	atomic.AddInt32(&c.loads, 1)
	return c.ConfigSource.Load()
}

// TestCHK_IPC_06_LoadConfigBeforeRun — LoadConfig applies the
// configuration synchronously (snapshot has settings at once) and Run does
// not load it a second time at start; LoadConfig is idempotent.
func TestCHK_IPC_06_LoadConfigBeforeRun(t *testing.T) {
	src := &countingSource{ConfigSource: NewMemConfig(Config{Settings: model.DefaultSettings(), Profiles: []model.Profile{prof("a")}})}
	opt := baseOpt(newFactory(), src, &fakeClock{t: t0})
	opt.PollInterval, opt.ConfigCheck = time.Hour, time.Hour
	m := New(opt)
	if m.Snapshot().Settings != nil {
		t.Fatal("settings before the first load")
	}
	m.LoadConfig()
	m.LoadConfig()
	if s := m.Snapshot(); s.Settings == nil || len(s.Profiles) != 1 || atomic.LoadInt32(&src.loads) != 1 {
		t.Fatalf("after LoadConfig: %+v loads %d", s.Settings, src.loads)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- m.Run(ctx) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if n := atomic.LoadInt32(&src.loads); n != 1 {
		t.Fatalf("Run reloaded at start: %d loads", n)
	}
}
