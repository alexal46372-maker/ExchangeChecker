package trayui

import (
	"encoding/binary"
	"errors"
	"sort"
)

// ICO icons of the tray (phase 11, PLAN §12 p.144, CHK-TRAY-49). They are
// drawn here with integer arithmetic only (the same bytes on every
// machine), written by tools/mkres into internal/wintray/icons/*.ico
// (committed, checked by mkres -check and the CI double run) and
// embedded into the tray: the notification icon, the window icons, the
// settings navigation and the database cards.

// IconSizes are the images of every .ico: 16/32 at 96 dpi, 20/40 at
// 120 dpi, 24/48 at 144 dpi.
var IconSizes = []int{16, 20, 24, 32, 40, 48}

// Icon file names (without .ico).
const (
	IcoStateOK      = "state_ok"
	IcoStateWarning = "state_warning"
	IcoStateError   = "state_error"
	IcoStateOffline = "state_offline"
	IcoStateProblem = "state_problem"
	IcoStateBlank   = "state_blank"
	IcoNavDatabases = "nav_databases"
	IcoNavGeneral   = "nav_general"
	IcoNavViber     = "nav_viber"
	IcoNavMode      = "nav_mode"
	IcoApp          = "app"
)

// StateIco is the .ico name of a tray icon kind.
func StateIco(k Icon) string {
	switch k {
	case IconOK:
		return IcoStateOK
	case IconWarning:
		return IcoStateWarning
	case IconError:
		return IcoStateError
	case IconProblem:
		return IcoStateProblem
	case IconBlank:
		return IcoStateBlank
	}
	return IcoStateOffline
}

// CardIco is the .ico of a card colour.
func CardIco(c Color) string {
	switch c {
	case ColorGreen:
		return IcoStateOK
	case ColorYellow:
		return IcoStateWarning
	case ColorRed:
		return IcoStateError
	}
	return IcoStateOffline
}

// IconNames lists every generated icon, sorted.
func IconNames() []string {
	n := []string{IcoStateOK, IcoStateWarning, IcoStateError, IcoStateOffline, IcoStateProblem, IcoStateBlank,
		IcoNavDatabases, IcoNavGeneral, IcoNavViber, IcoNavMode, IcoApp}
	sort.Strings(n)
	return n
}

// IconICO returns the .ico file of name (every size of IconSizes).
func IconICO(name string) ([]byte, error) {
	var imgs [][]uint32
	for _, s := range IconSizes {
		p, err := iconImage(name, s)
		if err != nil {
			return nil, err
		}
		imgs = append(imgs, p)
	}
	return EncodeICO(IconSizes, imgs), nil
}

// iconImage: size×size top-down premultiplied BGRA of name.
func iconImage(name string, size int) ([]uint32, error) {
	switch name {
	case IcoStateOK:
		return IconPixels(IconOK, size), nil
	case IcoStateWarning:
		return IconPixels(IconWarning, size), nil
	case IcoStateError:
		return IconPixels(IconError, size), nil
	case IcoStateOffline:
		return IconPixels(IconOffline, size), nil
	case IcoStateProblem:
		return IconPixels(IconProblem, size), nil
	case IcoStateBlank:
		return IconPixels(IconBlank, size), nil
	}
	shapes, ok := glyphs[name]
	if !ok {
		return nil, errors.New("trayui: unknown icon " + name)
	}
	return render(shapes, size), nil
}

// shape on a 48×48 design grid; inside reports whether the point
// (X/den, Y/den) in design units is covered.
type shape struct {
	rgb    [3]uint8
	inside func(x, y, den int64) bool
}

func circle(cx, cy, r int64) func(x, y, den int64) bool {
	return func(x, y, den int64) bool {
		dx, dy := x-cx*den, y-cy*den
		return dx*dx+dy*dy <= r*r*den*den
	}
}

func ellipse(cx, cy, rx, ry int64) func(x, y, den int64) bool {
	return func(x, y, den int64) bool {
		dx, dy := x-cx*den, y-cy*den
		return dx*dx*ry*ry+dy*dy*rx*rx <= rx*rx*ry*ry*den*den
	}
}

func rect(l, t, r, b int64) func(x, y, den int64) bool {
	return func(x, y, den int64) bool { return x >= l*den && x <= r*den && y >= t*den && y <= b*den }
}

func ring(cx, cy, r1, r2 int64) func(x, y, den int64) bool {
	o, i := circle(cx, cy, r2), circle(cx, cy, r1)
	return func(x, y, den int64) bool { return o(x, y, den) && !i(x, y, den) }
}

// diag: a bar along a diagonal through (cx, cy), half width w, half
// length l (sign +1: «\», -1: «/»).
func diag(cx, cy, w, l, sign int64) func(x, y, den int64) bool {
	return func(x, y, den int64) bool {
		dx, dy := x-cx*den, y-cy*den
		a, b := dx-sign*dy, dx+sign*dy // a: across (×√2), b: along (×√2)
		if a < 0 {
			a = -a
		}
		if b < 0 {
			b = -b
		}
		return a*a <= 2*w*w*den*den && b*b <= 2*l*l*den*den
	}
}

var (
	blue   = [3]uint8{0x2F, 0x6E, 0xBA}
	light  = [3]uint8{0x8C, 0xB8, 0xE8}
	slate  = [3]uint8{0x55, 0x5E, 0x68}
	purple = [3]uint8{0x73, 0x60, 0xF2}
	white  = [3]uint8{0xFF, 0xFF, 0xFF}
)

// glyphs: later shapes paint over earlier ones.
var glyphs = map[string][]shape{
	IcoNavDatabases: { // a cylinder
		{blue, rect(9, 11, 39, 37)},
		{blue, ellipse(24, 37, 15, 6)},
		{light, ellipse(24, 11, 15, 6)},
		{light, rect(11, 23, 37, 25)},
	},
	IcoNavGeneral: { // a gear
		{slate, circle(24, 24, 14)},
		{slate, rect(20, 4, 28, 44)},
		{slate, rect(4, 20, 44, 28)},
		{slate, diag(24, 24, 4, 19, 1)},
		{slate, diag(24, 24, 4, 19, -1)},
		{white, circle(24, 24, 6)},
	},
	IcoNavViber: { // a speech bubble with three dots
		{purple, circle(24, 21, 18)},
		{purple, diag(14, 37, 4, 7, -1)},
		{white, circle(15, 21, 3)},
		{white, circle(24, 21, 3)},
		{white, circle(33, 21, 3)},
	},
	IcoNavMode: { // two sliders
		{slate, rect(4, 11, 44, 19)},
		{slate, rect(4, 29, 44, 37)},
		{blue, circle(16, 15, 9)},
		{blue, circle(32, 33, 9)},
	},
	IcoApp: { // the cylinder with a green state dot
		{blue, rect(7, 9, 35, 35)},
		{blue, ellipse(21, 35, 14, 6)},
		{light, ellipse(21, 9, 14, 6)},
		{[3]uint8{0x2E, 0xA0, 0x43}, circle(35, 35, 11)},
		{white, ring(35, 35, 11, 13)},
	},
}

// render supersamples the shapes 4×4 per pixel.
func render(shapes []shape, size int) []uint32 {
	const ss = 4
	den := int64(2 * size * ss)
	out := make([]uint32, size*size)
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			var r, g, b, n int
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					x := int64(2*(px*ss+sx)+1) * 48
					y := int64(2*(py*ss+sy)+1) * 48
					for i := len(shapes) - 1; i >= 0; i-- {
						if shapes[i].inside(x, y, den) {
							c := shapes[i].rgb
							r, g, b, n = r+int(c[0]), g+int(c[1]), b+int(c[2]), n+1
							break
						}
					}
				}
			}
			if n == 0 {
				continue
			}
			a := uint8(n * 255 / (ss * ss))
			out[py*size+px] = argb(a, uint8(r/n), uint8(g/n), uint8(b/n))
		}
	}
	return out
}

// EncodeICO writes 32-bit images (top-down premultiplied BGRA, as
// IconPixels) as an .ico file: BMP images, straight alpha, empty AND mask.
func EncodeICO(sizes []int, imgs [][]uint32) []byte {
	le := binary.LittleEndian
	n := len(sizes)
	head := make([]byte, 6+16*n)
	le.PutUint16(head[2:], 1)
	le.PutUint16(head[4:], uint16(n))
	var body []byte
	off := len(head)
	for i, s := range sizes {
		maskRow := (s + 31) / 32 * 4
		img := make([]byte, 40+s*s*4+maskRow*s)
		le.PutUint32(img[0:], 40)
		le.PutUint32(img[4:], uint32(s))
		le.PutUint32(img[8:], uint32(2*s))
		le.PutUint16(img[12:], 1)
		le.PutUint16(img[14:], 32)
		le.PutUint32(img[20:], uint32(s*s*4+maskRow*s))
		for y := 0; y < s; y++ {
			row := imgs[i][(s-1-y)*s:] // bottom-up
			for x := 0; x < s; x++ {
				p := row[x]
				a := uint8(p >> 24)
				pr, pg, pb := uint8(p>>16), uint8(p>>8), uint8(p)
				var r, g, b uint8
				if a != 0 {
					r, g, b = unpremul(pr, a), unpremul(pg, a), unpremul(pb, a)
				}
				o := 40 + (y*s+x)*4
				img[o], img[o+1], img[o+2], img[o+3] = b, g, r, a
			}
		}
		e := head[6+16*i:]
		e[0], e[1] = byte(s), byte(s)
		le.PutUint16(e[4:], 1)
		le.PutUint16(e[6:], 32)
		le.PutUint32(e[8:], uint32(len(img)))
		le.PutUint32(e[12:], uint32(off))
		off += len(img)
		body = append(body, img...)
	}
	return append(head, body...)
}

func unpremul(c, a uint8) uint8 {
	v := (uint32(c)*255 + uint32(a)/2) / uint32(a)
	if v > 255 {
		v = 255
	}
	return uint8(v)
}

// DecodeICO returns the size×size image of an .ico written by EncodeICO
// (top-down premultiplied BGRA for CreateIconFromPixels): the image of
// that size, otherwise the nearest larger (or the largest) one scaled
// by nearest neighbour.
func DecodeICO(ico []byte, size int) ([]uint32, error) {
	le := binary.LittleEndian
	if len(ico) < 6 || le.Uint16(ico[2:]) != 1 {
		return nil, errors.New("trayui: not an .ico")
	}
	n := int(le.Uint16(ico[4:]))
	if len(ico) < 6+16*n || n == 0 {
		return nil, errors.New("trayui: .ico directory truncated")
	}
	// exact size, else the smallest larger one, else the largest
	best, bestS := -1, 0
	for i := 0; i < n; i++ {
		s := int(ico[6+16*i])
		if s == 0 {
			s = 256
		}
		better := false
		switch {
		case best < 0:
			better = true
		case bestS == size:
		case s == size:
			better = true
		case s > size && (bestS < size || s < bestS):
			better = true
		case s < size && bestS < size && s > bestS:
			better = true
		}
		if better {
			best, bestS = i, s
		}
	}
	e := ico[6+16*best:]
	sz, off := int(le.Uint32(e[8:])), int(le.Uint32(e[12:]))
	if off+sz > len(ico) || sz < 40+bestS*bestS*4 || le.Uint16(ico[off+14:]) != 32 {
		return nil, errors.New("trayui: .ico image not 32-bit")
	}
	src := ico[off+40:]
	out := make([]uint32, size*size)
	for y := 0; y < size; y++ {
		sy := y * bestS / size
		for x := 0; x < size; x++ {
			sx := x * bestS / size
			o := ((bestS-1-sy)*bestS + sx) * 4
			b, g, r, a := src[o], src[o+1], src[o+2], src[o+3]
			out[y*size+x] = argb(a, r, g, b)
		}
	}
	return out, nil
}
