package main

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"candle/lang"
)

// What the user keeps by name, after TradingView's templates and layouts:
// a template is a chart's indicators, put on any chart from 指标; a
// workspace the layout as it is, each chart's market, bars and indicators,
// the splits and what the charts sync, put back from the layout menu.

type indTemplate struct {
	Name       string
	Indicators []indicator
}

type workspace struct {
	Name   string
	Layout string `json:",omitempty"`
	Charts []chartSettings
	Active int
	Sync   syncSettings
	Splits [][]float64 `json:",omitempty"` // the layout's, as dragged
}

// cloneChart is cs sharing nothing with it.
func cloneChart(cs chartSettings) chartSettings {
	cs.Indicators = slices.Clone(cs.Indicators)
	for j := range cs.Indicators {
		cs.Indicators[j] = cloneIndicator(cs.Indicators[j])
	}
	return cs
}

// fixKept fits what a damaged file holds: no name, a name kept twice (the
// last wins, as saving again under it does), charts too few for the
// layout or more than there can be.
func fixKept(s *settings) {
	s.Templates = keepNamed(s.Templates, func(t *indTemplate) string { return t.Name })
	for i := range s.Templates {
		s.Templates[i].Indicators = fixIndicators(s.Templates[i].Indicators)
	}
	s.Workspaces = keepNamed(s.Workspaces, func(w *workspace) string { return w.Name })
	s.Workspaces = slices.DeleteFunc(s.Workspaces, func(w workspace) bool { return len(w.Charts) == 0 })
	for i := range s.Workspaces {
		w := &s.Workspaces[i]
		w.Charts = w.Charts[:min(len(w.Charts), maxCells)]
		for j := range w.Charts {
			w.Charts[j].fix()
		}
		if _, n := layoutOf(w.Layout); n == 1 {
			w.Layout = "" // one chart, or a name this version does not know
		}
		_, n := layoutOf(w.Layout)
		for len(w.Charts) < n {
			w.Charts = append(w.Charts, cloneChart(w.Charts[0]))
		}
		w.Active = min(max(w.Active, 0), n-1)
	}
	if !slices.ContainsFunc(s.Workspaces, func(w workspace) bool { return w.Name == s.Workspace }) {
		s.Workspace = ""
	}
}

// keepNamed drops those with no name and, of those named alike, the earlier.
func keepNamed[T any](list []T, name func(*T) string) []T {
	out := make([]T, 0, len(list))
	for i := range list {
		n := name(&list[i])
		if strings.TrimSpace(n) == "" {
			continue
		}
		out = slices.DeleteFunc(out, func(o T) bool { return name(&o) == n })
		out = append(out, list[i])
	}
	return out
}

// putNamed is list with v in place of the one named alike, else at the end.
func putNamed[T any](list []T, v T, name func(*T) string) []T {
	list = slices.Clone(list)
	if i := slices.IndexFunc(list, func(o T) bool { return name(&o) == name(&v) }); i >= 0 {
		list[i] = v
		return list
	}
	return append(list, v)
}

func templateName(t *indTemplate) string { return t.Name }
func workspaceName(w *workspace) string  { return w.Name }

// saveTemplate keeps the active chart's indicators as name.
func (a *application) saveTemplate(name string) {
	name = strings.TrimSpace(name)
	if name == "" || len(a.indicators) == 0 {
		return
	}
	tp := indTemplate{Name: name, Indicators: cloneChart(chartSettings{Indicators: a.indicators}).Indicators}
	a.set.Templates = putNamed(a.set.Templates, tp, templateName)
	a.save()
}

// applyTemplate puts tp's indicators on the chart in place of its own, as
// TradingView's templates do; adding, after its own.
func (a *application) applyTemplate(tp indTemplate, adding bool) {
	add := cloneChart(chartSettings{Indicators: tp.Indicators}).Indicators
	if !adding {
		a.indicators = fixIndicators(add)
		clear(a.series)
		clear(a.paneViews)
		a.save()
		return
	}
	for i := range add {
		add[i].ID = 0 // numbered after the chart's own
	}
	a.indicators = fixIndicators(append(slices.Clone(a.indicators), add...))
	a.save()
}

// saveWorkspace keeps the layout as name: the charts it shows and how.
func (a *application) saveWorkspace(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	n := a.shownCells()
	w := workspace{Name: name, Layout: a.set.Layout, Active: min(a.active, n-1), Sync: a.set.Sync,
		Splits: slices.Clone(a.set.Splits[a.layoutID()])}
	for _, c := range a.cells[:n] {
		w.Charts = append(w.Charts, cloneChart(c.saved()))
	}
	a.set.Workspaces = putNamed(a.set.Workspaces, w, workspaceName)
	a.set.Workspace = name
	a.save()
}

// loadWorkspace shows the charts w keeps in place of those there: the
// layout's charts stop asking for their bars and w's start.
func (a *application) loadWorkspace(w workspace) {
	for _, c := range a.cells[:a.shownCells()] {
		a.on(c, a.park)
	}
	a.cells = a.cells[:0:0]
	for _, cs := range w.Charts {
		a.cells = append(a.cells, newCell(cloneChart(cs), a.set.Chart))
	}
	a.set.Layout, a.set.Sync, a.set.Workspace, a.maxed = w.Layout, w.Sync, w.Name, -1
	a.setSplitsOf(a.layoutID(), w.Splits)
	a.active = w.Active
	a.cell = a.activeCell()
	for _, c := range a.cells[:a.shownCells()] {
		a.on(c, func() { a.open(c.symbol, c.interval) })
	}
	a.save()
}

func (a *application) setSplitsOf(id string, all [][]float64) {
	splits := maps.Clone(a.set.Splits)
	if splits == nil {
		splits = map[string][][]float64{}
	}
	if all == nil {
		delete(splits, id)
	} else {
		splits[id] = slices.Clone(all)
	}
	a.set.Splits = splits
}

// defaultTemplateName is the indicators' short names, as a name to start from.
func (a *application) defaultTemplateName() string {
	var parts []string
	for _, ind := range a.indicators {
		if k, ok := kindOf(ind.Kind); ok {
			parts = append(parts, k.short)
		}
	}
	name := strings.Join(parts, " + ")
	if r := []rune(name); len(r) > 40 {
		name = string(r[:40]) + "…"
	}
	return name
}

// templates, at the top of the list of indicators, are those kept: a
// click puts one on the chart; 存为模板 names the chart's to keep.
func (a *application) templates(c *ui.Context, t *ui.Theme, k palette) {
	ui.Row(c).FillWidth().Padding(20, 12, 6, 0).Gap(6).AlignItems(ui.Center).Children(func() { // as section's
		ui.Text(c, lang.T("指标模板")).FontSize(12).TextColor(t.TextMuted).Grow(1)
		if !a.tplNaming && len(a.indicators) > 0 && ui.Button(c, lang.T("存为模板")).Label(lang.T("把图上的指标存为模板")).Clicked() {
			a.tplNaming, a.tplName, a.tplFocus = true, a.defaultTemplateName(), true
		}
	})
	if a.tplNaming {
		taken := slices.ContainsFunc(a.set.Templates, func(o indTemplate) bool { return o.Name == strings.TrimSpace(a.tplName) })
		nameRow(c, k, "tpl-name", &a.tplName, &a.tplFocus, "模板名称", taken, func(keep bool) {
			if keep {
				a.saveTemplate(a.tplName)
			}
			a.tplNaming = false
		})
	}
	if len(a.set.Templates) == 0 && !a.tplNaming {
		ui.Text(c, lang.T("把图上的一组指标存下来，换个图表一点就全套加上。")).FontSize(12).TextColor(t.TextMuted).Padding(4, 12).FillWidth()
	}
	for i, tp := range a.set.Templates {
		row := ui.Row(c.Key(fmt.Sprintf("tpl-%d", i))).FillWidth().Height(40).Shrink(0).Padding(0, 12).Gap(12).AlignItems(ui.Center).
			Radius(6).Cursor(ui.CursorPointer).Label(lang.F("套用模板 %s", tp.Name))
		if row.Hovered() {
			row.Background(k.hover)
		}
		row.Children(func() {
			ui.Text(c, tp.Name).FontSize(13).FontWeight(600).Shrink(0).MaxWidth(200).SingleLine()
			var parts []string
			for _, ind := range tp.Indicators {
				if kind, ok := kindOf(ind.Kind); ok {
					parts = append(parts, kind.short)
				}
			}
			ui.Text(c, strings.Join(parts, " · ")).FontSize(12).TextColor(t.TextMuted).Grow(1).MinWidth(0).SingleLine()
			if legendButton(c, t, k, fmt.Sprintf("tpl-add-%d", i), lang.F("追加模板 %s", tp.Name), plusIcon).Clicked() {
				a.afterBuild(func() { a.applyTemplate(tp, true) })
			}
			if legendButton(c, t, k, fmt.Sprintf("tpl-del-%d", i), lang.F("删除模板 %s", tp.Name), trashIcon).Clicked() {
				name := tp.Name
				a.later = append(a.later, func() {
					a.set.Templates = slices.DeleteFunc(slices.Clone(a.set.Templates), func(o indTemplate) bool { return o.Name == name })
					a.save()
				})
			}
		})
		if row.Clicked() {
			a.afterBuild(func() { a.applyTemplate(tp, false) })
		}
	}
}

// workspaces, in the layout menu, are the layouts kept: a click shows one;
// 保存当前布局 names the one shown to keep.
func (a *application) workspaces(c *ui.Context, t *ui.Theme, k palette) {
	ui.Box(c).FillWidth().Height(1).Margin(6, 0).Background(k.line)
	ui.Row(c).FillWidth().Padding(0, 6, 4, 6).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Text(c, lang.T("保存的布局")).FontSize(12).TextColor(t.TextMuted).Grow(1)
		if !a.wsNaming && ui.Button(c, lang.T("保存当前布局")).Label(lang.T("把现在的布局和每个图表存下来")).Clicked() {
			// The one opened last, to save it again as it is now; else a new one.
			name := a.set.Workspace
			if name == "" {
				name = lang.F("我的布局 %d", len(a.set.Workspaces)+1)
			}
			a.wsNaming, a.wsName, a.wsFocus = true, name, true
		}
	})
	if a.wsNaming {
		taken := slices.ContainsFunc(a.set.Workspaces, func(o workspace) bool { return o.Name == strings.TrimSpace(a.wsName) })
		nameRow(c, k, "ws-name", &a.wsName, &a.wsFocus, "布局名称", taken, func(keep bool) {
			if keep {
				a.saveWorkspace(a.wsName)
			}
			a.wsNaming = false
		})
	}
	if len(a.set.Workspaces) == 0 && !a.wsNaming {
		ui.Text(c, lang.T("存下布局、每个图表的品种、周期和指标，以后一点就回来。")).FontSize(12).TextColor(t.TextMuted).Padding(0, 6, 4, 6).MaxWidth(300)
	}
	for i, w := range a.set.Workspaces {
		root, _ := layoutOf(w.Layout)
		current := w.Name == a.set.Workspace
		row := ui.Row(c.Key(fmt.Sprintf("ws-%d", i))).FillWidth().Height(32).Padding(0, 6).Gap(8).AlignItems(ui.Center).
			Radius(4).Cursor(ui.CursorPointer).Label(lang.F("打开布局 %s", w.Name))
		switch {
		case current:
			row.Background(k.chosen)
		case row.Hovered():
			row.Background(k.hover)
		}
		row.Children(func() {
			col := t.TextMuted
			if current {
				col = k.accent
			}
			layoutIcon(c, root, col)
			ui.Text(c, w.Name).FontSize(13).Grow(1).MinWidth(0).MaxWidth(220).SingleLine()
			if legendButton(c, t, k, fmt.Sprintf("ws-del-%d", i), lang.F("删除布局 %s", w.Name), trashIcon).Clicked() {
				name := w.Name
				a.later = append(a.later, func() {
					a.set.Workspaces = slices.DeleteFunc(slices.Clone(a.set.Workspaces), func(o workspace) bool { return o.Name == name })
					if a.set.Workspace == name {
						a.set.Workspace = ""
					}
					a.save()
				})
			}
		})
		if row.Clicked() {
			a.layoutMenu = false
			a.afterBuild(func() { a.loadWorkspace(w) })
		}
	}
}

// nameRow is a name being typed for what is kept, with 保存 and 取消;
// Enter keeps it, Escape does not. A name taken already is saved over:
// the button says 覆盖.
func nameRow(c *ui.Context, k palette, key string, name *string, focus *bool, label string, taken bool, done func(keep bool)) {
	ui.Row(c).FillWidth().Padding(0, 6, 6, 6).Gap(6).AlignItems(ui.Center).Children(func() {
		in := ui.TextInput(c.Key(key), name).Grow(1).MinWidth(0).Height(30).FontSize(13).Background(k.field).Label(lang.T(label))
		if *focus {
			in.Focus()
			*focus = false
		}
		submitted := in.Submitted()
		saveLabel := "保存"
		if taken {
			saveLabel = "覆盖"
		}
		save := ui.Button(c.Key(key+"-save"), lang.T(saveLabel)).Label(lang.T(saveLabel)).Clicked()
		cancel := ui.Button(c, lang.T("取消")).Clicked()
		switch {
		case (submitted || save) && strings.TrimSpace(*name) != "":
			done(true)
		case cancel:
			done(false)
		}
	})
}
