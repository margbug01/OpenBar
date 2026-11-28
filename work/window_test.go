package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// The column of drawing tools starts folded away, the chart taking its
// room; 画图 by the market's name shows it and folds it again, kept in the
// settings. Folded, the bar of favorites draws and the keys work.
func TestDrawToolsFoldAway(t *testing.T) {
	a := testApp()
	a.set.DrawTools = defaultSettings().DrawTools
	tt := newTester(a, false)
	if _, ok := tt.Find("趋势线"); ok || chartLeft(a) != 0 {
		t.Fatalf("the column shows at first, the chart from x %v", chartLeft(a))
	}
	shot(t, tt, "tools-folded-light")

	click(t, tt, "常用 水平线")
	clickPlot(tt, a, 300, 300)
	if ds := drawings(a); len(ds) != 1 || ds[0].Kind != "hline" || a.sel != ds[0].ID {
		t.Fatalf("drew %+v from the bar, selected %d", ds, a.sel)
	}
	tt.Key(0, ui.KeyDelete)
	tt.Frame()
	if ds := drawings(a); len(ds) != 0 {
		t.Fatalf("Delete left %+v", ds)
	}

	click(t, tt, "画图")
	if _, ok := tt.Find("趋势线"); !ok || !a.set.DrawTools || chartLeft(a) != toolsW {
		t.Fatalf("画图 did not show the column: kept %v, the chart from x %v", a.set.DrawTools, chartLeft(a))
	}
	tt.SetDark(true)
	tt.Frame()
	tt.Frame()
	shot(t, tt, "tools-open-dark")
	click(t, tt, "画图")
	if _, ok := tt.Find("趋势线"); ok || a.set.DrawTools {
		t.Fatal("画图 again left the column")
	}
}
