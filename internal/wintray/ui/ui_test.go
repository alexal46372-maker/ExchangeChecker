package ui

import (
	"fmt"
	"testing"
	"unicode/utf8"
)

func metrics(dpi int32) *Metrics {
	px := func(v int32) int32 { return (v*dpi + 48) / 96 }
	return &Metrics{Px: px, LineH: px(15), Ascent: px(12),
		Text: func(s string) (int32, int32) { return px(int32(utf8.RuneCountInString(s)) * 6), px(15) }}
}

func overlap(a, b Rect) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}

func inside(in, out Rect) bool {
	return in.X >= out.X && in.Y >= out.Y && in.X+in.W <= out.X+out.W && in.Y+in.H <= out.Y+out.H
}

// TestCHK_TRAY_45_Grid — the field column, the baseline of label and
// field, no overlaps, scaling, relayout on hide / show.
func TestCHK_TRAY_45_Grid(t *testing.T) {
	for _, dpi := range []int32{96, 120, 144} {
		m := metrics(dpi)
		name, host, port := &Ctl{Kind: Label, Text: "Имя:"}, &Ctl{Kind: Label, Text: "Сервер (host\\instance):"}, &Ctl{Kind: Label, Text: "Порт:"}
		eName, eHost, ePort := &Ctl{Kind: Edit, Grow: true}, &Ctl{Kind: Edit, Grow: true}, &Ctl{Kind: Edit, W: 60}
		chk := &Ctl{Kind: Check, Text: "Включён"}
		box := &Ctl{Kind: Group, Text: "Подключение"}
		ok, cancel := &Ctl{Kind: Button, Text: "Сохранить"}, &Ctl{Kind: Button, Text: "Закрыть"}
		g := &Grid{Rows: []GridRow{{Label: name, Field: eName}, {Label: host, Field: eHost}, {Label: port, Field: ePort}, {Field: chk}}}
		root := &Pad{All: P12, Child: &Col{Items: []Node{&GroupBox{Box: box, Child: g}, &Row{Right: true, Items: []Node{ok, cancel}}}}}
		f := &Form{Root: root, M: m, MinW: m.Px(400)}
		f.Relayout()
		tag := fmt.Sprintf("%d dpi", dpi)
		fx := eName.Rect.X
		for _, c := range []*Ctl{eHost, ePort, chk} {
			if c.Rect.X != fx {
				t.Errorf("%s: field column %d != %d", tag, c.Rect.X, fx)
			}
		}
		for _, p := range [][2]*Ctl{{name, eName}, {host, eHost}, {port, ePort}} {
			if p[0].Baseline(m) != p[1].Baseline(m) {
				t.Errorf("%s: baseline %q %d != %d", tag, p[0].Text, p[0].Baseline(m), p[1].Baseline(m))
			}
			if lw, _ := m.Text(p[0].Text); p[0].Rect.W < lw || p[0].Rect.X+p[0].Rect.W > fx {
				t.Errorf("%s: label %q %+v (text %d, field x %d)", tag, p[0].Text, p[0].Rect, lw, fx)
			}
		}
		all := []*Ctl{name, host, port, eName, eHost, ePort, chk, ok, cancel}
		for i, a := range all {
			if !inside(a.Rect, box.Rect) && a != ok && a != cancel {
				t.Errorf("%s: %q outside the group", tag, a.Text)
			}
			for _, b := range all[i+1:] {
				if overlap(a.Rect, b.Rect) {
					t.Errorf("%s: %q %+v overlaps %q %+v", tag, a.Text, a.Rect, b.Text, b.Rect)
				}
			}
		}
		if r := cancel.Rect; r.X+r.W != f.W-m.Px(P12) || ok.Rect.X+ok.Rect.W+m.Px(P8) != r.X {
			t.Errorf("%s: buttons not right-aligned: %+v %+v width %d", tag, ok.Rect, r, f.W)
		}
		if eName.Rect.X+eName.Rect.W != box.Rect.X+box.Rect.W-m.Px(P12) || ePort.Rect.W != m.Px(60) {
			t.Errorf("%s: grow / fixed width %+v %+v", tag, eName.Rect, ePort.Rect)
		}
		h := f.H
		f.Show(false, host, eHost)
		if f.H >= h || eHost.Placed || host.Placed || ePort.Rect.Y-eName.Rect.Y > m.Px(40) {
			t.Errorf("%s: hide: height %d -> %d, port at %d", tag, h, f.H, ePort.Rect.Y)
		}
		f.Show(true, host, eHost)
		if f.H != h || !eHost.Placed {
			t.Errorf("%s: show again: %d != %d", tag, f.H, h)
		}
		ui := []*Ctl{host, eHost}
		SetHidden(&Col{Items: []Node{ui[0], ui[1]}}, true)
		f.Relayout()
		if host.Placed || eHost.Placed {
			t.Error("SetHidden")
		}
	}
}

// TestCHK_TRAY_45_Paddings — 4 / 8 / 12 / 16 scale with the DPI; texts
// are measured (a longer label widens the label column); Space and
// hidden-only containers.
func TestCHK_TRAY_45_Paddings(t *testing.T) {
	for _, dpi := range []int32{96, 144} {
		m := metrics(dpi)
		for _, p := range []int32{P4, P8, P12, P16} {
			c := &Ctl{Kind: Box, W: 10, Hgt: 10}
			w, h := Layout(&Pad{All: p, Child: c}, m, 0, 0, 0)
			if c.Rect.X != m.Px(p) || w != m.Px(10)+2*m.Px(p) || h != w {
				t.Errorf("%d dpi pad %d: %+v %dx%d", dpi, p, c.Rect, w, h)
			}
		}
		short, long := &Ctl{Kind: Label, Text: "a"}, &Ctl{Kind: Label, Text: "a much longer label"}
		f1, f2 := &Ctl{Kind: Edit}, &Ctl{Kind: Edit}
		g := &Grid{Rows: []GridRow{{Label: short, Field: f1}, {Label: long, Field: f2}}}
		Layout(g, m, 0, 0, 0)
		lw, _ := m.Text(long.Text)
		if f1.Rect.X != lw+m.Px(P12) {
			t.Errorf("%d dpi: field x %d, want %d", dpi, f1.Rect.X, lw+m.Px(P12))
		}
		hidden := &Ctl{Kind: Edit, Hidden: true}
		if w, h := Layout(&Row{Items: []Node{hidden}}, m, 0, 0, 0); w != 0 || h != 0 || hidden.Placed {
			t.Error("a row of hidden controls takes space")
		}
		b := &Ctl{Kind: Button, Text: "Проверить подключение к серверу"}
		Layout(b, m, 0, 0, 0)
		if tw, _ := m.Text(b.Text); b.Rect.W < tw+m.Px(P16) {
			t.Errorf("button %d < text %d", b.Rect.W, tw)
		}
		ml := &Ctl{Kind: Label, Text: "one\r\nthe longest line\r\nx", Lines: 3}
		Layout(ml, m, 0, 0, 0)
		if tw, _ := m.Text("the longest line"); ml.Rect.W != tw || ml.Rect.H != 3*m.LineH {
			t.Errorf("multi-line label %+v", ml.Rect)
		}
	}
}

// TestCHK_TRAY_45_RowBaseline — a row of a label, an edit, a combo, a
// check box and a button shares one baseline.
func TestCHK_TRAY_45_RowBaseline(t *testing.T) {
	m := metrics(120)
	cs := []*Ctl{{Kind: Label, Text: "Время:"}, {Kind: Edit}, {Kind: Combo, Text: "Русский"}, {Kind: Check, Text: "Да"}, {Kind: Button, Text: "Тест"}}
	items := make([]Node, len(cs))
	for i, c := range cs {
		items[i] = c
	}
	Layout(&Row{Items: items}, m, 5, 7, 0)
	b := cs[0].Baseline(m)
	for _, c := range cs[1:] {
		if c.Baseline(m) != b {
			t.Errorf("kind %d baseline %d != %d", c.Kind, c.Baseline(m), b)
		}
		if c.Rect.Y < 7 {
			t.Errorf("kind %d above the row", c.Kind)
		}
	}
}

// TestCHK_TRAY_47_SharedColumns — grids of one page share the field
// column; Shrink narrows the wide boxes.
func TestCHK_TRAY_47_SharedColumns(t *testing.T) {
	m := metrics(96)
	a := &Grid{Rows: []GridRow{{Label: &Ctl{Kind: Label, Text: "ab", Lines: 1}, Field: &Ctl{Kind: Edit, W: 300}}}}
	b := &Grid{Rows: []GridRow{{Label: &Ctl{Kind: Label, Text: "a much longer label", Lines: 1}, Field: &Ctl{Kind: Edit, W: 300}}}}
	if a.FieldX(m) == b.FieldX(m) {
		t.Fatal("own columns differ")
	}
	Share(a, b)
	if a.FieldX(m) != b.FieldX(m) || a.FieldX(m) != 19*6+12 {
		t.Errorf("shared column: %d / %d", a.FieldX(m), b.FieldX(m))
	}
	b.Rows[0].Label.Hidden, b.Rows[0].Field.(*Ctl).Hidden = true, true
	if a.FieldX(m) != 2*6+12 {
		t.Errorf("a hidden grid does not widen the column: %d", a.FieldX(m))
	}
	col := &Col{Items: []Node{a, b}}
	if !Shrink(col, 150) || a.Rows[0].Field.(*Ctl).W != 150 || Shrink(&Ctl{Kind: Edit, W: 100}, 50) {
		t.Errorf("shrink: %d", a.Rows[0].Field.(*Ctl).W)
	}
	if Shrink(col, 100); a.Rows[0].Field.(*Ctl).W != 150 {
		t.Errorf("below 200 units nothing shrinks: %d", a.Rows[0].Field.(*Ctl).W)
	}
}
