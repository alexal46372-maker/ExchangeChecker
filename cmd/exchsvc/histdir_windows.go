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
// implicit WRITE_DAC for that user).
func secureHistoryDir(dir string) error {
	if err := winapi.SetFileDACL(dir, model.HistorySDDL); err != nil {
		return err
	}
	_ = winapi.SetFileOwner(dir, winapi.SIDAdministrators)
	return nil
}
