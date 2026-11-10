//go:build payload

package main

import (
	"embed"
	"fmt"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/wininstall"
	"github.com/alexal46372-maker/ExchangeChecker/third_party/inpout"
)

// The payload is stored gzip-compressed (tools/mkzip gz, phase 7e) and
// decompressed with stdlib compress/gzip only (wininstall.GunzipPayload).
//
//go:embed payload/ExchangeCheckerSvc.exe.gz payload/ExchangeCheckerTray.exe.gz payload/ExchangeCheckerUninstall.exe.gz
var payloadFS embed.FS

func payloadFiles() (map[string][]byte, error) {
	gz := map[string][]byte{}
	for _, name := range []string{config.ServiceExeName, config.TrayExeName, config.UninstallExeName} {
		b, err := payloadFS.ReadFile("payload/" + name + ".gz")
		if err != nil {
			return nil, fmt.Errorf("embedded %s.gz: %w", name, err)
		}
		gz[name] = b
	}
	// on error: nil files → install refuses (ErrNoPayload); the cause is
	// logged by the caller
	files, err := wininstall.GunzipPayload(gz)
	if err != nil {
		return nil, err
	}
	// phase 14 (CHK-SETUP-19): the signed InpOutx64 driver, stored as is
	// (not gzip'ed: the setup exe carries the exact signed bytes); the plan
	// checks its sha256 against config.InpOutSHA256 before writing it.
	files[wininstall.DriverPayloadName] = inpout.SysFile
	return files, nil
}
