package main

import (
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"candle/lang"
)

// A drawing's settings, after TradingView's: its style, its text, where its
// points are, and on which bar sizes it shows. Double-clicking a drawing,
// or the gear on its bar, opens them. As the chart settings, what changes
// shows at once; 确认 keeps it as one step to undo, 取消, Escape or a click
// beside the dialog puts it back. 存为默认 makes the tool draw so from then on.

const timeLayout = "2006-01-02 15:04"

var (
	fibSizes   = []option{{"10", "10"}, {"11", "11"}, {"12", "12"}, {"14", "14"}, {"16", "16"}}
	riskModes  = []option{{"pct", "账户的 %"}, {"amt", "金额"}}
	vlineTextH = []option{{"1", "底部"}, {"2", "中间"}, {"3", "顶部"}}
	vlineTextV = []option{{"1", "左侧"}, {"2", "线上"}, {"3", "右侧"}}
)

// drawTabs are the tabs of a drawing of kind's settings.
func drawTabs(kind string) []string {
	var tabs []string
	if kind != "text" {
		tabs = append(tabs, "样式")
	}
	if hasText(kind) {
		tabs = append(tabs, "文字")
	}
	return append(tabs, "坐标|画线", "可见性")
}

func (a *application) drawingByID(id int64) *drawing {
	ds := a.set.Drawings[a.symbol]
	for i := range ds {
		if ds[i].ID == id {
			return &ds[i]
		}
	}
	return nil
}

// openDrawing opens the settings of drawing id, selecting it.
func (a *application) openDrawing(id int64) {
	d := a.drawingByID(id)
	if d == nil {
		return
	}
	before := cloneDrawings(a.set.Drawings[a.symbol])
	// What showed by default becomes what the dialog edits.
	if d.Kind == "fib" && len(d.Levels) == 0 {
		d.Levels, d.OneColor = legacyFib(), true
	}
	if isPosition(d.Kind) {
		d.Account = account(d)
		if d.RiskAmt {
			d.Risk = riskMoney(d)
		} else {
			d.Risk = riskPct(d)
		}
	}
	a.sel, a.drawEdit, a.drawBefore, a.drawDialog, a.drawEditing, a.drawTab = id, id, before, true, true, 0
	a.drawOpened = cloneDrawings(a.set.Drawings[a.symbol])
	clear(a.ptTimes)
}

// closeDrawing keeps the drawing as set, one step to undo, or puts back the
// market's drawings as they were.
func (a *application) closeDrawing(keep bool) {
	switch {
	case !keep || reflect.DeepEqual(a.drawOpened, a.set.Drawings[a.symbol]): // nothing changed: not even the defaults made plain as it opened
		a.setDrawings(a.drawBefore)
	default:
		a.pushUndo(a.drawBefore)
		a.lastEdit = ""
	}
	a.drawDialog, a.drawEditing, a.drawEdit, a.drawBefore, a.drawOpened = false, false, 0, nil, nil
	a.save()
}

func (a *application) drawingDialog(c *ui.Context, t *ui.Theme, k palette) {
	if a.drawEditing && !a.drawDialog { // closed by Escape or a click beside it
		a.closeDrawing(false)
	}
	if !a.drawDialog {
		return
	}
	d := a.drawingByID(a.drawEdit)
	if d == nil {
		a.drawDialog, a.drawEditing = false, false
		return
	}
	tool, _ := toolOf(d.Kind)
	tabs := drawTabs(d.Kind)
	a.drawTab = min(max(a.drawTab, 0), len(tabs)-1)
	_, wh := c.Size()
	ui.DialogBase(c.Key("drawing-dialog"), &a.drawDialog, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.45))
		panel.Width(540).Height(min(640, wh-48)).Radius(10).Background(k.bg).Border(1, k.line).
			Shadow(0, 12, 40, 0, ui.RGBA(0, 0, 0, 0.35)).Label(lang.F("%s 设置", lang.T(tool.label)))
		ui.Column(c).FillWidth().FillHeight().Children(func() {
			dialogHeader(c, t, k, tool.label, "关闭画线设置", func() { a.closeDrawing(false) })
			ui.Row(c).FillWidth().Shrink(0).Padding(0, 24, 6, 24).Children(func() {
				shown := make([]string, len(tabs))
				for i, tab := range tabs {
					shown[i] = lang.T(tab)
				}
				ui.Tabs(c.Key("drawing-tabs"), &a.drawTab, shown...)
			})
			tab := tabs[a.drawTab]
			ui.Scroll(c.Key("drawing-tab-" + tab)).Grow(1).FillWidth().MinHeight(0).Children(func() {
				ui.Column(c).FillWidth().Padding(4, 28, 20, 24).Children(func() {
					switch tab {
					case "样式":
						a.drawStylePage(c, t, k, d)
					case "文字":
						a.drawTextPage(c, t, k, d)
					case "坐标|画线":
						a.drawPointsPage(c, t, d)
					case "可见性":
						a.drawVisibilityPage(c, t, d)
					}
				})
			})
			a.drawDialogButtons(c, k, d, tool)
		})
	})
}

func (a *application) drawDialogButtons(c *ui.Context, k palette, d *drawing, tool drawTool) {
	ui.Row(c).FillWidth().Height(64).Shrink(0).Padding(0, 20).Gap(10).AlignItems(ui.Center).BorderWidth(1, 0, 0, 0).BorderColor(k.line).Children(func() {
		def, kept := a.set.DrawDefaults[d.Kind]
		isDefault := kept && reflect.DeepEqual(styleOf(def), styleOf(*d))
		label := "存为默认"
		if isDefault {
			label = "已是默认"
		}
		if ui.Button(c.Key("drawing-save-default"), lang.T(label)).Tooltip(lang.F("以后新画的%s都用这个样式", lang.T(tool.label))).Clicked() && !isDefault {
			if a.set.DrawDefaults == nil {
				a.set.DrawDefaults = map[string]drawing{}
			}
			a.set.DrawDefaults[d.Kind] = styleOf(*d)
			a.save()
		}
		if ui.Button(c.Key("drawing-reset"), lang.T("恢复默认")).Tooltip(lang.T("换回默认的样式，位置不变")).Clicked() {
			fresh := a.newDrawing(d.Kind, d.ID, 0)
			fresh.Pts, fresh.Locked, fresh.Text = d.Pts, d.Locked, d.Text
			*d = fresh
			if d.Kind == "fib" && len(d.Levels) == 0 {
				d.Levels = defaultFibLevels()
			}
		}
		ui.Box(c).Grow(1)
		if ui.Button(c, lang.T("取消")).Clicked() {
			a.closeDrawing(false)
		}
		if ui.PrimaryButton(c, lang.T("确认")).Clicked() {
			a.closeDrawing(true)
		}
	})
}

// triCheck is a check box for a switch whose default is def.
func triCheck(c *ui.Context, key, name string, v *tri, def bool) {
	on := v.on(def)
	ui.Row(c).FillWidth().MinHeight(36).AlignItems(ui.Center).Children(func() {
		ui.Checkbox(c.Key(key), &on, lang.T(name)).Label(lang.T(name))
	})
	if on != v.on(def) {
		*v = triOf(on)
	}
}

func boolCheck(c *ui.Context, key, name string, v *bool) {
	ui.Row(c).FillWidth().MinHeight(36).AlignItems(ui.Center).Children(func() {
		ui.Checkbox(c.Key(key), v, lang.T(name)).Label(lang.T(name))
	})
}

// intChoice is a drop-down of opts keyed by numbers, for *v.
func (a *application) intChoice(c *ui.Context, key string, opts []option, v *int, def int) ui.Element {
	cur := *v
	if cur == 0 {
		cur = def
	}
	return a.dropdown(c, key, opts, strconv.Itoa(cur), func(s string) { *v, _ = strconv.Atoi(s) }).Width(120).MinWidth(120)
}

func (a *application) drawStylePage(c *ui.Context, t *ui.Theme, k palette, d *drawing) {
	id := strconv.FormatInt(d.ID, 10)
	key := func(s string) string { return "画线设置 " + s + " " + id }
	col := d.Color.or(k.accent)
	lineRow := func(name string) {
		row(c, name, func() {
			a.well(c, key("颜色"), col, func(h hexColor) { d.Color = h }).Label(lang.F("%s颜色", lang.T(name))).Tooltip(lang.T("颜色"))
			widthButtons(c, t, k, key("线宽"), d.Width, func(w float64) { d.Width = w })
			a.dropdown(c, "线型", lineStyles, strconv.Itoa(d.Style), func(v string) { d.Style, _ = strconv.Atoi(v) }).Width(96).MinWidth(96)
		})
	}
	switch {
	case d.Kind == "range":
		row(c, "颜色", func() {
			a.well(c, key("上涨"), d.Color.or(ui.Hex(barUp)), func(h hexColor) { d.Color = h }).Label(lang.T("上涨颜色")).Tooltip(lang.T("向上"))
			a.well(c, key("下跌"), d.Color2.or(ui.Hex(barDown)), func(h hexColor) { d.Color2 = h }).Label(lang.T("下跌颜色")).Tooltip(lang.T("向下"))
		})
		row(c, "箭头", func() { widthButtons(c, t, k, key("线宽"), d.Width, func(w float64) { d.Width = w }) })
		triCheck(c, key("背景"), "背景", &d.Back, true)
		triCheck(c, key("读数"), "显示读数（涨跌、K线数、时长）", &d.Stats, true)
		row(c, "读数字号", func() {
			size := 12
			if d.Size > 0 {
				size = int(d.Size)
			}
			a.dropdown(c, "读数字号", drawTextSizes, strconv.Itoa(size), func(v string) { d.Size, _ = strconv.ParseFloat(v, 64) }).Width(96).MinWidth(96)
		})
		return
	case isPosition(d.Kind):
		row(c, "颜色", func() {
			a.well(c, key("止盈"), d.Color.or(ui.Hex(barUp)), func(h hexColor) { d.Color = h }).Label(lang.T("止盈颜色")).Tooltip(lang.T("止盈"))
			a.well(c, key("止损"), d.Color2.or(ui.Hex(barDown)), func(h hexColor) { d.Color2 = h }).Label(lang.T("止损颜色")).Tooltip(lang.T("止损"))
		})
		row(c, "账户金额", func() {
			ui.NumberInput(c.Key(key("账户")), &d.Account, 1, 1e12, 100).Width(170).Shrink(0).Label(lang.T("账户金额"))
		})
		row(c, "风险", func() {
			step, hi := 0.5, 100.0
			if d.RiskAmt {
				step, hi = 10, 1e12
			}
			ui.NumberInput(c.Key(key("风险")), &d.Risk, 0.01, hi, step).Width(170).Shrink(0).Label(lang.T("风险"))
			mode := map[bool]string{false: "pct", true: "amt"}[d.RiskAmt]
			a.dropdown(c, "风险单位", riskModes, mode, func(v string) {
				if amt := v == "amt"; amt != d.RiskAmt {
					money := riskMoney(d)
					d.RiskAmt = amt
					if amt {
						d.Risk = money
					} else {
						d.Risk = money / account(d) * 100
					}
				}
			}).Width(110).MinWidth(110)
		})
		if q, ok := positionSize(d); ok {
			row(c, "数量", func() { ui.Text(c, qtyText(q)).FontSize(13) })
		}
		triCheck(c, key("读数"), "显示读数（价格、涨跌、盈亏、盈亏比）", &d.Stats, true)
		return
	case d.Kind == "fib":
		triCheck(c, key("趋势线"), "趋势线（两点之间的虚线）", &d.Trend, true)
		lineRow("档位线")
	default:
		lineRow("线条")
	}
	if hasExtend(d.Kind) {
		row(c, "延伸", func() {
			ui.Checkbox(c.Key(key("向左")), &d.ExtL, lang.T("向左")).Label(lang.T("向左延伸"))
			if d.Kind == "ray" {
				on := true
				ui.Checkbox(c.Key(key("向右")), &on, lang.T("向右")).Label(lang.T("向右延伸")).Disabled(true)
			} else {
				ui.Checkbox(c.Key(key("向右")), &d.ExtR, lang.T("向右")).Label(lang.T("向右延伸"))
			}
		})
	}
	if hasBack(d.Kind) {
		on := backOn(d)
		ui.Row(c).FillWidth().MinHeight(40).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Checkbox(c.Key(key("背景")), &on, lang.T("背景")).Label(lang.T("背景")).Width(150).Shrink(0)
			if d.Kind == "fib" {
				o := strconv.FormatFloat(float64(fibOpacity(d)), 'f', -1, 32)
				a.dropdown(c, "背景不透明度", opacities, o, func(v string) { d.Opacity, _ = strconv.ParseFloat(v, 64) }).Width(96).MinWidth(96).Tooltip(lang.T("不透明度"))
			} else {
				a.well(c, key("背景色"), backColor(d, col), func(h hexColor) { d.Fill = h }).Label(lang.T("背景颜色")).Tooltip(lang.T("背景颜色（可调透明度）"))
			}
		})
		if on != backOn(d) {
			d.Back = triOf(on)
		}
	}
	if hasMid(d.Kind) {
		triCheck(c, key("中线"), "中线", &d.Mid, d.Kind == "channel")
	}
	if hasLabels(d.Kind) {
		name := map[string]string{"hline": "价格标签（价格轴上）", "hray": "价格标签（价格轴上）", "vline": "时间标签（时间轴上）"}[d.Kind]
		if name == "" {
			name = "坐标轴标签（各点的价格和时间）"
		}
		triCheck(c, key("标签"), name, &d.Labels, labelsOn(&drawing{Kind: d.Kind}))
	}
	if d.Kind == "trend" || d.Kind == "ray" {
		triCheck(c, key("统计"), "显示统计（涨跌、K线数、时长、角度）", &d.Stats, false)
	}
	if d.Kind == "fib" {
		a.fibSettings(c, t, k, d, key, col)
	}
}

// fibSettings are a retracement's: its levels, how they read, and which
// way they run.
func (a *application) fibSettings(c *ui.Context, t *ui.Theme, k palette, d *drawing, key func(string) string, col ui.Color) {
	boolCheck(c, key("反转"), "反转（0 和 1 对调）", &d.Reverse)
	triCheck(c, key("价格"), "显示价格", &d.Stats, true)
	row(c, "档位文字", func() {
		a.dropdown(c, "档位文字", levelTexts, strconv.Itoa(d.LevelText), func(v string) { d.LevelText, _ = strconv.Atoi(v) }).Width(110).MinWidth(110)
		a.dropdown(c, "标签位置", labelSides, strconv.Itoa(d.LabelSide), func(v string) { d.LabelSide, _ = strconv.Atoi(v) }).Width(96).MinWidth(96).Tooltip(lang.T("标签在左边还是右边"))
		a.dropdown(c, "标签字号", fibSizes, strconv.Itoa(int(fibSize(d))), func(v string) { d.Size, _ = strconv.ParseFloat(v, 64) }).Width(80).MinWidth(80).Tooltip(lang.T("字号"))
	})
	boolCheck(c, key("统一颜色"), "各档位都用线条的颜色", &d.OneColor)
	section(c, t, "档位")
	id := d.ID
	half := (len(d.Levels) + 1) / 2
	ui.Row(c).FillWidth().Gap(18).Children(func() {
		for col0 := range 2 {
			ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(4).Children(func() {
				for i := col0 * half; i < min((col0+1)*half, len(d.Levels)); i++ {
					lv := &d.Levels[i]
					lvKey, name := "档位 "+strconv.Itoa(i+1), lang.F("档位 %d", i+1)
					ui.Row(c).FillWidth().Height(34).Gap(6).AlignItems(ui.Center).Children(func() {
						on := !lv.Off
						ui.Checkbox(c.Key(key(lvKey+" 显示")), &on, "").Label(lang.F("%s 显示", name)).Shrink(0)
						lv.Off = !on
						ui.NumberInput(c.Key(key(lvKey+" 数值")), &lv.V, -100, 100, 0.001).Grow(1).MinWidth(96).Label(lang.F("%s 数值", name))
						if !d.OneColor {
							a.well(c, key(lvKey+" 颜色"), lv.C.or(col), func(h hexColor) {
								if dd := a.drawingByID(id); dd != nil && i < len(dd.Levels) {
									dd.Levels[i].C = h
								}
							}).Label(lang.F("%s 颜色", name))
						}
						if legendButton(c, t, k, key(lvKey+" 删除"), lang.F("%s 删除", name), clearIcon).Clicked() {
							a.later = append(a.later, func() {
								if dd := a.drawingByID(id); dd != nil && i < len(dd.Levels) {
									dd.Levels = slices.Delete(dd.Levels, i, i+1)
								}
							})
						}
					})
				}
			})
		}
	})
	ui.Row(c).FillWidth().Padding(10, 0, 0, 0).Gap(8).Children(func() {
		if ui.Button(c.Key(key("添加档位")), lang.T("添加档位")).Clicked() {
			a.later = append(a.later, func() {
				if dd := a.drawingByID(id); dd != nil {
					next := 1.0
					for _, l := range dd.Levels {
						next = max(next, l.V+0.5)
					}
					dd.Levels = append(dd.Levels, fibLevel{V: next, C: hx("#787b86")})
				}
			})
		}
		if ui.Button(c.Key(key("默认档位")), lang.T("恢复默认档位")).Tooltip(lang.T("TradingView 的档位和颜色")).Clicked() {
			a.later = append(a.later, func() {
				if dd := a.drawingByID(id); dd != nil {
					dd.Levels, dd.OneColor = defaultFibLevels(), false
				}
			})
		}
	})
}

func (a *application) drawTextPage(c *ui.Context, t *ui.Theme, k palette, d *drawing) {
	id := strconv.FormatInt(d.ID, 10)
	key := func(s string) string { return "画线设置 " + s + " " + id }
	col := d.Color.or(k.accent)
	ui.TextArea(c.Key(key("文字")), &d.Text).FillWidth().Height(84).FontSize(13).Placeholder(lang.T("在画线旁边写点什么")).Label(lang.T("文字内容"))
	ui.Box(c).Height(8)
	row(c, "样式", func() {
		if d.Kind == "text" {
			a.well(c, key("文字颜色"), col, func(h hexColor) { d.Color = h }).Label(lang.T("文字颜色")).Tooltip(lang.T("颜色"))
		} else {
			a.well(c, key("文字颜色"), textColor(d, col), func(h hexColor) { d.TextColor = h }).Label(lang.T("文字颜色")).Tooltip(lang.T("颜色"))
		}
		a.dropdown(c, "文字字号", drawTextSizes, strconv.Itoa(int(textSize(d))), func(v string) { d.Size, _ = strconv.ParseFloat(v, 64) }).Width(80).MinWidth(80).Tooltip(lang.T("字号"))
		ui.Checkbox(c.Key(key("粗体")), &d.Bold, lang.T("粗体")).Label(lang.T("粗体"))
		ui.Checkbox(c.Key(key("斜体")), &d.Italic, lang.T("斜体")).Label(lang.T("斜体"))
	})
	switch d.Kind {
	case "text":
		on := backOn(d)
		ui.Row(c).FillWidth().MinHeight(40).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Checkbox(c.Key(key("背景")), &on, lang.T("背景")).Label(lang.T("文字背景")).Width(150).Shrink(0)
			a.well(c, key("背景色"), backColor(d, col), func(h hexColor) { d.Fill = h }).Label(lang.T("背景颜色"))
		})
		if on != backOn(d) {
			d.Back = triOf(on)
		}
		ui.Row(c).FillWidth().MinHeight(40).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Checkbox(c.Key(key("边框")), &d.Border, lang.T("边框")).Label(lang.T("文字边框")).Width(150).Shrink(0)
			a.well(c, key("边框色"), d.Color2.or(col), func(h hexColor) { d.Color2 = h }).Label(lang.T("边框颜色"))
		})
	case "vline":
		row(c, "位置", func() {
			a.intChoice(c, "文字位置", vlineTextH, &d.TextH, textH(d))
			a.intChoice(c, "文字在线的哪边", vlineTextV, &d.TextV, textV(d))
		})
	case "rect":
		row(c, "位置", func() {
			a.intChoice(c, "文字水平位置", textHs, &d.TextH, textH(d))
			a.intChoice(c, "文字垂直位置", textBoxVs, &d.TextV, textV(d))
		})
	default:
		row(c, "位置", func() {
			a.intChoice(c, "文字水平位置", textHs, &d.TextH, textH(d))
			a.intChoice(c, "文字垂直位置", textVs, &d.TextV, textV(d))
		})
	}
}

// pointNames are what d's points are called, in the language shown, and whether each has a
// price and a time to set.
func pointNames(d *drawing) (names []string, price, when []bool) {
	switch d.Kind {
	case "long", "short":
		return []string{lang.T("入场"), lang.T("止盈"), lang.T("止损"), lang.T("右边界")}, []bool{true, true, true, false}, []bool{true, false, false, true}
	case "hline":
		return []string{lang.T("价格")}, []bool{true}, []bool{false}
	case "vline":
		return []string{lang.T("时间")}, []bool{false}, []bool{true}
	case "hray", "text":
		return []string{lang.T("位置")}, []bool{true}, []bool{true}
	}
	for i := range d.Pts {
		names = append(names, lang.F("点 %d", i+1))
		price, when = append(price, true), append(when, true)
	}
	return names, price, when
}

func (a *application) drawPointsPage(c *ui.Context, t *ui.Theme, d *drawing) {
	if d.Locked {
		ui.Text(c, lang.T("画线已锁定，解锁后才能改位置。")).FontSize(12).TextColor(t.TextMuted).Padding(8, 0)
	}
	names, price, when := pointNames(d)
	for i := range min(len(names), len(d.Pts)) {
		ui.Row(c).FillWidth().MinHeight(40).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, names[i]).FontSize(13).Width(56).Shrink(0)
			if price[i] {
				a.priceInput(c, d, i, names[i])
			}
			if when[i] {
				a.timeInput(c, d, i, names[i])
			}
		})
	}
	if isPosition(d.Kind) {
		fixPosition(d)
	}
	ui.Text(c, lang.T("时间按图表的时区，写法如 2026-11-15 09:30，落在最近的一根K线上。")).FontSize(12).TextColor(t.TextMuted).Padding(10, 0, 0, 0)
}

// pointText is the text typed for a point's key, from what it is until the
// dialog closes: kept as typed while it reads as nothing yet.
func (a *application) pointText(key, now string) *string {
	text := a.ptTimes[key]
	if text == nil {
		text = &now
		a.ptTimes[key] = text
	}
	return text
}

// priceInput sets the price of d's point i, as typed. A text input, not a
// NumberInput, whose field is too narrow for prices of eight digits.
func (a *application) priceInput(c *ui.Context, d *drawing, i int, name string) {
	key := "画线坐标 价格 " + strconv.FormatInt(d.ID, 10) + " " + strconv.Itoa(i)
	now := formatPrice(d.Pts[i].P, a.chartDecimals(d.Pts[i].P))
	text := a.pointText(key, now)
	ui.TextInput(c.Key(key), text).Width(170).Shrink(0).ReadOnly(d.Locked).Label(lang.F("%s 价格", name)).Tooltip(lang.T("价格"))
	if *text == now { // as it is: not rounded to what shows
		return
	}
	if v, err := strconv.ParseFloat(strings.TrimSpace(*text), 64); err == nil && !d.Locked && !math.IsInf(v, 0) {
		d.Pts[i].P = v
	}
}

// timeInput sets the time of d's point i, as typed in the chart's zone: on
// the bar nearest it, once it reads as a time.
func (a *application) timeInput(c *ui.Context, d *drawing, i int, name string) {
	key := "画线坐标 时间 " + strconv.FormatInt(d.ID, 10) + " " + strconv.Itoa(i)
	loc := a.set.Chart.loc()
	now := time.UnixMilli(d.Pts[i].T).In(loc).Format(timeLayout)
	text := a.pointText(key, now)
	ui.TextInput(c.Key(key), text).Width(170).Shrink(0).ReadOnly(d.Locked).Label(lang.F("%s 时间", name)).Tooltip(lang.T("时间，如 2026-11-15 09:30"))
	tm, err := time.ParseInLocation(timeLayout, strings.TrimSpace(*text), loc)
	if *text == now || err != nil || d.Locked {
		return
	}
	ms := tm.UnixMilli()
	if a.mapReady() {
		m := a.mapper()
		ms = m.timeAt(math.Round(m.idx(ms)))
	}
	d.Pts[i].T = ms
}

func (a *application) drawVisibilityPage(c *ui.Context, t *ui.Theme, d *drawing) {
	ui.Text(c, lang.T("在这些周期上显示：")).FontSize(13).Padding(8, 0)
	for _, iv := range intervals {
		on := !hiddenOn(d, iv.key)
		ui.Row(c).FillWidth().MinHeight(34).AlignItems(ui.Center).Children(func() {
			ui.Checkbox(c.Key("画线可见 "+iv.key), &on, lang.T(iv.label)).Label(lang.F("%s 显示", lang.T(iv.label)))
		})
		switch {
		case on && hiddenOn(d, iv.key):
			d.HideOn = slices.DeleteFunc(d.HideOn, func(s string) bool { return s == iv.key })
		case !on && !hiddenOn(d, iv.key):
			d.HideOn = append(d.HideOn, iv.key)
		}
	}
	if len(d.HideOn) == 0 {
		d.HideOn = nil
	}
	ui.Text(c, lang.T("在没勾的周期上看不到它，也点不到它。")).FontSize(12).TextColor(t.TextMuted).Padding(10, 0, 0, 0)
}
