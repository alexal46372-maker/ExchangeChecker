//go:build windows

package wintray

import (
	"github.com/alexal46372-maker/ExchangeChecker/internal/traysettings"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// UIModeValue is the HKCU value of «Простой» / «Экспертный» (phase 10 D,
// CHK-TRAY-40) under LangKey.
const UIModeValue = "UIMode"

// RegistryUIModeStore keeps the settings window mode per user.
type RegistryUIModeStore struct {
	Key string // "" = LangKey
}

func (s RegistryUIModeStore) key() string {
	if s.Key == "" {
		return LangKey
	}
	return s.Key
}

// Load returns the stored mode; missing = «Простой».
func (s RegistryUIModeStore) Load() (traysettings.UIMode, error) {
	v, ok, err := winapi.RegGetStringView(winapi.HKEY_CURRENT_USER, s.key(), UIModeValue, 0)
	if err != nil || !ok {
		return traysettings.UISimple, err
	}
	return traysettings.NormalizeUIMode(v), nil
}

// Save stores the mode.
func (s RegistryUIModeStore) Save(m traysettings.UIMode) error {
	return winapi.RegSetStringView(winapi.HKEY_CURRENT_USER, s.key(), UIModeValue, string(traysettings.NormalizeUIMode(string(m))), 0)
}
