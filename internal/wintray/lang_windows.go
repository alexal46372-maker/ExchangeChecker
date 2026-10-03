package wintray

import (
	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// LangStore keeps the per-user tray language preference ("" = as in
// Windows, "ru", "en"; phase 6e, CHK-TRAY-08). It is not part of the
// machine settings: every user chooses it in the tray menu.
type LangStore interface {
	Load() (string, error)
	Save(pref string) error
}

// LangKey is the per-user registry key of the tray (HKCU).
const LangKey = `Software\ExchangeChecker\Tray`

// LangValue is the REG_SZ value with the preference.
const LangValue = "Language"

// RegistryLangStore stores the preference in HKCU\Key\Language; an empty
// preference deletes the value.
type RegistryLangStore struct {
	Key string // "" = LangKey
}

func (s RegistryLangStore) key() string {
	if s.Key == "" {
		return LangKey
	}
	return s.Key
}

// Load implements LangStore; unknown values read as "".
func (s RegistryLangStore) Load() (string, error) {
	v, err := winapi.RegGetString(winapi.HKEY_CURRENT_USER, s.key(), LangValue)
	return NormalizePref(v), err
}

// Save implements LangStore.
func (s RegistryLangStore) Save(pref string) error {
	pref = NormalizePref(pref)
	if pref == "" {
		return winapi.RegDeleteValue(winapi.HKEY_CURRENT_USER, s.key(), LangValue)
	}
	return winapi.RegSetString(winapi.HKEY_CURRENT_USER, s.key(), LangValue, pref)
}

// NormalizePref returns "ru", "en" or "" (as in Windows).
func NormalizePref(p string) string {
	switch p {
	case model.LangRU, model.LangEN:
		return p
	}
	return ""
}

// ResolveLang is the UI language of a preference: the preference, else
// the Windows UI language, else RU.
func ResolveLang(pref string) string { return i18n.Resolve(pref, winapi.UserUILanguage) }

// LoadLang reads the preference of the current user (errors = as in
// Windows) and returns it with the resolved UI language.
func LoadLang(s LangStore) (pref, lang string) {
	if s != nil {
		if p, err := s.Load(); err == nil {
			pref = NormalizePref(p)
		}
	}
	return pref, ResolveLang(pref)
}
