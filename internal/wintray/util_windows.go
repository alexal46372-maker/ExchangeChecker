//go:build windows

package wintray

import (
	"syscall"
)

// utf16From converts s to UTF-16 without the terminating zero.
func utf16From(s string) ([]uint16, error) {
	u, err := syscall.UTF16FromString(s)
	if err != nil {
		return nil, err
	}
	return u[:len(u)-1], nil
}
