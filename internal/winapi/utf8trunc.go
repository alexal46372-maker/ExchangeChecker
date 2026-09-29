package winapi

import "unicode/utf8"

// EventLogMaxBytes caps one Event Log insertion string (ReportEventW
// accepts up to 31839 characters per string; UTF-8 bytes >= UTF-16 units,
// so the byte cap is always within the limit).
const EventLogMaxBytes = 31000

// TruncateUTF8 returns s cut to at most max bytes on a rune boundary: a
// multi-byte character (Cyrillic, ...) is never split into an invalid
// sequence (CHK-MON-15). Invalid input bytes are left as they are.
func TruncateUTF8(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	i := max
	// step back to the start of the rune that crosses the limit
	for i > 0 && !utf8.RuneStart(s[i]) {
		i--
	}
	// keep a complete rune that ends exactly at i; when stepping back
	// found no rune start within UTFMax, cut bytewise
	if max-i >= utf8.UTFMax {
		return s[:max]
	}
	return s[:i]
}
