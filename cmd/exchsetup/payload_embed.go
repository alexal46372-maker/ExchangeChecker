//go:build payload

package main

import (
	"embed"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
)

//go:embed payload/ExchangeCheckerSvc.exe payload/ExchangeCheckerTray.exe payload/ExchangeCheckerUninstall.exe
var payloadFS embed.FS

func payloadFiles() map[string][]byte {
	out := map[string][]byte{}
	for _, name := range []string{config.ServiceExeName, config.TrayExeName, config.UninstallExeName} {
		b, err := payloadFS.ReadFile("payload/" + name)
		if err != nil {
			return nil
		}
		out[name] = b
	}
	return out
}
