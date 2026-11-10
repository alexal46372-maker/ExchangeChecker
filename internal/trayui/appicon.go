package trayui

// The application icon (phase 13, CHK-TRAY-89; «metal and chrome» since
// phase 15, metal.go): a deep-blue tile in a chrome frame with two exchange
// arrows (→ on top, ← below). The arrows are green in the main icon (exe
// resource, window icons, installer); the tray uses the same picture and
// recolours only the arrows for its state (CHK-TRAY-90). Rendered
// deterministically (float64, standard library), so tools/mkres writes the
// same bytes on every run.

// ExeIconSizes are the images of cmd/exchtray/exchtray.ico (the exe and
// installer icon: RT_ICON 1–4; 256 is a PNG image, phase 15).
var ExeIconSizes = []int{16, 32, 48, 256}

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

// AppIconPixels is the size×size application icon (top-down
// premultiplied BGRA) with arrows of colour arrow.
func AppIconPixels(arrow [3]uint8, size int) []uint32 {
	return metalPixels(arrow, size, false)
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
// icon with TrayArrow(k) arrows; IconProblem adds a red dot in a white
// ring (bottom right).
func TrayIconPixels(k Icon, size int) []uint32 {
	return metalPixels(TrayArrow(k), size, k == IconProblem)
}
