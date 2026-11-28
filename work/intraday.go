package main

import (
	"context"
	"math"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"candle/lang"
	"candle/market"
)

// intraday is the minutes of a chart of days (分时图), as Chinese apps show
// them when a day's candle is double-clicked: the price minute by minute,
// its average, the close the day before and the volume, of one day or of a
// few in a row. PageUp and PageDown go a day back and on, the wheel shows
// more days or fewer.
type intraday struct {
	sym, name string
	days      []market.Bar // the chart's days as the dialog opened
	last      int          // the newest day shown, an index of days
	span      int          // how many days show, 1 to maxIntraDays
	decimals  int
	volume    bool // whether the bars count what traded: a currency pair's do not, nor have an average
	feed      market.Feed
	got       map[int64]*dayMinutes // the days asked for, by their bar's time
	ctx       context.Context
	stop      context.CancelFunc
	wheel     float32   // a touchpad's scrolling not yet a day
	keep      time.Time // till then a click beside the dialog is the second of a double click, not one closing it
}

// dayMinutes are a day's minutes as they came: of 1 minute or of 5 ("" none
// to be had), or what went wrong.
type dayMinutes struct {
	bars    []market.Bar
	iv      string
	minutes int // the minutes a whole day trades, 0 unknown
	err     string
	in      bool
}

// shownDay is a day with minutes to draw, and the close the day before (0
// unknown).
type shownDay struct {
	day  market.Bar
	prev float64
	m    *dayMinutes
}

const maxIntraDays = 10

// openIntraday opens the minutes of the day whose candle's column x is in,
// on a chart of days, reporting whether one is: a double click right of the
// newest goes back to it, as it does in TradingView. keep is till when a
// click beside the dialog does not close it, for a dialog a single click
// opens.
func (a *application) openIntraday(x, y float32, keep time.Time) bool {
	n := len(a.bars)
	if a.interval != "1d" || y >= a.plotH || x >= a.plotW || !a.mapReady() {
		return false
	}
	v := a.view
	v.clamp(a.plotW, n)
	i := int(math.Round(float64(v.index(x, a.plotW, n))))
	if i < 0 || i >= n {
		return false
	}
	a.closeIntraday()
	base, _ := a.names(a.symbol)
	ctx, stop := context.WithCancel(context.Background())
	a.intra = &intraday{
		sym: a.symbol, name: base + a.after(a.symbol), days: a.bars, last: i, span: 1,
		decimals: a.chartDecimals(a.bars[i].C), volume: hasVolume(a.symbol), feed: a.barFeed(a.symbol),
		got: map[int64]*dayMinutes{}, ctx: ctx, stop: stop, keep: keep,
	}
	a.drag = dragNone
	a.loadIntraday()
	return true
}

// loadIntraday asks for the minutes of the days shown not asked for yet,
// each day on its own: those in come back when the days shown move.
func (a *application) loadIntraday() {
	d := a.intra
	for i := d.first(); i <= d.last; i++ {
		t := d.days[i].T
		if d.got[t] != nil {
			continue
		}
		m := &dayMinutes{}
		d.got[t] = m
		from, open, to, minutes := dayWindow(d.sym, t)
		m.minutes = minutes
		ctx, feed, sym := d.ctx, d.feed, d.sym
		a.spawn(func() {
			bars, iv, err := dayBars(ctx, feed, sym, from, open, to)
			a.post(func() {
				m.in, m.bars, m.iv = true, bars, iv
				if err != nil {
					m.err = shorten(errText(err), 160)
				}
			})
		})
	}
}

// shiftIntraday shows the days by days later (earlier, negative), as far
// as the chart's days go.
func (a *application) shiftIntraday(by int) {
	d := a.intra
	d.last = min(max(d.last+by, d.span-1), len(d.days)-1)
	a.loadIntraday()
}

// spanIntraday shows by days more (fewer, negative), before the newest shown.
func (a *application) spanIntraday(by int) {
	d := a.intra
	d.span = min(max(d.span+by, 1), maxIntraDays, d.last+1)
	a.loadIntraday()
}

// intradayWheel shows a day more for a notch of the wheel up, a day fewer
// for one down; a touchpad's scrolling, every half notch.
func (a *application) intradayWheel(ev ui.InputEvent) bool {
	d := a.intra
	if d == nil || ev.Kind != ui.InputScroll {
		return false
	}
	dy := ev.DY
	switch {
	case ev.Momentum:
		return true
	case ev.Precise:
		d.wheel += ev.DY
		if math.Abs(float64(d.wheel)) < notch/2 {
			return true
		}
		dy, d.wheel = d.wheel, 0
	}
	switch {
	case dy < 0:
		a.spanIntraday(1)
	case dy > 0:
		a.spanIntraday(-1)
	}
	return true
}

// dayWindow is when the bars of sym's day opening at t open, from to to,
// when it opens and how many minutes it trades (0 unknown). A future's day
// begins the evening before; the auction before a market opens belongs to
// its day. Binance and the currency pairs trade round the clock.
func dayWindow(sym string, t int64) (from, open, to int64, minutes int) {
	in, ok := market.InstrumentOf(sym)
	if !ok {
		return t, t, t + 86_400_000, 1440
	}
	open = in.SessionStart(t)
	from = open - 10*60_000
	if in.FX {
		from = open
	}
	return from, open, open + 86_400_000, in.Minutes()
}

func (a *application) closeIntraday() {
	if a.intra != nil {
		a.intra.stop()
		a.intra = nil
	}
}

// dayBars are the bars opening in [from, to): of a minute where the feed
// still has all of them, else of 5 minutes (Eastmoney keeps a day of
// minutes, Yahoo a month); none where it has neither.
func dayBars(ctx context.Context, feed market.Feed, sym string, from, open, to int64) ([]market.Bar, string, error) {
	for _, iv := range []string{"1m", "5m"} {
		bars, whole, err := barsIn(ctx, feed, sym, iv, from, open, to)
		if err != nil {
			return nil, "", err
		}
		if whole && len(bars) > 0 {
			return bars, iv, nil
		}
	}
	return nil, "", nil
}

// barsIn pages back from to until it reaches open, a page of a thousand at
// a time (a day of Binance's minutes takes two), and keeps the bars from
// from to to. whole is whether the feed's bars go back to open, so that none
// of the day's are missing: Eastmoney's minutes begin at today's.
// shortcut: a market's first day reads as missing when it opens late; fine while nobody looks at listing days.
func barsIn(ctx context.Context, feed market.Feed, sym, iv string, from, open, to int64) (bars []market.Bar, whole bool, err error) {
	end := to - 1
	if to > time.Now().UnixMilli() {
		end = 0 // the day is still on
	}
	for range 4 {
		got, err := feed.Klines(ctx, sym, iv, end, 1000)
		if err != nil {
			return nil, false, err
		}
		bars = append(got, bars...)
		if len(got) > 0 && got[0].T <= open {
			whole = true
			break
		}
		if len(got) < 1000 {
			break
		}
		end = got[0].T - 1
	}
	out := bars[:0]
	for _, b := range bars {
		if b.T >= from && b.T < to {
			out = append(out, b)
		}
	}
	return out, whole, nil
}

// first is the oldest day shown.
func (d *intraday) first() int { return d.last - d.span + 1 }

// prev is the close the day before day i, 0 unknown.
func (d *intraday) prev(i int) float64 {
	if i > 0 {
		return d.days[i-1].C
	}
	return 0
}

// shown are the days shown that have minutes, oldest first, and of the rest
// how many are on their way, have none to be had and failed, with the
// first failure.
func (d *intraday) shown() (days []shownDay, waiting, none, failed int, err string) {
	for i := d.first(); i <= d.last; i++ {
		m := d.got[d.days[i].T]
		switch {
		case m == nil || !m.in:
			waiting++
		case m.err != "":
			if failed++; err == "" {
				err = m.err
			}
		case len(m.bars) == 0:
			none++
		default:
			days = append(days, shownDay{day: d.days[i], prev: d.prev(i), m: m})
		}
	}
	return days, waiting, none, failed, err
}

func (d *intraday) title(loc *time.Location) string {
	at := func(i int) time.Time { return time.UnixMilli(d.days[i].T).In(loc) }
	if d.span == 1 {
		return lang.F("%s 分时 · %s %s", d.name, at(d.last).Format("2006-01-02"), weekday(at(d.last)))
	}
	return lang.F("%s 分时 · %s 至 %s，%d 天", d.name, at(d.first()).Format("2006-01-02"), at(d.last).Format("2006-01-02"), d.span)
}

func (a *application) intradayDialog(c *ui.Context, t *ui.Theme, k palette) {
	d := a.intra
	if d == nil {
		return
	}
	open, closed := true, false
	ww, wh := c.Size()
	loc := a.set.Chart.loc()
	title := d.title(loc)
	ui.DialogBase(c.Key("intraday"), &open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.45))
		panel.Width(min(880, ww-60)).Height(min(580, wh-40)).Radius(10).Background(k.bg).Border(1, k.line).
			Shadow(0, 12, 40, 0, ui.RGBA(0, 0, 0, 0.35)).Label(title)
		ui.Column(c).FillWidth().FillHeight().Children(func() {
			for _, s := range []struct {
				key ui.Key
				by  int
			}{{ui.KeyPageUp, -1}, {ui.KeyLeft, -1}, {ui.KeyPageDown, 1}, {ui.KeyRight, 1}} {
				c.OnWindowShortcut(0, s.key, func() { a.shiftIntraday(s.by) })
			}
			dialogHeader(c, t, k, title, "关闭分时", func() { closed = true })
			ui.RichText(c, d.summary(k)...).SingleLine().Padding(0, 24, 10, 24)
			box := ui.Box(c.Key("intraday-chart")).FillWidth().Grow(1).MinHeight(0).Margin(0, 16, 0, 16)
			hx, hy, over := box.PointerPosition()
			box.HandleInput(a.intradayWheel).
				Draw(func(p *ui.Painter, r ui.Rect) { d.draw(p, r, k, loc, hx, hy, over) })
			days, waiting, none, failed, err := d.shown()
			msg, detail := "", ""
			switch {
			case len(days) > 0: // they draw
			case waiting > 0:
				msg = lang.T("正在加载分时…")
			case failed > 0:
				msg, detail = lang.T("分时没加载上"), err
			case d.span == 1:
				msg, detail = lang.T("这一天的分时拿不到"), lang.T("数据源只留最近一段时间的分钟线（东方财富约一个半月、Yahoo 两个月），更早的日子没有")
			default:
				msg, detail = lang.T("这几天的分时都拿不到"), lang.T("数据源只留最近一段时间的分钟线（东方财富约一个半月、Yahoo 两个月），更早的日子没有")
			}
			if msg != "" {
				box.Children(func() {
					ui.Column(c).FillWidth().FillHeight().AlignItems(ui.Center).Justify(ui.Center).Gap(8).Children(func() {
						ui.Text(c, msg).FontSize(14).FontWeight(600)
						if detail != "" {
							ui.Text(c, detail).FontSize(12).TextColor(t.TextMuted).MaxWidth(520).TextAlign(ui.Center)
						}
					})
				})
			}
			// Under the minutes: the days not drawn among those that are,
			// and the keys.
			var notes []string
			if len(days) > 0 {
				if waiting > 0 {
					notes = append(notes, lang.F("还有 %d 天在加载", waiting))
				}
				if none > 0 {
					notes = append(notes, lang.F("%d 天的分时数据源已经不给了，没有画", none))
				}
				if failed > 0 {
					notes = append(notes, lang.F("%d 天没加载上：%s", failed, err))
				}
			}
			ui.Row(c).FillWidth().Height(34).Shrink(0).Padding(0, 24).Gap(16).AlignItems(ui.Center).Children(func() {
				ui.Text(c, strings.Join(notes, lang.T("；"))).FontSize(12).TextColor(t.TextMuted).Grow(1).MinWidth(0).SingleLine()
				ui.Text(c, lang.T("PageUp/PageDown 切换日期 · 滚轮增减天数")).FontSize(12).TextColor(t.TextMuted).Shrink(0).SingleLine()
			})
		})
	})
	// The close button, Escape or a click beside it; but the second click of
	// the double click whose first opened it closes nothing.
	if closed || !open && !c.Now().Before(d.keep) {
		a.closeIntraday()
	}
}

// summary is the days' candle read out, as one, and the size of the bars
// below.
func (d *intraday) summary(k palette) []ui.Span {
	b := d.days[d.first()]
	for _, x := range d.days[d.first()+1 : d.last+1] {
		b.H, b.L, b.V = max(b.H, x.H), min(b.L, x.L), b.V+x.V
	}
	b.C = d.days[d.last].C
	prev := d.prev(d.first())
	f := func(v float64) string { return formatPrice(v, d.decimals) }
	ref := prev
	if ref <= 0 {
		ref = b.O
	}
	col := k.move(b.C - ref)
	out := []ui.Span{
		span(lang.T("开")+" ", 13, k.muted, 400), span(f(b.O)+"   ", 13, col, 400),
		span(lang.T("高")+" ", 13, k.muted, 400), span(f(b.H)+"   ", 13, col, 400),
		span(lang.T("低")+" ", 13, k.muted, 400), span(f(b.L)+"   ", 13, col, 400),
		span(lang.T("收")+" ", 13, k.muted, 400), span(f(b.C)+"   ", 13, col, 400),
	}
	if prev > 0 {
		out = append(out, span(formatSigned(b.C-prev, d.decimals)+" ("+pctText((b.C/prev-1)*100, 2)+")   ", 13, col, 400))
	}
	if d.volume {
		out = append(out, span(lang.T("量")+" ", 13, k.muted, 400), span(formatVolume(b.V), 13, k.text, 400))
	}
	days, _, _, _, _ := d.shown()
	ivs := map[string]bool{}
	for _, sd := range days {
		ivs[sd.m.iv] = true
	}
	switch {
	case ivs["1m"] && ivs["5m"]:
		out = append(out, span("   · "+lang.T("1 分钟，较早的几天 5 分钟"), 13, k.muted, 400))
	case ivs["1m"]:
		out = append(out, span("   · "+lang.T("1 分钟"), 13, k.muted, 400))
	case ivs["5m"]:
		out = append(out, span("   · "+lang.T("5 分钟（更细的已经没有了）"), 13, k.muted, 400))
	}
	return out
}

// draw draws the days with minutes in r, each its share of the width: the
// prices spread evenly above and below the close the day before the first,
// the volume under them, the hours the market is shut (a lunch break, a
// night) left out. A day still on fills only its part. Under the pointer,
// the crosshair: snapped to a minute, its price, move and time on the axes,
// the minute read out in a box.
func (d *intraday) draw(p *ui.Painter, r ui.Rect, k palette, loc *time.Location, hx, hy float32, over bool) {
	days, _, _, _, _ := d.shown()
	if len(days) == 0 {
		return
	}
	s := p.Scale()
	px := 1 / s
	mid := func(v float32) float32 { return (float32(math.Floor(float64(v*s))) + 0.5) / s }
	const sideW, bottomH, gap = 66, 22, 8
	plot := ui.Rect{X: r.X + sideW, Y: r.Y, W: r.W - 2*sideW, H: r.H - bottomH}
	if plot.W <= 0 || plot.H <= 0 {
		return
	}
	priceH := plot.H
	if d.volume {
		priceH = plot.H * 0.72
	}
	volTop, volH := plot.Y+priceH+gap, plot.H-priceH-gap
	segW := plot.W / float32(len(days))

	// The minutes laid out: each day's in slots of its share, the average
	// since its open (均价), and the close before each.
	type minute struct {
		x           float32
		b           market.Bar
		avg, before float64
	}
	var pts []minute
	starts := make([]int, len(days)+1) // where each day's minutes begin in pts
	slots := make([]int, len(days))    // a day's bars, and those still to come when it is on
	slotW := func(j int) float32 { return segW / float32(slots[j]) }
	ref := days[0].prev
	if ref <= 0 {
		ref = days[0].m.bars[0].O
	}
	var far, vmax float64
	for j, sd := range days {
		starts[j] = len(pts)
		slots[j] = max(len(sd.m.bars), int(int64(sd.m.minutes)*60_000/ivMs(sd.m.iv)))
		x0 := plot.X + float32(j)*segW
		before := sd.prev
		if before <= 0 {
			before = sd.m.bars[0].O
		}
		var pv, vol float64
		for i, b := range sd.m.bars {
			pv, vol, vmax = pv+(b.H+b.L+b.C)/3*b.V, vol+b.V, max(vmax, b.V)
			avg := b.C
			if vol > 0 {
				avg = pv / vol
			}
			pts = append(pts, minute{x: x0 + (float32(i)+0.5)*slotW(j), b: b, avg: avg, before: before})
			before = b.C
			far = max(far, math.Abs(b.C-ref))
			if d.volume {
				far = max(far, math.Abs(avg-ref))
			}
		}
	}
	starts[len(days)] = len(pts)
	if far == 0 {
		far = ref * 0.01
	}
	far *= 1.08
	y := func(v float64) float32 { return plot.Y + float32((ref+far-v)/(2*far))*priceH }

	outline(p, ui.Rect{X: plot.X, Y: plot.Y, W: plot.W, H: priceH}, px, k.line)
	if d.volume {
		outline(p, ui.Rect{X: plot.X, Y: volTop, W: plot.W, H: volH}, px, k.line)
	}
	for _, f := range []float64{1, 0.5, 0, -0.5, -1} {
		v := ref + f*far
		yy := mid(y(v))
		if f == 0 {
			dash(p, plot.X, yy, plot.X+plot.W, yy, px, k.muted, "")
		} else {
			p.Line(plot.X, yy, plot.X+plot.W, yy, px, k.grid)
		}
		ly := min(max(yy, plot.Y+8), plot.Y+priceH-8)
		col := k.muted
		if f != 0 {
			col = k.move(f)
		}
		label(p, plot.X-6, ly, span(formatPrice(v, d.decimals), 11, col, 400), alignEnd)
		label(p, plot.X+plot.W+6, ly, span(pctText((v/ref-1)*100, 2), 11, col, 400), alignStart)
	}
	for j := 1; j < len(days); j++ { // where each day begins
		xx := mid(plot.X + float32(j)*segW)
		p.Line(xx, plot.Y, xx, plot.Y+priceH, px, k.muted.Alpha(0.6))
		if d.volume {
			p.Line(xx, volTop, xx, volTop+volH, px, k.muted.Alpha(0.6))
		}
	}

	for j := range days {
		w := max(px, slotW(j)*0.6)
		for _, m := range pts[starts[j]:starts[j+1]] {
			col := k.up
			if m.b.C < m.before {
				col = k.down
			}
			if h := float32(m.b.V/max(vmax, 1e-12)) * volH; h > 0 {
				p.Fill(ui.Rect{X: m.x - w/2, Y: volTop + volH - h, W: w, H: h}, col.Alpha(0.7), 0)
			}
		}
	}
	mean, line := liner{p: p, w: 1.2, c: k.amber}, liner{p: p, w: 1.5, c: k.accent}
	for j := range days {
		for _, m := range pts[starts[j]:starts[j+1]] {
			if d.volume {
				mean.lineTo(m.x, y(m.avg))
			}
		}
		mean.lift()
	}
	for _, m := range pts {
		line.lineTo(m.x, y(m.b.C))
	}
	line.flush()

	// The minute under the pointer, if it is on the minutes, and its time
	// on the axis, where the axis's own labels give way to it.
	clock := func(t int64) string { return time.UnixMilli(t).In(loc).Format("15:04") }
	ty := plot.Y + plot.H + bottomH/2
	hover := over && hx >= plot.X-r.X && hx < plot.X-r.X+plot.W && hy >= 0 && hy < plot.H
	var m minute
	var when string
	var ts ui.Span
	var tx, tw float32
	if hover {
		j := min(int((hx-(plot.X-r.X))/segW), len(days)-1)
		m = pts[starts[j]+min(int((hx-(plot.X-r.X)-float32(j)*segW)/slotW(j)), starts[j+1]-starts[j]-1)]
		when = clock(m.b.T)
		if len(days) > 1 {
			when = time.UnixMilli(m.b.T).In(loc).Format("01-02 15:04")
		}
		ts = span(when, 11, k.onLabel, 400)
		tw, _ = p.MeasureText(0, ts)
		tw += 12
		tx = min(max(mid(m.x)-tw/2, plot.X), plot.X+plot.W-tw)
	}
	axisLabel := func(x float32, text string, a align) {
		sp := span(text, 11, k.muted, 400)
		w, _ := p.MeasureText(0, sp)
		x0 := x
		switch a {
		case alignCenter:
			x0 -= w / 2
		case alignEnd:
			x0 -= w
		}
		if hover && x0 < tx+tw+4 && x0+w > tx-4 {
			return
		}
		label(p, x0, ty, sp, alignStart)
	}

	// The time: of one day the open, the middle of it and the close, when
	// it is in; of a few, the date of each.
	if len(days) == 1 {
		bars, iv := days[0].m.bars, ivMs(days[0].m.iv)
		n := len(bars)
		axisLabel(plot.X, clock(bars[0].T), alignStart)
		if i := slots[0] / 2; i < n {
			axisLabel(pts[i].x, clock(bars[i].T), alignCenter)
		}
		if n == slots[0] {
			axisLabel(plot.X+plot.W, clock(bars[n-1].T+iv), alignEnd)
		}
	} else {
		date := func(sd shownDay) string { return time.UnixMilli(sd.day.T).In(loc).Format("01-02") }
		w, _ := p.MeasureText(0, span(date(days[0]), 11, k.muted, 400))
		step := max(1, int(math.Ceil(float64((w+12)/segW))))
		for j := 0; j < len(days); j += step {
			axisLabel(plot.X+(float32(j)+0.5)*segW, date(days[j]), alignCenter)
		}
	}

	// The crosshair.
	if !hover {
		return
	}
	xx := mid(m.x)
	dash(p, xx, plot.Y, xx, plot.Y+plot.H, px, k.cross, "")
	p.Fill(ui.Rect{X: xx - 2, Y: y(m.b.C) - 2, W: 4, H: 4}, k.accent, 2)
	tag := func(x, y, w float32, sp ui.Span, a align) { // a dark label on an axis
		p.Fill(ui.Rect{X: x, Y: y - 9, W: w, H: 18}, k.label, 2)
		if a == alignEnd {
			label(p, x+w-6, y, sp, a)
		} else {
			label(p, x+6, y, sp, a)
		}
	}
	if yy := mid(r.Y + hy); yy < plot.Y+priceH {
		dash(p, plot.X, yy, plot.X+plot.W, yy, px, k.cross, "")
		v := ref + far - float64((yy-plot.Y)/priceH)*2*far
		ly := min(max(yy, plot.Y+9), plot.Y+priceH-9)
		tag(r.X, ly, sideW-2, span(formatPrice(v, d.decimals), 11, k.onLabel, 400), alignEnd)
		tag(plot.X+plot.W+2, ly, sideW-2, span(pctText((v/ref-1)*100, 2), 11, k.onLabel, 400), alignStart)
	}
	tag(tx, ty, tw, ts, alignStart)

	// The minute read out, in a box in the corner away from the pointer.
	col := k.move(m.b.C - ref)
	rows := [][2]ui.Span{
		{span(lang.T("时间"), 11, k.muted, 400), span(when, 11, k.text, 400)},
		{span(lang.T("价格"), 11, k.muted, 400), span(formatPrice(m.b.C, d.decimals), 11, col, 400)},
		{span(lang.T("涨跌"), 11, k.muted, 400), span(formatSigned(m.b.C-ref, d.decimals), 11, col, 400)},
		{span(lang.T("涨幅"), 11, k.muted, 400), span(pctText((m.b.C/ref-1)*100, 2), 11, col, 400)},
	}
	if d.volume {
		rows = append(rows,
			[2]ui.Span{span(lang.T("均价"), 11, k.muted, 400), span(formatPrice(m.avg, d.decimals), 11, k.amber, 400)},
			[2]ui.Span{span(lang.T("成交量"), 11, k.muted, 400), span(formatVolume(m.b.V), 11, k.text, 400)})
	}
	var lw, vw float32
	for _, row := range rows {
		w0, _ := p.MeasureText(0, row[0])
		w1, _ := p.MeasureText(0, row[1])
		lw, vw = max(lw, w0), max(vw, w1)
	}
	const rowH, pad = 17, 7
	box := ui.Rect{X: plot.X + 6, Y: plot.Y + 6, W: pad + lw + 14 + vw + pad, H: pad + rowH*float32(len(rows)) + pad - 4}
	if xx < box.X+box.W+12 {
		box.X = plot.X + plot.W - 6 - box.W
	}
	p.Fill(box, k.bg.Alpha(0.92), 4)
	outline(p, box, px, k.line)
	for n, row := range rows {
		ry := box.Y + pad + rowH*(float32(n)+0.5) - 2
		label(p, box.X+pad, ry, row[0], alignStart)
		label(p, box.X+box.W-pad, ry, row[1], alignEnd)
	}
}

// ivMs is how long a bar of iv is: 5 minutes or 1.
func ivMs(iv string) int64 {
	if iv == "5m" {
		return 300_000
	}
	return 60_000
}
