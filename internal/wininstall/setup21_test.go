package wininstall

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
)

// recNotifier records shell notifications; with fake set it also appends
// them to fake.Steps, so their position relative to file / shortcut steps
// is checked against the real plan order.
type recNotifier struct {
	fake  *recordingSystem
	Calls []string
}

func (r *recNotifier) add(s string) {
	r.Calls = append(r.Calls, s)
	if r.fake != nil {
		r.fake.mu.Lock()
		r.fake.Steps = append(r.fake.Steps, "Notify|"+s)
		r.fake.mu.Unlock()
	}
}

func (r *recNotifier) UpdateItem(path string) { r.add("UPDATEITEM|" + path) }
func (r *recNotifier) AssocChanged()          { r.add("ASSOCCHANGED") }

const (
	t21Dir  = `C:\EC`
	t21Data = `C:\ProgramData\ExchangeChecker`
)

var t21Tray = t21Dir + `\` + config.TrayExeName

func t21Config(ver string, n ShellNotifier) InstallConfig {
	return InstallConfig{
		Opts: Options{Silent: true}, DefaultDir: t21Dir, DataDir: t21Data,
		Files: sampleFiles(), Version: ver, Now: time.Now(), Notifier: n,
	}
}

// t21Installed seeds an installed version oldVer whose shortcuts and
// DisplayIcon use <dir>\<oldIcon>, plus extra files in the folder.
func t21Installed(oldVer, oldIcon string, running bool, extra ...string) *recordingSystem {
	f := newFake()
	f.Installed = Installed{Present: true, Version: oldVer, Dir: t21Dir}
	f.Service = ServiceInfo{Exists: true, Running: running, AutoStart: true, ImagePath: `"` + t21Dir + `\` + config.ServiceExeName + `"`}
	f.Dirs[t21Dir] = true
	for n, b := range sampleFiles() {
		f.Files[t21Dir+`\`+n] = append([]byte("old-"), b...)
	}
	f.Files[t21Dir+`\`+oldIcon] = []byte("old-icon")
	for _, n := range extra {
		f.Files[t21Dir+`\`+n] = []byte("x-" + n)
	}
	f.Shortcut, f.DesktopLnk = t21Tray, t21Tray
	f.ShortcutIcon, f.DesktopIcon = t21Dir+`\`+oldIcon, t21Dir+`\`+oldIcon
	f.Uninst = UninstallValues(t21Dir, t21Dir+`\`+config.UninstallExeName, t21Dir+`\`+oldIcon, oldVer, 1, time.Now(), true)
	return f
}

func iconsIn(f *recordingSystem) []string {
	var out []string
	for k := range f.Files {
		if strings.HasPrefix(k, t21Dir+`\`) && IsTrayIconFile(k[len(t21Dir)+1:]) {
			out = append(out, k[len(t21Dir)+1:])
		}
	}
	return sortedStrings(out)
}

func sortedStrings(s []string) []string {
	out := append([]string(nil), s...)
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// TestCHK_SETUP_21_IconFileName — release: exchtray-<sanitized version>.ico;
// dev build (no version / 0.0.0-dev / nothing usable): exchtray-<first 12
// hex of sha256(icon)>.ico; only exchtray-*.ico names qualify for cleanup.
func TestCHK_SETUP_21_IconFileName(t *testing.T) {
	ico := AppIconBytes()
	sum := sha256.Sum256(ico)
	hash := hex.EncodeToString(sum[:])[:12]
	cases := []struct{ ver, want string }{
		{"0.15.0", "exchtray-0.15.0.ico"},
		{"0.0.0-ci.4217", "exchtray-0.0.0-ci.4217.ico"},
		{"1.0.0+build 7", "exchtray-1.0.0_build_7.ico"},
		{`..\..\Windows\evil`, "exchtray-Windows_evil.ico"},
		{"a/b:c*?", "exchtray-a_b_c.ico"},
		{"1..2", "exchtray-1.2.ico"},
		{"", "exchtray-" + hash + ".ico"},
		{"0.0.0-dev", "exchtray-" + hash + ".ico"},
		{" 0.0.0-dev ", "exchtray-" + hash + ".ico"},
		{"???", "exchtray-" + hash + ".ico"},
		{strings.Repeat("9", 100), "exchtray-" + strings.Repeat("9", 64) + ".ico"},
	}
	for _, c := range cases {
		got := IconFileName(c.ver, ico)
		if got != c.want {
			t.Errorf("IconFileName(%q)=%q want %q", c.ver, got, c.want)
		}
		if strings.ContainsAny(got, `\/:*?"<>| `) || !IsTrayIconFile(got) {
			t.Errorf("IconFileName(%q)=%q is not a plain exchtray-*.ico name", c.ver, got)
		}
	}
	// dev hash follows the bytes: another icon → another file name
	other := append(append([]byte(nil), ico...), 0)
	if a, b := IconFileName("0.0.0-dev", ico), IconFileName("0.0.0-dev", other); a == b {
		t.Fatalf("dev name does not depend on the icon bytes: %s", a)
	}
	// a release name does not depend on the bytes
	if IconFileName("2.0.0", ico) != IconFileName("2.0.0", other) {
		t.Fatal("release name must depend on the version only")
	}
	for name, want := range map[string]bool{
		"exchtray-0.15.0.ico": true, "EXCHTRAY-0.15.0.ICO": true, "exchtray-" + hash + ".ico": true,
		"exchtray.ico": false, "exchtray-.ico": false, "exchtray-1.ico.bak": false,
		"exchtray-1.ico.old": false, "app-1.ico": false, `sub\exchtray-1.ico`: false,
		"exchtray-1.exe": false,
	} {
		if IsTrayIconFile(name) != want {
			t.Errorf("IsTrayIconFile(%q) != %v", name, want)
		}
	}
	for in, want := range map[string]string{
		`C:\EC\exchtray-1.ico,0`:            `C:\EC\exchtray-1.ico`,
		`"C:\EC\ExchangeCheckerTray.exe",0`: `C:\EC\ExchangeCheckerTray.exe`,
		`C:\a,b\x.ico`:                      `C:\a,b\x.ico`,
		`C:\EC\x.exe,-101`:                  `C:\EC\x.exe`,
	} {
		if got := iconLocationPath(in); got != want {
			t.Errorf("iconLocationPath(%q)=%q want %q", in, got, want)
		}
	}
}

// TestCHK_SETUP_21_NotifyOrder — after the tray exe, the .ico and both
// shortcuts are written: UPDATEITEM(tray exe, .ico, Start .lnk, Desktop .lnk)
// then ASSOCCHANGED, exactly once each, ASSOCCHANGED last. Uninstall: the
// same after the files and shortcuts are deleted (removed .ico included).
// Negative: an install that fails before the icon step sends nothing.
func TestCHK_SETUP_21_NotifyOrder(t *testing.T) {
	ico := t21Dir + `\exchtray-0.16.0.ico`
	f := newFake()
	n := &recNotifier{fake: f}
	res := Install(f, t21Config("0.16.0", n), &memLog{})
	if res.Code != ExitOK {
		t.Fatalf("code=%d err=%v", res.Code, res.Err)
	}
	want := []string{
		"UPDATEITEM|" + t21Tray, "UPDATEITEM|" + ico,
		"UPDATEITEM|" + fakeStartLnk, "UPDATEITEM|" + fakeDesktopLnk, "ASSOCCHANGED",
	}
	if !reflect.DeepEqual(n.Calls, want) {
		t.Fatalf("install notifications:\n got %q\nwant %q", n.Calls, want)
	}
	first := indexOf(f.Steps, "Notify|")
	for _, s := range []string{"WriteFile|" + t21Tray, "WriteFile|" + ico, "CreateStartShortcut|", "CreateDesktopShortcut|", "WriteUninstallKey|"} {
		if i := indexOf(f.Steps, s); i < 0 || i > first {
			t.Fatalf("%s at %d must precede the first notification at %d: %v", s, i, first, f.Steps)
		}
	}
	if f.Steps[len(f.Steps)-1] != "Notify|ASSOCCHANGED" {
		t.Fatalf("ASSOCCHANGED must be the last step, got %q", f.Steps[len(f.Steps)-1])
	}

	u := &recNotifier{fake: f}
	ur := Uninstall(f, UninstallConfig{DataDir: t21Data, Notifier: u}, &memLog{})
	if ur.Code != ExitOK {
		t.Fatalf("uninstall code=%d err=%v", ur.Code, ur.Err)
	}
	if !reflect.DeepEqual(u.Calls, want) {
		t.Fatalf("uninstall notifications:\n got %q\nwant %q", u.Calls, want)
	}
	batch := -1 // start of the uninstall batch (the last UPDATEITEM of the tray exe)
	for i := len(f.Steps) - 1; i >= 0; i-- {
		if f.Steps[i] == "Notify|UPDATEITEM|"+t21Tray {
			batch = i
			break
		}
	}
	for _, s := range []string{"DeleteStartShortcut", "DeleteDesktopShortcut", "RemoveFile|" + ico, "RemoveFile|" + t21Tray} {
		i := -1
		for j := len(f.Steps) - 1; j >= 0; j-- {
			if strings.HasPrefix(f.Steps[j], s) {
				i = j
				break
			}
		}
		if i < 0 || i > batch {
			t.Fatalf("uninstall: %s at %d must precede notifications at %d: %v", s, i, batch, f.Steps)
		}
	}

	// negative: failure before the icon step (install folder DACL) → no
	// notifications at all
	g := newFake()
	g.FailAt = "SetDACL|" + t21Dir + "|"
	gn := &recNotifier{}
	if r := Install(g, t21Config("0.16.0", gn), &memLog{}); r.Code != ExitError {
		t.Fatalf("want failure, code=%d", r.Code)
	}
	if len(gn.Calls) != 0 {
		t.Fatalf("failed install before the icon step notified: %q", gn.Calls)
	}
}

// TestCHK_SETUP_21_UpgradeCleansOldIcons — upgrade 0.15.0 → 0.16.0: the
// new exchtray-0.16.0.ico exists (exchtray.ico bytes), both shortcuts and
// DisplayIcon point at it, every other exchtray-*.ico (old version, dev
// hash) is deleted only after StartService, unrelated files stay.
// Negatives: a rollback (StartService failure) keeps the old icon, deletes
// the new one and restores the shortcuts to the old icon; a same-version
// reinstall keeps (backs up and rewrites) the current file; a failed
// cleanup is a WARNING, not an error.
func TestCHK_SETUP_21_UpgradeCleansOldIcons(t *testing.T) {
	_, want := committedTrayICO(t)
	newIco := t21Dir + `\exchtray-0.16.0.ico`
	oldIco := t21Dir + `\exchtray-0.15.0.ico`
	devIco := t21Dir + `\exchtray-0a1b2c3d4e5f.ico`

	f := t21Installed("0.15.0", "exchtray-0.15.0.ico", true, "exchtray-0a1b2c3d4e5f.ico", "notes.ico", "exchtray-0.15.0.ico.txt")
	n := &recNotifier{fake: f}
	res := Install(f, t21Config("0.16.0", n), &memLog{})
	if res.Code != ExitOK || len(iconWarnings(res.Warnings)) != 0 {
		t.Fatalf("upgrade code=%d err=%v warnings=%v", res.Code, res.Err, res.Warnings)
	}
	if got := iconsIn(f); !reflect.DeepEqual(got, []string{"exchtray-0.16.0.ico"}) {
		t.Fatalf("icons after upgrade %v, want only exchtray-0.16.0.ico", got)
	}
	if !bytes.Equal(f.Files[newIco], want) {
		t.Fatal("new icon bytes are not cmd/exchtray/exchtray.ico")
	}
	for _, keep := range []string{"notes.ico", "exchtray-0.15.0.ico.txt"} {
		if _, ok := f.Files[t21Dir+`\`+keep]; !ok {
			t.Fatalf("cleanup removed an unrelated file %s", keep)
		}
	}
	if f.ShortcutIcon != newIco || f.DesktopIcon != newIco || displayIcon(f.Uninst) != newIco+",0" {
		t.Fatalf("after upgrade: start=%q desktop=%q DisplayIcon=%q want %q", f.ShortcutIcon, f.DesktopIcon, displayIcon(f.Uninst), newIco)
	}
	start := indexOf(f.Steps, "StartService|")
	for _, p := range []string{oldIco, devIco} {
		i := indexOf(f.Steps, "RemoveFile|"+p)
		if i < 0 || i < start {
			t.Fatalf("RemoveFile %s at %d must follow StartService at %d", p, i, start)
		}
	}
	if indexOf(f.Steps, "RemoveFile|"+newIco) >= 0 {
		t.Fatal("the current icon must not be removed on upgrade")
	}
	if n.Calls[1] != "UPDATEITEM|"+newIco || n.Calls[len(n.Calls)-1] != "ASSOCCHANGED" {
		t.Fatalf("upgrade notifications %q", n.Calls)
	}

	// negative 1: V2 fails to start → rollback
	r := t21Installed("0.15.0", "exchtray-0.15.0.ico", true)
	r.FailAt, r.FailOnce = "StartService|", true
	rn := &recNotifier{}
	res = Install(r, t21Config("0.16.0", rn), &memLog{})
	if res.Code != ExitError {
		t.Fatalf("rollback code=%d err=%v", res.Code, res.Err)
	}
	if got := iconsIn(r); !reflect.DeepEqual(got, []string{"exchtray-0.15.0.ico"}) {
		t.Fatalf("icons after rollback %v, want only exchtray-0.15.0.ico", got)
	}
	if !bytes.Equal(r.Files[oldIco], []byte("old-icon")) {
		t.Fatal("old icon changed by a rolled-back upgrade")
	}
	if r.ShortcutIcon != oldIco || r.DesktopIcon != oldIco || displayIcon(r.Uninst) != oldIco+",0" {
		t.Fatalf("after rollback: start=%q desktop=%q DisplayIcon=%q want %q", r.ShortcutIcon, r.DesktopIcon, displayIcon(r.Uninst), oldIco)
	}
	if len(rn.Calls) == 0 || rn.Calls[len(rn.Calls)-1] != "ASSOCCHANGED" || !contains(rn.Calls, "UPDATEITEM|"+oldIco) {
		t.Fatalf("rollback notifications %q", rn.Calls)
	}

	// negative 2: pre-fix install (DisplayIcon = tray exe) rolled back →
	// shortcuts back on the tray exe
	p := t21Installed("0.15.0", "unused.bin", false)
	p.Uninst = UninstallValues(t21Dir, t21Dir+`\`+config.UninstallExeName, t21Tray, "0.15.0", 1, time.Now(), true)
	p.ShortcutIcon, p.DesktopIcon = t21Tray, t21Tray
	p.FailAt, p.FailOnce = "WriteUninstallKey|", true
	if res = Install(p, t21Config("0.16.0", &recNotifier{}), &memLog{}); res.Code != ExitError {
		t.Fatalf("pre-fix rollback code=%d", res.Code)
	}
	if p.ShortcutIcon != t21Tray || p.DesktopIcon != t21Tray || len(iconsIn(p)) != 0 {
		t.Fatalf("pre-fix rollback: start=%q desktop=%q icons=%v", p.ShortcutIcon, p.DesktopIcon, iconsIn(p))
	}

	// negative 3: same-version reinstall keeps the current file
	s := t21Installed("0.16.0", "exchtray-0.16.0.ico", false, "exchtray-0.14.0.ico")
	if res = Install(s, t21Config("0.16.0", &recNotifier{}), &memLog{}); res.Code != ExitOK {
		t.Fatalf("reinstall code=%d err=%v", res.Code, res.Err)
	}
	if indexOf(s.Steps, "BackupFile|"+newIco) < 0 || indexOf(s.Steps, "RemoveBackup|"+newIco) < 0 || indexOf(s.Steps, "RemoveFile|"+newIco) >= 0 {
		t.Fatalf("reinstall must back up and keep %s: %v", newIco, s.Steps)
	}
	if got := iconsIn(s); !reflect.DeepEqual(got, []string{"exchtray-0.16.0.ico"}) || !bytes.Equal(s.Files[newIco], want) {
		t.Fatalf("reinstall icons %v", got)
	}

	// negative 4: cleanup failure → WARNING, exit 0, current icon in use
	w := t21Installed("0.15.0", "exchtray-0.15.0.ico", false)
	w.FailAlso = "RemoveFile|" + oldIco
	res = Install(w, t21Config("0.16.0", &recNotifier{}), &memLog{})
	if res.Code != ExitOK || len(iconWarnings(res.Warnings)) != 1 || !strings.Contains(iconWarnings(res.Warnings)[0], "старого файла значка") {
		t.Fatalf("cleanup failure: code=%d warnings=%v", res.Code, res.Warnings)
	}
	if w.ShortcutIcon != newIco {
		t.Fatalf("shortcut icon %q", w.ShortcutIcon)
	}
}

// TestCHK_SETUP_21_UninstallRemovesIcons — uninstall deletes every
// exchtray-*.ico (current and leftovers) and nothing else; a failed delete
// is an error that keeps the Uninstall key (retry possible).
func TestCHK_SETUP_21_UninstallRemovesIcons(t *testing.T) {
	f := t21Installed("0.16.0", "exchtray-0.16.0.ico", false, "exchtray-0.15.0.ico", "notes.ico")
	n := &recNotifier{}
	res := Uninstall(f, UninstallConfig{DataDir: t21Data, Notifier: n}, &memLog{})
	if res.Code != ExitOK {
		t.Fatalf("code=%d err=%v", res.Code, res.Err)
	}
	if got := iconsIn(f); len(got) != 0 {
		t.Fatalf("icons left after uninstall: %v", got)
	}
	if _, ok := f.Files[t21Dir+`\notes.ico`]; !ok {
		t.Fatal("uninstall removed an unrelated file")
	}
	for _, ico := range []string{"exchtray-0.15.0.ico", "exchtray-0.16.0.ico"} {
		if !contains(n.Calls, "UPDATEITEM|"+t21Dir+`\`+ico) {
			t.Fatalf("no UPDATEITEM for removed %s: %q", ico, n.Calls)
		}
	}
	if n.Calls[len(n.Calls)-1] != "ASSOCCHANGED" {
		t.Fatalf("ASSOCCHANGED must be last: %q", n.Calls)
	}

	g := t21Installed("0.16.0", "exchtray-0.16.0.ico", false)
	g.FailAlso = "RemoveFile|" + t21Dir + `\exchtray-0.16.0.ico`
	res = Uninstall(g, UninstallConfig{DataDir: t21Data, Notifier: &recNotifier{}}, &memLog{})
	if res.Code != ExitError || !strings.Contains(fmt.Sprint(res.Err), "удаление файла значка") {
		t.Fatalf("failed icon delete: code=%d err=%v", res.Code, res.Err)
	}
	if indexOf(g.Steps, "DeleteUninstallKey") >= 0 {
		t.Fatal("Uninstall key must stay when an icon could not be deleted")
	}
}

// TestCHK_SETUP_21_DevBuildHashName — a dev build (0.0.0-dev) names the file
// after the icon bytes: shortcuts / DisplayIcon use the hash name; a later
// dev build with a changed icon gets a new name and the old hash file goes.
func TestCHK_SETUP_21_DevBuildHashName(t *testing.T) {
	icoA := []byte("icon-A")
	icoB := []byte("icon-B")
	nameOf := func(b []byte) string {
		s := sha256.Sum256(b)
		return "exchtray-" + hex.EncodeToString(s[:])[:12] + ".ico"
	}
	f := newFake()
	c := t21Config("0.0.0-dev", &recNotifier{})
	c.Icon = icoA
	if res := Install(f, c, &memLog{}); res.Code != ExitOK {
		t.Fatalf("dev install code=%d err=%v", res.Code, res.Err)
	}
	pa := t21Dir + `\` + nameOf(icoA)
	if !bytes.Equal(f.Files[pa], icoA) || f.ShortcutIcon != pa || f.DesktopIcon != pa || displayIcon(f.Uninst) != pa+",0" {
		t.Fatalf("dev: icons=%v start=%q DisplayIcon=%q want %s", iconsIn(f), f.ShortcutIcon, displayIcon(f.Uninst), pa)
	}
	c.Icon = icoB
	if res := Install(f, c, &memLog{}); res.Code != ExitOK {
		t.Fatalf("dev reinstall code=%d err=%v", res.Code, res.Err)
	}
	pb := t21Dir + `\` + nameOf(icoB)
	if got := iconsIn(f); !reflect.DeepEqual(got, []string{nameOf(icoB)}) {
		t.Fatalf("dev reinstall icons %v want only %s", got, nameOf(icoB))
	}
	if f.ShortcutIcon != pb || displayIcon(f.Uninst) != pb+",0" {
		t.Fatalf("dev reinstall: start=%q DisplayIcon=%q want %s", f.ShortcutIcon, displayIcon(f.Uninst), pb)
	}
}
