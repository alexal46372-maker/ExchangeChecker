package trayui

// The application icon (phase 13, CHK-TRAY-89): a blue rounded square
// with two exchange arrows (→ on top, ← below). The arrows are green in
// the main icon (exe resource, window icons, installer); the tray uses
// the same picture and recolours only the arrows for its state
// (CHK-TRAY-90). Drawn with integer arithmetic only, like the other
// icons, so tools/mkres writes the same bytes on every machine.

// AppBlue is the background of the application icon (#1E6FD9, unchanged).
var AppBlue = [3]uint8{0x1E, 0x6F, 0xD9}

// Arrow colours of the application icon.
var (
	ArrowGreen  = [3]uint8{0x4A, 0xE0, 0x5E} // main icon / tray «норма»
	ArrowYellow = [3]uint8{0xFF, 0xD4, 0x1F} // tray: warning
	ArrowRed    = [3]uint8{0xFF, 0x3B, 0x30} // tray: error
	ArrowGrey   = [3]uint8{0xC8, 0xC8, 0xC8} // tray: no service / unknown
)

// ExeIconSizes are the images of cmd/exchtray/exchtray.ico (the exe and
// installer icon: RT_ICON 1–3).
var ExeIconSizes = []int{16, 32, 48}

// triangle: the point is inside (or on the edge of) a, b, c (design units).
func triangle(ax, ay, bx, by, cx, cy int64) func(x, y, den int64) bool {
	edge := func(px, py, qx, qy, x, y, den int64) int64 {
		return (qx-px)*den*(y-py*den) - (qy-py)*den*(x-px*den)
	}
	return func(x, y, den int64) bool {
		d1 := edge(ax, ay, bx, by, x, y, den)
		d2 := edge(bx, by, cx, cy, x, y, den)
		d3 := edge(cx, cy, ax, ay, x, y, den)
		neg := d1 < 0 || d2 < 0 || d3 < 0
		pos := d1 > 0 || d2 > 0 || d3 > 0
		return !(neg && pos)
	}
}

// roundRect: a rectangle with circular corners of radius r.
func roundRect(l, t, r, b, rad int64) func(x, y, den int64) bool {
	h, v := rect(l+rad, t, r-rad, b), rect(l, t+rad, r, b-rad)
	c := []func(x, y, den int64) bool{circle(l+rad, t+rad, rad), circle(r-rad, t+rad, rad), circle(l+rad, b-rad, rad), circle(r-rad, b-rad, rad)}
	return func(x, y, den int64) bool {
		if h(x, y, den) || v(x, y, den) {
			return true
		}
		for _, f := range c {
			if f(x, y, den) {
				return true
			}
		}
		return false
	}
}

// appShapes is the application icon with arrows of colour arrow.
func appShapes(arrow [3]uint8) []shape {
	return []shape{
		{AppBlue, roundRect(0, 0, 48, 48, 8)},
		// → on top: shaft and head
		{arrow, rect(8, 14, 30, 20)},
		{arrow, triangle(29, 7, 42, 17, 29, 27)},
		// ← below
		{arrow, rect(18, 28, 40, 34)},
		{arrow, triangle(19, 21, 6, 31, 19, 41)},
	}
}

// AppIconPixels is the size×size application icon (top-down
// premultiplied BGRA) with arrows of colour arrow.
func AppIconPixels(arrow [3]uint8, size int) []uint32 {
	return render(appShapes(arrow), size)
}

// ExeICO is cmd/exchtray/exchtray.ico: the main icon (green arrows) at
// ExeIconSizes.
func ExeICO() []byte {
	var imgs [][]uint32
	for _, s := range ExeIconSizes {
		imgs = append(imgs, AppIconPixels(ArrowGreen, s))
	}
	return EncodeICO(ExeIconSizes, imgs)
}

// Notification-area icons (phase 13, CHK-TRAY-90): the application icon
// whose arrows show the state. The blink logic (CHK-TRAY-38) is
// unchanged: it alternates IconError / IconWarning with IconBlank, and
// IconBlank is now the normal icon (green arrows) — so the arrows blink
// red (error) or yellow (warning).
const (
	IcoTrayOK      = "tray_ok"
	IcoTrayWarning = "tray_warning"
	IcoTrayError   = "tray_error"
	IcoTrayOffline = "tray_offline"
	IcoTrayProblem = "tray_problem"
	IcoTrayBlank   = "tray_blank"
)

// TrayIco is the .ico name of the notification icon of kind k.
func TrayIco(k Icon) string {
	switch k {
	case IconOK:
		return IcoTrayOK
	case IconWarning:
		return IcoTrayWarning
	case IconError:
		return IcoTrayError
	case IconProblem:
		return IcoTrayProblem
	case IconBlank:
		return IcoTrayBlank
	}
	return IcoTrayOffline
}

// TrayArrow is the arrow colour of the notification icon of kind k.
func TrayArrow(k Icon) [3]uint8 {
	switch k {
	case IconOK, IconBlank:
		return ArrowGreen
	case IconWarning:
		return ArrowYellow
	case IconError:
		return ArrowRed
	}
	return ArrowGrey
}

// TrayIconPixels is the notification icon of kind k: the application
// icon with TrayArrow(k) arrows; IconProblem adds a red dot.
func TrayIconPixels(k Icon, size int) []uint32 {
	s := appShapes(TrayArrow(k))
	if k == IconProblem {
		s = append(s, shape{white, circle(37, 37, 11)}, shape{ArrowRed, circle(37, 37, 9)})
	}
	return render(s, size)
}
