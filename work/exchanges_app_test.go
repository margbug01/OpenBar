package main

import (
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"candle/market"
)

// exchangeAPIs answers as Bybit and MEXC do, each with a market or two, BTC
// busiest on MEXC.
var exchangeAPIs = []struct{ url, body string }{
	{"api.bybit.com/v5/market/tickers?category=linear&symbol=BTCUSDT", `{"retCode":0,"retMsg":"OK","result":{"category":"linear","list":[{"symbol":"BTCUSDT","lastPrice":"60635.30","markPrice":"60635.70","prevPrice24h":"59170.00","turnover24h":"7362646172.3431","fundingRate":"0.0001","nextFundingTime":"1795680000000"}]}}`},
	{"api.bybit.com/v5/market/tickers?category=spot", `{"retCode":0,"retMsg":"OK","result":{"category":"spot","list":[{"symbol":"BTCUSDT","lastPrice":"60663.5","prevPrice24h":"59064.1","turnover24h":"1440696895.54"},{"symbol":"ETHUSDT","lastPrice":"1685.5","prevPrice24h":"1660.9","turnover24h":"293008551.54"}]}}`},
	{"api.bybit.com/v5/market/tickers?category=linear", `{"retCode":0,"retMsg":"OK","result":{"category":"linear","list":[{"symbol":"BTCUSDT","lastPrice":"60635.30","prevPrice24h":"59170.00","turnover24h":"7362646172.3431"},{"symbol":"BTCPERP","lastPrice":"60461.10","prevPrice24h":"59194.50","turnover24h":"65636420.2475"}]}}`},
	{"api.mexc.com/api/v3/ticker/24hr", `[{"symbol":"BTCUSDT","openPrice":"59050.13","lastPrice":"60664.69","quoteVolume":"9440908253.02"},{"symbol":"PEPEUSDT","openPrice":"0.0000041","lastPrice":"0.0000042","quoteVolume":"5000000"}]`},
	{"contract.mexc.com/api/v1/contract/ticker", `{"success":true,"code":0,"data":[{"symbol":"BTC_USDT","lastPrice":60630,"amount24":4427627484.0578,"riseFallRate":0.0397,"riseFallRatesOfTimezone":[0.0252,0.0033,0.0397]},{"symbol":"1000PEPE_USDT","lastPrice":0.0041,"amount24":30000000,"riseFallRate":0.01,"riseFallRatesOfTimezone":[0.02,0.0,0.01]}]}`},
}

type exchangeTransport struct{}

func (exchangeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	u := r.URL.String()
	for _, a := range exchangeAPIs {
		if strings.Contains(u, a.url) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(a.body)), Header: http.Header{}, Request: r}, nil
		}
	}
	return nil, fmt.Errorf("no answer for %s", u)
}

// Bybit's and MEXC's markets load once for the search, which finds them
// beside Binance's: a pair on every exchange together, Binance's first, each
// row saying its exchange. Each market comes from its exchange's feed, reads
// as its pair, goes in its section, and shows its venue and, a perpetual,
// its funding.
func TestOtherExchanges(t *testing.T) {
	a := perpApp()
	a.bin.HTTP = &http.Client{Transport: exchangeTransport{}}
	var spawned []func()
	a.spawn = func(f func()) { spawned = append(spawned, f) }
	a.loadExchanges()
	for _, f := range spawned {
		f()
	}
	ids := func(ts []market.Ticker) (s []string) {
		for _, x := range ts {
			s = append(s, x.Symbol)
		}
		return s
	}
	for q, want := range map[string][]string{
		"btc":  {"BTCUSDT", "BYBIT:BTCUSDT", "MEXC:BTCUSDT", "BTCUSDT.P", "BYBIT:BTCUSDT.P", "MEXC:BTCUSDT.P"},
		"pepe": {"PEPEUSDT", "MEXC:PEPEUSDT", "PEPEFDUSD", "1000PEPEUSDT.P", "MEXC:1000PEPEUSDT.P"},
		"eth":  {"BYBIT:ETHUSDT", "ETHUSDT.P"},
	} {
		if got := ids(a.findings(q, 10)); !slices.Equal(got[:min(len(got), len(want))], want) {
			t.Errorf("%s: %v, want %v", q, got, want)
		}
	}

	for sym, want := range map[string]string{
		"BYBIT:BTCUSDT": "market.Bybit spot", "BYBIT:BTCUSDT.P": "market.Bybit linear",
		"MEXC:BTCUSDT": "market.Mexc", "MEXC:BTCUSDT.P": "market.MexcPerp",
		"BTCUSDT": "market.Binance", "BTCUSDT.P": "market.Perp",
	} {
		f := a.feedOf(sym)
		got := fmt.Sprintf("%T", f)
		if b, ok := f.(market.Bybit); ok {
			got += " " + b.Category
		}
		if got != want {
			t.Errorf("%s from %s, want %s", sym, got, want)
		}
	}
	if g := byFeed([]string{"MEXC:BTCUSDT.P", "US:AAPL", "BYBIT:ETHUSDT", "BTCUSDT", "BYBIT:BTCUSDT", "BYBIT:BTCUSDT.P"}); !slices.EqualFunc(g,
		[][]string{{"BTCUSDT"}, {"BYBIT:ETHUSDT", "BYBIT:BTCUSDT"}, {"BYBIT:BTCUSDT.P"}, {"MEXC:BTCUSDT.P"}, {"US:AAPL"}}, slices.Equal) {
		t.Fatalf("by feed: %v", g)
	}
	for _, c := range []struct{ sym, venue, section, after, tag string }{
		{"BYBIT:BTCUSDT", "Bybit", "crypto", "/USDT", "Bybit"},
		{"BYBIT:BTCUSDT.P", "Bybit 永续", "perp", "/USDT 永续", "Bybit"},
		{"MEXC:BTCUSDT", "MEXC", "crypto", "/USDT", "MEXC"},
		{"MEXC:1000PEPEUSDT.P", "MEXC 永续", "perp", "/USDT 永续", "MEXC"},
		{"BTCUSDT", "Binance", "crypto", "/USDT", ""},
		{"BTCUSDT.P", "币安永续", "perp", "/USDT 永续", ""},
	} {
		if venue(c.sym) != c.venue || sectionOf(c.sym) != c.section || a.after(c.sym) != c.after || exchangeTag(c.sym) != c.tag {
			t.Errorf("%s: %q %q %q %q, want %+v", c.sym, venue(c.sym), sectionOf(c.sym), a.after(c.sym), exchangeTag(c.sym), c)
		}
	}
	if base, quote := a.names("MEXC:1000PEPEUSDT.P"); base != "1000PEPE" || quote != "USDT" {
		t.Fatalf("names: %s %s", base, quote)
	}

	// Picked from the search: watched, on the chart from Bybit's
	// perpetuals, its funding followed.
	st := newTester(a, true)
	a.query = "btc"
	st.Frame()
	st.Frame()
	for _, want := range []string{"BTC/USDT  Bybit", "BTC/USDT  MEXC", "BTC/USDT 永续  MEXC"} {
		if _, ok := st.Find(want); !ok {
			t.Fatalf("no row %q in the search", want)
		}
	}
	shot(t, st, "search-exchanges")
	spawned = nil
	a.pick("BYBIT:BTCUSDT.P")
	if a.set.Watch[0] != "BYBIT:BTCUSDT.P" || a.symbol != "BYBIT:BTCUSDT.P" {
		t.Fatalf("picked: %v %s", a.set.Watch[:2], a.symbol)
	}
	a.loading, a.bars, a.stream = false, fakeBars(600, 3_600_000), live
	// What the chart starts last follows the funding: its first answer.
	posts := make(chan func(), 1)
	a.post = func(f func()) { posts <- f }
	go spawned[len(spawned)-1]()
	(<-posts)()
	a.post = func(f func()) { f() }
	a.park()
	if a.fundingOf != "BYBIT:BTCUSDT.P" || a.funding.Mark != 60635.7 || a.funding.Rate != 0.0001 {
		t.Fatalf("funding %+v of %s", a.funding, a.fundingOf)
	}
	a.infos["BYBIT:BTCUSDT.P"] = market.Symbol{Name: "BYBIT:BTCUSDT.P", Base: "BTC", Quote: "USDT", Tick: 0.1}
	a.quotes["BYBIT:BTCUSDT.P"] = market.Ticker{Symbol: "BYBIT:BTCUSDT.P", Open: 59170, Last: 60635.3, QuoteVolume: 7e9}
	tt := newTester(a, true)
	for _, want := range []string{"  ·  1小时  ·  Bybit 永续", "标记 60635.7  ·  资金费率 +0.0100%", "BTC/USDT 永续  Bybit"} {
		if !tt.HasText(want) {
			t.Fatalf("no %q", want)
		}
	}
	shot(t, tt, "bybit-perp")
}
