//go:build windows

package sqlcheck

import (
	"syscall"
	"testing"
	"unicode/utf8"
)

// CHK-SQL-06: ANSI names from SSRP are decoded with the system code page.
func TestCHK_SQL_06_SSRPDecodeACP(t *testing.T) {
	if got := ssrpDecode([]byte("SRV-A;SQL2008")); got != "SRV-A;SQL2008" {
		t.Errorf("ascii: %q", got)
	}
	getACP := syscall.NewLazyDLL("kernel32.dll").NewProc("GetACP")
	acp := findCall(getACP)
	got := ssrpDecode([]byte{'S', 0xC0, 'X'})
	if !utf8.ValidString(got) || got == "" {
		t.Fatalf("%q", got)
	}
	if acp == 1252 && got != "SÀX" {
		t.Errorf("cp1252: %q", got)
	}
	t.Logf("ACP %d: %q", acp, got)
	in, ok := parseSSRP(svrResp("ServerName;S\xc0;InstanceName;I;;"))
	if !ok || len(in) != 1 || !utf8.ValidString(in[0].Host) || len(in[0].Host) < 2 {
		t.Errorf("%+v", in)
	}
}
