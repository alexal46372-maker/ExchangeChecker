package trayui

// RGB values of the colors (sRGB).
var colorRGB = map[Color][3]uint8{
	ColorGreen:  {0x2E, 0xA0, 0x43},
	ColorYellow: {0xF2, 0xC2, 0x00},
	ColorRed:    {0xD9, 0x30, 0x25},
	ColorGrey:   {0x9A, 0x9A, 0x9A},
}

// RGB returns the color's sRGB components.
func RGB(c Color) (r, g, b uint8) {
	v := colorRGB[c]
	return v[0], v[1], v[2]
}

// argb packs a premultiplied BGRA pixel as a little-endian uint32 (the
// layout of a 32-bit top-down DIB: B, G, R, A bytes).
func argb(a, r, g, b uint8) uint32 {
	pr := uint32(r) * uint32(a) / 255
	pg := uint32(g) * uint32(a) / 255
	pb := uint32(b) * uint32(a) / 255
	return uint32(a)<<24 | pr<<16 | pg<<8 | pb
}

// SquarePixels is a size×size top-down 32-bit premultiplied image: a
// filled square of color c with a dark 1-px border and a transparent
// margin of 2 px (menu item bitmap, MENUITEMINFO.hbmpItem).
func SquarePixels(c Color, size int) []uint32 {
	px := make([]uint32, size*size)
	if c == ColorNone {
		return px
	}
	r, g, b := RGB(c)
	for y := 2; y < size-2; y++ {
		for x := 2; x < size-2; x++ {
			if x == 2 || y == 2 || x == size-3 || y == size-3 {
				px[y*size+x] = argb(255, r/2, g/2, b/2)
			} else {
				px[y*size+x] = argb(255, r, g, b)
			}
		}
	}
	return px
}

// IconColor is the main color of a tray icon kind.
func IconColor(k Icon) Color {
	switch k {
	case IconOK:
		return ColorGreen
	case IconWarning:
		return ColorYellow
	case IconError:
		return ColorRed
	}
	return ColorGrey
}

// IconPixels is a size×size top-down 32-bit premultiplied image of the
// tray icon: a filled circle in IconColor(k); IconProblem gets a red
// center dot.
func IconPixels(k Icon, size int) []uint32 {
	px := make([]uint32, size*size)
	r0, g0, b0 := RGB(IconColor(k))
	c := float64(size-1) / 2
	rad := float64(size)/2 - 0.5
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx, dy := float64(x)-c, float64(y)-c
			d2 := dx*dx + dy*dy
			if d2 > rad*rad {
				continue
			}
			r, g, b := r0, g0, b0
			if d2 > (rad-1.2)*(rad-1.2) { // darker rim
				r, g, b = r/2, g/2, b/2
			}
			if k == IconProblem && d2 <= (rad*0.45)*(rad*0.45) {
				r, g, b = RGB(ColorRed)
			}
			px[y*size+x] = argb(255, r, g, b)
		}
	}
	return px
}
