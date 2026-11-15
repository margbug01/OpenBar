package main

import (
	"encoding/json"
	"image/color"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// styledDrawings are drawings set up as the settings dialog sets them up.
func styledDrawings(a *application) []drawing {
	ds := sampleDrawings(a)
	for i := range ds {
		d := &ds[i]
		switch d.Kind {
		case "trend":
			d.Style, d.ExtR, d.Stats, d.Text, d.Labels = lineDashed, true, triOn, "上升趋势", triOn
		case "hline":
			d.Text, d.Style = "关键阻力", lineDotted
		case "vline":
			d.Text = "CPI 公布"
		case "rect":
			d.Text, d.Mid, d.ExtR, d.Fill = "震荡区", triOn, true, hx("#ff980033")
		case "fib":
			d.Levels, d.LabelSide = defaultFibLevels(), 1
		case "text":
			d.Back, d.Border, d.Bold = triOn, true, true
		}
	}
	return ds
}

func TestStyledDrawingShots(t *testing.T) {
	if os.Getenv("CANDLE_SHOTS") == "" {
		t.Skip("CANDLE_SHOTS names no folder")
	}
	for _, dark := range []bool{true, false} {
		a := testApp()
		a.view.spacing = 10
		a.set.Drawings = map[string][]drawing{"BTCUSDT": styledDrawings(a)}
		a.nextID = 10
		tt := newTester(a, dark)
		tt.Frame()
		shot(t, tt, map[bool]string{true: "styled-dark", false: "styled-light"}[dark])
	}
	for _, kind := range []string{"fib", "trend", "rect", "text", "long"} {
		a := testApp()
		a.view.spacing = 10
		ds := styledDrawings(a)
		if kind == "long" {
			ds = append(ds, position(builtinDrawing("long"), ds[0].Pts[1], a.mapper()))
			ds[len(ds)-1].ID = 11
		}
		a.set.Drawings = map[string][]drawing{"BTCUSDT": ds}
		a.nextID = 11
		tt := newTester(a, true)
		for _, d := range ds {
			if d.Kind == kind {
				a.openDrawing(d.ID)
			}
		}
		tt.Frame()
		tt.Frame()
		shot(t, tt, "drawing-dialog-"+kind)
		if kind == "trend" { // its other tabs
			for i, name := range []string{"text", "points", "visibility"} {
				a.drawTab = i + 1
				tt.Frame()
				tt.Frame()
				shot(t, tt, "drawing-dialog-trend-"+name)
			}
		}
		if kind == "fib" { // its levels, below
			for range 4 {
				tt.Wheel(660, 500, 0, 240)
				tt.Frame()
			}
			tt.Frame()
			shot(t, tt, "drawing-dialog-fib-levels")
		}
	}
}

// Drawings saved before their settings were keep their look: a retracement
// its levels up to 1 in its one color, a channel its middle line, a
// horizontal line its price on the axis; and settings left as they come
// stay out of the file.
func TestOldDrawingsKeepTheirLook(t *testing.T) {
	var d []drawing
	old := `[{"ID":1,"Kind":"fib","Pts":[{"T":0,"P":100},{"T":1,"P":200}],"Color":"","Width":1,"Text":"","Size":0,"Locked":false},
		{"ID":2,"Kind":"channel","Pts":[{"T":0,"P":1},{"T":1,"P":2},{"T":0,"P":0}],"Color":"","Width":2,"Text":"","Size":0,"Locked":false},
		{"ID":3,"Kind":"hline","Pts":[{"T":0,"P":1}],"Color":"","Width":2,"Text":"","Size":0,"Locked":false}]`
	if err := json.Unmarshal([]byte(old), &d); err != nil {
		t.Fatal(err)
	}
	var on []float64
	for _, l := range fibLevels(&d[0]) {
		if !l.Off {
			on = append(on, l.V)
		}
	}
	if len(on) != 7 || on[0] != 0 || on[6] != 1 || !oneColor(&d[0]) {
		t.Errorf("an old retracement shows %v, one color %v", on, oneColor(&d[0]))
	}
	if !midOn(&d[1]) || !labelsOn(&d[2]) || midOn(&drawing{Kind: "rect"}) {
		t.Error("defaults: a channel's middle line, a horizontal line's price label, no box's middle line")
	}
	b, _ := json.Marshal(d[2])
	for _, f := range []string{"Style", "ExtL", "Back", "Fill", "Levels", "HideOn", "TextColor"} {
		if strings.Contains(string(b), `"`+f+`"`) {
			t.Errorf("%s written for a drawing left as it comes: %s", f, b)
		}
	}
}

// A retracement takes its levels as set: those turned off are not there to
// click, reversed 0 and 1 change places.
func TestFibLevels(t *testing.T) {
	a := testApp()
	tt := newTester(a, true)
	_ = tt
	m := a.mapper()
	n := len(a.bars)
	lo, hi := a.shown.lo+(a.shown.hi-a.shown.lo)*0.3, a.shown.lo+(a.shown.hi-a.shown.lo)*0.7
	d := drawing{ID: 1, Kind: "fib", Width: 1, Pts: []point{{a.bars[n-40].T, lo}, {a.bars[n-20].T, hi}}, Levels: defaultFibLevels()}
	x := m.x(a.bars[n-30].T)
	at := func(v float64) float32 { return m.y(fibPrice(&d, v)) }
	if !a.hits(m, &d, x, at(0.618)) {
		t.Error("0.618 is not there to click")
	}
	if a.hits(m, &d, x, at(1.272)) {
		t.Error("1.272, off by default, is there to click")
	}
	if fibPrice(&d, 0) != hi || fibPrice(&d, 1) != lo {
		t.Errorf("0 at %v, 1 at %v", fibPrice(&d, 0), fibPrice(&d, 1))
	}
	d.Reverse = true
	if fibPrice(&d, 0) != lo || fibPrice(&d, 1) != hi {
		t.Errorf("reversed: 0 at %v, 1 at %v", fibPrice(&d, 0), fibPrice(&d, 1))
	}
}

// A trend line extended to the right is there to click past its points.
func TestExtendedLineHits(t *testing.T) {
	a := testApp()
	newTester(a, true)
	m := a.mapper()
	n := len(a.bars)
	p := a.shown.lo + (a.shown.hi-a.shown.lo)/2
	d := drawing{ID: 1, Kind: "trend", Width: 2, Pts: []point{{a.bars[n-60].T, p}, {a.bars[n-50].T, p}}}
	past := m.x(a.bars[n-20].T)
	if a.hits(m, &d, past, m.y(p)) {
		t.Fatal("a trend line is there past its end")
	}
	d.ExtR = true
	if !a.hits(m, &d, past, m.y(p)) {
		t.Fatal("extended to the right, it is not there past its end")
	}
	if a.hits(m, &d, m.x(a.bars[n-80].T), m.y(p)) {
		t.Fatal("extended to the right, it is there before its start")
	}
}

// A dashed line has gaps where a solid one has none.
func TestDashedLine(t *testing.T) {
	c := ui.Hex("#2962ff")
	count := func(style int) (n int) {
		tt := ui.NewTester(func(cx *ui.Context) {
			ui.Box(cx).Size(400, 100).Draw(func(p *ui.Painter, r ui.Rect) {
				strokeLine(p, r.X+10, r.Y+20, r.X+390, r.Y+80, 2, c, style)
			})
		}, 400, 100)
		tt.Frame()
		img := tt.Image()
		for x := 20; x < 380; x++ {
			found := false
			for y := 0; y < 100 && !found; y++ {
				found = near8(img.RGBAAt(x, y), c, 90)
			}
			if found {
				n++
			}
		}
		return n
	}
	solid, dashed, dotted := count(lineSolid), count(lineDashed), count(lineDotted)
	if solid < 355 || dashed > solid*8/10 || dashed < solid*4/10 || dotted > solid*7/10 {
		t.Fatalf("columns with the line: solid %d, dashed %d, dotted %d", solid, dashed, dotted)
	}
}

// Double-clicking a drawing opens its settings; what changes there shows at
// once, 取消 puts it back, 确认 keeps it as one step to undo.
func TestDrawingSettingsDialog(t *testing.T) {
	a, tt := drawApp(t)
	click(t, tt, "趋势线")
	clickPlot(tt, a, 300, 300)
	clickPlot(tt, a, 600, 200)
	if len(drawings(a)) != 1 {
		t.Fatalf("drew %d", len(drawings(a)))
	}
	before := cloneDrawing(drawings(a)[0])
	open := func() {
		wx, wy := win(a, 450, 250)
		tt.Move(wx, wy)
		tt.Frame()
		tt.ClickAt(wx, wy)
		tt.ClickAt(wx, wy) // a double click: the clock has not moved
		tt.Frame()
		tt.Frame()
		if !a.drawDialog {
			t.Fatal("a double click on the line did not open its settings")
		}
	}
	open()
	click(t, tt, "向右延伸")
	if !drawings(a)[0].ExtR {
		t.Fatal("向右延伸 did not extend it at once")
	}
	click(t, tt, "取消")
	if a.drawDialog || drawings(a)[0].ExtR {
		t.Fatal("取消 kept it extended")
	}
	open()
	click(t, tt, "向右延伸")
	click(t, tt, "显示统计（涨跌、K线数、时长、角度）")
	click(t, tt, "确认")
	d := drawings(a)[0]
	if !d.ExtR || !statsOn(&d) {
		t.Fatalf("确认 did not keep it: %+v", d)
	}
	clickPlot(tt, a, 300, 450) // off the line
	press(tt, ui.Cmd, ui.KeyZ)
	if d := drawings(a)[0]; d.ExtR || statsOn(&d) || !equalPts(d.Pts, before.Pts) {
		t.Fatalf("one Ctrl+Z after 确认: %+v", d)
	}
}

func equalPts(a, b []point) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// 存为默认 makes the tool draw so from then on: its look, not its place.
func TestSaveAsDefault(t *testing.T) {
	a, tt := drawApp(t)
	click(t, tt, "矩形")
	clickPlot(tt, a, 300, 300)
	clickPlot(tt, a, 400, 200)
	a.openDrawing(drawings(a)[0].ID)
	tt.Frame()
	tt.Frame()
	click(t, tt, "中线")
	click(t, tt, "向左延伸")
	click(t, tt, "存为默认")
	click(t, tt, "确认")
	click(t, tt, "矩形")
	clickPlot(tt, a, 500, 300)
	clickPlot(tt, a, 600, 250)
	ds := drawings(a)
	if len(ds) != 2 || !midOn(&ds[1]) || !ds[1].ExtL || ds[1].ID == ds[0].ID {
		t.Fatalf("a box drawn after 存为默认: %+v", ds)
	}
	if equalPts(ds[0].Pts, ds[1].Pts) {
		t.Fatal("the default kept the points")
	}
}

// A drawing hidden on a bar size neither shows nor takes clicks there, and
// does on the others.
func TestHiddenOnAnInterval(t *testing.T) {
	a, tt := drawApp(t)
	click(t, tt, "水平线")
	clickPlot(tt, a, 300, 250)
	id := drawings(a)[0].ID
	a.openDrawing(id)
	tt.Frame()
	tt.Frame()
	click(t, tt, "可见性")
	click(t, tt, "1小时 显示")
	click(t, tt, "确认")
	if d := drawings(a)[0]; !hiddenOn(&d, "1h") || hiddenOn(&d, "4h") {
		t.Fatalf("hidden on %v", d.HideOn)
	}
	a.sel = 0
	clickPlot(tt, a, 500, 250)
	if a.sel == id {
		t.Fatal("a line hidden on 1小时 took a click there")
	}
	img := tt.Image()
	wx, wy := win(a, 0, a.mapper().y(drawings(a)[0].Pts[0].P))
	if n := countPixels(img, ui.Rect{X: wx + 100, Y: wy - 1, W: 400, H: 3}, func(c color.RGBA) bool { return near8(c, k(a).accent, 60) }); n > 50 {
		t.Fatalf("a line hidden on 1小时 shows there: %d pixels", n)
	}
	a.interval = "4h"
	if !a.visible(&drawings(a)[0]) {
		t.Fatal("hidden on 4小时 too")
	}
}

func k(*application) palette { return paletteFor(true) }

// A point's price and time are set in 坐标, the time as typed in the
// chart's zone, on the bar nearest it.
func TestTypedCoordinates(t *testing.T) {
	a, tt := drawApp(t)
	click(t, tt, "趋势线")
	clickPlot(tt, a, 300, 300)
	clickPlot(tt, a, 600, 200)
	a.openDrawing(drawings(a)[0].ID)
	tt.Frame()
	tt.Frame()
	click(t, tt, "坐标")
	set := func(label, text string) {
		r, ok := tt.Find(label)
		if !ok {
			t.Fatalf("no %s", label)
		}
		tt.ClickAt(r.X+30, r.Y+r.H/2)
		tt.Frame()
		press(tt, ui.Cmd, ui.KeyA)
		for _, ch := range text {
			tt.Type(string(ch))
			tt.Frame()
		}
		tt.Frame()
	}
	set("点 1 价格", "77777")
	bar := a.bars[len(a.bars)-100]
	set("点 2 时间", timeText(a, bar.T+20*60_000)) // 20 minutes into the bar
	click(t, tt, "确认")
	d := drawings(a)[0]
	if d.Pts[0].P != 77777 || d.Pts[1].T != bar.T {
		t.Fatalf("typed 77777 and a time in bar %d: %+v", bar.T, d.Pts)
	}
}

func timeText(a *application, ms int64) string {
	return time.UnixMilli(ms).In(a.set.Chart.loc()).Format(timeLayout)
}

// A position's risk, in money, sizes it as the same part of the account does.
func TestRiskInMoney(t *testing.T) {
	d := drawing{Kind: "long", Pts: []point{{0, 100}, {0, 110}, {0, 95}, {0, 100}}, Account: 2000, Risk: 1}
	q1, _ := positionSize(&d)
	d.RiskAmt, d.Risk = true, 20
	q2, _ := positionSize(&d)
	if q1 != 4 || q2 != 4 {
		t.Fatalf("1%% of 2000 over a 5 stop: %v; 20 in money: %v", q1, q2)
	}
}

// Opening a drawing's settings and closing them with 确认 changes nothing
// that was not changed: not its points, rounded to what shows, nor a step
// to undo.
func TestSettingsLeaveItAsItWas(t *testing.T) {
	a, tt := drawApp(t)
	p0, p1 := point{a.bars[500].T + 7, 77777.123456}, point{a.bars[550].T, 78888.987654}
	a.set.Drawings = map[string][]drawing{"BTCUSDT": {
		{ID: 1, Kind: "trend", Width: 2, Pts: []point{p0, p1}},
		{ID: 2, Kind: "fib", Width: 1, Pts: []point{p0, p1}},
		{ID: 3, Kind: "long", Width: 1, Pts: []point{p0, {p0.T, p1.P}, {p0.T, 77000}, {p1.T, p0.P}}},
	}}
	before := cloneDrawings(drawings(a))
	for id := range int64(3) {
		a.openDrawing(id + 1)
		for _, tab := range []int{0, 1, 2, 3} {
			a.drawTab = tab
			tt.Frame()
			tt.Frame()
		}
		click(t, tt, "确认")
		if !reflect.DeepEqual(drawings(a), before) || len(a.undo) != 0 {
			t.Fatalf("opened drawing %d and kept it: %+v, %d steps to undo", id+1, drawings(a)[id], len(a.undo))
		}
	}
}
