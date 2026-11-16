package wininstall

import (
	"crypto/sha256"
	_ "embed" // icons/exchtray.ico
	"encoding/hex"
	"strings"
)

// Versioned icon file (fix/setup-iconcache, CHK-SETUP-18 / CHK-SETUP-21).
//
// Win7 caches shell icons in IconCache.db keyed by path+index. The tray exe
// path (…\ExchangeCheckerTray.exe,0) never changes, so after an uninstall and
// a clean install of a version with a new icon Explorer keeps drawing the old
// cached picture. The installer therefore writes the icon as its own file
// exchtray-<version>.ico (bytes = trayui.ExeICO() = cmd/exchtray/exchtray.ico,
// embedded from icons/exchtray.ico which tools/mkres generates)
// and points the shortcuts' IconLocation and the Uninstall DisplayIcon at it:
// a new version means a new path, which cannot hit a stale cache entry.

const (
	// TrayIconPrefix / TrayIconExt: the versioned icon file name parts.
	TrayIconPrefix = "exchtray-"
	TrayIconExt    = ".ico"
	// devVersion is the default internal/version.Version of a local build
	// (CHK-VER-01): such a build has no real version, the name uses a hash.
	devVersion = "0.0.0-dev"
	// maxIconVersionLen bounds the version part of the file name.
	maxIconVersionLen = 64
	// iconHashLen: hex digits of sha256(icon) in a dev build's file name.
	iconHashLen = 12
)

// setupIcon: icons/exchtray.ico, written by tools/mkres from trayui.ExeICO()
// next to cmd/exchtray/exchtray.ico (same bytes; `mkres -check` and
// CHK-SETUP-18 keep them equal). Embedded instead of calling trayui so the
// setup does not link internal/trayui → internal/ipc (CHK-SETUP-11).
//
//go:embed icons/exchtray.ico
var setupIcon []byte

// AppIconBytes is the product icon written next to the exes: the same bytes
// as cmd/exchtray/exchtray.ico (trayui.ExeICO, CHK-TRAY-89).
func AppIconBytes() []byte { return setupIcon }

// sanitizeIconVersion keeps [0-9A-Za-z._-] of ver (anything else becomes
// '_'), trims dots/underscores at the ends (no "..", no trailing dot that
// Windows strips) and bounds the length. "" = nothing usable.
func sanitizeIconVersion(ver string) string {
	var b strings.Builder
	for _, r := range ver {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '-', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	s := b.String()
	for strings.Contains(s, "..") {
		s = strings.ReplaceAll(s, "..", ".")
	}
	s = strings.Trim(s, "._")
	if len(s) > maxIconVersionLen {
		s = strings.TrimRight(s[:maxIconVersionLen], "._")
	}
	return s
}

// IconFileName is exchtray-<sanitized version>.ico; a dev build (empty /
// default 0.0.0-dev / nothing left after sanitizing) gets a short hash of
// the icon bytes instead, so a changed icon still changes the path.
func IconFileName(ver string, ico []byte) string {
	v := sanitizeIconVersion(ver)
	if v == "" || strings.EqualFold(strings.TrimSpace(ver), devVersion) {
		sum := sha256.Sum256(ico)
		v = hex.EncodeToString(sum[:])[:iconHashLen]
	}
	return TrayIconPrefix + v + TrayIconExt
}

// IsTrayIconFile reports whether a plain file name is one of our versioned
// icons (exchtray-*.ico, case-insensitive); nothing else is ever removed by
// the icon cleanup.
func IsTrayIconFile(name string) bool {
	if strings.ContainsAny(name, `\/:`) {
		return false
	}
	l := strings.ToLower(name)
	return strings.HasPrefix(l, TrayIconPrefix) && strings.HasSuffix(l, TrayIconExt) &&
		len(l) > len(TrayIconPrefix)+len(TrayIconExt)
}

// iconLocationPath strips the ",index" suffix (and quotes) of a DisplayIcon
// value: `C:\EC\x.exe,0` → `C:\EC\x.exe`.
func iconLocationPath(v string) string {
	v = strings.TrimSpace(v)
	if i := strings.LastIndexByte(v, ','); i >= 0 {
		idx := strings.TrimSpace(v[i+1:])
		digits := idx != ""
		for j, r := range idx {
			if !(r >= '0' && r <= '9') && !(j == 0 && r == '-') {
				digits = false
			}
		}
		if digits {
			v = strings.TrimSpace(v[:i])
		}
	}
	return strings.Trim(v, `"`)
}

// prevDisplayIcon is the icon path of the previous Uninstall values ("" =
// unknown).
func prevDisplayIcon(vals []RegValue) string {
	for _, v := range vals {
		if v.Name == "DisplayIcon" && !v.DWord {
			return iconLocationPath(v.Str)
		}
	}
	return ""
}

// ShellNotifier tells Explorer that items changed (CHK-SETUP-21). Injected
// through InstallConfig / UninstallConfig for tests; nil = the platform
// notifier (Windows: SHChangeNotify).
type ShellNotifier interface {
	// UpdateItem: SHCNE_UPDATEITEM, SHCNF_PATHW, path.
	UpdateItem(path string)
	// AssocChanged: SHCNE_ASSOCCHANGED, SHCNF_IDLIST, 0, 0.
	AssocChanged()
}

func shellNotifier(n ShellNotifier) ShellNotifier {
	if n != nil {
		return n
	}
	return defaultShellNotifier()
}

// notifyShell sends UPDATEITEM for every non-empty path (in order, each path
// once) and then ASSOCCHANGED last.
func notifyShell(n ShellNotifier, log Logger, paths ...string) {
	seen := map[string]bool{}
	for _, p := range paths {
		k := strings.ToLower(p)
		if p == "" || seen[k] {
			continue
		}
		seen[k] = true
		n.UpdateItem(p)
	}
	n.AssocChanged()
	log.Printf("оболочка уведомлена: SHCNE_UPDATEITEM ×%d, SHCNE_ASSOCCHANGED", len(seen))
}

// staleTrayIcons lists exchtray-*.ico in dir except keep (case-insensitive;
// keep = "" lists all).
func staleTrayIcons(sys System, dir, keep string) ([]string, error) {
	names, err := sys.ListDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, n := range names {
		if IsTrayIconFile(n) && !strings.EqualFold(n, keep) {
			out = append(out, joinDir(dir, n))
		}
	}
	return out, nil
}
