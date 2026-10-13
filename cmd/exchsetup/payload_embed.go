//go:build payload

package main

import (
	"embed"
	"fmt"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/wininstall"
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
	return wininstall.GunzipPayload(gz)
}
