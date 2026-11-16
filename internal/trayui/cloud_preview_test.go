package trayui

import (
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// TestCHK_TRAY_92_CloudPreview writes the previews of the speech-bubble
// panel (only with EC_CLOUD_PREVIEW_DIR set): a 96-dpi scene — the work
// area, a bottom taskbar with the notification icon (TrayIconPixels of the
// blink phase, 24 px) and the bubble placed by PanelCloud next to it, its
// mask (CloudMask) filled with the panel colour (ChromePanel) and its frame
// (CloudFrame, PanelBorder px) in PanelFrameColor — the same geometry and
// colours as the tray window. cloud_<level>_<phase>.png plus a JSON of the
// text rectangle (the text itself needs a Windows font; the preview script
// draws sample lines there).
func TestCHK_TRAY_92_CloudPreview(t *testing.T) {
	dir := os.Getenv("EC_CLOUD_PREVIEW_DIR")
	if dir == "" {
		t.Skip("EC_CLOUD_PREVIEW_DIR not set")
	}
	const sw, sh, bar = 560, 200, 40
	work := CloudRect{0, 0, sw, sh - bar}
	isz := int32(24)
	icon := CloudRect{sw - 120, sh - bar + (bar-isz)/2, sw - 120 + isz, sh - bar + (bar-isz)/2 + isz}
	m := CloudMetrics{Tail: PanelTail, TailHalf: PanelTailHalf, Radius: PanelRadius, Gap: PanelGap}
	bodyW, bodyH := int32(380), int32(2*15+2*(6+PanelBorder))
	c := PanelCloud(bodyW, bodyH, icon, true, work, m)
	rgb := func(cr uint32) color.NRGBA {
		return color.NRGBA{uint8(cr), uint8(cr >> 8), uint8(cr >> 16), 255}
	}
	meta := map[string]any{}
	for _, lv := range []struct {
		name string
		red  bool
		ico  Icon
	}{{"yellow", false, IconWarning}, {"red", true, IconError}} {
		for _, ph := range []struct {
			name string
			off  bool
		}{{"on", false}, {"off", true}} {
			img := image.NewNRGBA(image.Rect(0, 0, sw, sh))
			for y := 0; y < sh; y++ { // desktop and taskbar
				for x := 0; x < sw; x++ {
					col := color.NRGBA{0x2D, 0x5F, 0x8F, 255}
					if y >= sh-bar {
						col = color.NRGBA{0x1C, 0x1C, 0x22, 255}
					}
					img.SetNRGBA(x, y, col)
				}
			}
			k := lv.ico
			if ph.off {
				k = IconBlank
			}
			pix := TrayIconPixels(k, int(isz))
			for y := int32(0); y < isz; y++ {
				for x := int32(0); x < isz; x++ {
					v := pix[y*isz+x]
					a := float64(v>>24) / 255
					if a == 0 {
						continue
					}
					bg := img.NRGBAAt(int(icon.L+x), int(icon.T+y))
					mix := func(f uint8, b uint8) uint8 { return uint8(float64(f) + float64(b)*(1-a) + 0.5) } // premultiplied
					img.SetNRGBA(int(icon.L+x), int(icon.T+y), color.NRGBA{mix(uint8(v>>16), bg.R), mix(uint8(v>>8), bg.G), mix(uint8(v), bg.B), 255})
				}
			}
			frame := rgb(PanelFrameColor(lv.red, ph.off))
			panel := rgb(ChromePanel())
			for y := int32(0); y < c.Win.H(); y++ {
				for x := int32(0); x < c.Win.W(); x++ {
					if !c.CloudMask(x, y) {
						continue
					}
					col := panel
					if c.CloudFrame(x, y, PanelBorder) {
						col = frame
					}
					img.SetNRGBA(int(c.Win.L+x), int(c.Win.T+y), col)
				}
			}
			name := "cloud_" + lv.name + "_" + ph.name
			f, err := os.Create(filepath.Join(dir, name+".png"))
			if err != nil {
				t.Fatal(err)
			}
			if err := png.Encode(f, img); err != nil {
				t.Fatal(err)
			}
			f.Close()
		}
	}
	tx := c.Win.L + c.Body.L + 8 + PanelBorder
	ty := c.Win.T + c.Body.T + 6 + PanelBorder
	meta["text"] = []int32{tx, ty, c.Win.L + c.Body.R - 8 - PanelBorder, c.Win.T + c.Body.B - 6 - PanelBorder}
	meta["textColor"] = []uint8{uint8(ChromeText()), uint8(ChromeText() >> 8), uint8(ChromeText() >> 16)}
	meta["cloud"] = c
	b, _ := json.MarshalIndent(meta, "", " ")
	if err := os.WriteFile(filepath.Join(dir, "cloud_meta.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}
