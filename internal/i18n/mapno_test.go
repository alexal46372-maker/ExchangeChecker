package i18n

import (
	"strings"
	"testing"
)

// TestCHK_TRAY_11_MapNoPointsToButton — «не настроены» names the existing
// «Таблицы обмена…» button instead of a future update (RU and EN).
func TestCHK_TRAY_11_MapNoPointsToButton(t *testing.T) {
	{
		s := T(KeySetMapNo)
		if !strings.Contains(s, T(KeyDiscButton)) || strings.Contains(s, "обновлении") || strings.Contains(s, "update") {
			t.Errorf("%s: %q", RU, s)
		}
	}
}
