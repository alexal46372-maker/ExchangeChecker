package winapi

import "testing"

func TestCHK_CFG_01_LanguageFromLANGID(t *testing.T) {
	cases := map[uint16]string{
		0x0419: "ru", // ru-RU
		0x0819: "ru", // ru-MD
		0x0409: "en", // en-US
		0x0809: "en", // en-GB
		0x0422: "ru", // uk-UA -> fallback RU
		0x0407: "ru", // de-DE -> fallback RU
		0x0423: "ru", // be-BY -> fallback RU
		0x0000: "ru", // unknown
	}
	for id, want := range cases {
		if got := LanguageFromLANGID(id); got != want {
			t.Errorf("LanguageFromLANGID(%#04x) = %q, want %q", id, got, want)
		}
	}
	if PrimaryLangID(0x0419) != LangPrimaryRussian {
		t.Error("PrimaryLangID(0x0419)")
	}
}

func TestCHK_CFG_01_UserUILanguage(t *testing.T) {
	got := UserUILanguage()
	if got != "ru" && got != "en" {
		t.Errorf("UserUILanguage() = %q, want ru or en", got)
	}
}
