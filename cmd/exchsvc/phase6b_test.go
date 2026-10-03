package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_CFG_03_StartupSecretsDACL — the service brings secrets.dpapi to
// SY+BA once at start; a failure is logged as a warning and the service
// still starts and serves IPC.
func TestCHK_CFG_03_StartupSecretsDACL(t *testing.T) {
	dir := t.TempDir()
	var calls int32
	s := startSvcWith(t, dir, 40, nil, &fakeEvents{}, func(ac *appConfig) {
		ac.SecureSecrets = func(st *config.Store) (bool, error) {
			atomic.AddInt32(&calls, 1)
			if st.Dir != dir {
				t.Errorf("store dir %q", st.Dir)
			}
			return true, errors.New("access is denied")
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if m, err := s.c.Call(ctx, model.CmdGetStatus); err != nil || m.Type != model.MsgStatus {
		t.Fatalf("service not serving after a DACL failure: %+v %v", m, err)
	}
	s.stop()
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("SecureSecrets called %d times, want 1", n)
	}
	b, err := os.ReadFile(filepath.Join(dir, LogFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "warning: secrets.dpapi") || !strings.Contains(string(b), "access is denied") {
		t.Fatalf("no warning in the log:\n%s", b)
	}
}

// TestCHK_CFG_03_StartupSecretsMissing — without secrets.dpapi the
// production start-up call creates nothing.
func TestCHK_CFG_03_StartupSecretsMissing(t *testing.T) {
	dir := t.TempDir()
	s := startSvc(t, dir, 40, nil, &fakeEvents{})
	s.stop()
	if _, err := os.Stat(filepath.Join(dir, config.SecretsFile)); !os.IsNotExist(err) {
		t.Fatalf("secrets.dpapi created at start: %v", err)
	}
}
