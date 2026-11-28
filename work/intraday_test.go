package main

import (
	"context"
	"math"
	"math/rand/v2"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"candle/market"
)

// pagedFeed answers Klines from bars by size, as Binance does: at most limit
// of them opening at or before end.
type pagedFeed struct {
	market.Feed
	bars  map[string][]market.Bar
	asked int
}

func (f *pagedFeed) Klines(_ context.Context, _, iv string, end int64, limit int) ([]market.Bar, error) {
	f.asked++
	all := f.bars[iv]
	if end > 0 {
		all = all[:sort.Search(len(all), func(i int) bool { return all[i].T > end })]
	}
	return all[max(len(all)-limit, 0):], nil
}

func run(from int64, n int, ivMs int64) []market.Bar {
	out := make([]market.Bar, n)
	for i := range out {
		out[i] = market.Bar{T: from + int64(i)*ivMs, O: 1, H: 1, L: 1, C: 1, V: 1}
	}
	return out
}

func TestDayBars(t *testing.T) {
	day := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	from, to := day, day+86_400_000
	ctx := context.Background()

	// Three days of minutes: the middle one takes two pages of a thousand.
	f := &pagedFeed{bars: map[string][]market.Bar{"1m": run(day-86_400_000, 3*1440, 60_000)}}
	got, iv, err := dayBars(ctx, f, "BTCUSDT", from, from, to)
	if err != nil || iv != "1m" || len(got) != 1440 || got[0].T != from || got[1439].T != to-60_000 || f.asked != 2 {
		t.Fatalf("a day of minutes: %d bars of %q, %d asked, %v", len(got), iv, f.asked, err)
	}

	// Minutes from the day's noon on only: the 5 minutes, which go back further.
	f = &pagedFeed{bars: map[string][]market.Bar{
		"1m": run(day+43_200_000, 2*1440, 60_000),
		"5m": run(day-86_400_000, 3*288, 300_000),
	}}
	if got, iv, _ := dayBars(ctx, f, "BTCUSDT", from, from, to); iv != "5m" || len(got) != 288 {
		t.Fatalf("minutes missing: %d bars of %q", len(got), iv)
	}

	// Eastmoney's minutes begin at today's open, and today's auction before
	// it: whole.
	open := day + 34_200_000
	f = &pagedFeed{bars: map[string][]market.Bar{"1m": append(run(open-60_000, 1, 60_000), run(open, 240, 60_000)...)}}
	if got, iv, _ := dayBars(ctx, f, "SSE:600519", open-600_000, open, open+86_400_000); iv != "1m" || len(got) != 241 {
		t.Fatalf("today's minutes: %d bars of %q", len(got), iv)
	}

	// Neither goes back that far.
	f = &pagedFeed{bars: map[string][]market.Bar{"1m": run(to, 100, 60_000), "5m": run(to, 100, 300_000)}}
	if got, iv, _ := dayBars(ctx, f, "BTCUSDT", from, from, to); got != nil || iv != "" {
		t.Fatalf("too old: %d bars of %q", len(got), iv)
	}
}

// dailyApp is BTC's days, the minutes of each to be filled by fillMinutes.
func dailyApp(t *testing.T) (*application, *ui.Tester) {
	a := testApp()
	a.open("BTCUSDT", "1d")
	a.loading, a.bars, a.atStart = false, fakeBars(300, 86_400_000), true
	tt := newTester(a, true)
	return a, tt
}

// fillMinutes brings the minutes asked for, as the feed would: a day's,
// wandering from its open to its close, up to now, none for the days in
// missing.
func fillMinutes(tt *ui.Tester, d *intraday, missing ...int64) {
	for t, m := range d.got {
		if m.in {
			continue
		}
		m.in = true
		if slices.Contains(missing, t) {
			continue
		}
		i := slices.IndexFunc(d.days, func(b market.Bar) bool { return b.T == t })
		m.iv, m.bars = "1m", fakeMinutes(d.days[i], 1440)
	}
	tt.Frame()
	tt.Frame()
}

func fakeMinutes(day market.Bar, n int) []market.Bar {
	r := rand.New(rand.NewPCG(uint64(day.T), 3))
	walk := make([]float64, n)
	w := 0.0
	for i := range walk {
		w += r.NormFloat64()
		walk[i] = w
	}
	var out []market.Bar
	last, now := day.O, time.Now().UnixMilli()
	for i := range n {
		at := day.T + int64(i)*60_000
		if at > now {
			break
		}
		f := float64(i) / float64(n-1)
		c := day.O + (day.C-day.O)*f + (walk[i]-walk[n-1]*f)*day.O*0.0003
		out = append(out, market.Bar{T: at, O: last, C: c, H: max(last, c), L: min(last, c), V: 20 + r.Float64()*60 + 400*math.Exp(-float64(i%480)/40)})
		last = c
	}
	return out
}

// at is where bar i's column is in the window, at the price v.
func at(a *application, i int, v float64) (float32, float32) {
	return a.bounds.X + a.view.x(i, a.plotW, len(a.bars)), a.bounds.Y + a.shown.y(v, a.axis)
}

// A double click anywhere up a day's column opens its minutes; right of
// the newest it goes back to the newest; on hours it does neither.
func TestIntradayFromTheColumn(t *testing.T) {
	a, tt := dailyApp(t)
	n := len(a.bars)
	i := n - 10
	b := a.bars[i]

	tt.ClickAt(at(a, i, b.H*1.02)) // well above the candle
	tt.ClickAt(at(a, i, b.H*1.02))
	tt.Frame()
	if d := a.intra; d == nil || d.days[d.last] != b || d.span != 1 || d.got[b.T] == nil {
		t.Fatalf("above the candle: %+v", d)
	}
	if !tt.HasText("正在加载分时…") {
		t.Fatal("no word of the minutes loading")
	}
	click(t, tt, "关闭分时")
	if a.intra != nil {
		t.Fatal("关闭分时 left it open")
	}

	a.view.spacing /= 2 // zoomed out
	tt.Frame()
	tt.Advance(time.Second)
	x, y := at(a, n-1, b.C)
	tt.ClickAt(x+3*a.view.spacing, y) // right of the newest
	tt.ClickAt(x+3*a.view.spacing, y)
	tt.Frame()
	if a.intra != nil || a.view.spacing != defaultView().spacing {
		t.Fatalf("right of the newest: intraday %v, spacing %v", a.intra != nil, a.view.spacing)
	}

	a.open("BTCUSDT", "1h")
	a.loading, a.bars = false, fakeBars(300, 3_600_000)
	tt.Frame()
	tt.Advance(time.Second)
	b = a.bars[n-10]
	tt.ClickAt(at(a, n-10, b.C))
	tt.ClickAt(at(a, n-10, b.C))
	if a.intra != nil {
		t.Fatal("hours opened the minutes")
	}
}

// With the setting on, a click opens the minutes too, not a drag; the
// second click of a double click, on the dialog's backdrop, keeps it open.
// The setting is in 商品 and saves.
func TestIntradayOnAClick(t *testing.T) {
	a, tt := dailyApp(t)
	a.file = filepath.Join(t.TempDir(), "settings.json")
	n := len(a.bars)
	tt.ClickAt(at(a, n-10, a.bars[n-10].C))
	tt.Frame()
	if a.intra != nil {
		t.Fatal("a click opened the minutes with the setting off")
	}

	openSettings(t, tt, "商品")
	tt.Scroll(700, 500, 0, 600) // the last on the page
	tt.Frame()
	click(t, tt, "单击 K 线打开分时")
	click(t, tt, "确认")
	if !a.set.Chart.ClickIntraday || !loadSettings(a.file).Chart.ClickIntraday {
		t.Fatal("the setting did not save")
	}

	tt.Advance(time.Second)
	x, y := at(a, n-10, a.bars[n-10].C)
	tt.Press(x, y)
	tt.Move(x+30, y)
	tt.Release(x+30, y)
	tt.Frame()
	if a.intra != nil {
		t.Fatal("a drag opened the minutes")
	}

	// Left of the dialog, so that the second click is on its backdrop.
	i := int(math.Round(float64(a.view.index(150-a.bounds.X, a.plotW, n))))
	tt.Advance(time.Second)
	x, y = at(a, i, a.bars[i].C)
	tt.ClickAt(x, y)
	tt.Frame()
	if d := a.intra; d == nil || d.last != i {
		t.Fatalf("a click: %+v", d)
	}
	tt.ClickAt(x, y)
	tt.Frame()
	if a.intra == nil {
		t.Fatal("the double click's second click closed it")
	}
	tt.Advance(time.Second)
	tt.ClickAt(x, y)
	tt.Frame()
	if a.intra != nil {
		t.Fatal("a click beside it later did not close it")
	}
}

// The pointer on the dialog is not on the chart behind it: its crosshair
// and time label stay put, and no other chart takes the time from it.
func TestIntradayCrosshairStaysInTheDialog(t *testing.T) {
	a, tt := dailyApp(t)
	n := len(a.bars)
	tt.ClickAt(at(a, n-10, a.bars[n-10].C))
	tt.ClickAt(at(a, n-10, a.bars[n-10].C))
	fillMinutes(tt, a.intra)
	tt.Move(500, 400)
	tt.Frame()
	before := tt.Image()
	tt.Move(800, 400)
	tt.Frame()
	after := tt.Image()
	changed := 0
	for y := 740; y < 820; y++ { // the chart's bottom and time axis, below the dialog
		for x := 0; x < 960; x++ {
			if before.RGBAAt(x, y) != after.RGBAAt(x, y) {
				changed++
			}
		}
	}
	if changed > 0 || a.cross.from != nil {
		t.Fatalf("the chart behind followed the pointer: %d pixels, crosshair synced %v", changed, a.cross.from != nil)
	}
}

// PageUp and PageDown (← →) go a day back and on, as far as the chart's
// days go; the wheel shows up to maxIntraDays days, down to one.
func TestIntradayDays(t *testing.T) {
	a, tt := dailyApp(t)
	n := len(a.bars)
	tt.ClickAt(at(a, n-3, a.bars[n-3].C))
	tt.ClickAt(at(a, n-3, a.bars[n-3].C))
	d := a.intra
	fillMinutes(tt, d)
	loc := a.set.Chart.loc()
	date := func(i int) string { return time.UnixMilli(a.bars[i].T).In(loc).Format("2006-01-02") }
	shows := func(want string) {
		t.Helper()
		tt.Frame()
		if !slices.ContainsFunc(tt.Texts(), func(s string) bool { return strings.Contains(s, want) }) {
			t.Fatalf("the title does not say %s: %v", want, d.title(loc))
		}
	}
	tt.Move(1200, 760) // off the minutes
	tt.Frame()
	shot(t, tt, "intraday-day")

	tt.Key(0, ui.KeyPageUp)
	shows(date(n - 4))
	if d.last != n-4 || d.got[a.bars[n-4].T] == nil {
		t.Fatalf("PageUp: day %d, asked %v", d.last, d.got[a.bars[n-4].T] != nil)
	}
	for range 5 {
		tt.Key(0, ui.KeyPageDown)
	}
	tt.Key(0, ui.KeyRight)
	shows(date(n - 1))
	if d.last != n-1 {
		t.Fatalf("PageDown past the newest: day %d of %d", d.last, n)
	}
	fillMinutes(tt, d)

	x, y := float32(660), float32(420)
	tt.Move(x, y)
	for range 5 {
		tt.Wheel(x, y, 0, -notch)
	}
	tt.Frame()
	if d.span != 6 || d.first() != n-6 {
		t.Fatalf("five notches up: %d days from %d", d.span, d.first())
	}
	shows(date(n - 6))
	fillMinutes(tt, d, a.bars[n-6].T) // a day the feed has none of
	if !slices.ContainsFunc(tt.Texts(), func(s string) bool { return strings.Contains(s, "1 天的分时数据源已经不给了") }) {
		t.Fatal("no word of the day missing")
	}
	tt.Move(1000, 300)
	tt.Frame()
	shot(t, tt, "intraday-days")
	tt.Key(0, ui.KeyPageUp)
	if d.first() != n-7 || d.last != n-2 {
		t.Fatalf("PageUp with days: %d to %d", d.first(), d.last)
	}
	for range 20 {
		tt.Wheel(x, y, 0, -notch)
	}
	if d.span != maxIntraDays {
		t.Fatalf("up to %d days", d.span)
	}
	for range 20 {
		tt.Wheel(x, y, 0, notch)
	}
	if d.span != 1 {
		t.Fatalf("down to %d days", d.span)
	}
}

// The crosshair snaps to a minute, and reads out its price, move and time
// on the axes and in a box; the shots are for a person to look at.
func TestIntradayCrosshairShots(t *testing.T) {
	a, tt := dailyApp(t)
	n := len(a.bars)
	tt.ClickAt(at(a, n-3, a.bars[n-3].C))
	tt.ClickAt(at(a, n-3, a.bars[n-3].C))
	fillMinutes(tt, a.intra)
	tt.Move(700, 330)
	tt.Frame()
	shot(t, tt, "intraday-hover")
	for range 3 {
		tt.Wheel(700, 330, 0, -notch)
	}
	fillMinutes(tt, a.intra)
	tt.Move(330, 300) // the box goes right of the pointer
	tt.Frame()
	shot(t, tt, "intraday-days-hover")
}
