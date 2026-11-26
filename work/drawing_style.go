package main

import (
	"fmt"
	"math"
	"slices"
	"strconv"

	"github.com/egoist/mygo/ui"

	"candle/lang"
)

// How each drawing looks, as TradingView's settings set it up: the line's
// style and its extensions, the background, a middle line, labels on the
// axes, a text along the line, the Fibonacci levels. A setting left at its
// zero is the kind's default, so drawings saved before these settings were
// keep their look.

// tri is a switch whose zero is the kind's default.
type tri int8

const (
	triDefault tri = iota
	triOn
	triOff
)

func (t tri) on(def bool) bool {
	switch t {
	case triOn:
		return true
	case triOff:
		return false
	}
	return def
}

func triOf(on bool) tri {
	if on {
		return triOn
	}
	return triOff
}

// The line styles.
const (
	lineSolid = iota
	lineDashed
	lineDotted
)

var lineStyles = []option{{"0", "实线"}, {"1", "虚线"}, {"2", "点线"}}

// Where a text goes along its line, or in its box; 0 is the kind's default.
const (
	textLeft = 1 + iota
	textCenter
	textRight
)

const (
	textAbove = 1 + iota // inside a box: at its top
	textMiddle
	textBelow
)

var (
	textHs = []option{{"1", "左"}, {"2", "中"}, {"3", "右"}}
	textVs = []option{{"1", "上方"}, {"2", "线上"}, {"3", "下方"}}
	// A box's text, and a vertical line's, which runs along it.
	textBoxVs  = []option{{"1", "顶部"}, {"2", "中间"}, {"3", "底部"}}
	opacities  = []option{{"0.05", "5%"}, {"0.1", "10%"}, {"0.15", "15%"}, {"0.2", "20%"}, {"0.3", "30%"}, {"0.4", "40%"}, {"0.5", "50%"}}
	levelTexts = []option{{"0", "数值"}, {"1", "百分比"}, {"2", "不显示"}}
	labelSides = []option{{"0", "左边"}, {"1", "右边"}}
)

// fibLevel is a level of a Fibonacci retracement: how far back from the
// second point to the first it is, its color, and whether it shows.
type fibLevel struct {
	V   float64
	C   hexColor `json:",omitzero"`
	Off bool     `json:",omitzero"`
}

// defaultFibLevels are TradingView's: the retracements and the extensions
// past them, in order, those it leaves off too.
func defaultFibLevels() []fibLevel {
	return []fibLevel{
		{0, hx("#787b86"), false}, {0.236, hx("#f23645"), false}, {0.382, hx("#ff9800"), false},
		{0.5, hx("#4caf50"), false}, {0.618, hx("#089981"), false}, {0.786, hx("#00bcd4"), false},
		{1, hx("#787b86"), false}, {1.272, hx("#ff9800"), true}, {1.414, hx("#4caf50"), true},
		{1.618, hx("#2962ff"), false}, {2, hx("#089981"), true}, {2.414, hx("#00bcd4"), true},
		{2.618, hx("#f23645"), false}, {3, hx("#2962ff"), true}, {3.618, hx("#9c27b0"), false},
		{4.236, hx("#e91e63"), false}, {-0.272, hx("#9c27b0"), true}, {-0.618, hx("#e91e63"), true},
	}
}

func hx(s string) hexColor { return hexColor(ui.Hex(s)) }

// legacyFib are the levels of a retracement drawn before they could be
// set: up to 1, in its one color.
func legacyFib() []fibLevel {
	var out []fibLevel
	for _, l := range defaultFibLevels() {
		l.Off = l.Off || l.V > 1
		out = append(out, l)
	}
	return out
}

// fibLevels are d's levels: as set, else as before they could be.
func fibLevels(d *drawing) []fibLevel {
	if len(d.Levels) == 0 {
		return legacyFib()
	}
	return d.Levels
}

// oneColor reports whether d's levels take its color: as set, and for a
// retracement drawn before its levels could be set.
func oneColor(d *drawing) bool { return d.OneColor || len(d.Levels) == 0 }

func lineWidth(d *drawing) float32 { return float32(max(d.Width, 1)) }

// extends reports whether d's line runs on to the left and to the right; a
// ray always to the right.
func extends(d *drawing) (left, right bool) { return d.ExtL, d.ExtR || d.Kind == "ray" }

// backOn reports whether d's background shows: a box's, a channel's and a
// retracement's unless turned off, a text's when turned on.
func backOn(d *drawing) bool { return d.Back.on(d.Kind != "text") }

// backColor is d's background as set, else its line's color, faint.
func backColor(d *drawing, line ui.Color) ui.Color {
	if d.Fill.set() {
		return ui.Color(d.Fill)
	}
	a := map[string]float32{"rect": 0.15, "channel": 0.1, "text": 0.15}[d.Kind]
	return line.Alpha(max(a, 0.1))
}

// fibOpacity is how strong the bands between a retracement's levels are.
func fibOpacity(d *drawing) float32 {
	if d.Opacity > 0 {
		return float32(min(d.Opacity, 1))
	}
	return 0.1
}

// midOn reports whether d shows its middle line: a channel's unless turned off.
func midOn(d *drawing) bool { return d.Mid.on(d.Kind == "channel") }

// labelsOn reports whether d shows where it is on the axes: the price of a
// horizontal line and the time of a vertical one unless turned off, the
// prices and times of the points of others when turned on.
func labelsOn(d *drawing) bool {
	return d.Labels.on(d.Kind == "hline" || d.Kind == "hray" || d.Kind == "vline")
}

// statsOn reports whether d reads out what it measures: a trend line's
// change when turned on; a price range's, a position's and a retracement's
// prices unless turned off.
func statsOn(d *drawing) bool { return d.Stats.on(d.Kind != "trend" && d.Kind != "ray") }

// fibTrendOn reports whether a retracement shows the line between its points.
func fibTrendOn(d *drawing) bool { return d.Trend.on(true) }

// textColor is the color of d's text: as set, else its line's.
func textColor(d *drawing, line ui.Color) ui.Color { return d.TextColor.or(line) }

func textH(d *drawing) int {
	if d.TextH > 0 {
		return d.TextH
	}
	if d.Kind == "hline" || d.Kind == "hray" {
		return textRight
	}
	return textCenter
}

func textV(d *drawing) int {
	if d.TextV > 0 {
		return d.TextV
	}
	if d.Kind == "rect" {
		return textMiddle
	}
	return textAbove
}

// textSpan is d's text, as its settings have it, in c.
func textSpan(d *drawing, text string, c ui.Color) ui.Span {
	w := 400
	if d.Kind == "text" {
		w = 500
	}
	if d.Bold {
		w = 700
	}
	sp := span(text, float32(textSize(d)), c, w)
	sp.Italic = d.Italic
	return sp
}

// The settings each kind takes.

func hasLineStyle(kind string) bool { return kind != "text" && kind != "range" && !isPosition(kind) }
func hasExtend(kind string) bool {
	return kind == "trend" || kind == "ray" || kind == "channel" || kind == "rect" || kind == "fib"
}
func hasBack(kind string) bool {
	return kind == "rect" || kind == "channel" || kind == "fib" || kind == "text" || kind == "range"
}
func hasMid(kind string) bool { return kind == "channel" || kind == "rect" }
func hasLabels(kind string) bool {
	return kind == "trend" || kind == "ray" || kind == "hline" || kind == "hray" || kind == "vline" || kind == "rect"
}
func hasText(kind string) bool {
	return kind == "trend" || kind == "ray" || kind == "hline" || kind == "hray" || kind == "vline" || kind == "rect" || kind == "text"
}

// hiddenOn reports whether d is hidden on bars of interval iv.
func hiddenOn(d *drawing, iv string) bool { return slices.Contains(d.HideOn, iv) }

// The parts of a drawing that are its look, which a tool's default keeps.
func styleOf(d drawing) drawing {
	d.ID, d.Pts, d.Locked, d.Text = 0, nil, false, ""
	d.Levels = slices.Clone(d.Levels)
	d.HideOn = slices.Clone(d.HideOn)
	return d
}

// newDrawing is a drawing of kind as its tool draws it: as the user kept
// its default, else as it comes.
func (a *application) newDrawing(kind string, id int64, n int) drawing {
	d, ok := a.set.DrawDefaults[kind]
	if ok {
		d = styleOf(d)
	} else {
		d = builtinDrawing(kind)
	}
	d.ID, d.Kind, d.Pts = id, kind, make([]point, n)
	if kind == "text" && d.Text == "" {
		d.Text = lang.T("文字")
	}
	return d
}

// builtinDrawing is a new drawing of kind as it comes.
func builtinDrawing(kind string) drawing {
	d := drawing{Kind: kind, Width: 2}
	switch kind {
	case "fib":
		d.Width, d.Levels = 1, defaultFibLevels()
	case "text":
		d.Text = lang.T("文字")
	case "long", "short":
		d.Width = 1
	}
	return d
}

// strokeLine draws a line from x0, y0 to x1, y1, w wide, in c, in style.
func strokeLine(p *ui.Painter, x0, y0, x1, y1, w float32, c ui.Color, style int) {
	l := liner{p: p, w: w, c: c}
	// Across or down, a filled rectangle: crisp on the pixel grid, as lines
	// were drawn before they had styles.
	seg := func(ax, ay, bx, by float32) {
		if ax == bx || ay == by {
			p.Line(ax, ay, bx, by, w, c)
			return
		}
		l.moveTo(ax, ay)
		l.lineTo(bx, by)
	}
	if style == lineSolid {
		seg(x0, y0, x1, y1)
		l.flush()
		return
	}
	on, off := max(4, 3*w), max(3, 2*w)
	if style == lineDotted {
		on, off = w, max(2, 1.6*w)
	}
	dx, dy := x1-x0, y1-y0
	n := float32(math.Hypot(float64(dx), float64(dy)))
	if n == 0 {
		return
	}
	// Only where it shows: an extended line runs thousands of DIPs on.
	v := p.Visible()
	t0, t1 := clipSeg(x0, y0, x1, y1, v.X-w, v.Y-w, v.X+v.W+w, v.Y+v.H+w)
	if t1 <= t0 {
		return
	}
	period := on + off
	start := float32(math.Floor(float64(t0*n/period))) * period
	for s := start; s < t1*n; s += period {
		a, b := max(s, 0)/n, min(s+on, n)/n
		if b <= a {
			continue
		}
		seg(x0+dx*a, y0+dy*a, x0+dx*b, y0+dy*b)
	}
	l.flush()
}

// clipSeg is the part of the segment from x0, y0 to x1, y1 inside the box
// l, t, r, b, as fractions of it (Liang–Barsky); t1 <= t0 for none.
func clipSeg(x0, y0, x1, y1, l, t, r, b float32) (float32, float32) {
	t0, t1 := float32(0), float32(1)
	dx, dy := x1-x0, y1-y0
	for _, e := range [][2]float32{{-dx, x0 - l}, {dx, r - x0}, {-dy, y0 - t}, {dy, b - y0}} {
		p, q := e[0], e[1]
		if p == 0 {
			if q < 0 {
				return 1, 0
			}
			continue
		}
		u := q / p
		if p < 0 {
			t0 = max(t0, u)
		} else {
			t1 = min(t1, u)
		}
	}
	return t0, t1
}

// extended is the segment from x0, y0 to x1, y1 run on as d extends.
func extended(d *drawing, x0, y0, x1, y1 float32) (float32, float32, float32, float32) {
	l, r := extends(d)
	if x1 < x0 { // left and right are on the screen
		l, r = r, l
	}
	if r {
		x1, y1 = far(x0, y0, x1, y1)
	}
	if l {
		x0, y0 = far(x1, y1, x0, y0)
	}
	return x0, y0, x1, y1
}

// strokeBox draws the edges of r in d's line.
func strokeBox(p *ui.Painter, r ui.Rect, w float32, c ui.Color, style int) {
	if style == lineSolid {
		p.Stroke(r, c, 0, w)
		return
	}
	strokeLine(p, r.X, r.Y, r.X+r.W, r.Y, w, c, style)
	strokeLine(p, r.X+r.W, r.Y, r.X+r.W, r.Y+r.H, w, c, style)
	strokeLine(p, r.X+r.W, r.Y+r.H, r.X, r.Y+r.H, w, c, style)
	strokeLine(p, r.X, r.Y+r.H, r.X, r.Y, w, c, style)
}

// lineText writes d's text along the line from x0, y0 to x1, y1: at its
// start, middle or end, above, on or below it, turned with it so as to read
// left to right.
func lineText(p *ui.Painter, d *drawing, x0, y0, x1, y1 float32, c ui.Color) {
	if d.Text == "" {
		return
	}
	if x1 < x0 || x1 == x0 && y1 > y0 {
		x0, y0, x1, y1 = x1, y1, x0, y0
	}
	sp := textSpan(d, d.Text, c)
	w, h := p.MeasureText(0, sp)
	f := map[int]float32{textLeft: 0, textCenter: 0.5, textRight: 1}[textH(d)]
	ox, oy := x0+(x1-x0)*f, y0+(y1-y0)*f
	angle := float32(math.Atan2(float64(y1-y0), float64(x1-x0)) * 180 / math.Pi)
	tx := map[int]float32{textLeft: 4, textCenter: -w / 2, textRight: -w - 4}[textH(d)]
	ty := map[int]float32{textAbove: -h - 3, textMiddle: -h / 2, textBelow: 3}[textV(d)]
	p.Transform(ui.Rotation(angle), ox, oy, func() {
		if textV(d) == textMiddle { // over the line: the chart's background behind it
			p.Fill(ui.Rect{X: ox + tx - 3, Y: oy + ty, W: w + 6, H: h}, c.Alpha(0.12), 3)
		}
		p.RichText(ox+tx, oy+ty, 0, sp)
	})
}

// boxText writes d's text in the box r, as its alignment has it.
func boxText(p *ui.Painter, d *drawing, r ui.Rect, c ui.Color) {
	if d.Text == "" {
		return
	}
	sp := textSpan(d, d.Text, c)
	w, h := p.MeasureText(r.W-8, sp)
	x := map[int]float32{textLeft: r.X + 4, textCenter: r.X + (r.W-w)/2, textRight: r.X + r.W - w - 4}[textH(d)]
	y := map[int]float32{textAbove: r.Y + 4, textMiddle: r.Y + (r.H-h)/2, textBelow: r.Y + r.H - h - 4}[textV(d)]
	p.RichText(x, y, max(r.W-8, 1), sp)
}

// fibText is the label of level l at price pr.
func fibText(d *drawing, l fibLevel, pr float64, dec int) string {
	s := ""
	switch d.LevelText {
	case 0:
		s = strconv.FormatFloat(l.V, 'f', -1, 64)
	case 1:
		s = strconv.FormatFloat(l.V*100, 'f', -1, 64) + "%"
	}
	if statsOn(d) {
		if s != "" {
			s += " "
		}
		s += "(" + formatPrice(pr, dec) + ")"
	}
	return s
}

// trendStats reads out a trend line from p0 to p1: how far it goes in
// price and percent, over how many bars and how long, and at what angle.
func trendStats(m mapper, p0, p1 point, dec int, angle float64) []string {
	move, pct := p1.P-p0.P, 0.0
	if p0.P != 0 {
		pct = move / p0.P * 100
	}
	bars := int(math.Round(m.idx(p1.T) - m.idx(p0.T)))
	return []string{
		fmt.Sprintf("%s (%+.2f%%)", formatSigned(move, dec), pct),
		lang.F("%d 根K线 · %s · %.1f°", bars, spanText(p1.T-p0.T), angle),
	}
}
