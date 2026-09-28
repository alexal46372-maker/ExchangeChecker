//go:build !windows

package winapi

// UserUILanguage returns the fallback language outside Windows.
func UserUILanguage() string { return "ru" }
