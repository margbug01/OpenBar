package market

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// MEXC's spot API is shaped as Binance's, but for its names of bar lengths
// and its pages of 500 bars; its streams come as protobuf only, the JSON
// ones being shut. From mainland China it takes a proxy (the system's).
const (
	mexcPrefix = "MEXC:"
	mexcREST   = "https://api.mexc.com"
	mexcWS     = "wss://wbs-api.mexc.com/ws"
	mexcPage   = 500
)

// Mexc reads MEXC's spot pairs, by OpenBar's names for them: "MEXC:BTCUSDT".
type Mexc struct{ HTTP *http.Client }

// Klines returns up to limit bars opening at or before end, as
// Binance.Klines, asking for them a page at a time from the newest.
func (m Mexc) Klines(ctx context.Context, symbol, interval string, end int64, limit int) ([]Bar, error) {
	iv, err := barLengthOf(interval)
	if err != nil {
		return nil, err
	}
	if end <= 0 {
		end = time.Now().UnixMilli()
	}
	var bars []Bar
	for len(bars) < limit {
		n := min(mexcPage, limit-len(bars))
		// MEXC reads a page on from its start, up to before its end; one n
		// bars long has n at most.
		q := url.Values{"symbol": {Code(symbol)}, "interval": {iv.mexc}, "limit": {strconv.Itoa(n)},
			"startTime": {strconv.FormatInt(end+1-int64(n)*iv.ms, 10)}, "endTime": {strconv.FormatInt(end+1, 10)}}
		body, err := get(ctx, m.HTTP, mexcREST+"/api/v3/klines?"+q.Encode())
		if err != nil {
			return nil, fmt.Errorf("mexc klines: %w", err)
		}
		page, err := parseKlines(body)
		if err != nil {
			return nil, fmt.Errorf("mexc klines: %w", err)
		}
		if len(page) == 0 {
			break
		}
		bars, end = append(page, bars...), page[0].T-1
	}
	return bars, nil
}

// Symbols looks markets up by name; one unknown name fails the lookup.
// MEXC tells a price's step as a number of decimals.
func (m Mexc) Symbols(ctx context.Context, names ...string) ([]Symbol, error) {
	codes := make([]string, len(names))
	for i, n := range names {
		codes[i] = Code(n)
	}
	q := url.Values{"symbols": {strings.Join(codes, ",")}}
	if len(codes) == 1 {
		q = url.Values{"symbol": {codes[0]}}
	}
	body, err := get(ctx, m.HTTP, mexcREST+"/api/v3/exchangeInfo?"+q.Encode())
	if err != nil {
		return nil, fmt.Errorf("mexc symbols: %w", err)
	}
	syms, err := parseMexcSymbols(body)
	if err != nil {
		return nil, fmt.Errorf("mexc symbols: %w", err)
	}
	if len(syms) != len(names) {
		return nil, fmt.Errorf("mexc symbols: %d of %d found", len(syms), len(names))
	}
	return syms, nil
}

func parseMexcSymbols(body []byte) ([]Symbol, error) {
	var r struct {
		Symbols []struct {
			Symbol    string `json:"symbol"`
			Base      string `json:"baseAsset"`
			Quote     string `json:"quoteAsset"`
			Precision int    `json:"quotePrecision"`
		} `json:"symbols"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	out := make([]Symbol, 0, len(r.Symbols))
	for _, s := range r.Symbols {
		out = append(out, Symbol{Name: mexcPrefix + s.Symbol, Base: s.Base, Quote: s.Quote, Tick: math.Pow10(-s.Precision)})
	}
	return out, nil
}

// Tickers returns every market traded in the last day, about 650 KB.
func (m Mexc) Tickers(ctx context.Context) ([]Ticker, error) {
	body, err := get(ctx, m.HTTP, mexcREST+"/api/v3/ticker/24hr")
	if err != nil {
		return nil, fmt.Errorf("mexc tickers: %w", err)
	}
	return parseMexcTickers(body)
}

func parseMexcTickers(body []byte) ([]Ticker, error) {
	var rows []struct {
		Symbol      string `json:"symbol"`
		Open        string `json:"openPrice"`
		Last        string `json:"lastPrice"`
		QuoteVolume string `json:"quoteVolume"`
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, err
	}
	out := make([]Ticker, 0, len(rows))
	for _, r := range rows {
		t, err := ticker(mexcPrefix+r.Symbol, r.Open, r.Last, r.QuoteVolume)
		if err == nil && t.QuoteVolume > 0 { // a market without a trade has no volume, null
			out = append(out, t)
		}
	}
	return out, nil
}

var mexcPing = map[string]string{"method": "PING"}

func mexcSubscription(channels ...string) []any {
	return []any{map[string]any{"method": "SUBSCRIPTION", "params": channels}}
}

// Stream follows the current bar as Binance.Stream does. MEXC does not say
// when a bar closes: closed is always false.
func (m Mexc) Stream(ctx context.Context, symbol, interval string, fn func(bar Bar, closed bool, at int64)) error {
	iv, err := barLengthOf(interval)
	if err != nil {
		return err
	}
	subs := mexcSubscription("spot@public.kline.v3.api.pb@" + Code(symbol) + "@" + iv.mexcFull)
	err = subscribe(ctx, m.HTTP, mexcWS, subs, mexcPing, func(data []byte) error {
		p, err := parseMexcPush(data)
		if err == nil && p.kline != nil {
			fn(*p.kline, false, p.at)
		}
		return err
	})
	return fmt.Errorf("mexc stream: %w", err)
}

// Watch follows the markets' last 24 hours as Binance.Watch does. MEXC takes
// 30 subscriptions on a connection.
func (m Mexc) Watch(ctx context.Context, symbols []string, fn func(Ticker)) error {
	channels := make([]string, len(symbols))
	for i, s := range symbols {
		channels[i] = "spot@public.miniTicker.v3.api.pb@" + Code(s) + "@24H"
	}
	err := subscribe(ctx, m.HTTP, mexcWS, mexcSubscription(channels...), mexcPing, func(data []byte) error {
		p, err := parseMexcPush(data)
		if err == nil && p.ticker != nil {
			fn(*p.ticker)
		}
		return err
	})
	return fmt.Errorf("mexc watch: %w", err)
}

// mexcPush is what MEXC's spot stream sent: a bar or a market's day, and
// when; neither for its answers to a subscription or a ping, which come as
// JSON.
type mexcPush struct {
	kline  *Bar
	ticker *Ticker
	at     int64
}

func parseMexcPush(data []byte) (mexcPush, error) {
	if len(data) > 0 && data[0] == '{' {
		var r struct {
			Msg string `json:"msg"`
		}
		if err := json.Unmarshal(data, &r); err != nil {
			return mexcPush{}, err
		}
		if strings.Contains(r.Msg, "Not Subscribed") {
			return mexcPush{}, errors.New(r.Msg)
		}
		return mexcPush{}, nil
	}
	// PushDataV3ApiWrapper: 3 the symbol, 5 when it was made, 308 a bar
	// (PublicSpotKlineV3Api), 309 a day (PublicMiniTickerV3Api).
	var p mexcPush
	var symbol string
	var kline, mini []byte
	err := pbFields(data, func(num int, v uint64, b []byte) {
		switch num {
		case 3:
			symbol = string(b)
		case 5:
			p.at = int64(v)
		case 308:
			kline = b
		case 309:
			mini = b
		}
	})
	if err != nil {
		return mexcPush{}, err
	}
	switch {
	case kline != nil:
		// 2 the open time in seconds, 3 open, 4 close, 5 high, 6 low, 7 volume.
		var t int64
		var s [5]string
		err = pbFields(kline, func(num int, v uint64, b []byte) {
			switch num {
			case 2:
				t = int64(v) * 1000
			case 3:
				s[0] = string(b)
			case 5:
				s[1] = string(b)
			case 6:
				s[2] = string(b)
			case 4:
				s[3] = string(b)
			case 7:
				s[4] = string(b)
			}
		})
		if err != nil {
			return mexcPush{}, err
		}
		bar, err := ohlcv(t, s)
		if err != nil {
			return mexcPush{}, fmt.Errorf("mexc kline: %w", err)
		}
		p.kline = &bar
	case mini != nil:
		// 2 the price, 4 its move over the zone subscribed to (24H), 7 the
		// day's volume in the quote asset.
		var price, rate, quote string
		err = pbFields(mini, func(num int, v uint64, b []byte) {
			switch num {
			case 2:
				price = string(b)
			case 4:
				rate = string(b)
			case 7:
				quote = string(b)
			}
		})
		if err != nil {
			return mexcPush{}, err
		}
		t, err := ticker(mexcPrefix+symbol, "0", price, quote)
		if err != nil {
			return mexcPush{}, err
		}
		r, err := strconv.ParseFloat(rate, 64)
		if err != nil {
			return mexcPush{}, fmt.Errorf("mexc ticker %s: %w", symbol, err)
		}
		t.Open = t.Last / (1 + r)
		p.ticker = &t
	}
	return p, nil
}

// pbFields reads a protobuf message's fields, handing fn each's number and
// its value: v of a varint, b of a string or a message.
func pbFields(data []byte, fn func(num int, v uint64, b []byte)) error {
	for len(data) > 0 {
		key, n := binary.Uvarint(data)
		if n <= 0 {
			return errors.New("protobuf: bad key")
		}
		data = data[n:]
		num := int(key >> 3)
		switch key & 7 {
		case 0:
			v, n := binary.Uvarint(data)
			if n <= 0 {
				return errors.New("protobuf: bad varint")
			}
			fn(num, v, nil)
			data = data[n:]
		case 2:
			l, n := binary.Uvarint(data)
			if n <= 0 || l > uint64(len(data)-n) {
				return errors.New("protobuf: bad length")
			}
			fn(num, 0, data[n:n+int(l)])
			data = data[n+int(l):]
		case 1, 5: // fixed 64 or 32 bits, which MEXC's messages do not use
			w := map[uint64]int{1: 8, 5: 4}[key&7]
			if len(data) < w {
				return errors.New("protobuf: short fixed field")
			}
			data = data[w:]
		default:
			return fmt.Errorf("protobuf: wire type %d", key&7)
		}
	}
	return nil
}
