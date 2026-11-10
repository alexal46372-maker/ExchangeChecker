package trayalert

// ToneLevel is the level of «Тон» (phase 14, CHK-TRAY-99): the PC speaker
// has no volume, so the volumes only switch the tone on or off — silent
// (no output is used at all, not even an IPC request) only when the
// profile volume or the master volume is exactly 0 %; otherwise full
// level (100), also when their product rounds down to 0 (1 % × 50 %).
// The sine WAV of the fallback uses the same level so the «громкость не
// регулируется» label stays true whichever output sounds. File volumes
// are not affected (Sound.Volume, EffectiveVolume).
func ToneLevel(s Sound) int {
	if s.ToneOn || s.Volume > 0 {
		return 100
	}
	return 0
}
