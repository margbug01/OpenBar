package pine

import (
	"os"
	"slices"
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
