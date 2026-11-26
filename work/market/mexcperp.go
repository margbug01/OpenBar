package market

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// MEXC's perpetuals have an API of their own, its streams in JSON. Its
// symbols part the coin from the money, BTC_USDT; OpenBar names that one
// "MEXC:BTCUSDT.P". From mainland China it takes a proxy (the system's).
const (
	mexcPerpREST = "https://contract.mexc.com/api/v1/contract"
	mexcPerpWS   = "wss://contract.mexc.com/edge"
)

// MexcPerp reads MEXC's USDT perpetuals.
type MexcPerp struct{ HTTP *http.Client }

// mexcContract is MEXC's symbol for a perpetual OpenBar names id.
func mexcContract(id string) string { return strings.TrimSuffix(Code(id), "USDT") + "_USDT" }

func mexcPerpID(contract string) string {
	return mexcPrefix + strings.ReplaceAll(contract, "_", "") + ".P"
}

// call asks MEXC's path, filling data with what it answers.
func (m MexcPerp) call(ctx context.Context, path string, data any) error {
	body, err := get(ctx, m.HTTP, mexcPerpREST+path)
	if err != nil {
		return err
	}
	return parseMexcPerp(body, data)
}

// MEXC answers 200 with its own word: {"success": true, "data": ...}.
func parseMexcPerp(body []byte, data any) error {
	var r struct {
		Success bool            `json:"success"`
		Code    int             `json:"code"`
		Msg     string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return err
	}
	if !r.Success {
		return fmt.Errorf("%d %s", r.Code, r.Msg)
	}
	return json.Unmarshal(r.Data, data)
}

// A perpetual's volume counts contracts, each a fixed amount of the coin
// (0.0001 BTC): bars read it in the coin, as Binance's do. The amounts,
// once asked for, are kept for every MexcPerp.
var mexcSizes sync.Map // contract → float64

func (m MexcPerp) size(ctx context.Context, contract string) (float64, error) {
	if s, ok := mexcSizes.Load(contract); ok {
		return s.(float64), nil
	}
	var d mexcDetail
	if err := m.call(ctx, "/detail?symbol="+url.QueryEscape(contract), &d); err != nil {
		return 0, err
	}
	mexcSizes.Store(contract, d.Size)
	return d.Size, nil
}

// Klines returns up to limit (at most 2000) bars opening at or before end,
// as Binance.Klines.
func (m MexcPerp) Klines(ctx context.Context, symbol, interval string, end int64, limit int) ([]Bar, error) {
	iv, err := barLengthOf(interval)
	if err != nil {
		return nil, err
	}
	c := mexcContract(symbol)
	size, err := m.size(ctx, c)
	if err != nil {
		return nil, fmt.Errorf("mexc perp klines: %w", err)
	}
	if end <= 0 {
		end = time.Now().UnixMilli()
	}
	q := url.Values{"interval": {iv.mexcFull}, "start": {strconv.FormatInt((end-int64(limit)*iv.ms)/1000+1, 10)}, "end": {strconv.FormatInt(end/1000, 10)}}
	var k mexcKlines
	if err := m.call(ctx, "/kline/"+c+"?"+q.Encode(), &k); err != nil {
		return nil, fmt.Errorf("mexc perp klines: %w", err)
	}
	bars, err := k.bars(size)
	if err != nil {
		return nil, fmt.Errorf("mexc perp klines: %w", err)
	}
	return bars[max(0, len(bars)-limit):], nil
}

// mexcKlines are bars in columns, their times in seconds, the oldest first.
type mexcKlines struct {
	Time                   []int64 `json:"time"`
	Open, Close, High, Low []float64
	Vol                    []float64 `json:"vol"` // in contracts
}

func (k mexcKlines) bars(size float64) ([]Bar, error) {
	n := len(k.Time)
	if len(k.Open) != n || len(k.Close) != n || len(k.High) != n || len(k.Low) != n || len(k.Vol) != n {
		return nil, fmt.Errorf("kline columns of unlike lengths")
	}
	bars := make([]Bar, n)
	for i := range n {
		bars[i] = Bar{T: k.Time[i] * 1000, O: k.Open[i], H: k.High[i], L: k.Low[i], C: k.Close[i], V: k.Vol[i] * size}
	}
	return bars, nil
}

type mexcDetail struct {
	Symbol string  `json:"symbol"`
	Base   string  `json:"baseCoin"`
	Quote  string  `json:"quoteCoin"`
	Tick   float64 `json:"priceUnit"`
	Size   float64 `json:"contractSize"`
	State  int     `json:"state"`
}

// Symbols looks perpetuals up. MEXC answers with all of them at once; one
// unknown name fails the lookup.
func (m MexcPerp) Symbols(ctx context.Context, names ...string) ([]Symbol, error) {
	var all []mexcDetail
	if err := m.call(ctx, "/detail", &all); err != nil {
		return nil, fmt.Errorf("mexc perp symbols: %w", err)
	}
	syms := mexcSymbols(all)
	var out []Symbol
	for _, n := range names {
		i := slices.IndexFunc(syms, func(s Symbol) bool { return s.Name == n })
		if i < 0 {
			return nil, fmt.Errorf("mexc perp: no %s", n)
		}
		out = append(out, syms[i])
	}
	return out, nil
}

// mexcSymbols keeps the USDT perpetuals trading (state 0), noting their
// contracts' sizes.
func mexcSymbols(all []mexcDetail) []Symbol {
	var out []Symbol
	for _, d := range all {
		if d.State != 0 || d.Quote != "USDT" {
			continue
		}
		mexcSizes.Store(d.Symbol, d.Size)
		out = append(out, Symbol{Name: mexcPerpID(d.Symbol), Base: d.Base, Quote: d.Quote, Tick: d.Tick})
	}
	return out
}

// mexcTicker is a perpetual's day, as both the REST API and the stream tell
// it.
type mexcTicker struct {
	Symbol string  `json:"symbol"`
	Last   float64 `json:"lastPrice"`
	Amount float64 `json:"amount24"` // in USDT
	// The move since the day began in UTC+8, and over the last 24 hours,
	// first among those by time zone.
	Rate   float64   `json:"riseFallRate"`
	ByZone []float64 `json:"riseFallRatesOfTimezone"`
}

func (x mexcTicker) ticker() Ticker {
	r := x.Rate
	if len(x.ByZone) > 0 {
		r = x.ByZone[0]
	}
	return Ticker{Symbol: mexcPerpID(x.Symbol), Open: x.Last / (1 + r), Last: x.Last, QuoteVolume: x.Amount}
}

// Tickers returns every USDT perpetual traded in the last day.
func (m MexcPerp) Tickers(ctx context.Context) ([]Ticker, error) {
	var all []mexcTicker
	if err := m.call(ctx, "/ticker", &all); err != nil {
		return nil, fmt.Errorf("mexc perp tickers: %w", err)
	}
	return mexcTickers(all), nil
}

func mexcTickers(all []mexcTicker) []Ticker {
	var out []Ticker
	for _, x := range all {
		if strings.HasSuffix(x.Symbol, "_USDT") && x.Amount > 0 {
			out = append(out, x.ticker())
		}
	}
	return out
}

// Funding asks for a perpetual's fair price, MEXC's mark, and funding.
func (m MexcPerp) Funding(ctx context.Context, symbol string) (Funding, error) {
	var r struct {
		Rate float64 `json:"fundingRate"`
		Next int64   `json:"nextSettleTime"`
		Fair float64 `json:"fairPrice"`
	}
	if err := m.call(ctx, "/funding_rate/"+mexcContract(symbol), &r); err != nil {
		return Funding{}, fmt.Errorf("mexc funding: %w", err)
	}
	return Funding{Mark: r.Fair, Rate: r.Rate, Next: r.Next}, nil
}

var mexcPerpPing = map[string]string{"method": "ping"}

// mexcPerpMessage is what MEXC's perpetuals' stream sends: data on a
// channel, push.kline or push.ticker, or rs.error.
type mexcPerpMessage struct {
	Channel string          `json:"channel"`
	Data    json.RawMessage `json:"data"`
	At      int64           `json:"ts"`
}

func parseMexcPerpMessage(data []byte) (mexcPerpMessage, error) {
	var m mexcPerpMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return m, err
	}
	if m.Channel == "rs.error" {
		return m, fmt.Errorf("mexc perp: %s", m.Data)
	}
	return m, nil
}

// Stream follows the current bar as Binance.Stream does. MEXC does not say
// when a bar closes: closed is always false.
func (m MexcPerp) Stream(ctx context.Context, symbol, interval string, fn func(bar Bar, closed bool, at int64)) error {
	iv, err := barLengthOf(interval)
	if err != nil {
		return err
	}
	c := mexcContract(symbol)
	size, err := m.size(ctx, c)
	if err != nil {
		return fmt.Errorf("mexc perp stream: %w", err)
	}
	subs := []any{map[string]any{"method": "sub.kline", "param": map[string]string{"symbol": c, "interval": iv.mexcFull}}}
	err = subscribe(ctx, m.HTTP, mexcPerpWS, subs, mexcPerpPing, func(data []byte) error {
		msg, err := parseMexcPerpMessage(data)
		if err != nil || msg.Channel != "push.kline" {
			return err
		}
		bar, err := parseMexcPerpKline(msg.Data, size)
		if err == nil {
			fn(bar, false, msg.At)
		}
		return err
	})
	return fmt.Errorf("mexc perp stream: %w", err)
}

// A bar on the stream: t its open in seconds, q its volume in contracts.
func parseMexcPerpKline(data []byte, size float64) (Bar, error) {
	var k struct {
		T          int64 `json:"t"`
		O, H, L, C float64
		Q          float64 `json:"q"`
	}
	if err := json.Unmarshal(data, &k); err != nil {
		return Bar{}, err
	}
	return Bar{T: k.T * 1000, O: k.O, H: k.H, L: k.L, C: k.C, V: k.Q * size}, nil
}

// Watch follows the perpetuals' last 24 hours as Binance.Watch does.
func (m MexcPerp) Watch(ctx context.Context, symbols []string, fn func(Ticker)) error {
	subs := make([]any, len(symbols))
	for i, s := range symbols {
		subs[i] = map[string]any{"method": "sub.ticker", "param": map[string]string{"symbol": mexcContract(s)}}
	}
	err := subscribe(ctx, m.HTTP, mexcPerpWS, subs, mexcPerpPing, func(data []byte) error {
		msg, err := parseMexcPerpMessage(data)
		if err != nil || msg.Channel != "push.ticker" {
			return err
		}
		var x mexcTicker
		if err := json.Unmarshal(msg.Data, &x); err != nil {
			return err
		}
		fn(x.ticker())
		return nil
	})
	return fmt.Errorf("mexc perp watch: %w", err)
}
