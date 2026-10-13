package checks

import (
	"os"
	"strings"
	"testing"
)

// TestCHK_DELIV_02_WindowsAuthAccount — DELIVERY.md describes Windows
// authentication as the computer account ДОМЕН\ИМЯ$ of the LocalSystem
// service, not a user DOMAIN\user (phase 8; no other login mode).
func TestCHK_DELIV_02_WindowsAuthAccount(t *testing.T) {
	b, err := os.ReadFile("../DELIVERY.md")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{`ДОМЕН\ИМЯ$`, "CREATE LOGIN [ДОМЕН\\ИМЯ$] FROM WINDOWS", "LocalSystem"} {
		if !strings.Contains(s, want) {
			t.Errorf("DELIVERY.md: %q missing", want)
		}
	}
	for _, bad := range []string{`DOMAIN\user`, "учётная запись службы или `DOMAIN", "«только логин»", "«Доверять сертификату сервера»"} {
		if strings.Contains(s, bad) {
			t.Errorf("DELIVERY.md still says %q", bad)
		}
	}
}
