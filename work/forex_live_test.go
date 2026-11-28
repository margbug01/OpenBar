package main

import (
	"net/http"
	"os"
	"testing"
	"time"

	"candle/market"
)

// TestForexAppLive runs the app against Yahoo for the currency pairs,
// without a window: eur finds them under 外汇 with their prices, the
// watchlist has their quotes, EUR/USD's hours and days come and go live, and
// a day's minutes open. CANDLE_LIVE=1 turns it on; CANDLE_SHOTS saves what
// it shows.
func TestForexAppLive(t *testing.T) {
	if os.Getenv("CANDLE_LIVE") == "" {
		t.Skip("CANDLE_LIVE is not set")
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = market.SystemProxy
	queue := make(chan func(), 1024)
	set := defaultSettings()
	set.Watch = []string{"FX:EURUSD", "FX:USDJPY", "FX:USDCNH", "BTCUSDT"}
	set.Charts[0].Symbol = "FX:EURUSD"
	a := newApplication(set, "", market.Binance{HTTP: &http.Client{Transport: tr, Timeout: 30 * time.Second}}, func(f func()) { queue <- f })
	run := func(what string, d time.Duration, done func() bool) {
		t.Helper()
		deadline := time.After(d)
		for !done() {
			select {
			case f := <-queue:
				f()
			case <-deadline:
				t.Fatalf("%s: not after %v", what, d)
			}
		}
	}
	a.start()
	run("the watchlist", 30*time.Second, func() bool {
		return a.quotes["FX:EURUSD"].Last > 0 && a.quotes["FX:USDJPY"].Last > 0 && a.infos["FX:EURUSD"].Tick > 0
	})
	for _, id := range set.Watch[:3] {
		t.Logf("watchlist %s: %s, %+.2f%%, step %v", id, a.priceText(id, a.quotes[id].Last), a.quotes[id].Change()*100, a.infos[id].Tick)
	}
	tt := newTester(a, true)
	a.query = "eur"
	run("the search's prices", 30*time.Second, func() bool {
		tt.Frame()
		return a.quotes["FX:EURJPY"].Last > 0 && a.quotes["FX:EURGBP"].Last > 0
	})
	tt.Frame()
	tt.Frame()
	if !tt.HasText("外汇") || !tt.HasText(" 欧元/日元") {
		t.Fatal("eur finds no pairs under 外汇")
	}
	shot(t, tt, "forex-search-eur")
	a.clearSearch()
	for _, iv := range []string{"1h", "1d"} {
		a.open("FX:EURUSD", iv)
		run(iv+" bars", 60*time.Second, func() bool { return len(a.bars) > 0 || a.err != "" })
		if a.err != "" {
			t.Fatal(a.err)
		}
		run(iv+" live", 30*time.Second, func() bool { return a.stream == live })
		last := a.bars[len(a.bars)-1]
		t.Logf("EUR/USD %s: %d bars, last %s %+v, legend %q", iv, len(a.bars), time.UnixMilli(last.T).UTC().Format("Mon 01-02 15:04 UTC"), last, venue(a.symbol))
		tt.Frame()
		tt.Frame()
		if tt.HasText("量 ") {
			t.Fatal("a volume read out")
		}
		shot(t, tt, "forex-"+iv)
	}
	// The minutes of the day before the newest.
	n := len(a.bars)
	b := a.bars[n-2]
	if !a.openIntraday(a.view.x(n-2, a.plotW, n), a.shown.y((b.H+b.L)/2, a.axis)) {
		t.Fatal("no candle to open")
	}
	run("the minutes", 30*time.Second, func() bool { return !a.intra.loading })
	d := a.intra
	if d.err != "" || len(d.bars) == 0 {
		t.Fatalf("minutes: %d, %s", len(d.bars), d.err)
	}
	t.Logf("minutes of %s: %d of %s, %s .. %s UTC", time.UnixMilli(b.T).UTC().Format("Mon 01-02 15:04"), len(d.bars), d.iv,
		time.UnixMilli(d.bars[0].T).UTC().Format("15:04"), time.UnixMilli(d.bars[len(d.bars)-1].T).UTC().Format("15:04"))
	tt.Frame()
	tt.Frame()
	shot(t, tt, "forex-intraday")
	a.park()
}
