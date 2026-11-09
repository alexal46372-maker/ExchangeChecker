//go:build windows

package wintray

import (
	"runtime"
	"testing"
	"time"
	"unsafe"

	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// TestCHK_TRAY_96_OwnerDrawSlider — the Volume item of the real right
// menu is owner-draw; WM_MEASUREITEM / WM_DRAWITEM to the tray window
// size and paint it; the speaker part changes with the level, the track
// with every value.
func TestCHK_TRAY_96_OwnerDrawSlider(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	sstore := &memSoundStore{}
	app, _, _ := startP10App(t, &alertService{}, &testBeeper{}, sstore)
	items := app.RightMenu()
	vi := volumeIndex(items)
	if vi < 0 || items[vi].ID != trayui.CmdVolume {
		t.Fatalf("no slider in %+v", items)
	}
	hm, err := buildMenu(items, &app.squares)
	if err != nil {
		t.Fatal(err)
	}
	defer winapi.DestroyMenu(hm)
	if _, mii := menuItem(t, hm, vi); mii.Type&winapi.MFT_OWNERDRAW == 0 || mii.ID != uint32(trayui.CmdVolume) {
		t.Fatalf("type %#x id %d", mii.Type, mii.ID)
	}
	m := winapi.MEASUREITEMSTRUCT{CtlType: winapi.ODT_MENU, ItemID: uint32(trayui.CmdVolume)}
	if winapi.SendMessage(app.hwnd, winapi.WM_MEASUREITEM, 0, uintptr(unsafe.Pointer(&m))) != 1 || m.ItemWidth != uint32(px(volumeItemW)) || m.ItemHeight != uint32(px(volumeItemH)) {
		t.Fatalf("measure %+v", m)
	}
	w, h := int32(m.ItemWidth), int32(m.ItemHeight)
	paint := func(v int) []byte {
		if err := app.SetMasterVolume(v); err != nil {
			t.Fatal(err)
		}
		before := app.volumeDraws
		buf, ok := winapi.RenderDC(w, h, func(dc uintptr) {
			d := winapi.DRAWITEMSTRUCT{CtlType: winapi.ODT_MENU, ItemID: uint32(trayui.CmdVolume), HDC: dc, RcItem: winapi.RECT{Right: w, Bottom: h}}
			winapi.SendMessage(app.hwnd, winapi.WM_DRAWITEM, 0, uintptr(unsafe.Pointer(&d)))
		})
		if !ok || app.volumeDraws != before+1 {
			t.Fatalf("draw %d", v)
		}
		return buf
	}
	// the «N %» text is drawn with the menu font (lfMenuFont), not the
	// font the DC happens to have
	if f := winapi.MenuFont(); f == 0 || app.volumeFont != f {
		t.Errorf("slider text font %#x, menu font %#x", app.volumeFont, f)
	}
	if lf, ok := winapi.MenuLogFont(); !ok || lf.FaceName[0] == 0 {
		t.Errorf("lfMenuFont %+v %v", lf, ok)
	}
	region := func(b []byte, x0, x1 int32) string {
		var s []byte
		for y := int32(0); y < h; y++ {
			s = append(s, b[(y*w+x0)*4:(y*w+x1)*4]...)
		}
		return string(s)
	}
	icon := px(8 + volumeIconW)
	l, r := volumeTrack(winapi.RECT{Right: w, Bottom: h})
	p0, p20, p30, p50, p100 := paint(0), paint(20), paint(30), paint(50), paint(100)
	if region(p20, 0, icon) != region(p30, 0, icon) {
		t.Error("same level, different speaker")
	}
	if region(p0, 0, icon) == region(p20, 0, icon) || region(p20, 0, icon) == region(p50, 0, icon) || region(p50, 0, icon) == region(p100, 0, icon) {
		t.Error("speaker does not follow the level")
	}
	if region(p20, l, r) == region(p30, l, r) {
		t.Error("track does not follow the value")
	}
	// a pixel of the track: the filled part grows
	hlAt := func(b []byte, x int32) bool {
		c := winapi.GetSysColor(winapi.COLOR_HIGHLIGHT)
		i := ((h/2)*w + x) * 4
		return b[i+2] == byte(c) && b[i+1] == byte(c>>8) && b[i] == byte(c>>16)
	}
	mid := l + (r-l)*3/4
	if hlAt(p50, mid) || !hlAt(p100, mid) {
		t.Error("filled part of the track")
	}
	// the value at x: live in memory, HKCU only on the release
	app.SetMasterVolume(10)
	saved := sstore.saves
	rc := winapi.RECT{Left: 100, Right: 100 + w, Top: 0, Bottom: h}
	tl, tr := volumeTrack(rc)
	if !app.volumeAtX(tr+20, rc) || app.SoundPrefs().Master != 100 || app.volumeAtX(tr, rc) {
		t.Errorf("right end %d", app.SoundPrefs().Master)
	}
	if !app.volumeAtX(tl-5, rc) || app.SoundPrefs().Master != 0 {
		t.Errorf("left end %d", app.SoundPrefs().Master)
	}
	if sstore.saves != saved || !menuVolume.dirty {
		t.Errorf("dragging wrote HKCU: saves %d→%d dirty %v", saved, sstore.saves, menuVolume.dirty)
	}
	app.storeMenuVolume()
	if sstore.saves != saved+1 || menuVolume.dirty {
		t.Errorf("release: saves %d→%d dirty %v", saved, sstore.saves, menuVolume.dirty)
	}
	app.storeMenuVolume()
	if sstore.saves != saved+1 {
		t.Error("a second write without a change")
	}
	// the speaker icon follows the DPI (cached per size)
	keep := testDPI
	testDPI = 96
	n96 := app.squares.speakerIcon(trayui.SpeakerLevel(1))
	testDPI = 144
	n144 := app.squares.speakerIcon(trayui.SpeakerLevel(1))
	testDPI = keep
	if n96 == 0 || n144 == 0 || n96 == n144 {
		t.Errorf("speaker icon per DPI: %#x %#x", n96, n144)
	}
	f96, f144 := alertTitleFontFor(96), alertTitleFontFor(144)
	if f96 == 0 || f144 == 0 || f96 == f144 || alertTitleFontFor(96) != f96 || alertTitleFontFor(144) != f144 {
		t.Errorf("alert title font per DPI: %#x %#x", f96, f144)
	}
}

// TestCHK_TRAY_96_DragInShownMenu — the real right menu is shown
// (TrackPopupMenuEx + WH_MSGFILTER hook); press, move, release on the
// slider change the master volume, play the preview cue and keep the menu
// open; Escape closes it without a command. Live: the press and every move
// (before the release) repaint the item in the shown menu window itself
// with the new value — not only some time later or on the release.
func TestCHK_TRAY_96_DragInShownMenu(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	store := &memSoundStore{}
	b := &testBeeper{}
	app, _, _ := startP10App(t, &alertService{}, b, store)
	app.SetMasterVolume(100)
	saves := store.saves
	vi := volumeIndex(app.RightMenu())
	// paint is one look at the last slider paint: count, window, value,
	// and the master volume at that moment
	type paint struct {
		n, value, master, saves int
		on                      uintptr
	}
	type result struct {
		err                    string
		mw                     uintptr
		openAfterUp            bool
		before, down, move, up paint
	}
	done := make(chan result, 1)
	go func() {
		var res result
		defer func() { done <- res }()
		var mw uintptr
		for i := 0; i < 100 && mw == 0; i++ {
			time.Sleep(50 * time.Millisecond)
			if w := winapi.FindWindow("#32768"); w != 0 && winapi.IsWindowVisible(w) {
				mw = w
			}
		}
		if mw == 0 {
			res.err = "menu not shown"
			return
		}
		hm := winapi.SendMessage(mw, 0x01E1, 0, 0) // MN_GETHMENU
		rc, ok := winapi.GetMenuItemRect(mw, hm, vi)
		if !ok {
			res.err = "GetMenuItemRect"
			return
		}
		l, r := volumeTrack(rc)
		cy := (rc.Top + rc.Bottom) / 2
		step := func(x int32, msg uint32) {
			winapi.SetCursorPos(x, cy)
			time.Sleep(80 * time.Millisecond)
			winapi.PostMessage(mw, msg, 0, 0)
			time.Sleep(150 * time.Millisecond)
		}
		look := func() paint {
			return paint{n: app.volumeDraws, value: app.volumeDrawnValue, master: app.soundPrefs.Master, on: app.volumeDrawnOn, saves: store.saves}
		}
		res.mw = mw
		res.before = look()
		step(l+(r-l)/5, winapi.WM_LBUTTONDOWN)
		res.down = look()
		step(l+(r-l)/2, winapi.WM_MOUSEMOVE)
		res.move = look()
		step(l+(r-l)/2, winapi.WM_LBUTTONUP)
		res.up = look()
		res.openAfterUp = winapi.IsWindowVisible(mw)
		winapi.PostMessage(mw, winapi.WM_KEYDOWN, winapi.VK_ESCAPE, 0)
		time.Sleep(100 * time.Millisecond)
		winapi.PostMessage(mw, winapi.WM_KEYDOWN, winapi.VK_ESCAPE, 0)
	}()
	app.ShowRightMenu()
	res := <-done
	if res.err != "" {
		t.Fatal(res.err)
	}
	if v := app.SoundPrefs().Master; v < 45 || v > 55 || store.saves != saves+1 {
		t.Errorf("master %d saves %d (want ~50, one write on the release)", v, store.saves-saves)
	}
	// HKCU is written on the release only, not on every move
	if res.down.saves != saves || res.move.saves != saves || res.up.saves != saves+1 {
		t.Errorf("writes: press %d move %d release %d (from %d)", res.down.saves, res.move.saves, res.up.saves, saves)
	}
	if !res.openAfterUp {
		t.Error("the release closed the menu")
	}
	// the press and the move (before the release) each repainted the item
	// in the shown menu window with the value just set
	prev := res.before
	for _, st := range []struct {
		name string
		p    paint
		lo   int
		hi   int
	}{{"press", res.down, 15, 25}, {"move", res.move, 45, 55}} {
		p := st.p
		if p.master < st.lo || p.master > st.hi {
			t.Errorf("%s: master %d, want %d..%d", st.name, p.master, st.lo, st.hi)
		}
		if p.n <= prev.n || p.on != res.mw || p.value != p.master {
			t.Errorf("%s: not repainted live: paints %d→%d, on %#x (menu %#x), painted %d, master %d",
				st.name, prev.n, p.n, p.on, res.mw, p.value, p.master)
		}
		prev = p
	}
	if res.up.value != res.up.master {
		t.Errorf("after release the item shows %d, master %d", res.up.value, res.up.master)
	}
	pumpUntil(t, "volume preview", func() bool { return b.has(volumePreviewFreqHz) })
	if menuVolume.app != nil || menuVolume.index != -1 || menuVolume.drag {
		t.Errorf("state left %+v", menuVolume)
	}
}
