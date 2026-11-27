package pine

import (
	"os"
	"slices"
	"strings"
	"testing"
)

const labelScript = `//@version=5
indicator("x", overlay = true, max_labels_count = 3)
var label last = na
lb = label.new(bar_index, high, str.tostring(close, format.mintick), style = label.style_label_up)
if close > 3
    label.delete(last)
last := lb
if close == 6
    lb.set_text("six")
    label.set_color(lb, color.red)
`

// Labels are kept from bar to bar till deleted, set by their functions and
// methods, the oldest going past max_labels_count; their text rounds to
// the tick with format.mintick.
func TestLabels(t *testing.T) {
	_, m := run(t, labelScript, closes(1, 2, 3, 4, 5, 6))
	got := m.Labels()
	var xs []int
	for _, lb := range got {
		xs = append(xs, lb.X)
	}
	// Bar 3's makes four, the oldest going; 3, 4 and 5 delete the one before.
	if !slices.Equal(xs, []int{1, 5}) {
		t.Fatalf("labels at bars %v", xs)
	}
	if got[1].Text != "six" || got[1].Color != float64(colors["red"]) || got[1].Y != 7 || got[1].Style != ShapeLabelUp || got[1].TextColor != float64(colors["black"]) {
		t.Fatalf("newest %+v", got[1])
	}
	if got[0].Text != "2.00" {
		t.Fatalf("oldest kept %+v", got[0])
	}
}

// The newest bar, run again as it changes, starts from the labels as they
// were before it: a tick does not leave a label of its own behind.
func TestLabelsRunAgain(t *testing.T) {
	s, err := Compile(labelScript)
	if err != nil {
		t.Fatal(err)
	}
	env := Env{IntervalMs: 3_600_000}
	bars := closes(1, 2, 3, 4, 5, 2)
	m := s.Run(nil, bars[:5], nil, 0, env)
	for _, c := range []float64{4, 5, 2} { // the newest bar ticking
		bars[5] = closes(0, 0, 0, 0, 0, c)[5]
		m = s.Run(m, bars, nil, 5, env)
	}
	whole := s.Run(nil, bars, nil, 0, env)
	if !slices.Equal(m.Labels(), whole.Labels()) {
		t.Fatalf("run again %+v\nrun whole %+v", m.Labels(), whole.Labels())
	}
}

// The Pivots High Low OpenBar ships labels each pivot with its price.
func TestPivotsLabels(t *testing.T) {
	src, err := os.ReadFile("../indicators/pivots.pine")
	if err != nil {
		t.Fatal(err)
	}
	cs := []float64{1, 2, 3, 2, 1, 2, 3, 4, 3, 2}
	s, err := Compile(string(src))
	if err != nil {
		t.Fatal(err)
	}
	m := s.Run(nil, closes(cs...), []float64{2, 2, 2, 2}, 0, Env{IntervalMs: 3_600_000, Tick: 0.1})
	if m.Err != nil {
		t.Fatal(m.Err)
	}
	got := m.Labels()
	if len(got) != 3 {
		t.Fatalf("labels %+v", got)
	}
	// The highs at bars 2 and 7, the low at 4, each pointing at it.
	want := map[int]struct {
		text  string
		style int
	}{2: {"4.0", ShapeLabelDown}, 4: {"0.0", ShapeLabelUp}, 7: {"5.0", ShapeLabelDown}}
	for _, lb := range got {
		w, ok := want[lb.X]
		if !ok || lb.Text != w.text || lb.Style != w.style {
			t.Fatalf("label %+v", lb)
		}
	}
}

const lineScript = `//@version=6
indicator("x", overlay = true, max_lines_count = 10)
var array<line> keep = array.new<line>()
l = line.new(bar_index - 1, low, bar_index, high, extend = extend.right, color = color.red, style = line.style_dashed, width = 2)
keep.push(l)
if keep.size() > 2
    old = keep.shift()
    old.delete()
plot(l.get_price(bar_index + 1), "next")
plot(array.size(line.all), "all")
`

// Lines are made, read and deleted, by functions and by methods: one taken
// from an array<line> too, whose kind shows only as the script runs.
func TestLines(t *testing.T) {
	_, m := run(t, lineScript, closes(1, 2, 3, 4, 5))
	got := m.Lines()
	if len(got) != 2 || got[0].X2 != 3 || got[1].X2 != 4 {
		t.Fatalf("lines %+v", got)
	}
	if l := got[1]; l.X1 != 3 || l.Y1 != 4 || l.Y2 != 6 || l.Extend != ExtendRight || l.Style != LineDashed || l.Width != 2 || l.Color != float64(colors["red"]) {
		t.Fatalf("newest %+v", l)
	}
	checkPlot(t, m, 0, 4, 5, 6, 7, 8) // high and two more of what it rises a bar
	checkPlot(t, m, 1, 1, 2, 2, 2, 2)
}

// A box is set by its setters, its text aligned as the script asks.
func TestBoxes(t *testing.T) {
	_, m := run(t, `//@version=6
indicator("x", overlay = true)
var box b = na
if bar_index == 1
    b := box.new(bar_index, high, bar_index + 2, low, bgcolor = color.new(color.green, 80), text = "zone", text_halign = text.align_left)
if bar_index == 3
    b.set_right(bar_index)
    box.set_text(b, "done")
plot(box.get_right(b))
`, closes(1, 2, 3, 4, 5))
	got := m.Boxes()
	if len(got) != 1 {
		t.Fatalf("boxes %+v", got)
	}
	if b := got[0]; b.Left != 1 || b.Right != 3 || b.Top != 3 || b.Bottom != 1 || b.Text != "done" || b.HAlign != AlignLeft ||
		b.VAlign != AlignCenter || b.Border != float64(colors["blue"]) || b.BorderWidth != 1 {
		t.Fatalf("box %+v", b)
	}
	checkPlot(t, m, 0, na, 3, 3, 3, 3)
}

// chart.point places labels and lines; a var keeps one from bar to bar,
// its fields read as p.price, and those nothing holds go as the bar ends.
func TestChartPoints(t *testing.T) {
	_, m := run(t, `//@version=6
indicator("x", overlay = true, max_lines_count = 1, max_labels_count = 1)
var chart.point start = na
if bar_index == 0
    start := chart.point.now(low)
p = chart.point.from_index(bar_index, high)
line.new(start, p, color = color.green)
label.new(chart.point.now(close), str.tostring(start.price), style = label.style_label_left)
plot(start.index)
`, closes(1, 2, 3, 4))
	ln, lb := m.Lines(), m.Labels()
	if len(ln) != 1 || ln[0].X1 != 0 || ln[0].Y1 != 0 || ln[0].X2 != 3 || ln[0].Y2 != 5 {
		t.Fatalf("lines %+v", ln)
	}
	if len(lb) != 1 || lb[0].X != 3 || lb[0].Y != 4 || lb[0].Text != "0" || lb[0].Style != LabelLeft {
		t.Fatalf("labels %+v", lb)
	}
	checkPlot(t, m, 0, 0, 0, 0, 0)
	if n := len(m.obj.points.byID); n != 1 {
		t.Fatalf("%d points kept", n)
	}
}

// The newest bar, run again as it changes, starts from the lines and the
// boxes as they were before it.
func TestObjectsRunAgain(t *testing.T) {
	src := lineScript + `
var box bx = na
if na(bx)
    bx := box.new(bar_index, high, bar_index, low)
bx.set_right(bar_index)
bx.set_top(math.max(bx.get_top(), high))
`
	s, err := Compile(src)
	if err != nil {
		t.Fatal(err)
	}
	env := Env{IntervalMs: 3_600_000}
	bars := closes(1, 2, 3, 4, 5, 2)
	m := s.Run(nil, bars[:5], nil, 0, env)
	for _, c := range []float64{4, 9, 2} {
		bars[5] = closes(0, 0, 0, 0, 0, c)[5]
		m = s.Run(m, bars, nil, 5, env)
	}
	whole := s.Run(nil, bars, nil, 0, env)
	if !slices.Equal(m.Lines(), whole.Lines()) || !slices.Equal(m.Boxes(), whole.Boxes()) {
		t.Fatalf("run again %+v %+v\nrun whole %+v %+v", m.Lines(), m.Boxes(), whole.Lines(), whole.Boxes())
	}
	if b := whole.Boxes(); len(b) != 1 || b[0].Top != 6 || b[0].Right != 5 {
		t.Fatalf("box %+v", b)
	}
}

// A method called on what a call gives, as keep.shift().delete(), runs.
func TestChainedMethods(t *testing.T) {
	_, m := run(t, strings.Replace(lineScript, "    old = keep.shift()\n    old.delete()\n", "    keep.shift().delete()\n", 1), closes(1, 2, 3, 4, 5))
	if got := m.Lines(); len(got) != 2 || got[0].X2 != 3 {
		t.Fatalf("lines %+v", got)
	}
	_, m = run(t, `//@version=6
indicator("x")
p = chart.point.now(high).copy()
plot(p.index)
plot(p.price)
`, closes(1, 2))
	checkPlot(t, m, 0, 0, 1)
	checkPlot(t, m, 1, 2, 3)
}
