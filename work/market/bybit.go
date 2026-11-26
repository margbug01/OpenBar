package market

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// Bybit's v5 API serves its spot pairs and its perpetuals alike, a category
// apart. From mainland China it takes a proxy (the system's).
const (
	bybitPrefix = "BYBIT:"
	bybitREST   = "https://api.bybit.com"
	bybitWS     = "wss://stream.bybit.com/v5/public/"
)

// Bybit reads Bybit's spot pairs, Category "spot", or its USDT perpetuals,
// "linear", by OpenBar's names for them: "BYBIT:BTCUSDT", "BYBIT:BTCUSDT.P".
type Bybit struct {
	HTTP     *http.Client
	Category string
}

func (b Bybit) perp() bool { return b.Category == "linear" }

// id is OpenBar's name for Bybit's symbol.
func (b Bybit) id(symbol string) string {
	if b.perp() {
		return bybitPrefix + symbol + ".P"
	}
	return bybitPrefix + symbol
}

// call asks Bybit's path with q, filling result with what it answers.
func (b Bybit) call(ctx context.Context, path string, q url.Values, result any) error {
	q.Set("category", b.Category)
	body, err := get(ctx, b.HTTP, bybitREST+path+"?"+q.Encode())
	if err != nil {
		return err
	}
	return parseBybit(body, result)
}

// Bybit answers 200 with its own code: {"retCode": 0, "result": ...}.
func parseBybit(body []byte, result any) error {
	var r struct {
		Code   int             `json:"retCode"`
		Msg    string          `json:"retMsg"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return err
	}
	if r.Code != 0 {
		return fmt.Errorf("%d %s", r.Code, r.Msg)
	}
	return json.Unmarshal(r.Result, result)
}

// Klines returns up to limit (at most 1000) bars opening at or before end,
// as Binance.Klines.
func (b Bybit) Klines(ctx context.Context, symbol, interval string, end int64, limit int) ([]Bar, error) {
	iv, err := barLengthOf(interval)
	if err != nil {
		return nil, err
	}
	q := url.Values{"symbol": {Code(symbol)}, "interval": {iv.bybit}, "limit": {strconv.Itoa(limit)}}
	if end > 0 {
		q.Set("end", strconv.FormatInt(end, 10))
	}
	var r struct {
		List [][]string `json:"list"`
	}
	if err := b.call(ctx, "/v5/market/kline", q, &r); err != nil {
		return nil, fmt.Errorf("bybit klines: %w", err)
	}
	bars, err := parseBybitKlines(r.List)
	if err != nil {
		return nil, fmt.Errorf("bybit klines: %w", err)
	}
	return bars, nil
}

// Each row is ["start", "open", "high", "low", "close", "volume", "turnover"],
// the newest first.
func parseBybitKlines(rows [][]string) ([]Bar, error) {
	bars := make([]Bar, 0, len(rows))
	for _, r := range slices.Backward(rows) {
		if len(r) < 6 {
			return nil, fmt.Errorf("kline row has %d fields", len(r))
		}
		t, err := strconv.ParseInt(r[0], 10, 64)
		if err != nil {
			return nil, err
		}
		bar, err := ohlcv(t, [5]string(r[1:6]))
		if err != nil {
			return nil, fmt.Errorf("kline %d: %w", t, err)
		}
		bars = append(bars, bar)
	}
	return bars, nil
}

type bybitInstrument struct {
	Symbol   string `json:"symbol"`
	Base     string `json:"baseCoin"`
	Quote    string `json:"quoteCoin"`
	Status   string `json:"status"`
	Contract string `json:"contractType"`
	Price    struct {
		Tick string `json:"tickSize"`
	} `json:"priceFilter"`
}

// Symbols looks markets up by name: one by itself, more among them all. One
// unknown name fails the lookup.
func (b Bybit) Symbols(ctx context.Context, names ...string) ([]Symbol, error) {
	q := url.Values{}
	if len(names) == 1 {
		q.Set("symbol", Code(names[0]))
	}
	var all []Symbol
	for {
		var r struct {
			List   []bybitInstrument `json:"list"`
			Cursor string            `json:"nextPageCursor"`
		}
		if b.perp() {
			q.Set("limit", "1000")
		}
		if err := b.call(ctx, "/v5/market/instruments-info", q, &r); err != nil {
			return nil, fmt.Errorf("bybit symbols: %w", err)
		}
		all = append(all, b.symbols(r.List)...)
		if r.Cursor == "" || len(names) == 1 {
			break
		}
		q.Set("cursor", r.Cursor)
	}
	var out []Symbol
	for _, n := range names {
		i := slices.IndexFunc(all, func(s Symbol) bool { return s.Name == n })
		if i < 0 {
			return nil, fmt.Errorf("bybit: no %s", n)
		}
		out = append(out, all[i])
	}
	return out, nil
}

// symbols keeps the markets trading, and of the linear contracts the USDT
// perpetuals, not the futures that expire nor the USDC perpetuals.
func (b Bybit) symbols(list []bybitInstrument) []Symbol {
	var out []Symbol
	for _, s := range list {
		if s.Status != "Trading" || b.perp() && (s.Contract != "LinearPerpetual" || s.Quote != "USDT") {
			continue
		}
		tick, _ := strconv.ParseFloat(s.Price.Tick, 64)
		out = append(out, Symbol{Name: b.id(s.Symbol), Base: s.Base, Quote: s.Quote, Tick: tick})
	}
	return out
}

type bybitTicker struct {
	Symbol   string `json:"symbol"`
	Last     string `json:"lastPrice"`
	Prev     string `json:"prevPrice24h"`
	Turnover string `json:"turnover24h"`
	// The perpetuals'.
	Mark string `json:"markPrice"`
	Rate string `json:"fundingRate"`
	Next string `json:"nextFundingTime"`
}

// Tickers returns every market of the category traded in the last day: of
// the linear contracts, the USDT perpetuals.
func (b Bybit) Tickers(ctx context.Context) ([]Ticker, error) {
	var r struct {
		List []bybitTicker `json:"list"`
	}
	if err := b.call(ctx, "/v5/market/tickers", url.Values{}, &r); err != nil {
		return nil, fmt.Errorf("bybit tickers: %w", err)
	}
	return b.tickers(r.List)
}

func (b Bybit) tickers(list []bybitTicker) ([]Ticker, error) {
	out := make([]Ticker, 0, len(list))
	for _, x := range list {
		// The linear contracts' others: BTCUSDT-26DEC25, BTCPERP (in USDC).
		if b.perp() && (strings.Contains(x.Symbol, "-") || !strings.HasSuffix(x.Symbol, "USDT")) {
			continue
		}
		t, err := ticker(b.id(x.Symbol), x.Prev, x.Last, x.Turnover)
		if err != nil {
			return nil, err
		}
		if t.QuoteVolume > 0 {
			out = append(out, t)
		}
	}
	return out, nil
}

// Funding asks for a perpetual's mark price and funding.
func (b Bybit) Funding(ctx context.Context, symbol string) (Funding, error) {
	if !b.perp() {
		return Funding{}, fmt.Errorf("bybit: %s is no perpetual", symbol)
	}
	var r struct {
		List []bybitTicker `json:"list"`
	}
	if err := b.call(ctx, "/v5/market/tickers", url.Values{"symbol": {Code(symbol)}}, &r); err != nil {
		return Funding{}, fmt.Errorf("bybit funding: %w", err)
	}
	if len(r.List) != 1 {
		return Funding{}, fmt.Errorf("bybit funding: %d tickers for %s", len(r.List), symbol)
	}
	x := r.List[0]
	var f Funding
	var err error
	if f.Mark, err = strconv.ParseFloat(x.Mark, 64); err != nil {
		return Funding{}, fmt.Errorf("bybit mark price %q", x.Mark)
	}
	if f.Rate, err = strconv.ParseFloat(x.Rate, 64); err != nil {
		return Funding{}, fmt.Errorf("bybit funding rate %q", x.Rate)
	}
	if f.Next, err = strconv.ParseInt(x.Next, 10, 64); err != nil {
		return Funding{}, fmt.Errorf("bybit funding time %q", x.Next)
	}
	return f, nil
}

var bybitPing = map[string]string{"op": "ping"}

// bybitTopics are the subscriptions to topics: Bybit takes ten at a time on
// a spot stream.
func bybitTopics(topics []string) (subs []any) {
	for c := range slices.Chunk(topics, 10) {
		subs = append(subs, map[string]any{"op": "subscribe", "args": c})
	}
	return subs
}

// bybitMessage is what Bybit's streams send: a topic's data, or an answer to
// a subscription or a ping.
type bybitMessage struct {
	Topic   string          `json:"topic"`
	Type    string          `json:"type"`
	Data    json.RawMessage `json:"data"`
	Op      string          `json:"op"`
	Success *bool           `json:"success"`
	Msg     string          `json:"ret_msg"`
}

func parseBybitMessage(data []byte) (bybitMessage, error) {
	var m bybitMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return m, err
	}
	if m.Op == "subscribe" && m.Success != nil && !*m.Success {
		return m, fmt.Errorf("bybit subscribe: %s", m.Msg)
	}
	return m, nil
}

// Stream follows the current bar as Binance.Stream does, its updates about
// every second.
func (b Bybit) Stream(ctx context.Context, symbol, interval string, fn func(bar Bar, closed bool, at int64)) error {
	iv, err := barLengthOf(interval)
	if err != nil {
		return err
	}
	subs := bybitTopics([]string{"kline." + iv.bybit + "." + Code(symbol)})
	err = subscribe(ctx, b.HTTP, bybitWS+b.Category, subs, bybitPing, func(data []byte) error {
		m, err := parseBybitMessage(data)
		if err != nil || !strings.HasPrefix(m.Topic, "kline.") {
			return err
		}
		bars, err := parseBybitKline(m.Data)
		for _, k := range bars {
			fn(k.bar, k.closed, k.at)
		}
		return err
	})
	return fmt.Errorf("bybit stream: %w", err)
}

type bybitBar struct {
	bar    Bar
	closed bool
	at     int64
}

func parseBybitKline(data []byte) ([]bybitBar, error) {
	var rows []struct {
		Start   int64  `json:"start"`
		O       string `json:"open"`
		H       string `json:"high"`
		L       string `json:"low"`
		C       string `json:"close"`
		V       string `json:"volume"`
		Confirm bool   `json:"confirm"`
		At      int64  `json:"timestamp"`
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, err
	}
	out := make([]bybitBar, 0, len(rows))
	for _, r := range rows {
		bar, err := ohlcv(r.Start, [5]string{r.O, r.H, r.L, r.C, r.V})
		if err != nil {
			return nil, err
		}
		out = append(out, bybitBar{bar, r.Confirm, r.At})
	}
	return out, nil
}

// Watch follows the markets' last 24 hours as Binance.Watch does. The
// perpetuals' stream sends what changed only, so what came before is kept
// to fill in the rest.
func (b Bybit) Watch(ctx context.Context, symbols []string, fn func(Ticker)) error {
	topics := make([]string, len(symbols))
	for i, s := range symbols {
		topics[i] = "tickers." + Code(s)
	}
	known := map[string]bybitTicker{}
	err := subscribe(ctx, b.HTTP, bybitWS+b.Category, bybitTopics(topics), bybitPing, func(data []byte) error {
		m, err := parseBybitMessage(data)
		if err != nil || !strings.HasPrefix(m.Topic, "tickers.") {
			return err
		}
		t, ok, err := b.mergeTicker(known, m.Data)
		if ok {
			fn(t)
		}
		return err
	})
	return fmt.Errorf("bybit watch: %w", err)
}

// mergeTicker puts what a ticker message has over what known has of its
// market, and reads the market's day once it is whole.
func (b Bybit) mergeTicker(known map[string]bybitTicker, data []byte) (Ticker, bool, error) {
	var x bybitTicker
	if err := json.Unmarshal(data, &x); err != nil {
		return Ticker{}, false, err
	}
	k := known[x.Symbol]
	k.Symbol, k.Last, k.Prev, k.Turnover = x.Symbol, cmp.Or(x.Last, k.Last), cmp.Or(x.Prev, k.Prev), cmp.Or(x.Turnover, k.Turnover)
	known[x.Symbol] = k
	if k.Last == "" || k.Prev == "" || k.Turnover == "" {
		return Ticker{}, false, nil
	}
	t, err := ticker(b.id(k.Symbol), k.Prev, k.Last, k.Turnover)
	return t, err == nil, err
}
