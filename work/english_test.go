package main

import (
	"strings"
	"testing"
	"unicode"

	"github.com/egoist/mygo/ui"

	"candle/lang"
)

// An English reader sees no Chinese: not on the chart, its menu, the
// watchlist or the search, nor in any dialog or any tab of one.
func TestEnglishShowsNoChinese(t *testing.T) {
	lang.Set("en")
	t.Cleanup(func() { lang.Set("") })
	a := testApp()
	a.view.spacing = 10
	ds := sampleDrawings(a)
	ds = append(ds, position(builtinDrawing("long"), ds[0].Pts[1], a.mapper()))
	for i := range ds {
		ds[i].ID, ds[i].Text = int64(i+1), "" // what the user writes is theirs
	}
	a.set.Drawings = map[string][]drawing{"BTCUSDT": ds}
	a.nextID = int64(len(ds))
	for i := range indKinds {
		a.addIndicator(&indKinds[i])
	}
	tt := newTester(a, true)
	seen := map[string]bool{}
	chinese := func(where string, texts []string) {
		t.Helper()
		for _, s := range texts {
			if han(s) && !seen[s] {
				seen[s] = true
				t.Errorf("%s: %q", where, s)
			}
		}
	}
	look := func(where string) {
		t.Helper()
		tt.Frame()
		tt.Frame()
		chinese(where, tt.Texts())
		shot(t, tt, "en-"+strings.NewReplacer(" ", "-", "›", "").Replace(where))
	}

	look("chart")
	tt.RightClickAt(chartLeft(a)+300, 300)
	chinese("chart menu", tt.Menu())
	tt.CloseMenu()

	click(t, tt, "Chart settings")
	for _, page := range []string{"Symbol", "Status line", "Scales", "Canvas", "About"} {
		click(t, tt, page)
		look("settings › " + page)
	}
	click(t, tt, "Cancel")

	click(t, tt, "Indicators")
	for range 12 {
		look("indicators")
		tt.Wheel(660, 500, 0, 300)
	}
	a.indList = false
	for _, ind := range a.indicators {
		a.openIndicator(ind.ID)
		look("indicator " + ind.Kind)
		a.closeIndicator(false)
	}

	for _, d := range ds {
		a.sel = d.ID
		look("drawing bar " + d.Kind)
		a.openDrawing(d.ID)
		for tab := range drawTabs(d.Kind) {
			a.drawTab = tab
			look("drawing " + d.Kind + " tab " + lang.T(drawTabs(d.Kind)[tab]))
		}
		a.closeDrawing(false)
	}
	a.sel = 0

	a.query = "pepe"
	look("search")
	a.query = ""

	a.openEditor("", newScript())
	look("script editor")
	a.closeEditor(true)
	tt.Key(0, ui.KeyEscape)
}

func han(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}
