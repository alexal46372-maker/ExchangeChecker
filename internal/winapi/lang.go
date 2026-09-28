package winapi

// Primary language IDs (winnt.h).
const (
	LangPrimaryEnglish uint16 = 0x09
	LangPrimaryRussian uint16 = 0x19
)

// PrimaryLangID extracts PRIMARYLANGID from a LANGID.
func PrimaryLangID(langID uint16) uint16 { return langID & 0x3ff }

// LanguageFromLANGID maps a Windows LANGID to a UI language code:
// Russian -> "ru", English -> "en", anything else -> "ru" (PLAN.md §7.4, §12 p.8).
func LanguageFromLANGID(langID uint16) string {
	switch PrimaryLangID(langID) {
	case LangPrimaryRussian:
		return "ru"
	case LangPrimaryEnglish:
		return "en"
	default:
		return "ru"
	}
}
