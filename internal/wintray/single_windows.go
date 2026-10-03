//go:build windows

package wintray

import (
	"syscall"

	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// MutexName keeps one tray per logon session.
const MutexName = `Local\ExchangeCheckerTray`

// SingleInstance takes the named mutex; ok is false when another tray of
// this session already holds it. release closes the handle.
func SingleInstance(name string) (release func(), ok bool, err error) {
	h, existed, err := winapi.CreateMutex(name)
	if err != nil {
		return func() {}, false, err
	}
	release = func() { syscall.CloseHandle(h) }
	if existed {
		release()
		return func() {}, false, nil
	}
	return release, true, nil
}
