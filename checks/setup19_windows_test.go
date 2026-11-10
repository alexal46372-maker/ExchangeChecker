//go:build windows

package checks_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"unsafe"

	"github.com/alexal46372-maker/ExchangeChecker/third_party/inpout"
)

// CHK-SETUP-19 on Windows: WinVerifyTrust (WINTRUST_ACTION_GENERIC_VERIFY_V2,
// no UI, no revocation / network) accepts the committed driver; one flipped
// byte gives TRUST_E_BAD_DIGEST. Test-only WinAPI (checks/, CHK-TRAY-10
// allowlist: WinVerifyTrust 5.0).

type winTrustFileInfo struct {
	CbStruct     uint32
	FilePath     *uint16
	File         uintptr
	KnownSubject uintptr
}

type winTrustData struct {
	CbStruct           uint32
	PolicyCallbackData uintptr
	SIPClientData      uintptr
	UIChoice           uint32
	RevocationChecks   uint32
	UnionChoice        uint32
	File               *winTrustFileInfo
	StateAction        uint32
	StateData          uintptr
	URLReference       *uint16
	ProvFlags          uint32
	UIContext          uint32
	SignatureSettings  uintptr
}

var genericVerifyV2 = syscall.GUID{Data1: 0x00AAC56B, Data2: 0xCD44, Data3: 0x11d0, Data4: [8]byte{0x8C, 0xC2, 0x00, 0xC0, 0x4F, 0xC2, 0x95, 0xEE}}

const (
	trustEBadDigest = 0x80096010
	wtdUINone       = 2
	wtdRevokeNone   = 0
	wtdChoiceFile   = 1
	wtdStateVerify  = 1
	wtdStateClose   = 2
	// WTD_REVOCATION_CHECK_NONE | WTD_CACHE_ONLY_URL_RETRIEVAL: offline
	wtdProvFlags = 0x00000010 | 0x00001000
)

func winVerifyTrust(t *testing.T, path string) uint32 {
	t.Helper()
	proc := syscall.NewLazyDLL("wintrust.dll").NewProc("WinVerifyTrust")
	if err := proc.Find(); err != nil {
		t.Fatal(err)
	}
	p, _ := syscall.UTF16PtrFromString(path)
	fi := winTrustFileInfo{FilePath: p}
	fi.CbStruct = uint32(unsafe.Sizeof(fi))
	wd := winTrustData{UIChoice: wtdUINone, RevocationChecks: wtdRevokeNone, UnionChoice: wtdChoiceFile,
		File: &fi, StateAction: wtdStateVerify, ProvFlags: wtdProvFlags}
	wd.CbStruct = uint32(unsafe.Sizeof(wd))
	invalid := ^uintptr(0) // INVALID_HANDLE_VALUE: no UI
	r, _, _ := proc.Call(invalid, uintptr(unsafe.Pointer(&genericVerifyV2)), uintptr(unsafe.Pointer(&wd)))
	wd.StateAction = wtdStateClose
	proc.Call(invalid, uintptr(unsafe.Pointer(&genericVerifyV2)), uintptr(unsafe.Pointer(&wd)))
	return uint32(r)
}

func TestCHK_SETUP_19_WinVerifyTrust(t *testing.T) {
	b := readDriver(t)
	dir := t.TempDir()
	good := filepath.Join(dir, inpout.SysName)
	if err := os.WriteFile(good, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if r := winVerifyTrust(t, good); r != 0 {
		t.Fatalf("CHK-SETUP-19: WinVerifyTrust(%s) = %#x, want 0 (ERROR_SUCCESS)", good, r)
	}
	bad := filepath.Join(dir, "corrupt.sys")
	c := append([]byte(nil), b...)
	c[0x400+16] ^= 0x01
	if err := os.WriteFile(bad, c, 0o644); err != nil {
		t.Fatal(err)
	}
	if r := winVerifyTrust(t, bad); r != trustEBadDigest {
		t.Fatalf("CHK-SETUP-19: WinVerifyTrust(corrupt) = %#x, want TRUST_E_BAD_DIGEST %#x", r, uint32(trustEBadDigest))
	}
	t.Logf("PASS CHK-SETUP-19 WinVerifyTrust: %s = 0 (valid), one flipped byte = %#x (TRUST_E_BAD_DIGEST)", inpout.SysName, uint32(trustEBadDigest))
}
