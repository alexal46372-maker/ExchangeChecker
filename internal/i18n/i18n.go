package i18n

import (
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// RU is the locale tag of the UI (the product is Russian only, phase 13
// CHK-TRAY-86), e.g. for <html lang>.
const RU = "ru"

// T returns the (Russian) text of a catalog key. Unknown keys are returned
// unchanged (the catalog test makes sure the UI uses no unknown key).
func T(key string) string {
	if s, ok := catalog[key]; ok {
		return s
	}
	return key
}

// Has reports whether key is in the catalog.
func Has(key string) bool {
	_, ok := catalog[key]
	return ok
}

// Keys lists the catalog keys (tests).
func Keys() []string {
	out := make([]string, 0, len(catalog))
	for k := range catalog {
		out = append(out, k)
	}
	return out
}

// Message renders a message key sent by the service (ProfileSnapshot.
// ErrorKey, StatusSnapshot.ConfigErrorKey, IPC errorKey): monitor.* keys
// through model.MonitorMessage, sql.* through model.SQLMessage, tray keys
// through the catalog; an unknown key gives a generic text with the key,
// never an empty string.
func Message(key string) string {
	if key == "" {
		return ""
	}
	if s := model.MonitorMessage(key); s != "" {
		return s
	}
	if s := model.SQLMessage(key); s != "" { // sql.* (G1)
		return s
	}
	if Has(key) {
		return T(key)
	}
	return T(KeyUnknownMessage) + " (" + key + ")"
}
