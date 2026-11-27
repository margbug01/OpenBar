package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 存为模板 keeps the chart's indicators by name; on a chart with none, a
// click on the template puts them all back as they were set, and the file
// keeps the template.
func TestIndicatorTemplate(t *testing.T) {
	a := withIndicators("ma", "macd")
	a.file = filepath.Join(t.TempDir(), "settings.json")
	a.indicators[0].Params[0] = 33
	tt := newTester(a, true)
	click(t, tt, "指标")
	click(t, tt, "把图上的指标存为模板")
	shot(t, tt, "template-naming")
	click(t, tt, "保存")
	shot(t, tt, "templates")
	if len(a.set.Templates) != 1 || a.set.Templates[0].Name != "MA + MACD" {
		t.Fatalf("templates after 存为模板: %+v", a.set.Templates)
	}
	for _, ind := range append([]indicator(nil), a.indicators...) {
		a.deleteIndicator(ind.ID)
	}
	tt.Frame()
	click(t, tt, "套用模板 MA + MACD")
	if len(a.indicators) != 2 || a.indicators[0].Kind != "ma" || a.indicators[0].Params[0] != 33 || a.indicators[1].Kind != "macd" {
		t.Fatalf("after applying: %+v", a.indicators)
	}
	a.indicators[0].Params[0] = 7
	if a.set.Templates[0].Indicators[0].Params[0] != 33 {
		t.Fatal("the chart shares its indicators with the template")
	}
	if s := loadSettings(a.file); len(s.Templates) != 1 || len(s.Templates[0].Indicators) != 2 {
		t.Fatalf("read back %+v", s.Templates)
	}
}

// 保存当前布局 keeps the layout and its charts; after another layout and
// market, a click on it brings both back, the charts loading their bars.
func TestSavedLayout(t *testing.T) {
	a := layoutApp("2h", [2]string{"ETHUSDT", "5m"})
	a.file = filepath.Join(t.TempDir(), "settings.json")
	k, _ := kindOf("rsi")
	a.on(a.cells[1], func() { a.addIndicator(k) })
	tt := newTester(a, true)
	click(t, tt, "选择布局")
	click(t, tt, "把现在的布局和每个图表存下来")
	shot(t, tt, "layout-naming")
	click(t, tt, "保存")
	shot(t, tt, "saved-layouts")
	if len(a.set.Workspaces) != 1 || a.set.Workspaces[0].Name != "我的布局 1" || len(a.set.Workspaces[0].Charts) != 2 {
		t.Fatalf("workspaces after saving: %+v", a.set.Workspaces)
	}
	a.setLayout("s")
	a.open("SOLUSDT", "1d")
	tt.Frame()
	if !a.layoutMenu {
		click(t, tt, "选择布局")
	}
	tt.Advance(time.Second)
	click(t, tt, "打开布局 我的布局 1")
	if a.set.Layout != "2h" || a.shownCells() != 2 || len(a.cells) != 2 {
		t.Fatalf("layout %q, %d charts", a.set.Layout, len(a.cells))
	}
	if c := a.cells[0]; c.symbol != "BTCUSDT" || c.interval != "1h" {
		t.Fatalf("first chart %s %s", c.symbol, c.interval)
	}
	if c := a.cells[1]; c.symbol != "ETHUSDT" || c.interval != "5m" || len(c.indicators) != 1 || c.indicators[0].Kind != "rsi" || c.share == nil {
		t.Fatalf("second chart %s %s %+v, feed %v", c.symbol, c.interval, c.indicators, c.share)
	}
	if _, ok := a.feeds[a.feedKey("SOLUSDT", "1d")]; ok || len(a.feeds) != 2 {
		t.Fatalf("%d feeds, SOL's still asked for", len(a.feeds))
	}
	if s := loadSettings(a.file); s.Layout != "2h" || len(s.Workspaces) != 1 || s.Workspaces[0].Charts[1].Symbol != "ETHUSDT" {
		t.Fatalf("read back %q %+v", s.Layout, s.Workspaces)
	}
}

// Settings that fail to save say so on the chart till they do.
func TestSaveFailureShows(t *testing.T) {
	a := testApp()
	dir := t.TempDir()
	blocker := filepath.Join(dir, "file")
	os.WriteFile(blocker, nil, 0o644)
	a.file = filepath.Join(blocker, "settings.json") // its folder is a file
	tt := newTester(a, true)
	a.save()
	tt.Frame()
	shot(t, tt, "save-failed")
	if a.saveErr == "" || !tt.HasText("设置没存上") {
		t.Fatalf("a failed save shows nothing: %q", a.saveErr)
	}
	a.file = filepath.Join(dir, "settings.json")
	a.save()
	tt.Frame()
	if a.saveErr != "" || tt.HasText("设置没存上") {
		t.Fatal("the notice stays after the settings saved")
	}
}
