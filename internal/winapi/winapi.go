package winapi

import "errors"

// ErrNotSupported is returned by stubs on non-Windows platforms (used only
// so that go vet / unit tests of pure packages run on any GOOS).
var ErrNotSupported = errors.New("winapi: not supported on this platform")

// DPAPI flags (wincrypt.h).
const (
	CryptProtectUIForbidden  uint32 = 0x1
	CryptProtectLocalMachine uint32 = 0x4
)

// dpapiDescription is the szDataDescr passed to CryptProtectData.
const dpapiDescription = "ExchangeChecker"
