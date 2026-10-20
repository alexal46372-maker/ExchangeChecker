// Package ui is the layout framework of the tray windows (phase 11,
// CHK-TRAY-45): rows, columns, a «label | field» grid with measured text,
// paddings of 4 / 8 / 12 / 16 units scaled to the DPI, baseline alignment
// and a relayout when controls are shown or hidden.
//
// The package is pure Go: sizes are 96-dpi units, the Metrics turn them
// into pixels and measure texts. The Windows side (wintray) creates the
// controls, measures with the window font and moves the controls to the
// rectangles computed here.
package ui

// Paddings and gaps in 96-dpi units.
const (
	P4  = 4
	P8  = 8
	P12 = 12
	P16 = 16
)

// Default heights (96 dpi), the grid of the tray (phase 10 D3).
const (
	EditH   = 23
	ButtonH = 28
	CheckH  = 20
	ButtonW = 90 // minimum push button width
	EditW   = 160
	BoxW    = 18 // check box / radio glyph + gap
)

// Kind is the control kind: it decides the natural size and the baseline.
type Kind int

const (
	Label  Kind = iota // STATIC text
	Edit               // EDIT (one line)
	Button             // push button
	Check              // check box / radio button
	Combo              // drop-down combo box
	Box                // list, list view, trackbar, multi-line edit: fixed size
	Group              // group box (see GroupBox)
)

// Metrics converts units and measures text in the window font.
type Metrics struct {
	// Px scales 96-dpi units to pixels.
	Px func(v int32) int32
	// Text is the extent of one line of text in pixels.
	Text func(s string) (w, h int32)
	// Ascent is the ascent of the font in pixels (baseline from the top
	// of a text line); LineH the height of a text line.
	Ascent, LineH int32
}

// Rect is a placed rectangle in pixels.
type Rect struct{ X, Y, W, H int32 }

// Node is a layout element.
type Node interface {
	// measure: natural size and baseline (from the top), pixels.
	measure(m *Metrics) (w, h, base int32)
	// place puts the node into the rectangle.
	place(m *Metrics, r Rect)
	visible() bool
}

// Ctl is one control.
type Ctl struct {
	H    uintptr // the window handle (set by the caller)
	Kind Kind
	// Text is measured (labels, buttons, check boxes); Lines > 1 for a
	// multi-line label (the longest line is measured).
	Text  string
	Lines int
	// W, Hgt: minimum size in units (0 = the default of the kind).
	W, Hgt int32
	// Grow: the control takes the free width of its row / column.
	Grow bool
	// Hidden: takes no space (Form.Show changes it and relayouts).
	Hidden bool
	// NoShrink: Shrink keeps the width (a text box: a static with
	// wrapped text must stay wide enough for it).
	NoShrink bool
	// Rect is the result of the last layout (pixels); Placed: the control
	// got a rectangle in the last layout (false = hide it).
	Rect   Rect
	Placed bool
}

func (c *Ctl) visible() bool { return c != nil && !c.Hidden }

func max32(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}

func (c *Ctl) textW(m *Metrics) int32 {
	w := int32(0)
	start := 0
	s := c.Text
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == '\n' {
			line := s[start:i]
			if n := len(line); n > 0 && line[n-1] == '\r' {
				line = line[:n-1]
			}
			lw, _ := m.Text(line)
			w = max32(w, lw)
			start = i + 1
		}
	}
	return w
}

func (c *Ctl) measure(m *Metrics) (w, h, base int32) {
	px := m.Px
	tw := c.textW(m)
	lines := int32(c.Lines)
	if lines < 1 {
		lines = 1
	}
	switch c.Kind {
	case Label:
		w, h = tw, lines*m.LineH
	case Edit:
		w, h = px(EditW), max32(px(EditH), m.LineH+px(8))
	case Combo:
		w, h = max32(px(EditW), tw+px(32)), max32(px(EditH), m.LineH+px(8))
	case Button:
		w, h = max32(px(ButtonW), tw+px(P16+P8)), max32(px(ButtonH), m.LineH+px(P12))
	case Check:
		w, h = tw+px(BoxW+P4), max32(px(CheckH), m.LineH+px(2))
	default:
		w, h = 0, 0
	}
	if c.W > 0 {
		w = max32(w, px(c.W))
		if c.Kind == Box || c.Kind == Edit || c.Kind == Combo {
			w = px(c.W)
		}
	}
	if c.Hgt > 0 {
		h = max32(h, px(c.Hgt))
		if c.Kind == Box {
			h = px(c.Hgt)
		}
	}
	return w, h, c.baseline(m, h)
}

// baseline of a control of height h: the text line is at the top of a
// label / box, vertically centred in the other kinds.
func (c *Ctl) baseline(m *Metrics, h int32) int32 {
	switch c.Kind {
	case Label, Box, Group:
		return m.Ascent
	}
	return (h-m.LineH)/2 + m.Ascent
}

// Baseline is the baseline of the placed control (pixels, window
// coordinates).
func (c *Ctl) Baseline(m *Metrics) int32 { return c.Rect.Y + c.baseline(m, c.Rect.H) }

func (c *Ctl) place(m *Metrics, r Rect) { c.Rect, c.Placed = r, true }

// Space is an empty gap (units).
type Space struct{ W, H int32 }

func (s Space) measure(m *Metrics) (int32, int32, int32) { return m.Px(s.W), m.Px(s.H), 0 }
func (s Space) place(*Metrics, Rect)                     {}
func (s Space) visible() bool                            { return true }

// Align of a row: baseline (default), top, or centre.
type Align int

const (
	Baseline Align = iota
	Top
	Center
)

// Row lays its items left to right. Right packs them to the right edge
// (a button bar); Grow items share the free width otherwise.
type Row struct {
	Gap   int32 // units, default P8
	Items []Node
	Align Align
	Right bool
	Grow  bool // the row itself takes the width of its parent
}

func gapOr(g, d int32) int32 {
	if g == 0 {
		return d
	}
	if g < 0 {
		return 0
	}
	return g
}

func (r *Row) visible() bool { return r != nil && anyVisible(r.Items) }

func anyVisible(items []Node) bool {
	for _, n := range items {
		if n != nil && n.visible() {
			return true
		}
	}
	return false
}

func (r *Row) metrics(m *Metrics) (w, h, base int32, sizes [][3]int32) {
	gap := m.Px(gapOr(r.Gap, P8))
	var above, below int32
	first := true
	sizes = make([][3]int32, len(r.Items))
	for i, n := range r.Items {
		if n == nil || !n.visible() {
			continue
		}
		cw, ch, cb := n.measure(m)
		sizes[i] = [3]int32{cw, ch, cb}
		if !first {
			w += gap
		}
		first = false
		w += cw
		switch r.Align {
		case Baseline:
			above, below = max32(above, cb), max32(below, ch-cb)
		default:
			h = max32(h, ch)
		}
	}
	if r.Align == Baseline {
		h, base = above+below, above
	} else {
		base = m.Ascent
	}
	return w, h, base, sizes
}

func (r *Row) measure(m *Metrics) (int32, int32, int32) {
	w, h, b, _ := r.metrics(m)
	return w, h, b
}

func grows(n Node) bool {
	switch v := n.(type) {
	case *Ctl:
		return v.Grow
	case *Row:
		return v.Grow || v.Right
	case *Col, *Grid, *GroupBox, *Pad:
		return true
	}
	return false
}

func (r *Row) place(m *Metrics, rc Rect) {
	w, _, base, sizes := r.metrics(m)
	gap := m.Px(gapOr(r.Gap, P8))
	free := rc.W - w
	n := 0
	for i, it := range r.Items {
		if it != nil && it.visible() && grows(it) && sizes[i][0] >= 0 {
			n++
		}
	}
	x := rc.X
	if r.Right && free > 0 {
		x += free
		n = 0
	}
	for i, it := range r.Items {
		if it == nil || !it.visible() {
			continue
		}
		cw, ch, cb := sizes[i][0], sizes[i][1], sizes[i][2]
		if n > 0 && free > 0 && grows(it) {
			add := free / int32(n)
			cw += add
			free -= add
			n--
		}
		y := rc.Y
		switch r.Align {
		case Baseline:
			y += base - cb
		case Center:
			y += (rc.H - ch) / 2
		}
		if _, ok := it.(*Ctl); !ok && r.Align != Baseline {
			ch = rc.H
		}
		it.place(m, Rect{x, y, cw, ch})
		x += cw + gap
	}
}

// Col lays its items top to bottom; containers and Grow controls take
// the full width, the others keep their natural width.
type Col struct {
	Gap   int32 // units, default P8
	Items []Node
}

func (c *Col) visible() bool { return c != nil && anyVisible(c.Items) }

func (c *Col) measure(m *Metrics) (w, h, base int32) {
	gap := m.Px(gapOr(c.Gap, P8))
	first := true
	for _, n := range c.Items {
		if n == nil || !n.visible() {
			continue
		}
		cw, ch, cb := n.measure(m)
		if first {
			base = cb
		} else {
			h += gap
		}
		first = false
		w = max32(w, cw)
		h += ch
	}
	return w, h, base
}

func (c *Col) place(m *Metrics, rc Rect) {
	gap := m.Px(gapOr(c.Gap, P8))
	y := rc.Y
	for _, n := range c.Items {
		if n == nil || !n.visible() {
			continue
		}
		cw, ch, _ := n.measure(m)
		if grows(n) {
			cw = rc.W
		}
		n.place(m, Rect{rc.X, y, cw, ch})
		y += ch + gap
	}
}

// GridRow is one «label | field» row; Label nil = the field alone in the
// field column; Span: the field takes both columns.
type GridRow struct {
	Label *Ctl
	Field Node
	Span  bool
}

// Grid is the «label | field» grid: the label column is as wide as the
// widest visible label, every field starts at the same x (the field
// column), label and field are on one baseline.
type Grid struct {
	Gap, ColGap int32 // units, defaults P8 / P12
	Rows        []GridRow
	// Cols: grids sharing one field column (phase 11, CHK-TRAY-47:
	// one field column for the whole page); nil = own column.
	Cols *Columns
}

// Columns makes grids share the label column width (the widest visible
// label of all of them).
type Columns struct{ Grids []*Grid }

// Share creates a Columns for the grids and attaches it to each.
func Share(gs ...*Grid) *Columns {
	c := &Columns{Grids: gs}
	for _, g := range gs {
		g.Cols = c
	}
	return c
}

func (g *Grid) visible() bool {
	for _, r := range g.Rows {
		if rowVisible(r) {
			return true
		}
	}
	return false
}

func rowVisible(r GridRow) bool {
	return r.Field != nil && r.Field.visible() || r.Label.visible()
}

func (g *Grid) labelW(m *Metrics) int32 {
	if g.Cols == nil {
		return g.ownLabelW(m)
	}
	var w int32
	for _, o := range g.Cols.Grids {
		if o != nil && o.visible() {
			w = max32(w, o.ownLabelW(m))
		}
	}
	return w
}

func (g *Grid) ownLabelW(m *Metrics) int32 {
	var w int32
	for _, r := range g.Rows {
		if rowVisible(r) && !r.Span && r.Label.visible() {
			lw, _, _ := r.Label.measure(m)
			w = max32(w, lw)
		}
	}
	return w
}

func (g *Grid) rowSize(m *Metrics, r GridRow) (lw, fw, h, base int32) {
	var la, lb, fa, fb int32
	if r.Label.visible() {
		var lh, lbs int32
		lw, lh, lbs = r.Label.measure(m)
		la, lb = lbs, lh-lbs
	}
	if r.Field != nil && r.Field.visible() {
		var fh, fbs int32
		fw, fh, fbs = r.Field.measure(m)
		fa, fb = fbs, fh-fbs
	}
	base = max32(la, fa)
	return lw, fw, base + max32(lb, fb), base
}

// FieldX is the x of the field column relative to the grid (pixels).
func (g *Grid) FieldX(m *Metrics) int32 {
	lw := g.labelW(m)
	if lw == 0 {
		return 0
	}
	return lw + m.Px(gapOr(g.ColGap, P12))
}

func (g *Grid) measure(m *Metrics) (w, h, base int32) {
	gap := m.Px(gapOr(g.Gap, P8))
	fx := g.FieldX(m)
	first := true
	for _, r := range g.Rows {
		if !rowVisible(r) {
			continue
		}
		lw, fw, rh, rb := g.rowSize(m, r)
		if first {
			base = rb
		} else {
			h += gap
		}
		first = false
		if r.Span {
			w = max32(w, max32(lw, fw))
		} else {
			w = max32(w, fx+fw)
		}
		h += rh
	}
	return w, h, base
}

func (g *Grid) place(m *Metrics, rc Rect) {
	gap := m.Px(gapOr(g.Gap, P8))
	fx := g.FieldX(m)
	y := rc.Y
	for _, r := range g.Rows {
		if !rowVisible(r) {
			continue
		}
		lw, fw, rh, rb := g.rowSize(m, r)
		if r.Label.visible() {
			_, lh, lbs := r.Label.measure(m)
			r.Label.place(m, Rect{rc.X, y + rb - lbs, lw, lh})
		}
		if r.Field != nil && r.Field.visible() {
			_, fh, fbs := r.Field.measure(m)
			x, avail := rc.X+fx, rc.W-fx
			if r.Span {
				x, avail = rc.X, rc.W
			}
			if grows(r.Field) {
				fw = avail
			}
			r.Field.place(m, Rect{x, y + rb - fbs, fw, fh})
		}
		y += rh + gap
	}
}

// Pad surrounds its child with paddings (units; Left/Top/Right/Bottom,
// All for all four).
type Pad struct {
	All, Left, Top, Right, Bottom int32
	Child                         Node
}

func (p *Pad) sides(m *Metrics) (l, t, r, b int32) {
	f := func(v int32) int32 {
		if v == 0 {
			v = p.All
		}
		if v < 0 {
			return 0
		}
		return m.Px(v)
	}
	return f(p.Left), f(p.Top), f(p.Right), f(p.Bottom)
}

func (p *Pad) visible() bool { return p.Child != nil && p.Child.visible() }

func (p *Pad) measure(m *Metrics) (int32, int32, int32) {
	l, t, r, b := p.sides(m)
	w, h, base := p.Child.measure(m)
	return w + l + r, h + t + b, base + t
}

func (p *Pad) place(m *Metrics, rc Rect) {
	l, t, r, b := p.sides(m)
	p.Child.place(m, Rect{rc.X + l, rc.Y + t, rc.W - l - r, rc.H - t - b})
}

// GroupBox is a group box control around its content: the caption line
// on top, P8 inside.
type GroupBox struct {
	Box   *Ctl // Kind Group, Text = caption
	Child Node
}

func (g *GroupBox) visible() bool { return g.Box.visible() && g.Child != nil && g.Child.visible() }

func (g *GroupBox) inner(m *Metrics) (l, t, r, b int32) {
	return m.Px(P12), m.LineH + m.Px(P8), m.Px(P12), m.Px(P8) // bottom P8: no gap above the grid step
}

func (g *GroupBox) measure(m *Metrics) (int32, int32, int32) {
	l, t, r, b := g.inner(m)
	w, h, _ := g.Child.measure(m)
	cw := g.Box.textW(m) + m.Px(P16)
	return max32(w+l+r, cw), h + t + b, m.Ascent
}

func (g *GroupBox) place(m *Metrics, rc Rect) {
	l, t, r, b := g.inner(m)
	g.Box.Rect, g.Box.Placed = rc, true
	g.Child.place(m, Rect{rc.X + l, rc.Y + t, rc.W - l - r, rc.H - t - b})
}

// Layout places root at (x, y) with at least the width minW (pixels) and
// returns the size it takes.
func Layout(root Node, m *Metrics, x, y, minW int32) (w, h int32) {
	if root == nil {
		return 0, 0
	}
	Walk(root, func(c *Ctl) { c.Placed = false })
	if !root.visible() {
		return 0, 0
	}
	w, h, _ = root.measure(m)
	if minW > w {
		w = minW
	}
	root.place(m, Rect{x, y, w, h})
	return w, h
}

// Walk visits every control of the tree (also hidden ones).
func Walk(n Node, f func(*Ctl)) {
	switch v := n.(type) {
	case *Ctl:
		if v != nil {
			f(v)
		}
	case *Row:
		for _, c := range v.Items {
			Walk(c, f)
		}
	case *Col:
		for _, c := range v.Items {
			Walk(c, f)
		}
	case *Grid:
		for _, r := range v.Rows {
			if r.Label != nil {
				f(r.Label)
			}
			Walk(r.Field, f)
		}
	case *Pad:
		Walk(v.Child, f)
	case *GroupBox:
		f(v.Box)
		Walk(v.Child, f)
	}
}

// SetHidden hides / shows the controls of a subtree.
func SetHidden(n Node, hidden bool) { Walk(n, func(c *Ctl) { c.Hidden = hidden }) }

// Form is a laid-out window content: Show changes the visibility of
// controls and lays the form out again (Apply moves the controls).
type Form struct {
	Root Node
	M    *Metrics
	// X, Y: origin; MinW: minimum width (pixels).
	X, Y, MinW int32
	// Apply is called for every control after a layout (move it, show or
	// hide it).
	Apply func(c *Ctl)
	// W, H: the size of the last layout.
	W, H int32
}

// Relayout lays the form out and applies every control.
func (f *Form) Relayout() {
	f.W, f.H = Layout(f.Root, f.M, f.X, f.Y, f.MinW)
	if f.Apply != nil {
		Walk(f.Root, f.Apply)
	}
}

// Show shows or hides controls and relayouts when anything changed.
func (f *Form) Show(on bool, cs ...*Ctl) {
	changed := false
	for _, c := range cs {
		if c != nil && c.Hidden == on {
			c.Hidden = !on
			changed = true
		}
	}
	if changed {
		f.Relayout()
	}
}

// Shrink narrows the wide boxes of a subtree (Edit / Combo / Box of at
// least 200 units) by d units, not below 120: a window that is wider
// than the work area (phase 11, CHK-TRAY-47). It reports whether
// anything changed.
func Shrink(n Node, d int32) bool {
	changed := false
	Walk(n, func(c *Ctl) {
		if (c.Kind == Edit || c.Kind == Combo || c.Kind == Box) && !c.NoShrink && c.W >= 200 {
			w := c.W - d
			if w < 120 {
				w = 120
			}
			if w != c.W {
				c.W, changed = w, true
			}
		}
	})
	return changed
}
