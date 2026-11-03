//go:build windows

package winapi

// MessageBeep (phase 13 item 1, CHK-TRAY-97): the last fallback of «Тон»
// after PlaySound(SND_MEMORY) and Beep — the system "Critical Stop" sound
// of the sound card. user32, Windows 2000+.

var procMessageBeep = moduser32.NewProc("MessageBeep")

// MB_ICONHAND of MessageBeep.
const MB_ICONHAND = 0x00000010

// MessageBeep plays the system sound of type t (MB_ICONHAND…).
func MessageBeep(t uint32) error {
	r, _, e := callProc(procMessageBeep, uintptr(t))
	if r == 0 {
		return lastErr(e)
	}
	return nil
}
