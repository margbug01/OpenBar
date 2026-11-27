package pine

import (
	"maps"
	"math"
	"slices"
	"sort"
)

// The objects a script makes: labels, lines and boxes, which it draws, and
// chart points, which place them. A variable holding one holds its id, na
// once deleted. The ids of all of them come from one count, so an id tells
// what it is: l.delete() on a variable whose kind OpenBar did not follow,
// as one taken from an array<line>, finds its object all the same.
//
// A script keeps max_labels_count labels, max_lines_count lines and
// max_boxes_count boxes, 50 unless it says (500 at most), the oldest going
// as more come. Points live while a var, or an array, holds them. Like
// arrays, all are put back as the newest bar began when it runs again.

// The kinds of objects, as Pine names their types.
const (
	objLabel = "label"
	objLine  = "line"
	objBox   = "box"
	objPoint = "chart.point"
)

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
	Size             int     // as plotshape's: 0 auto … 5 huge, past that points
	time             bool    // placed by time: its getters give times
}

// Line styles past LineSolid, LineDashed and LineDotted: solid with an
// arrow at the first point, the second, or both.
const (
	LineArrowLeft = 10 + iota
	LineArrowRight
	LineArrowBoth
)

// How a line or a box runs on past its points.
const (
	ExtendNone = iota
	ExtendLeft
	ExtendRight
	ExtendBoth
)

// Segment is a line as it stands: Line is taken, for the line of a place in the source.
type Segment struct {
	X1, X2 int // the bars
	Y1, Y2 float64
	Extend int
	Color  float64
	Style  int
	Width  float64
	time   bool
}

// Where a box's text goes in it.
const (
	AlignLeft = iota
	AlignCenter
	AlignRight
	AlignTop
	AlignBottom
)

// Box is one as it stands.
type Box struct {
	Left, Right    int // the bars
	Top, Bottom    float64
	Border, Bg     float64 // packed colors, NaN for none
	BorderWidth    float64
	BorderStyle    int
	Extend         int
	Text           string
	TextColor      float64
	TextSize       int
	HAlign, VAlign int
	time           bool
}

// point is a chart.point: a bar, by its index or its time, and a price.
type point struct{ index, time, price float64 }

// kept are the objects of a kind by id, and the ids by age.
type kept[T any] struct {
	byID  map[int]T
	order []int
}

func (k kept[T]) clone() kept[T] {
	return kept[T]{byID: maps.Clone(k.byID), order: slices.Clone(k.order)}
}

// add keeps v as id, the oldest going past keep; keep 0 keeps no order,
// for points, which go as nothing holds them.
func (k *kept[T]) add(id int, v T, keep int) {
	if k.byID == nil {
		k.byID = map[int]T{}
	}
	k.byID[id] = v
	if keep == 0 {
		return
	}
	k.order = append(k.order, id)
	if len(k.order) > keep {
		for _, old := range k.order[:len(k.order)-keep] {
			delete(k.byID, old)
		}
		k.order = slices.Clone(k.order[len(k.order)-keep:])
	}
}

// get is the object id holds; ok is false for na or one deleted.
func (k kept[T]) get(id float64) (T, bool) {
	var zero T
	if math.IsNaN(id) {
		return zero, false
	}
	v, ok := k.byID[int(id)]
	return v, ok
}

func (k *kept[T]) set(id float64, f func(*T)) {
	if v, ok := k.get(id); ok {
		f(&v)
		k.byID[int(id)] = v
	}
}

func (k *kept[T]) del(id float64) {
	if _, ok := k.get(id); ok {
		i := int(id)
		delete(k.byID, i)
		k.order = slices.DeleteFunc(slices.Clone(k.order), func(o int) bool { return o == i })
	}
}

func (k kept[T]) list() []T {
	out := make([]T, 0, len(k.order))
	for _, id := range k.order {
		out = append(out, k.byID[id])
	}
	return out
}

type objects struct {
	next   int
	labels kept[Label]
	lines  kept[Segment]
	boxes  kept[Box]
	points kept[point]
}

func (o objects) clone() objects {
	return objects{next: o.next, labels: o.labels.clone(), lines: o.lines.clone(), boxes: o.boxes.clone(), points: o.points.clone()}
}

func (m *Machine) newID() int {
	m.obj.next++
	return m.obj.next
}

// kindOf is the kind of the object id holds, "" for none.
func (m *Machine) kindOf(id float64) string {
	if math.IsNaN(id) {
		return ""
	}
	i := int(id)
	switch {
	case has(m.obj.labels, i):
		return objLabel
	case has(m.obj.lines, i):
		return objLine
	case has(m.obj.boxes, i):
		return objBox
	case has(m.obj.points, i):
		return objPoint
	}
	return ""
}

func has[T any](k kept[T], id int) bool {
	_, ok := k.byID[id]
	return ok
}

// Labels, Lines and Boxes are those the script left, the oldest first.
func (m *Machine) Labels() []Label  { return m.obj.labels.list() }
func (m *Machine) Lines() []Segment { return m.obj.lines.list() }
func (m *Machine) Boxes() []Box     { return m.obj.boxes.list() }

// sweepPoints drops, as a bar ends, the points no var and no array holds.
func (m *Machine) sweepPoints(slots []int) {
	if len(m.obj.points.byID) == 0 {
		return
	}
	keep := map[int]bool{}
	for _, s := range slots {
		if v := m.buf[s][m.i]; !math.IsNaN(v) {
			keep[int(v)] = true
		}
	}
	for _, a := range m.arrs.byID {
		for _, v := range a {
			if !math.IsNaN(v) {
				keep[int(v)] = true
			}
		}
	}
	for id := range m.obj.points.byID {
		if !keep[id] {
			delete(m.obj.points.byID, id)
		}
	}
}

const (
	defaultObjects = 50
	maxObjects     = 500
)

// barOfTime is the bar opening at t, or the one it falls in; past the
// newest, counted on in the chart's interval.
func (m *Machine) barOfTime(t float64) int {
	n := len(m.bars)
	if n == 0 || math.IsNaN(t) {
		return noBar
	}
	if last := m.bars[n-1].T; int64(t) > last && m.env.IntervalMs > 0 {
		return n - 1 + int((int64(t)-last)/m.env.IntervalMs)
	}
	i := sort.Search(n, func(i int) bool { return m.bars[i].T > int64(t) })
	return max(i-1, 0)
}

// timeOfBar is when bar i opens; past the newest, counted on.
func (m *Machine) timeOfBar(i int) float64 {
	n := len(m.bars)
	if i == noBar || n == 0 {
		return nan
	}
	if i < 0 {
		return float64(m.bars[0].T + int64(i)*m.env.IntervalMs)
	}
	if i < n {
		return float64(m.bars[i].T)
	}
	return float64(m.bars[n-1].T + int64(i-n+1)*m.env.IntervalMs)
}

// noBar is the bar of an x that is na.
const noBar = math.MinInt32

const (
	xlocIndex = 0
	xlocTime  = 1
)

// barAt is the bar x names, as a bar index or a time; noBar for na.
func (m *Machine) barAt(x, xloc float64) int {
	if xloc == xlocTime {
		return m.barOfTime(x)
	}
	if math.IsNaN(x) {
		return noBar
	}
	return int(math.Round(x))
}

// xOf is what a getter gives of bar i: its index, or its time.
func (m *Machine) xOf(i int, byTime bool) float64 {
	if byTime {
		return m.timeOfBar(i)
	}
	return float64(i)
}

// pointAt is the bar and the price the point id holds, by xloc; ok is
// false for na, or a point with no bar.
func (m *Machine) pointAt(id, xloc float64) (int, float64, bool) {
	p, ok := m.obj.points.get(id)
	if !ok {
		return 0, nan, false
	}
	bar := noBar
	if xloc == xlocTime && !math.IsNaN(p.time) || math.IsNaN(p.index) {
		bar = m.barOfTime(p.time)
	} else {
		bar = int(math.Round(p.index))
	}
	return bar, p.price, bar != noBar
}

func (m *Machine) addPoint(p point) float64 {
	id := m.newID()
	m.obj.points.add(id, p, 0)
	return float64(id)
}

// An argument as written: its name, "" by place, and its value.
type argv struct {
	name string
	v    cx
}

// rawArgs compiles a call's arguments, to be lined up with the
// parameters once it is known which of a function's forms they are.
func (c *compiler) rawArgs(x *callX, sc *scope) ([]argv, error) {
	out := make([]argv, len(x.args))
	named := false
	for i, a := range x.args {
		if a.name != "" {
			named = true
		} else if named {
			return nil, errAt(a.t, "带名字的参数后面不能再跟不带名字的")
		}
		v, err := c.expr(a.x, sc)
		if err != nil {
			return nil, err
		}
		v.tok = a.x.at()
		out[i] = argv{a.name, v}
	}
	return out, nil
}

// bindArgs lines raw up with params, by place, then by name.
func bindArgs(fn string, t token, raw []argv, params []string) ([]cx, error) {
	out := make([]cx, len(params))
	for i := range out {
		out[i] = cx{k: kMissing}
	}
	for i, a := range raw {
		at := i
		if a.name != "" {
			at = slices.Index(params, a.name)
			if at < 0 {
				if ignoredArgs[a.name] {
					continue
				}
				return nil, errAt(a.v.tok, "%s 没有叫 %s 的参数", fn, a.name)
			}
		}
		if at >= len(params) {
			return nil, errAt(a.v.tok, "%s 最多 %d 个参数", fn, len(params))
		}
		out[at] = a.v
	}
	return out, nil
}

// objArgs are a call's arguments lined up with its parameters.
type objArgs struct {
	params []string
	a      []cx
}

func (o objArgs) get(p string) cx {
	if i := slices.Index(o.params, p); i >= 0 {
		return o.a[i]
	}
	return cx{k: kMissing}
}

func (o objArgs) num(p string, def float64) func(m *Machine) float64 {
	v := o.get(p)
	if !given(v) || v.f == nil {
		return func(*Machine) float64 { return def }
	}
	return v.f
}

func (o objArgs) text(p string) func(m *Machine) string {
	if v := o.get(p); given(v) && v.k == kStr {
		return v.s
	}
	return func(*Machine) string { return "" }
}

// textParams are those that take text; the rest take numbers, but those
// whose text OpenBar has no use for.
var textParams = map[string]bool{"text": true, "tooltip": true}
var anyParams = map[string]bool{"text_font_family": true, "text_formatting": true, "force_overlay": true, "text_wrap": true}

// fits reports whether the arguments are of the kinds the parameters take.
func (o objArgs) fits() bool {
	for i, p := range o.params {
		v := o.a[i]
		if !given(v) || anyParams[p] || isNaX(v) {
			continue
		}
		if textParams[p] != (v.k == kStr) || v.k != kStr && v.k != kFloat {
			return false
		}
	}
	return true
}

// An objMethod is a function of a kind: its parameters, those of its form
// with points when it has one, and what it compiles to.
type objMethod struct {
	params, byPoint []string
	build           func(c *compiler, o objArgs, pts bool) cx
}

var objMethods = map[string]map[string]objMethod{}

// objIgnored are the functions that change nothing OpenBar draws.
var objIgnored = map[string]bool{
	"label.set_tooltip": true, "label.set_textalign": true, "label.set_text_font_family": true, "label.set_text_formatting": true,
	"box.set_text_wrap": true, "box.set_text_font_family": true, "box.set_text_formatting": true,
}

// objCall compiles kind.name with the arguments raw, or the method name of
// recv: of the kind given, or, "" for one OpenBar did not follow, of
// whichever kind the object turns out to be.
func (c *compiler) objCall(kind, name string, recv *cx, raw []argv, t token) (cx, error) {
	kinds := []string{kind}
	if kind == "" {
		kinds = nil
		for _, k := range []string{objLabel, objLine, objBox} {
			if _, ok := objMethods[k][name]; ok {
				kinds = append(kinds, k)
			}
		}
		if len(kinds) == 0 {
			return naX, nil
		}
	}
	var built []cx
	for _, k := range kinds {
		fn := k + "." + name
		meth, ok := objMethods[k][name]
		if !ok {
			if !objIgnored[fn] {
				c.warn(t, "OpenBar 还不支持 %s，会被跳过", fn)
			}
			return naX, nil
		}
		c.s.objects = true
		params, pts := meth.params, false
		if meth.byPoint != nil && len(raw) > 0 {
			first := raw[0]
			if first.name == meth.byPoint[0] || first.name == "" && first.v.obj == objPoint {
				params, pts = meth.byPoint, true
			}
		}
		if recv != nil {
			params = params[1:]
		}
		a, err := bindArgs(fn, t, raw, params)
		if err != nil {
			return cx{}, err
		}
		if recv != nil {
			a, params = append([]cx{*recv}, a...), append([]string{"id"}, params...)
		}
		o := objArgs{params, a}
		if !o.fits() {
			c.warn(t, "OpenBar 画不了这个 %s（参数的写法还不支持），会被跳过", fn)
			return naX, nil
		}
		built = append(built, meth.build(c, o, pts))
	}
	if len(built) == 1 {
		return built[0], nil
	}
	// Of an object whose kind shows only as it runs: each kind's, by it.
	for _, b := range built[1:] {
		if b.k != built[0].k {
			return built[0], nil
		}
	}
	id := recv.f
	pick := func(m *Machine) cx {
		k := m.kindOf(id(m))
		for i, kd := range kinds {
			if kd == k {
				return built[i]
			}
		}
		return cx{}
	}
	switch built[0].k {
	case kStr:
		return cx{k: kStr, s: func(m *Machine) string {
			if b := pick(m); b.s != nil {
				return b.s(m)
			}
			return ""
		}}, nil
	case kVoid:
		return cx{k: kVoid, f: func(m *Machine) float64 {
			if b := pick(m); b.f != nil {
				b.f(m)
			}
			return nan
		}}, nil
	}
	out := numX(func(m *Machine) float64 {
		if b := pick(m); b.f != nil {
			return b.f(m)
		}
		return nan
	})
	out.obj = built[0].obj
	return out, nil
}

// objX is a value holding an object of kind.
func objX(kind string, f func(m *Machine) float64) cx {
	out := numX(f)
	out.obj = kind
	return out
}

func voidX(f func(m *Machine)) cx {
	return cx{k: kVoid, f: func(m *Machine) float64 { f(m); return nan }}
}

// The functions, by kind. Each setter is the object's field set to an
// argument: setters builds them from a table.
type setter[T any] struct {
	params []string
	set    func(m *Machine, o *T, v []float64, s string)
}

func setters[T any](kind string, k func(m *Machine) *kept[T], table map[string]setter[T]) {
	for name, st := range table {
		objMethods[kind][name] = objMethod{params: append([]string{"id"}, st.params...), build: func(c *compiler, o objArgs, _ bool) cx {
			id := o.num("id", nan)
			fs := make([]func(*Machine) float64, len(st.params))
			for i, p := range st.params {
				fs[i] = o.num(p, nan)
			}
			text := o.text("text")
			return voidX(func(m *Machine) {
				v := make([]float64, len(fs))
				for i, f := range fs {
					v[i] = f(m)
				}
				s := text(m)
				k(m).set(id(m), func(ob *T) { st.set(m, ob, v, s) })
			})
		}}
	}
}

// common gives kind copy, delete and getters.
func common[T any](kind string, k func(m *Machine) *kept[T], keep func(m *Machine) int, getters map[string]func(m *Machine, o T) float64) {
	objMethods[kind]["copy"] = objMethod{params: []string{"id"}, build: func(c *compiler, o objArgs, _ bool) cx {
		id := o.num("id", nan)
		return objX(kind, func(m *Machine) float64 {
			v, ok := k(m).get(id(m))
			if !ok {
				return nan
			}
			n := m.newID()
			k(m).add(n, v, keep(m))
			return float64(n)
		})
	}}
	objMethods[kind]["delete"] = objMethod{params: []string{"id"}, build: func(c *compiler, o objArgs, _ bool) cx {
		id := o.num("id", nan)
		return voidX(func(m *Machine) { k(m).del(id(m)) })
	}}
	for name, g := range getters {
		objMethods[kind][name] = objMethod{params: []string{"id"}, build: func(c *compiler, o objArgs, _ bool) cx {
			id := o.num("id", nan)
			return numX(func(m *Machine) float64 {
				if v, ok := k(m).get(id(m)); ok {
					return g(m, v)
				}
				return nan
			})
		}}
	}
}

// at is what new and the setters of a point take: the bar and the price
// of the point argument p, or of the bar x and the price y.
func at(o objArgs, pts bool, p, x, y string) func(m *Machine, xloc float64) (int, float64) {
	if pts {
		pf := o.num(p, nan)
		return func(m *Machine, xloc float64) (int, float64) {
			bar, price, ok := m.pointAt(pf(m), xloc)
			if !ok {
				return noBar, nan
			}
			return bar, price
		}
	}
	xf, yf := o.num(x, nan), o.num(y, nan)
	return func(m *Machine, xloc float64) (int, float64) { return m.barAt(xf(m), xloc), yf(m) }
}

func pointSetter(p string, set func(m *Machine, id float64, bar int, price float64)) objMethod {
	return objMethod{params: []string{"id", p}, build: func(c *compiler, o objArgs, _ bool) cx {
		id, pt := o.num("id", nan), at(o, true, p, "", "")
		return voidX(func(m *Machine) {
			if bar, price := pt(m, xlocIndex); bar != noBar {
				set(m, id(m), bar, price)
			}
		})
	}}
}

func init() {
	for _, k := range []string{objLabel, objLine, objBox} {
		objMethods[k] = map[string]objMethod{}
	}
	labels := func(m *Machine) *kept[Label] { return &m.obj.labels }
	lines := func(m *Machine) *kept[Segment] { return &m.obj.lines }
	boxes := func(m *Machine) *kept[Box] { return &m.obj.boxes }

	// Labels.
	objMethods[objLabel]["new"] = objMethod{
		params:  []string{"x", "y", "text", "xloc", "yloc", "color", "style", "textcolor", "size", "textalign", "tooltip", "text_font_family", "force_overlay", "text_formatting"},
		byPoint: []string{"point", "text", "xloc", "yloc", "color", "style", "textcolor", "size", "textalign", "tooltip", "text_font_family", "force_overlay", "text_formatting"},
		build: func(c *compiler, o objArgs, pts bool) cx {
			textDef := colors["white"] // version 6's; black before
			if c.version < 6 {
				textDef = colors["black"]
			}
			pt, xl, yl := at(o, pts, "point", "x", "y"), o.num("xloc", xlocIndex), o.num("yloc", LocAbsolute)
			col, tcol, st, sz, text := o.num("color", float64(colors["blue"])), o.num("textcolor", float64(textDef)), o.num("style", ShapeLabelDown), o.num("size", 0), o.text("text")
			return objX(objLabel, func(m *Machine) float64 {
				xloc := xl(m)
				bar, y := pt(m, xloc)
				yloc := int(yl(m))
				if bar == noBar || yloc == LocAbsolute && math.IsNaN(y) {
					return nan
				}
				id := m.newID()
				m.obj.labels.add(id, Label{X: bar, Y: y, YLoc: yloc, Text: text(m), Style: int(st(m)), Color: col(m),
					TextColor: tcol(m), Size: int(sz(m)), time: xloc == xlocTime}, m.s.MaxLabels)
				return float64(id)
			})
		}}
	common(objLabel, labels, func(m *Machine) int { return m.s.MaxLabels }, map[string]func(*Machine, Label) float64{
		"get_x": func(m *Machine, l Label) float64 { return m.xOf(l.X, l.time) },
		"get_y": func(_ *Machine, l Label) float64 { return l.Y },
	})
	objMethods[objLabel]["get_text"] = objMethod{params: []string{"id"}, build: func(c *compiler, o objArgs, _ bool) cx {
		id := o.num("id", nan)
		return cx{k: kStr, s: func(m *Machine) string {
			lb, _ := m.obj.labels.get(id(m))
			return lb.Text
		}}
	}}
	setters(objLabel, labels, map[string]setter[Label]{
		"set_text":      {[]string{"text"}, func(_ *Machine, l *Label, _ []float64, s string) { l.Text = s }},
		"set_x":         {[]string{"x"}, func(m *Machine, l *Label, v []float64, _ string) { l.X = m.barAt(v[0], xlocOf(l.time)) }},
		"set_y":         {[]string{"y"}, func(_ *Machine, l *Label, v []float64, _ string) { l.Y = v[0] }},
		"set_xy":        {[]string{"x", "y"}, func(m *Machine, l *Label, v []float64, _ string) { l.X, l.Y = m.barAt(v[0], xlocOf(l.time)), v[1] }},
		"set_xloc":      {[]string{"x", "xloc"}, func(m *Machine, l *Label, v []float64, _ string) { l.X, l.time = m.barAt(v[0], v[1]), v[1] == xlocTime }},
		"set_yloc":      {[]string{"yloc"}, func(_ *Machine, l *Label, v []float64, _ string) { l.YLoc = int(v[0]) }},
		"set_color":     {[]string{"color"}, func(_ *Machine, l *Label, v []float64, _ string) { l.Color = v[0] }},
		"set_textcolor": {[]string{"textcolor"}, func(_ *Machine, l *Label, v []float64, _ string) { l.TextColor = v[0] }},
		"set_style":     {[]string{"style"}, func(_ *Machine, l *Label, v []float64, _ string) { l.Style = int(v[0]) }},
		"set_size":      {[]string{"size"}, func(_ *Machine, l *Label, v []float64, _ string) { l.Size = int(v[0]) }},
	})
	objMethods[objLabel]["set_point"] = pointSetter("point", func(m *Machine, id float64, bar int, price float64) {
		m.obj.labels.set(id, func(l *Label) { l.X, l.Y = bar, price })
	})

	// Lines.
	objMethods[objLine]["new"] = objMethod{
		params:  []string{"x1", "y1", "x2", "y2", "xloc", "extend", "color", "style", "width", "force_overlay"},
		byPoint: []string{"first_point", "second_point", "xloc", "extend", "color", "style", "width", "force_overlay"},
		build: func(c *compiler, o objArgs, pts bool) cx {
			p1, p2 := at(o, pts, "first_point", "x1", "y1"), at(o, pts, "second_point", "x2", "y2")
			xl, ext, col, st, w := o.num("xloc", xlocIndex), o.num("extend", ExtendNone), o.num("color", float64(colors["blue"])), o.num("style", LineSolid), o.num("width", 1)
			return objX(objLine, func(m *Machine) float64 {
				xloc := xl(m)
				x1, y1 := p1(m, xloc)
				x2, y2 := p2(m, xloc)
				if x1 == noBar || x2 == noBar || math.IsNaN(y1) || math.IsNaN(y2) {
					return nan
				}
				id := m.newID()
				m.obj.lines.add(id, Segment{X1: x1, Y1: y1, X2: x2, Y2: y2, Extend: int(ext(m)), Color: col(m), Style: int(st(m)),
					Width: w(m), time: xloc == xlocTime}, m.s.MaxLines)
				return float64(id)
			})
		}}
	common(objLine, lines, func(m *Machine) int { return m.s.MaxLines }, map[string]func(*Machine, Segment) float64{
		"get_x1": func(m *Machine, l Segment) float64 { return m.xOf(l.X1, l.time) },
		"get_x2": func(m *Machine, l Segment) float64 { return m.xOf(l.X2, l.time) },
		"get_y1": func(_ *Machine, l Segment) float64 { return l.Y1 },
		"get_y2": func(_ *Machine, l Segment) float64 { return l.Y2 },
	})
	objMethods[objLine]["get_price"] = objMethod{params: []string{"id", "x"}, build: func(c *compiler, o objArgs, _ bool) cx {
		id, xf := o.num("id", nan), o.num("x", nan)
		return numX(func(m *Machine) float64 {
			l, ok := m.obj.lines.get(id(m))
			if !ok || l.X1 == l.X2 {
				return nan
			}
			return l.Y1 + (l.Y2-l.Y1)*(xf(m)-float64(l.X1))/float64(l.X2-l.X1)
		})
	}}
	setters(objLine, lines, map[string]setter[Segment]{
		"set_x1":  {[]string{"x"}, func(m *Machine, l *Segment, v []float64, _ string) { l.X1 = m.barAt(v[0], xlocOf(l.time)) }},
		"set_x2":  {[]string{"x"}, func(m *Machine, l *Segment, v []float64, _ string) { l.X2 = m.barAt(v[0], xlocOf(l.time)) }},
		"set_y1":  {[]string{"y"}, func(_ *Machine, l *Segment, v []float64, _ string) { l.Y1 = v[0] }},
		"set_y2":  {[]string{"y"}, func(_ *Machine, l *Segment, v []float64, _ string) { l.Y2 = v[0] }},
		"set_xy1": {[]string{"x", "y"}, func(m *Machine, l *Segment, v []float64, _ string) { l.X1, l.Y1 = m.barAt(v[0], xlocOf(l.time)), v[1] }},
		"set_xy2": {[]string{"x", "y"}, func(m *Machine, l *Segment, v []float64, _ string) { l.X2, l.Y2 = m.barAt(v[0], xlocOf(l.time)), v[1] }},
		"set_xloc": {[]string{"x1", "x2", "xloc"}, func(m *Machine, l *Segment, v []float64, _ string) {
			l.X1, l.X2, l.time = m.barAt(v[0], v[2]), m.barAt(v[1], v[2]), v[2] == xlocTime
		}},
		"set_extend": {[]string{"extend"}, func(_ *Machine, l *Segment, v []float64, _ string) { l.Extend = int(v[0]) }},
		"set_color":  {[]string{"color"}, func(_ *Machine, l *Segment, v []float64, _ string) { l.Color = v[0] }},
		"set_style":  {[]string{"style"}, func(_ *Machine, l *Segment, v []float64, _ string) { l.Style = int(v[0]) }},
		"set_width":  {[]string{"width"}, func(_ *Machine, l *Segment, v []float64, _ string) { l.Width = v[0] }},
	})
	objMethods[objLine]["set_first_point"] = pointSetter("point", func(m *Machine, id float64, bar int, price float64) {
		m.obj.lines.set(id, func(l *Segment) { l.X1, l.Y1 = bar, price })
	})
	objMethods[objLine]["set_second_point"] = pointSetter("point", func(m *Machine, id float64, bar int, price float64) {
		m.obj.lines.set(id, func(l *Segment) { l.X2, l.Y2 = bar, price })
	})

	// Boxes.
	rest := []string{"border_color", "border_width", "border_style", "extend", "xloc", "bgcolor", "text", "text_size", "text_color",
		"text_halign", "text_valign", "text_wrap", "text_font_family", "force_overlay", "text_formatting"}
	objMethods[objBox]["new"] = objMethod{
		params:  append([]string{"left", "top", "right", "bottom"}, rest...),
		byPoint: append([]string{"top_left", "bottom_right"}, rest...),
		build: func(c *compiler, o objArgs, pts bool) cx {
			lt, rb := at(o, pts, "top_left", "left", "top"), at(o, pts, "bottom_right", "right", "bottom")
			xl, ext := o.num("xloc", xlocIndex), o.num("extend", ExtendNone)
			border, bw, bs := o.num("border_color", float64(colors["blue"])), o.num("border_width", 1), o.num("border_style", LineSolid)
			bg, text, tc, ts := o.num("bgcolor", float64(colors["blue"])), o.text("text"), o.num("text_color", float64(colors["black"])), o.num("text_size", 0)
			ha, va := o.num("text_halign", AlignCenter), o.num("text_valign", AlignCenter)
			return objX(objBox, func(m *Machine) float64 {
				xloc := xl(m)
				l, t := lt(m, xloc)
				r, b := rb(m, xloc)
				if l == noBar || r == noBar || math.IsNaN(t) || math.IsNaN(b) {
					return nan
				}
				id := m.newID()
				m.obj.boxes.add(id, Box{Left: l, Top: t, Right: r, Bottom: b, Border: border(m), BorderWidth: bw(m), BorderStyle: int(bs(m)),
					Extend: int(ext(m)), Bg: bg(m), Text: text(m), TextColor: tc(m), TextSize: int(ts(m)),
					HAlign: int(ha(m)), VAlign: int(va(m)), time: xloc == xlocTime}, m.s.MaxBoxes)
				return float64(id)
			})
		}}
	common(objBox, boxes, func(m *Machine) int { return m.s.MaxBoxes }, map[string]func(*Machine, Box) float64{
		"get_left":   func(m *Machine, b Box) float64 { return m.xOf(b.Left, b.time) },
		"get_right":  func(m *Machine, b Box) float64 { return m.xOf(b.Right, b.time) },
		"get_top":    func(_ *Machine, b Box) float64 { return b.Top },
		"get_bottom": func(_ *Machine, b Box) float64 { return b.Bottom },
	})
	setters(objBox, boxes, map[string]setter[Box]{
		"set_left":    {[]string{"left"}, func(m *Machine, b *Box, v []float64, _ string) { b.Left = m.barAt(v[0], xlocOf(b.time)) }},
		"set_right":   {[]string{"right"}, func(m *Machine, b *Box, v []float64, _ string) { b.Right = m.barAt(v[0], xlocOf(b.time)) }},
		"set_top":     {[]string{"top"}, func(_ *Machine, b *Box, v []float64, _ string) { b.Top = v[0] }},
		"set_bottom":  {[]string{"bottom"}, func(_ *Machine, b *Box, v []float64, _ string) { b.Bottom = v[0] }},
		"set_lefttop": {[]string{"left", "top"}, func(m *Machine, b *Box, v []float64, _ string) { b.Left, b.Top = m.barAt(v[0], xlocOf(b.time)), v[1] }},
		"set_rightbottom": {[]string{"right", "bottom"}, func(m *Machine, b *Box, v []float64, _ string) {
			b.Right, b.Bottom = m.barAt(v[0], xlocOf(b.time)), v[1]
		}},
		"set_xloc": {[]string{"left", "right", "xloc"}, func(m *Machine, b *Box, v []float64, _ string) {
			b.Left, b.Right, b.time = m.barAt(v[0], v[2]), m.barAt(v[1], v[2]), v[2] == xlocTime
		}},
		"set_extend":       {[]string{"extend"}, func(_ *Machine, b *Box, v []float64, _ string) { b.Extend = int(v[0]) }},
		"set_border_color": {[]string{"color"}, func(_ *Machine, b *Box, v []float64, _ string) { b.Border = v[0] }},
		"set_border_width": {[]string{"width"}, func(_ *Machine, b *Box, v []float64, _ string) { b.BorderWidth = v[0] }},
		"set_border_style": {[]string{"style"}, func(_ *Machine, b *Box, v []float64, _ string) { b.BorderStyle = int(v[0]) }},
		"set_bgcolor":      {[]string{"color"}, func(_ *Machine, b *Box, v []float64, _ string) { b.Bg = v[0] }},
		"set_text":         {[]string{"text"}, func(_ *Machine, b *Box, _ []float64, s string) { b.Text = s }},
		"set_text_color":   {[]string{"text_color"}, func(_ *Machine, b *Box, v []float64, _ string) { b.TextColor = v[0] }},
		"set_text_size":    {[]string{"text_size"}, func(_ *Machine, b *Box, v []float64, _ string) { b.TextSize = int(v[0]) }},
		"set_text_halign":  {[]string{"text_halign"}, func(_ *Machine, b *Box, v []float64, _ string) { b.HAlign = int(v[0]) }},
		"set_text_valign":  {[]string{"text_valign"}, func(_ *Machine, b *Box, v []float64, _ string) { b.VAlign = int(v[0]) }},
	})
	objMethods[objBox]["get_text"] = objMethod{params: []string{"id"}, build: func(c *compiler, o objArgs, _ bool) cx {
		id := o.num("id", nan)
		return cx{k: kStr, s: func(m *Machine) string {
			b, _ := m.obj.boxes.get(id(m))
			return b.Text
		}}
	}}
	objMethods[objBox]["set_top_left_point"] = pointSetter("point", func(m *Machine, id float64, bar int, price float64) {
		m.obj.boxes.set(id, func(b *Box) { b.Left, b.Top = bar, price })
	})
	objMethods[objBox]["set_bottom_right_point"] = pointSetter("point", func(m *Machine, id float64, bar int, price float64) {
		m.obj.boxes.set(id, func(b *Box) { b.Right, b.Bottom = bar, price })
	})

	for k, v := range objConsts {
		builtinVars[k] = func(*compiler) cx { return konstX(v) }
	}
	// label.all and the others: an array of the ids, the oldest first.
	for name, k := range map[string]func(m *Machine) []int{
		"label.all": func(m *Machine) []int { return m.obj.labels.order },
		"line.all":  func(m *Machine) []int { return m.obj.lines.order },
		"box.all":   func(m *Machine) []int { return m.obj.boxes.order },
	} {
		builtinVars[name] = func(c *compiler) cx {
			c.s.arrays = true
			out := numX(func(m *Machine) float64 {
				ids := k(m)
				v := make([]float64, len(ids))
				for i, id := range ids {
					v[i] = float64(id)
				}
				return m.newArray(v)
			})
			out.arr = true
			return out
		}
	}
}

func xlocOf(byTime bool) float64 {
	if byTime {
		return xlocTime
	}
	return xlocIndex
}

// pointCall compiles chart.point.name.
func (c *compiler) pointCall(name string, raw []argv, t token) (cx, error) {
	params := map[string][]string{"new": {"time", "index", "price"}, "now": {"price"}, "from_index": {"index", "price"},
		"from_time": {"time", "price"}, "copy": {"id"}}[name]
	if params == nil {
		c.warn(t, "OpenBar 还不支持 %s，会被跳过", "chart.point."+name)
		return naX, nil
	}
	a, err := bindArgs("chart.point."+name, t, raw, params)
	if err != nil {
		return cx{}, err
	}
	o := objArgs{params, a}
	if !o.fits() {
		return cx{}, errAt(t, "chart.point.%s 的参数要是数值", name)
	}
	c.s.objects, c.s.points = true, true
	tf, xf, pf, id := o.num("time", nan), o.num("index", nan), o.num("price", nan), o.num("id", nan)
	return objX(objPoint, func(m *Machine) float64 {
		switch name {
		case "now":
			return m.addPoint(point{index: float64(m.i), time: float64(m.bars[m.i].T), price: pf(m)})
		case "copy":
			if p, ok := m.obj.points.get(id(m)); ok {
				return m.addPoint(p)
			}
			return nan
		}
		return m.addPoint(point{index: xf(m), time: tf(m), price: pf(m)})
	}), nil
}

// pointFields are what a chart.point holds, read as p.price.
var pointFields = map[string]func(point) float64{
	"index": func(p point) float64 { return p.index },
	"time":  func(p point) float64 { return p.time },
	"price": func(p point) float64 { return p.price },
}

var objConsts = map[string]float64{
	"label.style_none": LabelNone, "label.style_label_center": LabelCenter,
	"label.style_label_left": LabelLeft, "label.style_label_right": LabelRight,
	"label.style_label_up": ShapeLabelUp, "label.style_label_down": ShapeLabelDown,
	"label.style_label_upper_left": ShapeLabelUp, "label.style_label_upper_right": ShapeLabelUp,
	"label.style_label_lower_left": ShapeLabelDown, "label.style_label_lower_right": ShapeLabelDown,
	"label.style_xcross": ShapeXCross, "label.style_cross": ShapeCross, "label.style_circle": ShapeCircle,
	"label.style_triangleup": ShapeTriangleUp, "label.style_triangledown": ShapeTriangleDown,
	"label.style_arrowup": ShapeArrowUp, "label.style_arrowdown": ShapeArrowDown,
	"label.style_square": ShapeSquare, "label.style_diamond": ShapeDiamond, "label.style_flag": ShapeFlag,
	"line.style_solid": LineSolid, "line.style_dashed": LineDashed, "line.style_dotted": LineDotted,
	"line.style_arrow_left": LineArrowLeft, "line.style_arrow_right": LineArrowRight, "line.style_arrow_both": LineArrowBoth,
	"extend.none": ExtendNone, "extend.left": ExtendLeft, "extend.right": ExtendRight, "extend.both": ExtendBoth,
	"text.align_left": AlignLeft, "text.align_center": AlignCenter, "text.align_right": AlignRight,
	"text.align_top": AlignTop, "text.align_bottom": AlignBottom,
	"yloc.price": LocAbsolute, "yloc.abovebar": LocAboveBar, "yloc.belowbar": LocBelowBar,
	"xloc.bar_index": xlocIndex, "xloc.bar_time": xlocTime,
}
