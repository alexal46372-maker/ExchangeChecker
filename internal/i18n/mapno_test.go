package i18n

import (
	"strings"
	"testing"
)

// TestCHK_TRAY_11_MapNoPointsToButton — «не настроены» names the existing
// «Таблицы обмена…» button instead of a future update (RU and EN).
func TestCHK_TRAY_11_MapNoPointsToButton(t *testing.T) {
	for _, l := range []string{RU, EN} {
		s := T(KeySetMapNo, l)
		if !strings.Contains(s, T(KeyDiscButton, l)) || strings.Contains(s, "обновлении") || strings.Contains(s, "update") {
			t.Errorf("%s: %q", l, s)
		}
	}
}
