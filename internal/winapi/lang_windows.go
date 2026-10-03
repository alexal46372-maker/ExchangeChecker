//go:build windows

package winapi

// UserUILanguage returns "ru" or "en" from kernel32 GetUserDefaultUILanguage
// (fallback "ru").
func UserUILanguage() string {
	if err := procGetUserDefaultUILanguage.Find(); err != nil {
		return "ru"
	}
	r, _, _ := callProc(procGetUserDefaultUILanguage)
	return LanguageFromLANGID(uint16(r))
}
