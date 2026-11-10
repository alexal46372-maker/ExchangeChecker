package trayalert

// ToneLevel is the level of «Тон» (phase 14, CHK-TRAY-99): the PC speaker
// has no volume, so the profile volume × master volume only switches the
// tone on or off — 0 % = silent (no output is used at all, not even an
// IPC request), anything above 0 = full level (100). The sine WAV of the
// fallback uses the same level so the «громкость не регулируется» label
// stays true whichever output sounds.
func ToneLevel(effective int) int {
	if effective <= 0 {
		return 0
	}
	return 100
}
