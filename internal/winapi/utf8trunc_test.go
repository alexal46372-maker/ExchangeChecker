package winapi

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestCHK_MON_15_TruncateUTF8 — Event Log messages are cut on a rune
// boundary: Cyrillic (2 bytes), CJK (3) and emoji (4) are never split.
func TestCHK_MON_15_TruncateUTF8(t *testing.T) {
	for _, tc := range []struct {
		s    string
		max  int
		want string
	}{
		{"abc", 5, "abc"},
		{"abc", 3, "abc"},
		{"abc", 2, "ab"},
		{"абв", 3, "а"},  // 2nd rune would be split
		{"абв", 4, "аб"}, // exact boundary
		{"a中b", 3, "a"},  // 3-byte rune
		{"a😀", 4, "a"},   // 4-byte rune
		{"a😀", 5, "a😀"},  // fits
		{"abc", 0, ""},
		{"\xff\xfe\xfd\xfc\xfb\xfa", 5, "\xff\xfe\xfd\xfc\xfb"}, // invalid input: bytewise
	} {
		if got := TruncateUTF8(tc.s, tc.max); got != tc.want {
			t.Errorf("TruncateUTF8(%q, %d) = %q, want %q", tc.s, tc.max, got, tc.want)
		}
	}
	long := strings.Repeat("Ошибка опроса базы. ", 3000) // > EventLogMaxBytes
	for _, off := range []int{0, 1} {
		s := strings.Repeat("x", off) + long
		got := TruncateUTF8(s, EventLogMaxBytes)
		if len(got) > EventLogMaxBytes || len(got) < EventLogMaxBytes-utf8.UTFMax || !utf8.ValidString(got) {
			t.Errorf("offset %d: len %d valid %v", off, len(got), utf8.ValidString(got))
		}
		if n := utf8.RuneCountInString(got); n > 31839 {
			t.Errorf("too many characters for ReportEventW: %d", n)
		}
	}
}
