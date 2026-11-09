//go:build windows

package winapi

// IsAbove reports whether top-level window a is above b in the z-order
// (b is found walking GW_HWNDNEXT from a). GetWindow: Windows 2000+.
// Phase 13 CHK-TRAY-95: the screen flash overlay stays below the alert
// window and the panel by the clock.
func IsAbove(a, b uintptr) bool {
	if a == 0 || b == 0 || a == b {
		return false
	}
	c, _, _ := callProc(procGetWindow, a, GW_HWNDNEXT)
	for n := 0; c != 0 && n < 65536; n++ {
		if c == b {
			return true
		}
		c, _, _ = callProc(procGetWindow, c, GW_HWNDNEXT)
	}
	return false
}
