package main

import (
	"net/http"
	"os"
	"slices"
	"testing"
	"time"

	"candle/market"
)

// TestExchangesAppLive runs the app against Bybit and MEXC, without a
// window, as TestLive does against Binance: their markets in the search,
// on the watchlist with their quotes and steps, and on the chart with live
// bars and, a perpetual, its funding. CANDLE_LIVE=1 turns it on.
func TestExchangesAppLive(t *testing.T) {
	if os.Getenv("CANDLE_LIVE") == "" {
		t.Skip("CANDLE_LIVE is not set")
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = market.SystemProxy
	queue := make(chan func(), 1024)
	ids := []string{"BYBIT:BTCUSDT", "BYBIT:ETHUSDT.P", "MEXC:BTCUSDT", "MEXC:ETHUSDT.P"}
	set := defaultSettings()
	set.Watch = ids
	set.Charts[0].Symbol = ids[0]
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
	run("the exchanges' lists", 60*time.Second, func() bool {
		return len(slices.DeleteFunc(slices.Clone(a.others), func(t market.Ticker) bool { return !slices.Contains(ids, t.Symbol) })) == len(ids)
	})
	t.Logf("search lists: %d markets of Bybit and MEXC", len(a.others))
	hits := a.findings("eth", 100)
	for _, id := range []string{"BYBIT:ETHUSDT", "MEXC:ETHUSDT", "BYBIT:ETHUSDT.P", "MEXC:ETHUSDT.P"} {
		if !slices.ContainsFunc(hits, func(h market.Ticker) bool { return h.Symbol == id }) {
			t.Fatalf("eth finds no %s: %v", id, hits)
		}
	}
	run("the watchlist", 30*time.Second, func() bool {
		return !slices.ContainsFunc(ids, func(id string) bool { return a.quotes[id].Last == 0 || a.infos[id].Tick == 0 })
	})
	for _, id := range ids {
		t.Logf("watchlist %s: last %v, %+.2f%% over 24h, step %v", id, a.quotes[id].Last, a.quotes[id].Change()*100, a.infos[id].Tick)
	}
	for _, id := range ids {
		t0 := time.Now()
		a.open(id, "1h")
		run(id+" first page", 30*time.Second, func() bool { return len(a.bars) > 0 || a.err != "" })
		if a.err != "" {
			t.Fatalf("%s: %s", id, a.err)
		}
		run(id+" live bar", 30*time.Second, func() bool { return a.stream == live })
		if market.IsPerp(id) {
			run(id+" funding", 30*time.Second, func() bool { return a.fundingOf == id })
		}
		t.Logf("%s: %d bars, live after %v, legend %q", id, len(a.bars), time.Since(t0).Round(time.Millisecond), venue(id))
		if market.IsPerp(id) {
			t.Logf("%s: mark %v, funding %+.4f%%", id, a.funding.Mark, a.funding.Rate*100)
		}
	}
	a.park()
}
