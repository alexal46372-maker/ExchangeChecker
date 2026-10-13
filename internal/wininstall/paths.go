package wininstall

import (
	"errors"
	"fmt"
	"strings"
)

// MaxDirLen leaves room for the longest file name below MAX_PATH (260).
const MaxDirLen = 200

// ErrBadDir: the install folder is not acceptable.
var ErrBadDir = errors.New("setup: install folder")

// CleanDir normalizes a Windows path ("/" → "\", no trailing "\"
// except nothing) without touching the disk.
func CleanDir(p string) string {
	p = strings.ReplaceAll(strings.TrimSpace(p), "/", `\`)
	for len(p) > 3 && strings.HasSuffix(p, `\`) {
		p = p[:len(p)-1]
	}
	return p
}

// ValidateDir checks the folder text (docs/PHASE7.md §1.3): absolute
// "X:\…", not UNC, not a drive root, no relative parts or invalid
// characters, not inside dataDir (%ProgramData%\ExchangeChecker), short
// enough. The drive type (fixed disk) is checked by the Windows System.
func ValidateDir(dir, dataDir string) error {
	d := CleanDir(dir)
	bad := func(why string) error { return fmt.Errorf("%w %q: %s", ErrBadDir, dir, why) }
	switch {
	case d == "":
		return bad("empty")
	case strings.HasPrefix(d, `\\`):
		return bad("network (UNC) paths are not supported")
	case len(d) < 3 || d[1] != ':' || d[2] != '\\' || !isLetter(d[0]):
		return bad("not an absolute path X:\\…")
	case len(d) == 3:
		return bad("drive root")
	case len(d) > MaxDirLen:
		return bad("too long")
	}
	for _, part := range strings.Split(d[3:], `\`) {
		if part == "" || part == "." || part == ".." || strings.TrimRight(part, ". ") != part {
			return bad("invalid folder name")
		}
		if strings.ContainsAny(part, `<>:"|?*`) {
			return bad("invalid character")
		}
		for _, r := range part {
			if r < 32 {
				return bad("invalid character")
			}
		}
	}
	if dataDir != "" {
		dd := strings.ToLower(CleanDir(dataDir))
		ld := strings.ToLower(d)
		if ld == dd || strings.HasPrefix(ld, dd+`\`) || strings.HasPrefix(dd, ld+`\`) {
			return bad("overlaps the data folder " + dataDir)
		}
	}
	return nil
}

func isLetter(c byte) bool { return (c|0x20) >= 'a' && (c|0x20) <= 'z' }
