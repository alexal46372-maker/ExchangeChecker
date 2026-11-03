package trayui

// Bold check mark of a database item in the left menu (phase 13,
// CHK-TRAY-91): a box with a thick check mark in the level color when
// polling is on, an empty box when it is off. Drawn as a 32-bit menu item
// bitmap (MENUITEMINFO.hbmpItem) by the same integer renderer as the
// icons (48-unit design grid).

// segment is a stroke of width w between (ax, ay) and (bx, by) with round
// caps (design units).
func segment(ax, ay, bx, by, w int64) func(x, y, den int64) bool {
	dx, dy := bx-ax, by-ay
	ll := dx*dx + dy*dy
	return func(x, y, den int64) bool {
		px, py := x-ax*den, y-ay*den
		// projection t = (p·d) / |d|² clamped to [0, 1] (scaled by den)
		t := (px*dx + py*dy) / ll
		if t < 0 {
			t = 0
		} else if t > den {
			t = den
		}
		ex, ey := px-dx*t, py-dy*t
		return 4*(ex*ex+ey*ey) <= w*w*den*den
	}
}

// Check mark geometry (design units).
const (
	checkBoxL, checkBoxT, checkBoxR, checkBoxB = 4, 4, 44, 44
	checkBorder                                = 4
	checkStroke                                = 9
)

var (
	checkBoxFill   = [3]uint8{0xFF, 0xFF, 0xFF}
	checkBoxBorder = [3]uint8{0x50, 0x50, 0x50}
)

// checkShapes: the box and, when checked, the bold check mark of c
// (dark outline under the colored stroke, readable on yellow / white).
func checkShapes(c Color, checked bool) []shape {
	sh := []shape{
		{checkBoxBorder, rect(checkBoxL, checkBoxT, checkBoxR, checkBoxB)},
		{checkBoxFill, rect(checkBoxL+checkBorder, checkBoxT+checkBorder, checkBoxR-checkBorder, checkBoxB-checkBorder)},
	}
	if !checked {
		return sh
	}
	r, g, b := RGB(c)
	dark := [3]uint8{r / 2, g / 2, b / 2}
	col := [3]uint8{r, g, b}
	for _, s := range []struct {
		rgb [3]uint8
		w   int64
	}{{dark, checkStroke + 4}, {col, checkStroke}} {
		sh = append(sh,
			shape{s.rgb, segment(11, 25, 20, 35, s.w)},
			shape{s.rgb, segment(20, 35, 38, 12, s.w)})
	}
	return sh
}

// CheckPixels is a size×size top-down 32-bit premultiplied image of the
// database check box: checked = bold check mark of c, otherwise empty.
func CheckPixels(c Color, checked bool, size int) []uint32 {
	return render(checkShapes(c, checked), size)
}
