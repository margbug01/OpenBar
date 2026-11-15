package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
)

// settings are kept between runs in %APPDATA%\OpenBar\settings.json, and on
// macOS in ~/Library/Application Support/OpenBar/settings.json.
type settings struct {
	// Layout is which of TradingView's layouts shows the charts, "" for one
	// chart. Charts are the layout's charts, in TradingView's order, those a
	// layout with fewer hides too; Active is the one the toolbar acts on.
	Layout string `json:",omitempty"`
	Charts []chartSettings
	Active int
	Sync   syncSettings
	// Splits are the shares of the parts of each layout as dragged, by
	// layout and its splits' node: nil for alike.
	Splits     map[string][][]float64 `json:",omitempty"`
	Watch      []string
	Chart      style
	Drawings   map[string][]drawing // by market
	Magnet     bool
	AutoUpdate bool // check for a new version now and then
	// FuturesRTH shows futures' intraday bars in the regular trading hours
	// only (RTH), not the whole electronic session (ETH).
	FuturesRTH bool `json:",omitempty"`
	// Sections are the watchlist's sections in the order they show, by key
	// (crypto, futures, us); Folded those folded away.
	Sections []string `json:",omitempty"`
	Folded   []string `json:",omitempty"`
	// FavTools are the drawing tools on the bar of favorites, which floats
	// over the chart, its top left FavX and FavY of the way across and down
	// it; FavBar shows it.
	FavTools   []string
	FavBar     bool
	FavX, FavY float64
	// DrawDefaults are how the tools draw, by kind, as kept with 存为默认 in
	// a drawing's settings: its look, not its points.
	DrawDefaults map[string]drawing `json:",omitempty"`
}

// chartSettings are a chart's: its market, its bars and its indicators.
type chartSettings struct {
	Symbol     string
	Interval   string
	Indicators []indicator
}

// savedSettings are what the file holds: the settings, and the active
// chart as versions before the layouts kept the one chart.
type savedSettings struct {
	settings
	Symbol     string
	Interval   string
	Indicators []indicator
}

func defaultSettings() settings {
	return settings{
		Charts:     []chartSettings{{Symbol: "BTCUSDT", Interval: "1h"}},
		Sync:       syncSettings{Crosshair: true},
		Watch:      []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "BNBUSDT", "XRPUSDT", "DOGEUSDT", "SUIUSDT", "ADAUSDT"},
		Chart:      defaultStyle(),
		Magnet:     true,
		AutoUpdate: true,
		FavTools:   []string{"trend", "hline", "rect", "fib", "long", "short"},
		FavBar:     true,
		FavX:       1,
		FavY:       0.01,
	}
}

func settingsPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(appDir(dir), "settings.json")
}

// appDir is OpenBar's folder in base. OpenBar was Candle up to 0.6.1: the
// first time, Candle's folder becomes OpenBar's, with the settings, the
// drawings, the indicators and the window's place; if it cannot be moved it
// stays in use, so nothing saved is lost.
func appDir(base string) string {
	dir, old := filepath.Join(base, "OpenBar"), filepath.Join(base, "Candle")
	if _, err := os.Stat(dir); err == nil {
		return dir
	}
	if _, err := os.Stat(old); err != nil {
		return dir
	}
	if os.Rename(old, dir) != nil {
		return old
	}
	return dir
}

// loadSettings reads the file; what is missing or damaged keeps its default.
// A file from before the layouts has the one chart.
func loadSettings(file string) settings {
	saved := savedSettings{settings: defaultSettings()}
	saved.Charts = nil
	if data, err := os.ReadFile(file); err == nil {
		json.Unmarshal(data, &saved)
	}
	s := saved.settings
	if len(s.Charts) == 0 {
		s.Charts = []chartSettings{{Symbol: saved.Symbol, Interval: saved.Interval, Indicators: saved.Indicators}}
	}
	s.Charts = s.Charts[:min(len(s.Charts), maxCells)]
	for i := range s.Charts {
		s.Charts[i].fix()
	}
	s.Active = min(max(s.Active, 0), len(s.Charts)-1)
	s.Chart.fix()
	for sym, ds := range s.Drawings { // what a damaged file holds that cannot be drawn
		s.Drawings[sym] = slices.DeleteFunc(ds, func(d drawing) bool {
			t, ok := toolOf(d.Kind)
			return !ok || len(d.Pts) != t.points
		})
	}
	seen := map[string]bool{}
	s.FavTools = slices.DeleteFunc(s.FavTools, func(k string) bool {
		_, ok := toolOf(k)
		drop := !ok || seen[k]
		seen[k] = true
		return drop
	})
	s.FavX, s.FavY = min(max(s.FavX, 0), 1), min(max(s.FavY, 0), 1)
	return s
}

// fix gives a chart missing its market or its bars the default ones.
func (cs *chartSettings) fix() {
	if _, ok := intervalOf(cs.Interval); !ok {
		cs.Interval = "1h"
	}
	if cs.Symbol == "" {
		cs.Symbol = "BTCUSDT"
	}
	cs.Indicators = fixIndicators(cs.Indicators)
}

// saveSettings writes beside the file and renames over it, so a crash halfway
// leaves the old one. An empty name saves nothing, for the tests.
func saveSettings(file string, s settings) error {
	if file == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	saved := savedSettings{settings: s}
	if len(s.Charts) > 0 {
		cs := s.Charts[min(max(s.Active, 0), len(s.Charts)-1)]
		saved.Symbol, saved.Interval, saved.Indicators = cs.Symbol, cs.Interval, cs.Indicators
	}
	data, err := json.MarshalIndent(saved, "", "  ")
	if err != nil {
		return err
	}
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}
