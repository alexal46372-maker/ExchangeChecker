//go:build !payload

package main

// emptyPayload is used by builds without -tags payload (dev, uninstaller).
// Install refuses with ErrNoPayload; uninstall still works.
func payloadFiles() map[string][]byte { return nil }
