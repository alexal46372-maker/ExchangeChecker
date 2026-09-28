//go:build windows

package config

import "github.com/alexal46372-maker/ExchangeChecker/internal/winapi"

// replaceFile uses MoveFileExW(MOVEFILE_REPLACE_EXISTING|MOVEFILE_WRITE_THROUGH).
func replaceFile(from, to string) error { return winapi.MoveFileReplace(from, to) }
