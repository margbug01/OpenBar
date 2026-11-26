package market

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// Crypto is a feed of a crypto exchange's spot pairs or of its perpetuals:
// a Feed that also lists every market it has, for the search.
type Crypto interface {
	Feed
	Tickers(ctx context.Context) ([]Ticker, error)
}

// A Funder tells a perpetual's mark price and funding.
type Funder interface {
	Funding(ctx context.Context, symbol string) (Funding, error)
}

var (
	_ Crypto = Binance{}
	_ Crypto = Perp{}
	_ Crypto = Bybit{}
	_ Crypto = Mexc{}
	_ Crypto = MexcPerp{}
	_ Funder = Perp{}
	_ Funder = Bybit{}
	_ Funder = MexcPerp{}
)

// An Exchange is a crypto exchange whose spot pairs and USDT perpetuals
// OpenBar shows. OpenBar names its markets as TradingView does: Prefix,
// the exchange's own symbol, and ".P" for a perpetual: "BYBIT:BTCUSDT",
// "BYBIT:BTCUSDT.P". Binance's have no prefix, as they had before the
// others came, so the settings, watchlists and drawings keyed by them stay.
type Exchange struct {
	Prefix     string
	Name       string // as the legend shows it: "Bybit"
	Spot, Perp func(*http.Client) Crypto
}

// Exchanges are the crypto exchanges, Binance first: for a pair all of
// them have, the search shows Binance's first.
var Exchanges = []Exchange{
	{"", "Binance", func(c *http.Client) Crypto { return Binance{HTTP: c} }, func(c *http.Client) Crypto { return Perp{HTTP: c} }},
	{bybitPrefix, "Bybit", func(c *http.Client) Crypto { return Bybit{HTTP: c, Category: "spot"} }, func(c *http.Client) Crypto { return Bybit{HTTP: c, Category: "linear"} }},
	{mexcPrefix, "MEXC", func(c *http.Client) Crypto { return Mexc{HTTP: c} }, func(c *http.Client) Crypto { return MexcPerp{HTTP: c} }},
}

// ExchangeOf is the exchange a crypto market is on; false for a future's,
// a stock's or an index's.
func ExchangeOf(id string) (Exchange, bool) {
	if IsListed(id) {
		return Exchange{}, false
	}
	for _, ex := range Exchanges[1:] {
		if strings.HasPrefix(id, ex.Prefix) {
			return ex, true
		}
	}
	return Exchanges[0], true
}

// Feed is the feed of a crypto market: its exchange's spot pairs' or
// perpetuals'.
func (ex Exchange) Feed(c *http.Client, id string) Crypto {
	if IsPerp(id) {
		return ex.Perp(c)
	}
	return ex.Spot(c)
}

// barLength is one of OpenBar's bar lengths ("1h"), as the exchanges whose
// names for them are not Binance's write it.
type barLength struct {
	ms       int64
	bybit    string // "60"
	mexc     string // MEXC's spot REST API: "60m"
	mexcFull string // its spot streams and its perpetuals: "Min60"
}

var barLengths = map[string]barLength{
	"1m":  {60_000, "1", "1m", "Min1"},
	"5m":  {300_000, "5", "5m", "Min5"},
	"15m": {900_000, "15", "15m", "Min15"},
	"1h":  {3_600_000, "60", "60m", "Min60"},
	"4h":  {14_400_000, "240", "4h", "Hour4"},
	"1d":  {86_400_000, "D", "1d", "Day1"},
	"1w":  {604_800_000, "W", "1W", "Week1"},
}

func barLengthOf(interval string) (barLength, error) {
	b, ok := barLengths[interval]
	if !ok {
		return barLength{}, fmt.Errorf("no bars of %q", interval)
	}
	return b, nil
}

// Code is a crypto market's symbol on its exchange, without the exchange
// and the perpetual's ".P": "BTCUSDT" for "BYBIT:BTCUSDT.P".
func Code(id string) string {
	ex, _ := ExchangeOf(id)
	return strings.TrimSuffix(strings.TrimPrefix(id, ex.Prefix), ".P")
}
