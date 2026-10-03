//go:build windows

package config

import (
	"os"

	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// replaceFile uses MoveFileExW(MOVEFILE_REPLACE_EXISTING|MOVEFILE_WRITE_THROUGH).
func replaceFile(from, to string) error { return winapi.MoveFileReplace(from, to) }

// createSecret creates the temp file with SecretsSDDL applied at creation.
func createSecret(tmp string) (*os.File, error) { return winapi.CreateFileSDDL(tmp, SecretsSDDL) }

// secureSecretFile replaces the DACL of an existing secrets.dpapi with the
// protected SecretsSDDL (SetNamedSecurityInfoW +
// PROTECTED_DACL_SECURITY_INFORMATION): inherited ACEs (e.g. Users-read
// from %ProgramData%) are removed.
func secureSecretFile(path string) error { return winapi.SetFileDACL(path, SecretsSDDL) }
