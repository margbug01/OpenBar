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
	tt.Move(1319, 819)
	tt.Frame()
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
	tt.Move(1319, 819)
	tt.Frame()
	shot(t, tt, "tools-open-dark")
	click(t, tt, "画图")
	if _, ok := tt.Find("趋势线"); ok || a.set.DrawTools {
		t.Fatal("画图 again left the column")
	}
}

// The watchlist goes right of the charts, under them or nowhere, as the
// button in the title bar picks: under them it spans the charts' width in
// columns, its top edge dragged higher, and hidden, the button brings it back.
func TestWatchlistGoesWhereChosen(t *testing.T) {
	a := testApp()
	a.set.DrawTools = false
	tt := newTester(a, false)
	if r, ok := tt.Find("ETH/USDT"); !ok || r.X < 1320-watchW {
		t.Fatalf("ETH's row at %+v, want on the right", r)
	}
	tt.Move(1319, 819) // no crosshair in the picture
	tt.Frame()
	shot(t, tt, "watch-right-light")
	place := func(label, key string) {
		t.Helper()
		click(t, tt, "自选列表的位置")
		click(t, tt, "自选列表："+label)
		if a.set.Chart.Watchlist != key || a.watchMenu {
			t.Fatalf("picked %s: place %q, menu open %v", label, a.set.Chart.Watchlist, a.watchMenu)
		}
	}

	place("放在图表下方", "bottom")
	tt.Frame()
	under := func() float32 { return a.area.Y + a.area.H }
	if a.area.W != 1320 || under() != 820-float32(a.set.Chart.WatchH) {
		t.Fatalf("the charts at %+v over a band %v high", a.area, a.set.Chart.WatchH)
	}
	xs := map[float32]bool{}
	for _, s := range []string{"ETH/USDT", "SOL/USDT", "BNB/USDT", "XRP/USDT"} {
		r, ok := tt.Find(s)
		if !ok || r.Y < under() {
			t.Fatalf("%s's row at %+v, want under the charts", s, r)
		}
		xs[r.X] = true
	}
	if len(xs) != 4 {
		t.Fatalf("the four markets after BTC in %d columns, want 4", len(xs))
	}
	tt.Move(1319, 819) // no crosshair in the picture
	tt.Frame()
	shot(t, tt, "watch-bottom-light")

	y := under() + 2
	tt.Press(600, y)
	tt.Move(600, y-30)
	tt.Move(600, y-60)
	tt.Release(600, y-60)
	tt.Frame()
	tt.Frame()
	if a.set.Chart.WatchH != 280 || under() != 820-280 {
		t.Fatalf("dragged 60 up: band %v high, the charts end at %v", a.set.Chart.WatchH, under())
	}
	tt.SetDark(true)
	tt.ClickAt(150, under()+20)
	tt.Type("pepe")
	tt.Frame()
	tt.Frame()
	if r, ok := tt.Find("PEPE/USDT"); !ok || r.X > watchW || r.Y < under() {
		t.Fatalf("the search's PEPE at %+v, want in the band's left column", r)
	}
	tt.Move(1319, 819) // no crosshair in the picture
	tt.Frame()
	shot(t, tt, "watch-bottom-search-dark")
	tt.Key(0, ui.KeyEscape)
	tt.Frame()

	place("隐藏", "hidden")
	tt.Frame()
	if _, ok := tt.Find("ETH/USDT"); ok || a.area.W != 1320 || under() != 820 {
		t.Fatalf("hidden: ETH shows %v, the charts at %+v", ok, a.area)
	}
	tt.Move(1319, 819) // no crosshair in the picture
	tt.Frame()
	shot(t, tt, "watch-hidden-dark")
	place("放在图表右边", "")
	if _, ok := tt.Find("ETH/USDT"); !ok {
		t.Fatal("the watchlist did not come back")
	}
}
