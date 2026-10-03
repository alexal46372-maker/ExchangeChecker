package i18n

import (
	"strings"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// Languages of the UI.
const (
	RU = model.LangRU
	EN = model.LangEN
)

// Normalize returns RU or EN: "en" (any case) is English, anything else
// is Russian.
func Normalize(lang string) string {
	if strings.EqualFold(strings.TrimSpace(lang), EN) {
		return EN
	}
	return RU
}

// Resolve returns the UI language: an explicit "ru"/"en" override (the
// per-user tray setting, phase 6e) wins, otherwise osLang() (the Windows
// UI language); the result is always RU or EN (fallback RU).
func Resolve(override string, osLang func() string) string {
	switch strings.ToLower(strings.TrimSpace(override)) {
	case RU:
		return RU
	case EN:
		return EN
	}
	if osLang == nil {
		return RU
	}
	return Normalize(osLang())
}

// Default is Resolve without an override: the Windows UI language, else RU.
func Default() string { return Resolve("", winapi.UserUILanguage) }

// T returns the text of a catalog key in lang. Unknown keys are returned
// unchanged (the catalog test makes sure the UI uses no unknown key).
func T(key, lang string) string {
	if m, ok := catalog[key]; ok {
		if Normalize(lang) == EN {
			return m[1]
		}
		return m[0]
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
// through model.MonitorMessage, tray keys through the catalog; a key the
// tray does not know yet (e.g. sql.* until they move out of sqlcheck,
// PLAN G1) gives a generic text with the key, never an empty string.
func Message(key, lang string) string {
	if key == "" {
		return ""
	}
	if s := model.MonitorMessage(key, Normalize(lang)); s != "" {
		return s
	}
	if Has(key) {
		return T(key, lang)
	}
	return T(KeyUnknownMessage, lang) + " (" + key + ")"
}
