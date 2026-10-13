//go:build windows

package main

import (
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// secureHistoryDir gives the history folder the protected DACL SYSTEM +
// Administrators (architect decision 1, CHK-SVC-03), inherited by the
// day files; a DACL failure disables the history. The owner is set to
// Administrators best effort (a folder pre-created by a user keeps no
// implicit WRITE_DAC for that user); an owner failure is logged as a
// warning via warn.
func secureHistoryDir(dir string, warn func(format string, args ...interface{})) error {
	if err := winapi.SetFileDACL(dir, model.HistorySDDL); err != nil {
		return err
	}
	if err := winapi.SetFileOwner(dir, winapi.SIDAdministrators); err != nil && warn != nil {
		warn("warning: %s: cannot set the owner to Administrators: %v", dir, err)
	}
	return nil
}
