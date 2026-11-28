package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"candle/market"
)

// The markets beyond the coins come from where they are timely: A shares and
// Hong Kong's from Eastmoney, Korea's and the VIX from Yahoo; each goes in
// its own section and reads as its code and Chinese name, with its exchange
// and how late it is.
func TestListedMarkets(t *testing.T) {
	a := testApp()
	for sym, want := range map[string]string{
		"SSE:600795": "market.Eastmoney", "HKEX:HSI": "market.Eastmoney", "SZSE:399006": "market.Eastmoney",
		"KRX:000660": "market.Yahoo", "CBOE:VIX": "market.Yahoo", "CME:NQ": "market.Yahoo", "BTCUSDT": "market.Binance", "BTCUSDT.P": "market.Perp",
	} {
		if got := fmt.Sprintf("%T", a.feedOf(sym)); got != want {
			t.Errorf("%s from %s, want %s", sym, got, want)
		}
	}
	if g := byFeed([]string{"SSE:600795", "BTCUSDT", "KRX:000660", "HKEX:HSI", "US:AAPL"}); len(g) != 3 ||
		!slices.Equal(g[0], []string{"BTCUSDT"}) || !slices.Equal(g[1], []string{"KRX:000660", "US:AAPL"}) || !slices.Equal(g[2], []string{"SSE:600795", "HKEX:HSI"}) {
		t.Fatalf("by feed: %v", g)
	}
	for _, c := range []struct{ sym, section, venue, unit, after string }{
		{"SSE:600795", "cn", "上交所", "股", ""},
		{"SSE:000001", "cn", "上交所", "", " 上证指数"},
		{"SZSE:000001", "cn", "深交所", "股", ""},
		{"HKEX:00700", "hk", "港交所", "股", ""},
		{"HKEX:HSI", "hk", "港交所", "", " 恒生指数"},
		{"KRX:000660", "kr", "韩国交易所 · 晚 20 分钟", "股", ""},
		{"CBOE:VIX", "us", "Cboe · 晚 15 分钟", "", " 恐慌指数"},
		{"CME:NQ", "futures", "CME · 晚 10 分钟", "手", " 纳指 E-mini"},
		{"US:AAPL", "us", "美股", "股", " 苹果"},
	} {
		if s, v, u, af := sectionOf(c.sym), venue(c.sym), a.volumeUnit(c.sym), a.after(c.sym); s != c.section || v != c.venue || u != c.unit || af != c.after {
			t.Errorf("%s: section %q, venue %q, unit %q, after %q", c.sym, s, v, u, af)
		}
	}
	a.infos["SSE:600795"] = market.Symbol{Name: "SSE:600795", Base: "600795", Tick: 0.01, Desc: "国电电力"}
	if base, quote := a.names("SSE:600795"); base != "600795" || quote != "" || a.after("SSE:600795") != " 国电电力" {
		t.Fatalf("600795 reads %q %q %q", base, quote, a.after("SSE:600795"))
	}
	if lateWhat("CBOE:VIX") != "Cboe 指数" || lateWhat("KRX:000660") != "韩国交易所股票" || lateWhat("CME:NQ") != "CME 期货" {
		t.Fatal("what comes late")
	}
}

// On the chart: an A share, live, its exchange in the legend, its day lines
// at 9:30 in Shanghai; the watchlist's sections; the VIX, from Yahoo, said to
// be 15 minutes late.
func TestListedOnTheChart(t *testing.T) {
	var spawned int
	a := yahooApp("SSE:600795", "1h", 3_600_000)
	a.spawn = func(func()) { spawned++ }
	a.infos["SSE:600795"] = market.Symbol{Name: "SSE:600795", Base: "600795", Tick: 0.01, Desc: "国电电力"}
	a.set.Watch = []string{"SSE:600795", "HKEX:HSI", "KRX:000660", "CBOE:VIX"}
	for i, s := range a.set.Watch {
		a.quotes[s] = market.Ticker{Symbol: s, Open: 100, Last: 101 + float64(i)}
	}
	tt := newTester(a, true)
	for _, want := range []string{" 国电电力", "  ·  1小时  ·  上交所", "实时", "A股", "港股", "韩股", "美股", " 恒生指数", " 恐慌指数"} {
		if !tt.HasText(want) {
			t.Fatalf("no %q", want)
		}
	}
	if tt.HasText("加密货币") || a.icons.listing || a.icons.urls != nil || spawned != 0 {
		t.Fatalf("coins: a section %v, their icons looked for %v, %d started", tt.HasText("加密货币"), a.icons.listing, spawned)
	}
	// Its name reads as a stock's does, after the code, small and muted, not
	// as a pair's quote asset.
	if title, _ := a.legendSpans(paletteFor(true), len(a.bars)-1); title[1].Text != " 国电电力" || title[1].Size != 12 || title[1].Weight != 0 {
		t.Fatalf("legend: %+v", title[:2])
	}
	sh, _ := time.LoadLocation("Asia/Shanghai")
	t0 := time.Date(2026, 10, 29, 14, 0, 0, 0, sh).UnixMilli()
	if got := a.dayOf()(t0); got != time.Date(2026, 10, 29, 9, 30, 0, 0, sh).UnixMilli() {
		t.Fatalf("Shanghai's day begins at %s", time.UnixMilli(got).In(sh))
	}
	if a.countsDown(86_400_000) || !a.countsDown(3_600_000) {
		t.Fatal("an A share's day counts down")
	}
	b := yahooApp("CBOE:VIX", "1h", 3_600_000)
	tt = newTester(b, true)
	for _, want := range []string{" 恐慌指数", "  ·  1小时  ·  Cboe · 晚 15 分钟", "晚 15 分"} {
		if !tt.HasText(want) {
			t.Fatalf("no %q", want)
		}
	}
	if tt.HasText("实时") {
		t.Fatal("the VIX live")
	}
}

// The search finds the indices without asking anyone, by code, Chinese name
// and pinyin, under their markets' headings; the stocks Eastmoney finds by a
// code in digits after them.
func TestSearchFindsListed(t *testing.T) {
	pairs := []market.Ticker{{Symbol: "BTCUSDT", QuoteVolume: 9e9}, {Symbol: "VIXUSDT", QuoteVolume: 1e5}}
	stocks := market.WithPopular(nil)
	ids := func(q string, found []market.Instrument) (s []string) {
		for _, h := range searchAll(q, pairs, localMarkets, stocks, found, 10) {
			s = append(s, h.Symbol)
		}
		return s
	}
	found := market.Found([]market.Suggestion{{Code: "000001", Name: "平安银行", Market: 0}, {Code: "600795", Name: "国电电力", Market: 1}})
	for _, c := range []struct {
		q     string
		found []market.Instrument
		want  []string
	}{
		{"vix", nil, []string{"VIXUSDT", "CBOE:VIX"}}, // the coin first, as BTC
		{"恐慌", nil, []string{"CBOE:VIX", "US:UVXY"}},
		{"hsi", nil, []string{"HKEX:HSI"}},
		{"恒生", nil, []string{"HKEX:HSI", "HKEX:HSTECH"}},
		{"上证指数", nil, []string{"SSE:000001"}},
		{"szzs", nil, []string{"SSE:000001"}},
		{"000001", found[:1], []string{"SSE:000001", "SZSE:000001"}},
		{"600795", found[1:], []string{"SSE:600795"}},
		{"nz", nil, []string{"CME:NQ", "CME:MNQ", "FX:NZDUSD", "US:QQQ", "US:TQQQ", "US:SQQQ"}}, // as before, and NZD/USD by its code
		{"eurusd", nil, []string{"FX:EURUSD"}},
		{"eur/usd", nil, []string{"FX:EURUSD"}},
		{"欧元/美元", nil, []string{"FX:EURUSD"}},
		{"欧元", nil, []string{"FX:EURUSD", "FX:EURJPY", "FX:EURGBP", "FX:EURCHF", "FX:EURAUD"}},
		{"oymy", nil, []string{"FX:EURUSD"}},
		{"rmb", nil, []string{"FX:USDCNH", "FX:USDCNY"}},
	} {
		if got := ids(c.q, c.found); !slices.Equal(got, c.want) {
			t.Errorf("%q: %v, want %v", c.q, got, c.want)
		}
	}
	var heads []string
	for _, g := range groupHits(searchAll("恒生", pairs, localMarkets, stocks, nil, 10)) {
		heads = append(heads, g.sec.label)
	}
	if !slices.Equal(heads, []string{"港股"}) {
		t.Fatalf("恒生 under %v", heads)
	}
	heads = nil
	for _, g := range groupHits(searchAll("eurusd", append(pairs, market.Ticker{Symbol: "EURUSDT"}), localMarkets, stocks, nil, 10)) {
		heads = append(heads, g.sec.label)
	}
	if !slices.Equal(heads, []string{"外汇", "加密货币"}) {
		t.Fatalf("eurusd under %v", heads)
	}
}

// A currency pair picked from the search goes on the watchlist under 外汇 and
// on the chart as EUR/USD, from Yahoo, live, its prices to a tenth of a pip
// (a yen pair's to three decimals), its days from midnight in London, and no
// volume read out: Yahoo's is all 0.
func TestForexOnTheChart(t *testing.T) {
	a := testApp()
	a.set.Watch = []string{"BTCUSDT"}
	a.quotes["FX:EURUSD"] = market.Ticker{Symbol: "FX:EURUSD", Open: 1.0975, Last: 1.0938525199890137}
	tt := newTester(a, true)
	a.query = "eur"
	tt.Frame()
	tt.Frame()
	if !tt.HasText("外汇") || !tt.HasText("1.09385") {
		t.Fatal("EUR/USD is not under 外汇 in the results with its price")
	}
	click(t, tt, "EUR/USD 欧元/美元")
	if a.symbol != "FX:EURUSD" || a.set.Watch[0] != "FX:EURUSD" || sectionOf(a.symbol) != "fx" {
		t.Fatalf("after picking EUR/USD: showing %s, watch %v", a.symbol, a.set.Watch)
	}
	if _, ok := a.feedOf(a.symbol).(market.Yahoo); !ok {
		t.Fatal("EUR/USD not from Yahoo")
	}
	bars := fakeBars(600, 3_600_000)
	for i := range bars {
		b := &bars[i]
		b.O, b.H, b.L, b.C, b.V = b.O/72000, b.H/72000, b.L/72000, b.C/72000, 0
	}
	a.loading, a.bars, a.atStart, a.stream = false, bars, true, live
	tt.Frame()
	tt.Frame()
	for _, want := range []string{"外汇", " 欧元/美元", "  ·  1小时  ·  外汇", "实时"} {
		if !tt.HasText(want) {
			t.Fatalf("no %q", want)
		}
	}
	if _, spans := a.legendSpans(paletteFor(true), len(a.bars)-1); slices.ContainsFunc(spans, func(s ui.Span) bool { return strings.Contains(s.Text, "量") }) {
		t.Fatalf("the legend reads out a volume: %+v", spans)
	}
	if a.priceText("FX:EURUSD", 1.0938525199890137) != "1.09385" || a.priceText("FX:USDJPY", 159.51499938964844) != "159.515" {
		t.Fatalf("prices %s, %s", a.priceText("FX:EURUSD", 1.0938525199890137), a.priceText("FX:USDJPY", 159.51499938964844))
	}
	london, _ := time.LoadLocation("Europe/London")
	for _, d := range []time.Time{time.Date(2026, 11, 27, 21, 0, 0, 0, london), time.Date(2026, 7, 3, 0, 30, 0, 0, london)} {
		midnight := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, london).UnixMilli()
		if got := a.dayOf()(d.UnixMilli()); got != midnight {
			t.Fatalf("the day holding %s begins at %s", d, time.UnixMilli(got).In(london))
		}
		// Its minutes: the whole day from midnight, nothing of the day before.
		if from, open, to, minutes := dayWindow(a.symbol, midnight); from != midnight || open != midnight || to != midnight+86_400_000 || minutes != 1440 {
			t.Fatalf("the minutes of %s: from %d, open %d, to %d, %d a day", d, from, open, to, minutes)
		}
	}
}

// A code in digits asks Eastmoney, as a Chinese name does; the A share it
// finds shows under A股 with its name, and its price comes from Eastmoney,
// not Yahoo.
func TestSearchAsksEastmoneyForCodes(t *testing.T) {
	var mu sync.Mutex
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.URL.Path)
		mu.Unlock()
		switch r.URL.Path {
		case "/api/suggest/get":
			fmt.Fprint(w, `{"QuotationCodeTable":{"Data":[{"Code":"600795","Name":"国电电力","PinYin":"GDDL","MktNum":"1","SecurityTypeName":"沪A"}],"Status":0}}`)
		case "/api/qt/ulist.np/get":
			fmt.Fprint(w, `{"rc":0,"data":{"diff":[{"f2":5.38,"f12":"600795","f13":1,"f18":5.26}]}}`)
		default:
			fmt.Fprint(w, `{}`)
		}
	}))
	defer srv.Close()
	queue := make(chan func(), 100)
	a := testApp()
	a.east = market.Eastmoney{HTTP: srv.Client(), Base: srv.URL, His: srv.URL, Push: srv.URL}
	a.yahoo = market.Yahoo{HTTP: srv.Client(), Base: srv.URL}
	a.usFailed = time.Now()
	a.spawn = func(f func()) { go f() }
	a.post = func(f func()) { queue <- f }
	tt := newTester(a, true)
	a.query = "600795"
	tt.Frame()
	deadline := time.After(3 * time.Second)
	for a.quotes["SSE:600795"].Last == 0 {
		select {
		case f := <-queue:
			f()
			tt.Frame()
		case <-deadline:
			t.Fatalf("no price; asked %v", asked)
		}
	}
	tt.Frame()
	tt.Frame()
	if !tt.HasText(" 国电电力") || !tt.HasText("A股") || !tt.HasText("5.38") || !tt.HasText("+2.28%") {
		t.Fatalf("name %v, heading %v, price %v", tt.HasText(" 国电电力"), tt.HasText("A股"), tt.HasText("5.38"))
	}
	mu.Lock()
	defer mu.Unlock()
	if slices.ContainsFunc(asked, func(p string) bool { return strings.Contains(p, "spark") }) {
		t.Fatalf("asked Yahoo for an A share's price: %v", asked)
	}
}
