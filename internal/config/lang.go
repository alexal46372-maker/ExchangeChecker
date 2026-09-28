package config

import (
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// EffectiveLanguage returns the UI language: an explicit "ru"/"en" from
// settings, otherwise the Windows UI language (GetUserDefaultUILanguage;
// not RU/EN -> "ru"), PLAN.md §7.4 / §12 p.8.
func EffectiveLanguage(s model.Settings) string {
	switch s.Language {
	case model.LangRU, model.LangEN:
		return s.Language
	}
	return winapi.UserUILanguage()
}
