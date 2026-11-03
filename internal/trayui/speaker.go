package trayui

// Volume slider of the tray right menu (phase 13 item 11, CHK-TRAY-96):
// the master volume trackbar is drawn inside the menu (owner-draw item),
// and its speaker icon follows every move: 0 % crossed out, then one, two
// or three sound waves, at 100 % the speaker "shouts" (three bold red
// waves and burst rays).

// SpeakerLevel of a volume.
type SpeakerLevel int

const (
	SpeakerMute SpeakerLevel = iota
	SpeakerWave1
	SpeakerWave2
	SpeakerWave3
	SpeakerShout
)

// VolumeLevel maps 0..100 to the speaker picture.
func VolumeLevel(v int) SpeakerLevel {
	switch {
	case v <= 0:
		return SpeakerMute
	case v < 34:
		return SpeakerWave1
	case v < 67:
		return SpeakerWave2
	case v < 100:
		return SpeakerWave3
	}
	return SpeakerShout
}

// VolumeFromX maps a point of the track [left, right) to 0..100 (clamped).
func VolumeFromX(x, left, right int) int {
	if right <= left+1 {
		return 0
	}
	v := ((x-left)*100 + (right-left-1)/2) / (right - left - 1)
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

var (
	speakerBody  = [3]uint8{0x30, 0x30, 0x30}
	speakerWave  = [3]uint8{0x1E, 0x6F, 0xD9}
	speakerShout = [3]uint8{0xFF, 0x3B, 0x30}
	speakerCross = [3]uint8{0xD9, 0x30, 0x25}
)

// arc is a ring part right of cx (sound wave), |y-cy| < half height.
func arc(cx, cy, r1, r2 int64) func(x, y, den int64) bool {
	rg := ring(cx, cy, r1, r2)
	return func(x, y, den int64) bool {
		dx, dy := x-cx*den, y-cy*den
		if dx <= 0 {
			return false
		}
		if dy < 0 {
			dy = -dy
		}
		return dy*10 < dx*13 && rg(x, y, den) // ±52° fan
	}
}

func speakerShapes(l SpeakerLevel) []shape {
	sh := []shape{
		{speakerBody, rect(4, 18, 13, 30)},
		{speakerBody, triangle(12, 18, 24, 7, 24, 24)},
		{speakerBody, triangle(12, 30, 24, 24, 24, 41)},
		{speakerBody, rect(12, 18, 24, 30)},
	}
	if l == SpeakerMute {
		return append(sh,
			shape{speakerCross, segment(29, 15, 44, 33, 5)},
			shape{speakerCross, segment(29, 33, 44, 15, 5)})
	}
	col, w := speakerWave, int64(4)
	if l == SpeakerShout {
		col, w = speakerShout, 5
	}
	n := int(l) // waves 1..3; shout = 3 + rays
	if n > 3 {
		n = 3
	}
	for i := 0; i < n; i++ {
		r := int64(9 + 7*i)
		sh = append(sh, shape{col, arc(24, 24, r, r+w)})
	}
	if l == SpeakerShout {
		sh = append(sh,
			shape{col, segment(41, 6, 46, 1, 3)},
			shape{col, segment(43, 24, 47, 24, 3)},
			shape{col, segment(41, 42, 46, 47, 3)})
	}
	return sh
}

// SpeakerPixels is a size×size top-down 32-bit premultiplied image of the
// speaker of level l.
func SpeakerPixels(l SpeakerLevel, size int) []uint32 { return render(speakerShapes(l), size) }
