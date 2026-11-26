package market

import (
	"context"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestExchangesLive asks Bybit's and MEXC's APIs themselves, spot and
// perpetuals, through the system proxy. CANDLE_LIVE=1 turns it on.
func TestExchangesLive(t *testing.T) {
	if os.Getenv("CANDLE_LIVE") == "" {
		t.Skip("CANDLE_LIVE is not set")
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = SystemProxy
	c := &http.Client{Transport: tr, Timeout: 30 * time.Second}
	for _, ex := range Exchanges[1:] {
		for _, id := range []string{ex.Prefix + "BTCUSDT", ex.Prefix + "BTCUSDT.P"} {
			t.Run(id, func(t *testing.T) { liveFeed(t, ex.Feed(c, id), id) })
		}
	}
}

func liveFeed(t *testing.T, f Crypto, id string) {
	ctx := context.Background()
	for _, iv := range []string{"1h", "1w"} {
		bars, err := f.Klines(ctx, id, iv, 0, 1000)
		if err != nil || len(bars) < 200 || !slices.IsSortedFunc(bars, func(a, b Bar) int { return int(a.T - b.T) }) {
			t.Fatalf("klines %s: %d, %v", iv, len(bars), err)
		}
		last := bars[len(bars)-1]
		t.Logf("klines %s: %d, %s … %s, last %+v", iv, len(bars), time.UnixMilli(bars[0].T).UTC().Format(time.DateOnly),
			time.UnixMilli(last.T).UTC().Format(time.DateTime), last)
		if iv == "1h" {
			end := bars[0].T - 1
			older, err := f.Klines(ctx, id, iv, end, 1000)
			if err != nil || len(older) == 0 || older[len(older)-1].T > end {
				t.Fatalf("older: %d, %v", len(older), err)
			}
			t.Logf("older 1h: %d, the newest opening %v before the first", len(older), time.Duration(bars[0].T-older[len(older)-1].T)*time.Millisecond)
			// The day's open, as the watchlist asks for it: the bar opening at end.
			at := bars[len(bars)-3].T
			one, err := f.Klines(ctx, id, iv, at, 1)
			if err != nil || len(one) != 1 || one[0].T != at {
				t.Fatalf("the bar opening at %d: %+v, %v", at, one, err)
			}
		}
	}
	syms, err := f.Symbols(ctx, id)
	if err != nil || len(syms) != 1 || syms[0].Name != id || syms[0].Tick <= 0 || syms[0].Base != "BTC" {
		t.Fatalf("symbols: %+v, %v", syms, err)
	}
	t.Logf("symbols: %+v", syms[0])
	tk, err := f.Tickers(ctx)
	if err != nil || !slices.ContainsFunc(tk, func(x Ticker) bool { return x.Symbol == id }) {
		t.Fatalf("tickers: %d, %v", len(tk), err)
	}
	i := slices.IndexFunc(tk, func(x Ticker) bool { return x.Symbol == id })
	t.Logf("tickers: %d, %+v (%+.2f%%)", len(tk), tk[i], tk[i].Change()*100)
	if fu, ok := f.(Funder); ok && IsPerp(id) {
		got, err := fu.Funding(ctx, id)
		if err != nil || got.Mark <= 0 || got.Next < time.Now().UnixMilli() {
			t.Fatalf("funding: %+v, %v", got, err)
		}
		t.Logf("funding: mark %v, rate %+.4f%%, next in %v", got.Mark, got.Rate*100, time.Until(time.UnixMilli(got.Next)).Round(time.Second))
	}
	sctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	n, t0 := 0, time.Now()
	f.Stream(sctx, id, "1m", func(b Bar, closed bool, at int64) {
		if n++; n <= 2 {
			t.Logf("stream 1m: update %d after %v: %+v closed=%v at=%d", n, time.Since(t0).Round(time.Millisecond), b, closed, at)
		}
		if n == 2 {
			cancel()
		}
	})
	cancel()
	if n < 2 {
		t.Fatalf("stream: %d updates", n)
	}
	wctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	got := map[string]Ticker{}
	t0 = time.Now()
	eth := strings.Replace(id, "BTC", "ETH", 1)
	f.Watch(wctx, []string{id, eth}, func(x Ticker) {
		if _, ok := got[x.Symbol]; !ok {
			t.Logf("watch: %s after %v: %+v (%+.2f%%)", x.Symbol, time.Since(t0).Round(time.Millisecond), x, x.Change()*100)
		}
		got[x.Symbol] = x
		if len(got) == 2 {
			cancel()
		}
	})
	cancel()
	for _, s := range []string{id, eth} {
		if x := got[s]; x.Last <= 0 || x.Open <= 0 || x.QuoteVolume <= 0 {
			t.Fatalf("watch: %v", got)
		}
	}
}
