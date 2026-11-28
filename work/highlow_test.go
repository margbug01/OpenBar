package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// The highest and lowest bars in sight are labeled beside their wicks: on
// the right of a bar, unless the label would run off the chart, then on
// its left, whole either way.
func TestHighLowMarksStayOnTheChart(t *testing.T) {
	for _, c := range []struct {
		name string
		dark bool
		high func(from, to int) int // the bar made the highest
	}{
		{"highlow-left-light", false, func(from, to int) int { return from + 1 }},
		{"highlow-right-dark", true, func(from, to int) int { return to }},
	} {
		a := testApp()
		tt := newTester(a, c.dark)
		n := len(a.bars)
		from, to := a.view.span(a.plotW, n)
		hi, lo := c.high(from, to), (from+to)/2
		a.bars[hi].H *= 1.03 // far above the rest, so the label's row has nothing else in it
		a.bars[lo].L *= 0.97
		tt.Frame()
		tt.Frame()
		shot(t, tt, c.name)
		img := tt.Image()
		left, top := chartLeft(a), chartTop(a)
		for _, m := range []struct {
			i     int
			price float64
		}{{hi, a.bars[hi].H}, {lo, a.bars[lo].L}} {
			x, y := left+a.view.x(m.i, a.plotW, n), top+a.shown.y(m.price, a.axis)
			strip := func(x0, x1 float32) int {
				return inkBox(img, ui.Rect{X: x0, Y: y - 7, W: x1 - x0, H: 14})[0]
			}
			lw, rw := strip(left+1, x-2), strip(x+2, left+a.plotW-1)
			if lw > 0 == (rw > 0) || max(lw, rw) < 40 {
				t.Errorf("%s: bar %d's label %d wide on the left, %d on the right; want one side, whole", c.name, m.i, lw, rw)
			}
			if right := m.i == to && m.price == a.bars[hi].H; right && rw > 0 {
				t.Errorf("%s: the newest bar's label is on its right, off the chart", c.name)
			}
		}
	}
}
