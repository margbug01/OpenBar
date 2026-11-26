package market

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"testing"
)

// Each market's ID names its exchange's feed, spot or perpetual, and its
// symbol there; Binance's have no prefix, the others' TradingView's.
func TestExchangeOf(t *testing.T) {
	for id, want := range map[string]struct{ ex, feed, code string }{
		"BTCUSDT":             {"Binance", "market.Binance", "BTCUSDT"},
		"BTCUSDT.P":           {"Binance", "market.Perp", "BTCUSDT"},
		"BYBIT:BTCUSDT":       {"Bybit", "market.Bybit/spot", "BTCUSDT"},
		"BYBIT:BTCUSDT.P":     {"Bybit", "market.Bybit/linear", "BTCUSDT"},
		"MEXC:PEPEUSDT":       {"MEXC", "market.Mexc", "PEPEUSDT"},
		"MEXC:1000PEPEUSDT.P": {"MEXC", "market.MexcPerp", "1000PEPEUSDT"},
	} {
		ex, ok := ExchangeOf(id)
		feed := ex.Feed(nil, id)
		name := fmt.Sprintf("%T", feed)
		if b, isBybit := feed.(Bybit); isBybit {
			name += "/" + b.Category
		}
		if !ok || ex.Name != want.ex || name != want.feed || Code(id) != want.code {
			t.Errorf("%s: %s %s %s, want %+v", id, ex.Name, name, Code(id), want)
		}
	}
	for _, id := range []string{"US:AAPL", "CME:NQ", "SSE:600000", "HKEX:00700"} {
		if _, ok := ExchangeOf(id); ok {
			t.Errorf("%s on a crypto exchange", id)
		}
	}
	if c := mexcContract("MEXC:1000PEPEUSDT.P"); c != "1000PEPE_USDT" || mexcPerpID(c) != "MEXC:1000PEPEUSDT.P" {
		t.Fatalf("MEXC's contract %s", c)
	}
}

// answers is an http.Client answering each request with the body of the
// first key its URL holds, noting the URLs.
func answers(asked *[]string, bodies ...string) *http.Client {
	return &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		u := r.URL.String()
		*asked = append(*asked, u)
		for i := 0; i < len(bodies); i += 2 {
			if strings.Contains(u, bodies[i]) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(bodies[i+1])), Header: http.Header{}, Request: r}, nil
			}
		}
		return &http.Response{StatusCode: 404, Status: "404 Not Found", Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}, Request: r}, nil
	})}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Bybit's answers, as it gave them: bars the newest first, the linear
// contracts' others (in USDC, expiring) among the perpetuals.
func TestBybitREST(t *testing.T) {
	ctx := context.Background()
	var asked []string
	c := answers(&asked,
		"/kline", `{"retCode":0,"retMsg":"OK","result":{"category":"spot","symbol":"BTCUSDT","list":[["1795672800000","60812.6","60812.8","60643.8","60667.5","62.135949","3774249.8860962"],["1795669200000","60975.9","61074","60776","60812.6","563.985932","34359982.3903443"]]},"retExtInfo":{},"time":1795672945688}`,
		"/instruments-info", `{"retCode":0,"retMsg":"OK","result":{"category":"linear","list":[{"symbol":"BTCPERP","contractType":"LinearPerpetual","status":"Trading","baseCoin":"BTC","quoteCoin":"USDC","priceFilter":{"minPrice":"0.10","maxPrice":"1999999.80","tickSize":"0.10"}},{"symbol":"BTCUSDT","contractType":"LinearPerpetual","status":"Trading","baseCoin":"BTC","quoteCoin":"USDT","priceFilter":{"minPrice":"0.10","maxPrice":"1999999.80","tickSize":"0.10"}},{"symbol":"BTCUSDT-04DEC26","contractType":"LinearFutures","status":"Trading","baseCoin":"BTC","quoteCoin":"USDT","priceFilter":{"minPrice":"0.1","maxPrice":"1999999.8","tickSize":"0.1"}}],"nextPageCursor":""}}`,
		"symbol=BTCUSDT", `{"retCode":0,"retMsg":"OK","result":{"category":"linear","list":[{"symbol":"BTCUSDT","lastPrice":"60635.30","indexPrice":"60665.64","markPrice":"60635.70","prevPrice24h":"59170.00","price24hPcnt":"0.024764","turnover24h":"7362646172.3431","volume24h":"124419.2440","fundingRate":"0.0001","nextFundingTime":"1795680000000"}]}}`,
		"/tickers", `{"retCode":0,"retMsg":"OK","result":{"category":"linear","list":[{"symbol":"BTCPERP","lastPrice":"60461.10","prevPrice24h":"59194.50","turnover24h":"65636420.2475","markPrice":"60461.10","fundingRate":"0.0001","nextFundingTime":"1795680000000"},{"symbol":"ETHUSDT","lastPrice":"1685.55","prevPrice24h":"1660.73","turnover24h":"2428979944.9672","markPrice":"1685.55","fundingRate":"0.0001","nextFundingTime":"1795680000000"},{"symbol":"BTCUSDT-04DEC26","lastPrice":"60826.1","prevPrice24h":"59173.7","turnover24h":"202814.0417","markPrice":"60603.2","fundingRate":"","nextFundingTime":"0"}]}}`,
	)
	spot, perp := Bybit{HTTP: c, Category: "spot"}, Bybit{HTTP: c, Category: "linear"}
	bars, err := spot.Klines(ctx, "BYBIT:BTCUSDT", "1h", 1795672800000, 2)
	if err != nil || len(bars) != 2 || bars[0] != (Bar{T: 1795669200000, O: 60975.9, H: 61074, L: 60776, C: 60812.6, V: 563.985932}) || bars[1].T != 1795672800000 {
		t.Fatalf("klines %+v, %v", bars, err)
	}
	if !strings.Contains(asked[0], "category=spot") || !strings.Contains(asked[0], "interval=60&") || !strings.Contains(asked[0], "end=1795672800000") || !strings.Contains(asked[0], "symbol=BTCUSDT") {
		t.Fatalf("asked %s", asked[0])
	}
	if _, err := spot.Klines(ctx, "BYBIT:BTCUSDT", "1w", 0, 2); err != nil || !strings.Contains(asked[1], "interval=W") {
		t.Fatalf("weekly: %s, %v", asked[1], err)
	}
	syms, err := perp.Symbols(ctx, "BYBIT:BTCUSDT.P", "BYBIT:BTCUSDT.P")
	if err != nil || len(syms) != 2 || syms[0] != (Symbol{Name: "BYBIT:BTCUSDT.P", Base: "BTC", Quote: "USDT", Tick: 0.1}) {
		t.Fatalf("symbols %+v, %v", syms, err)
	}
	if _, err := perp.Symbols(ctx, "BYBIT:BTCPERP.P", "BYBIT:BTCUSDT.P"); err == nil {
		t.Fatal("a USDC perpetual found")
	}
	tk, err := perp.Tickers(ctx)
	if err != nil || len(tk) != 1 || tk[0] != (Ticker{Symbol: "BYBIT:ETHUSDT.P", Open: 1660.73, Last: 1685.55, QuoteVolume: 2428979944.9672}) {
		t.Fatalf("tickers %+v, %v", tk, err)
	}
	f, err := perp.Funding(ctx, "BYBIT:BTCUSDT.P")
	if err != nil || f != (Funding{Mark: 60635.7, Rate: 0.0001, Next: 1795680000000}) {
		t.Fatalf("funding %+v, %v", f, err)
	}
	if _, err := spot.Funding(ctx, "BYBIT:BTCUSDT"); err == nil {
		t.Fatal("a spot pair's funding")
	}
	if err := parseBybit([]byte(`{"retCode":10001,"retMsg":"params error: symbol invalid","result":{},"retExtInfo":{},"time":1}`), &struct{}{}); err == nil {
		t.Fatal("an error answered as fine")
	}
}

// Bybit's streams, as they came: a bar, and the perpetuals' tickers, whole
// once and then only what changed.
func TestBybitStreams(t *testing.T) {
	m, err := parseBybitMessage([]byte(`{"topic":"kline.60.BTCUSDT","data":[{"start":1795672800000,"end":1795676399999,"interval":"60","open":"60790","close":"60636.8","high":"60795","low":"60580","volume":"439.748","turnover":"26671003.3669","confirm":false,"timestamp":1795673060200}],"ts":1795673060200,"type":"snapshot"}`))
	if err != nil {
		t.Fatal(err)
	}
	k, err := parseBybitKline(m.Data)
	if err != nil || len(k) != 1 || k[0] != (bybitBar{Bar{T: 1795672800000, O: 60790, H: 60795, L: 60580, C: 60636.8, V: 439.748}, false, 1795673060200}) {
		t.Fatalf("kline %+v, %v", k, err)
	}
	if _, err := parseBybitMessage([]byte(`{"success":false,"ret_msg":"Invalid symbol :[kline.60.NOPE]","conn_id":"x","op":"subscribe"}`)); err == nil {
		t.Fatal("a refused subscription taken")
	}
	b := Bybit{Category: "linear"}
	known := map[string]bybitTicker{}
	for i, msg := range []string{
		`{"symbol":"BTCUSDT","tickDirection":"ZeroMinusTick","price24hPcnt":"0.024789","lastPrice":"60636.80","prevPrice24h":"59170.00","highPrice24h":"61630.80","lowPrice24h":"57257.00","prevPrice1h":"60910.00","markPrice":"60639.08","indexPrice":"60668.12","turnover24h":"7374849821.4910","volume24h":"124620.5510","nextFundingTime":"1795680000000","fundingRate":"0.0001"}`,
		`{"symbol":"BTCUSDT","bid1Price":"60636.80","bid1Size":"0.582"}`,
		`{"symbol":"BTCUSDT","tickDirection":"PlusTick","price24hPcnt":"0.024791","lastPrice":"60636.90","turnover24h":"7374850306.5862","volume24h":"124620.5590","ask1Price":"60636.90","ask1Size":"6.807"}`,
	} {
		got, ok, err := b.mergeTicker(known, []byte(msg))
		want := Ticker{Symbol: "BYBIT:BTCUSDT.P", Open: 59170, Last: []float64{60636.8, 60636.8, 60636.9}[i], QuoteVolume: []float64{7374849821.491, 7374849821.491, 7374850306.5862}[i]}
		if err != nil || !ok || got != want {
			t.Fatalf("ticker %d: %+v %v %v", i, got, ok, err)
		}
	}
	if _, ok, _ := b.mergeTicker(known, []byte(`{"symbol":"ETHUSDT","bid1Price":"1"}`)); ok {
		t.Fatal("a ticker with no price")
	}
}

// MEXC's spot answers, as it gave them: Binance's shape, a price's step as
// decimals, a page read on from its start.
func TestMexcREST(t *testing.T) {
	ctx := context.Background()
	var asked []string
	c := answers(&asked,
		"endTime=1795669199999", `[]`, // before the pair's first bar
		"/klines", `[[1795669200000,"60971.2","61070.16","60772.51","60807.01","124.16871541",1795672800000,"7562528.58"],[1795672800000,"60807.01","60809.13","60643.87","60671.64","7.73620065",1795676400000,"469535.53"]]`,
		"/exchangeInfo", `{"symbols": [{"symbol": "PEPEUSDT", "status": "1", "baseAsset": "PEPE", "quoteAsset": "USDT", "quotePrecision": 9, "baseAssetPrecision": 0}]}`,
		"/ticker/24hr", `[{"symbol": "ETHUSDT", "priceChange": "35.09", "priceChangePercent": "0.0212", "prevClosePrice": "1653.67", "lastPrice": "1688.76", "bidPrice": "1688.74", "bidQty": "6.06894", "askPrice": "1688.75", "askQty": "3.49625", "openPrice": "1653.67", "highPrice": "1729.14", "lowPrice": "1610", "volume": "185479.55249", "quoteVolume": "308554797.3", "openTime": 1795672952031, "closeTime": 1795672959085, "count": null}, {"symbol": "NTXUSDT", "priceChange": "0", "priceChangePercent": "0", "prevClosePrice": "0.000699", "lastPrice": "0.000699", "bidPrice": "0.00033", "bidQty": "38234.87", "askPrice": "0.0006", "askQty": "8000", "openPrice": "0.000699", "highPrice": "0.000699", "lowPrice": "0.000699", "volume": "0", "quoteVolume": "0", "openTime": 1795672923007, "closeTime": 1795672959084, "count": null}, {"symbol":"NEWUSDT","openPrice":"1","lastPrice":"1","quoteVolume":null}]`,
	)
	m := Mexc{HTTP: c}
	bars, err := m.Klines(ctx, "MEXC:BTCUSDT", "1h", 1795672800000, 2)
	if err != nil || len(bars) != 2 || bars[1] != (Bar{T: 1795672800000, O: 60807.01, H: 60809.13, L: 60643.87, C: 60671.64, V: 7.73620065}) {
		t.Fatalf("klines %+v, %v", bars, err)
	}
	if !strings.Contains(asked[0], "interval=60m") || !strings.Contains(asked[0], "startTime=1795665600001") || !strings.Contains(asked[0], "endTime=1795672800000") {
		t.Fatalf("asked %s", asked[0])
	}
	// 1000 bars take pages of 500 from the newest, until one is empty.
	asked = nil
	bars, err = m.Klines(ctx, "MEXC:BTCUSDT", "1w", 1795672800000, 1000)
	if err != nil || len(bars) != 2 || len(asked) != 2 || !strings.Contains(asked[0], "limit=500") || !strings.Contains(asked[1], "endTime=1795669199999") || !strings.Contains(asked[0], "interval=1W") {
		t.Fatalf("pages %v: %d bars, %v", asked, len(bars), err)
	}
	syms, err := m.Symbols(ctx, "MEXC:PEPEUSDT")
	if err != nil || len(syms) != 1 || syms[0].Name != "MEXC:PEPEUSDT" || syms[0].Base != "PEPE" || math.Abs(syms[0].Tick-1e-9) > 1e-18 {
		t.Fatalf("symbols %+v, %v", syms, err)
	}
	if _, err := m.Symbols(ctx, "MEXC:PEPEUSDT", "MEXC:NOPEUSDT"); err == nil {
		t.Fatal("an unknown pair found")
	}
	tk, err := m.Tickers(ctx)
	if err != nil || len(tk) != 1 || tk[0] != (Ticker{Symbol: "MEXC:ETHUSDT", Open: 1653.67, Last: 1688.76, QuoteVolume: 308554797.3}) {
		t.Fatalf("tickers %+v, %v", tk, err)
	}
}

// MEXC's spot stream sends protobuf, as these did; its answers to a
// subscription come as JSON.
func TestMexcStream(t *testing.T) {
	data, err := os.ReadFile("testdata/mexc-kline.pb")
	if err != nil {
		t.Fatal(err)
	}
	p, err := parseMexcPush(data)
	if err != nil || p.kline == nil || *p.kline != (Bar{T: 1795672800000, O: 60807.01, H: 60809.13, L: 60575.93, C: 60646.49, V: 25.33928457}) || p.at < 1795672800000 {
		t.Fatalf("kline %+v, %v", p, err)
	}
	data, err = os.ReadFile("testdata/mexc-miniticker.pb")
	if err != nil {
		t.Fatal(err)
	}
	p, err = parseMexcPush(data)
	if err != nil || p.ticker == nil || p.ticker.Symbol != "MEXC:BTCUSDT" || p.ticker.Last != 60646.47 || p.ticker.QuoteVolume != 441061132.54 ||
		math.Abs(p.ticker.Change()-0.026) > 1e-9 {
		t.Fatalf("ticker %+v, %v", p.ticker, err)
	}
	if p, err := parseMexcPush([]byte(`{"id":0,"code":0,"msg":"spot@public.kline.v3.api.pb@BTCUSDT@Min60"}`)); err != nil || p.kline != nil || p.ticker != nil {
		t.Fatalf("the answer to a subscription: %+v, %v", p, err)
	}
	if _, err := parseMexcPush([]byte(`{"id":0,"code":0,"msg":"Not Subscribed successfully! [spot@public.kline.v3.api@BTCUSDT@Min60].  Reason： Blocked! "}`)); err == nil {
		t.Fatal("a refused subscription taken")
	}
	if _, err := parseMexcPush(data[:len(data)-3]); err == nil {
		t.Fatal("a cut message read")
	}
}

// MEXC's perpetuals' answers, as it gave them: bars in columns, in seconds
// and contracts; days of USDT's and USDC's perpetuals.
func TestMexcPerpREST(t *testing.T) {
	ctx := context.Background()
	var asked []string
	c := answers(&asked,
		"/detail?symbol=", `{"success":true,"code":0,"data":{"symbol":"BTC_USDT","displayName":"BTC_USDT永续","displayNameEn":"BTC_USDT PERPETUAL","baseCoin":"BTC","quoteCoin":"USDT","settleCoin":"USDT","contractSize":0.0001,"priceScale":1,"priceUnit":0.1,"state":0}}`,
		"/detail", `{"success":true,"code":0,"data":[{"symbol":"BTC_USDT","baseCoin":"BTC","quoteCoin":"USDT","contractSize":0.0001,"priceUnit":0.1,"state":0,"futureType":1},{"symbol":"BTC_USDC","baseCoin":"BTC","quoteCoin":"USDC","contractSize":0.0001,"priceUnit":0.1,"state":0,"futureType":1}]}`,
		"/kline/", `{"success":true,"code":0,"data":{"time":[1795665600,1795669200,1795672800],"open":[60756.9,60950.8,60790.4],"close":[60950.8,60790.4,60628.8],"high":[61115.6,61045.9,60795.3],"low":[60742.0,60749.0,60590.8],"vol":[1.7780261E7,1.1968525E7,2031623.0],"amount":[1.0838084453397E8,7.289345465565E7,1.232564825269E7],"realOpen":[60756.8,60950.9,60792.5],"realClose":[60950.8,60790.4,60628.8],"realHigh":[61115.6,61045.9,60795.3],"realLow":[60742.0,60749.0,60590.8]}}`,
		"/ticker", `{"success":true,"code":0,"data":[{"contractId":11,"symbol":"ETH_USDT","lastPrice":1687.85,"bid1":1687.85,"ask1":1687.86,"volume24":97639376,"amount24":1617853989.5281,"holdVol":40709736,"lower24Price":1608.21,"high24Price":1729.41,"riseFallRate":0.0241,"riseFallValue":39.85,"indexPrice":1688.75,"fairPrice":1687.95,"fundingRate":3.6e-05,"timestamp":1795673314950,"riseFallRates":{"zone":"UTC+8","r":0.0241,"v":39.85},"riseFallRatesOfTimezone":[0.0188,-0.0023,0.0241]},{"contractId":1104,"symbol":"BTC_USDC","lastPrice":60539.7,"amount24":72858322.35434,"riseFallRate":0.039,"riseFallRatesOfTimezone":[0.025,0.003,0.039]}]}`,
		"/funding_rate/", `{"success":true,"code":0,"data":{"symbol":"BTC_USDT","fundingRate":0.000081,"maxFundingRate":0.0018,"minFundingRate":-0.0018,"collectCycle":8,"nextSettleTime":1795680000000,"timestamp":1795673047807,"idxPrice":60664,"fairPrice":60635,"fundingType":0}}`,
	)
	m := MexcPerp{HTTP: c}
	mexcSizes.Delete("BTC_USDT")
	bars, err := m.Klines(ctx, "MEXC:BTCUSDT.P", "1h", 1795672800000, 2)
	if b := bars[len(bars)-1]; err != nil || len(bars) != 2 || b.T != 1795672800000 || b.O != 60790.4 || b.H != 60795.3 || b.L != 60590.8 || b.C != 60628.8 || math.Abs(b.V-203.1623) > 1e-9 {
		t.Fatalf("klines %+v, %v", bars, err)
	}
	if k := asked[len(asked)-1]; !strings.Contains(k, "/kline/BTC_USDT?") || !strings.Contains(k, "interval=Min60") || !strings.Contains(k, "end=1795672800&") {
		t.Fatalf("asked %v", asked)
	}
	syms, err := m.Symbols(ctx, "MEXC:BTCUSDT.P")
	if err != nil || len(syms) != 1 || syms[0] != (Symbol{Name: "MEXC:BTCUSDT.P", Base: "BTC", Quote: "USDT", Tick: 0.1}) {
		t.Fatalf("symbols %+v, %v", syms, err)
	}
	if _, err := m.Symbols(ctx, "MEXC:BTCUSDC.P"); err == nil {
		t.Fatal("a USDC perpetual found")
	}
	tk, err := m.Tickers(ctx)
	if err != nil || len(tk) != 1 || tk[0].Symbol != "MEXC:ETHUSDT.P" || tk[0].Last != 1687.85 || math.Abs(tk[0].Change()-0.0188) > 1e-9 {
		t.Fatalf("tickers %+v, %v", tk, err)
	}
	f, err := m.Funding(ctx, "MEXC:BTCUSDT.P")
	if err != nil || f != (Funding{Mark: 60635, Rate: 0.000081, Next: 1795680000000}) {
		t.Fatalf("funding %+v, %v", f, err)
	}
	if err := parseMexcPerp([]byte(`{"success":false,"code":1001,"message":"contract not exists"}`), &struct{}{}); err == nil {
		t.Fatal("an error answered as fine")
	}
}

// MEXC's perpetuals' stream, as it came.
func TestMexcPerpStream(t *testing.T) {
	msg, err := parseMexcPerpMessage([]byte(`{"symbol":"BTC_USDT","data":{"symbol":"BTC_USDT","interval":"Min60","t":1795672800,"o":60790.4,"c":60620.2,"h":60795.3,"l":60590.8,"a":12544197.0373,"q":2067666,"ro":60792.5,"rc":60620.2,"rh":60795.3,"rl":60590.8},"channel":"push.kline","ts":1795673063120}`))
	if err != nil || msg.Channel != "push.kline" || msg.At != 1795673063120 {
		t.Fatalf("message %+v, %v", msg, err)
	}
	bar, err := parseMexcPerpKline(msg.Data, 0.0001)
	if err != nil || bar.T != 1795672800000 || bar.O != 60790.4 || bar.H != 60795.3 || bar.L != 60590.8 || bar.C != 60620.2 || math.Abs(bar.V-206.7666) > 1e-9 {
		t.Fatalf("bar %+v, %v", bar, err)
	}
	if _, err := parseMexcPerpMessage([]byte(`{"channel":"rs.error","data":"no such symbol","ts":1}`)); err == nil {
		t.Fatal("an error taken")
	}
	// The move over 24 hours is the first by time zone; riseFallRate is
	// UTC+8's day.
	msg, err = parseMexcPerpMessage([]byte(`{"symbol":"BTC_USDT","data":{"symbol":"BTC_USDT","lastPrice":60610.3,"riseFallRate":0.0393,"fairPrice":60612.5,"indexPrice":60640.5,"volume24":746096067,"amount24":4427985773.72953,"maxBidPrice":66704.5,"minAskPrice":54576.4,"lower24Price":57261.7,"high24Price":61600,"timestamp":1795673077146,"bid1":60610.2,"ask1":60610.3,"holdVol":480014262,"riseFallValue":2296.2,"fundingRate":0.000081,"zone":"UTC+8","riseFallRates":[0.0393,-0.0393,-0.2461,-0.2449,-0.1784,-0.3028],"riseFallRatesOfTimezone":[0.0249,0.0029,0.0393]},"channel":"push.ticker","ts":1795673077146}`))
	var x mexcTicker
	if err != nil || json.Unmarshal(msg.Data, &x) != nil {
		t.Fatalf("ticker message: %v", err)
	}
	if got := x.ticker(); got.Symbol != "MEXC:BTCUSDT.P" || got.Last != 60610.3 || got.QuoteVolume != 4427985773.72953 || math.Abs(got.Change()-0.0249) > 1e-9 {
		t.Fatalf("ticker %+v", got)
	}
}
