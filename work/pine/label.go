package pine

import (
	"maps"
	"math"
	"slices"
	"sort"
)

// Labels a script makes with label.new: a text at a bar and a price, in a
// box pointing at it, as TradingView draws them. A variable holding one
// holds its id, na once deleted; a script keeps max_labels_count of them,
// 50 unless it says (500 at most), the oldest going as more come. Like
// arrays, they are put back as the newest bar began when it runs again.

// Label styles past the shapes, which a label takes too.
const (
	LabelNone = ShapeChar + 1 + iota
	LabelCenter
	LabelLeft
	LabelRight
)

// Label is one as it stands.
type Label struct {
	X                int     // the bar
	Y                float64 // the price, when YLoc is LocAbsolute
	YLoc             int     // LocAbsolute, LocAboveBar or LocBelowBar
	Text             string
	Style            int     // a Shape kind, or LabelNone…
	Color, TextColor float64 // packed, NaN for none
	Size             int     // as plotshape's
}

type labels struct {
	byID  map[int]Label
	order []int // the ids by age
	next  int
}

func (l labels) clone() labels {
	return labels{byID: maps.Clone(l.byID), order: slices.Clone(l.order), next: l.next}
}

const (
	defaultLabels = 50
	maxLabels     = 500
)

// Labels are the labels the script left, the oldest first.
func (m *Machine) Labels() []Label {
	out := make([]Label, 0, len(m.lbls.order))
	for _, id := range m.lbls.order {
		out = append(out, m.lbls.byID[id])
	}
	return out
}

func (m *Machine) addLabel(lb Label) float64 {
	if m.lbls.byID == nil {
		m.lbls.byID = map[int]Label{}
	}
	m.lbls.next++
	id := m.lbls.next
	m.lbls.byID[id] = lb
	m.lbls.order = append(m.lbls.order, id)
	if keep := m.s.MaxLabels; len(m.lbls.order) > keep {
		for _, old := range m.lbls.order[:len(m.lbls.order)-keep] {
			delete(m.lbls.byID, old)
		}
		m.lbls.order = slices.Clone(m.lbls.order[len(m.lbls.order)-keep:])
	}
	return float64(id)
}

// label is the label id holds; ok is false for na or one deleted.
func (m *Machine) label(id float64) (Label, bool) {
	if math.IsNaN(id) {
		return Label{}, false
	}
	lb, ok := m.lbls.byID[int(id)]
	return lb, ok
}

func (m *Machine) setLabel(id float64, f func(*Label)) {
	if lb, ok := m.label(id); ok {
		f(&lb)
		m.lbls.byID[int(id)] = lb
	}
}

// barOfTime is the bar opening at t, or the one it falls in; past the
// newest, counted on in the chart's interval.
func (m *Machine) barOfTime(t float64) int {
	n := len(m.bars)
	if n == 0 || math.IsNaN(t) {
		return -1
	}
	if last := m.bars[n-1].T; int64(t) > last && m.env.IntervalMs > 0 {
		return n - 1 + int((int64(t)-last)/m.env.IntervalMs)
	}
	i := sort.Search(n, func(i int) bool { return m.bars[i].T > int64(t) })
	return max(i-1, 0)
}

const (
	xlocIndex = 0
	xlocTime  = 1
)

// labelCall compiles label.name, or the method name of the label recv.
func (c *compiler) labelCall(name string, recv *cx, x *callX, sc *scope) (cx, error) {
	fn := "label." + name
	params, ok := labelParams[name]
	if !ok {
		if labelIgnored[name] {
			return naX, nil
		}
		return cx{}, errAt(x.t, "OpenBar 不认识函数 %s", fn)
	}
	c.s.labels = true
	if recv != nil {
		params = params[1:]
	}
	a, err := c.args(x, params, sc)
	if err != nil {
		return cx{}, err
	}
	if recv != nil {
		a = append([]cx{*recv}, a...)
		params = labelParams[name]
	}
	arg := func(p string) cx {
		if i := slices.Index(params, p); i >= 0 {
			return a[i]
		}
		return cx{k: kMissing}
	}
	// What OpenBar cannot place, as label.new(point, text) with a chart.point,
	// is skipped as before labels were drawn, not failed.
	for i, p := range params {
		if given(a[i]) && (p == "text") != (a[i].k == kStr) && !isNaX(a[i]) {
			c.warn(x.t, "OpenBar 画不了这个 %s（用 chart.point 定位的 label 还不支持），会被跳过", fn)
			return naX, nil
		}
	}
	num := func(p string, def float64) func(m *Machine) float64 {
		v := arg(p)
		if !given(v) {
			return func(*Machine) float64 { return def }
		}
		return v.f
	}
	text := func(m *Machine) string { return "" }
	if t := arg("text"); given(t) && t.k == kStr {
		text = t.s
	}
	id := num("id", nan)
	xAt := func(m *Machine, xv, loc float64) int {
		if loc == xlocTime {
			return m.barOfTime(xv)
		}
		if math.IsNaN(xv) {
			return -1
		}
		return int(math.Round(xv))
	}
	void := func(f func(m *Machine)) cx {
		return cx{k: kVoid, f: func(m *Machine) float64 { f(m); return nan }}
	}
	switch name {
	case "new":
		xv, xl, yv, yl := num("x", nan), num("xloc", xlocIndex), num("y", nan), num("yloc", LocAbsolute)
		textDef := colors["white"] // version 6's; black before
		if c.version < 6 {
			textDef = colors["black"]
		}
		col, tcol, st, sz := num("color", float64(colors["blue"])), num("textcolor", float64(textDef)), num("style", ShapeLabelDown), num("size", 0)
		return numX(func(m *Machine) float64 {
			bar, y, yloc := xAt(m, xv(m), xl(m)), yv(m), int(yl(m))
			if bar < 0 || yloc == LocAbsolute && math.IsNaN(y) {
				return nan
			}
			return m.addLabel(Label{X: bar, Y: y, YLoc: yloc, Text: text(m), Style: int(st(m)), Color: col(m), TextColor: tcol(m), Size: int(sz(m))})
		}), nil
	case "copy":
		return numX(func(m *Machine) float64 {
			if lb, ok := m.label(id(m)); ok {
				return m.addLabel(lb)
			}
			return nan
		}), nil
	case "delete":
		return void(func(m *Machine) {
			if _, ok := m.label(id(m)); ok {
				i := int(id(m))
				delete(m.lbls.byID, i)
				m.lbls.order = slices.DeleteFunc(slices.Clone(m.lbls.order), func(o int) bool { return o == i })
			}
		}), nil
	case "get_x":
		return numX(func(m *Machine) float64 {
			if lb, ok := m.label(id(m)); ok {
				return float64(lb.X)
			}
			return nan
		}), nil
	case "get_y":
		return numX(func(m *Machine) float64 {
			if lb, ok := m.label(id(m)); ok {
				return lb.Y
			}
			return nan
		}), nil
	case "get_text":
		return cx{k: kStr, s: func(m *Machine) string {
			lb, _ := m.label(id(m))
			return lb.Text
		}}, nil
	case "set_text":
		return void(func(m *Machine) { t := text(m); m.setLabel(id(m), func(lb *Label) { lb.Text = t }) }), nil
	case "set_x":
		xv := num("x", nan)
		return void(func(m *Machine) { b := xAt(m, xv(m), xlocIndex); m.setLabel(id(m), func(lb *Label) { lb.X = b }) }), nil
	case "set_xloc":
		xv, xl := num("x", nan), num("xloc", xlocIndex)
		return void(func(m *Machine) { b := xAt(m, xv(m), xl(m)); m.setLabel(id(m), func(lb *Label) { lb.X = b }) }), nil
	case "set_y":
		yv := num("y", nan)
		return void(func(m *Machine) { y := yv(m); m.setLabel(id(m), func(lb *Label) { lb.Y = y }) }), nil
	case "set_xy":
		xv, yv := num("x", nan), num("y", nan)
		return void(func(m *Machine) {
			b, y := xAt(m, xv(m), xlocIndex), yv(m)
			m.setLabel(id(m), func(lb *Label) { lb.X, lb.Y = b, y })
		}), nil
	case "set_yloc":
		yl := num("yloc", LocAbsolute)
		return void(func(m *Machine) { v := int(yl(m)); m.setLabel(id(m), func(lb *Label) { lb.YLoc = v }) }), nil
	case "set_color":
		cv := num("color", nan)
		return void(func(m *Machine) { v := cv(m); m.setLabel(id(m), func(lb *Label) { lb.Color = v }) }), nil
	case "set_textcolor":
		cv := num("textcolor", nan)
		return void(func(m *Machine) { v := cv(m); m.setLabel(id(m), func(lb *Label) { lb.TextColor = v }) }), nil
	case "set_style":
		sv := num("style", ShapeLabelDown)
		return void(func(m *Machine) { v := int(sv(m)); m.setLabel(id(m), func(lb *Label) { lb.Style = v }) }), nil
	case "set_size":
		sv := num("size", 0)
		return void(func(m *Machine) { v := int(sv(m)); m.setLabel(id(m), func(lb *Label) { lb.Size = v }) }), nil
	}
	return cx{}, errAt(x.t, "OpenBar 不认识函数 %s", fn)
}

// labelParams are the label functions OpenBar runs and their parameters,
// as TradingView names them.
var labelParams = map[string][]string{
	"new":           {"x", "y", "text", "xloc", "yloc", "color", "style", "textcolor", "size", "textalign", "tooltip", "text_font_family", "force_overlay", "text_formatting"},
	"copy":          {"id"},
	"delete":        {"id"},
	"get_x":         {"id"},
	"get_y":         {"id"},
	"get_text":      {"id"},
	"set_text":      {"id", "text"},
	"set_x":         {"id", "x"},
	"set_xloc":      {"id", "x", "xloc"},
	"set_y":         {"id", "y"},
	"set_xy":        {"id", "x", "y"},
	"set_yloc":      {"id", "yloc"},
	"set_color":     {"id", "color"},
	"set_textcolor": {"id", "textcolor"},
	"set_style":     {"id", "style"},
	"set_size":      {"id", "size"},
}

// labelIgnored are those that change nothing OpenBar draws.
var labelIgnored = map[string]bool{"set_tooltip": true, "set_textalign": true, "set_text_font_family": true,
	"set_text_formatting": true, "set_point": true}

var labelConsts = map[string]float64{
	"label.style_none": LabelNone, "label.style_label_center": LabelCenter,
	"label.style_label_left": LabelLeft, "label.style_label_right": LabelRight,
	"label.style_label_up": ShapeLabelUp, "label.style_label_down": ShapeLabelDown,
	"label.style_label_upper_left": ShapeLabelUp, "label.style_label_upper_right": ShapeLabelUp,
	"label.style_label_lower_left": ShapeLabelDown, "label.style_label_lower_right": ShapeLabelDown,
	"label.style_xcross": ShapeXCross, "label.style_cross": ShapeCross, "label.style_circle": ShapeCircle,
	"label.style_triangleup": ShapeTriangleUp, "label.style_triangledown": ShapeTriangleDown,
	"label.style_arrowup": ShapeArrowUp, "label.style_arrowdown": ShapeArrowDown,
	"label.style_square": ShapeSquare, "label.style_diamond": ShapeDiamond, "label.style_flag": ShapeFlag,
	"yloc.price": LocAbsolute, "yloc.abovebar": LocAboveBar, "yloc.belowbar": LocBelowBar,
	"xloc.bar_index": xlocIndex, "xloc.bar_time": xlocTime,
}

func init() {
	for k, v := range labelConsts {
		builtinVars[k] = func(*compiler) cx { return konstX(v) }
	}
}
