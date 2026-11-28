package market

import (
	"slices"
	"strings"
	"time"
)

// The markets beyond the coins, the CME's and New York's: Shanghai's and
// Shenzhen's A shares and indices, and Hong Kong's, from Eastmoney, which
// has them as they trade (Yahoo has them 30 and 15 minutes late); Korea's,
// and Cboe's VIX, from Yahoo, 20 and 15 minutes late (Eastmoney has no VIX,
// and its Korean minutes carry no volume).
//
// OpenBar names them by exchange, as TradingView does: SSE:600795 (国电电力),
// SSE:000001 (上证指数), SZSE:000001 (平安银行), HKEX:00700, HKEX:HSI,
// KRX:000660, CBOE:VIX; and the currency pairs FX:EURUSD, from Yahoo too, as
// they trade.

// The exchanges' clocks.
var (
	shanghai = zone("Asia/Shanghai", 8)
	hongKong = zone("Asia/Hong_Kong", 8)
	seoul    = zone("Asia/Seoul", 9)
	london   = zone("Europe/London", 0)
)

func zone(name string, hours int) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		return time.FixedZone(name, hours*3600)
	}
	return l
}

func hm(h, m int) time.Duration { return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute }

// Indices are the indices OpenBar offers by name: found by their Chinese
// names and pinyin, as the futures are. Ticker is the source's code for them.
var Indices = []Instrument{
	{ID: "CBOE:VIX", Code: "VIX", Name: "恐慌指数", Pinyin: "kong huang zhi shu", Ticker: "^VIX", Tick: 0.01, Index: true,
		Loc: newYork, Opens: hm(3, 0), Delay: 15 * time.Minute, Venue: "Cboe"},
	cnIndex("SSE", "000001", "上证指数", "shang zheng zhi shu"),
	cnIndex("SZSE", "399001", "深证成指", "shen zheng cheng zhi"),
	cnIndex("SZSE", "399006", "创业板指", "chuang ye ban zhi"),
	cnIndex("SSE", "000300", "沪深300", "hu shen"),
	cnIndex("SSE", "000688", "科创50", "ke chuang"),
	hkIndex("HSI", "100.HSI", "恒生指数", "heng sheng zhi shu"),
	hkIndex("HSTECH", "124.HSTECH", "恒生科技指数", "heng sheng ke ji zhi shu"),
	hkIndex("HSCEI", "100.HSCEI", "国企指数", "guo qi zhi shu"),
}

// Forex are the currency pairs OpenBar offers, from Yahoo (EURUSD=X): the
// majors, the common crosses and the yuan and Hong Kong dollar. Yahoo's days
// begin at midnight in London, its weeks on Monday then, and its pairs carry
// no volume.
var Forex = []Instrument{
	fx("EUR", "USD", "欧元/美元", "ou yuan mei yuan"),
	fx("GBP", "USD", "英镑/美元", "ying bang mei yuan"),
	fx("USD", "JPY", "美元/日元", "mei yuan ri yuan"),
	fx("AUD", "USD", "澳元/美元", "ao yuan mei yuan"),
	fx("USD", "CAD", "美元/加元", "mei yuan jia yuan"),
	fx("USD", "CHF", "美元/瑞郎", "mei yuan rui lang"),
	fx("NZD", "USD", "纽元/美元", "niu yuan mei yuan"),
	fx("EUR", "JPY", "欧元/日元", "ou yuan ri yuan"),
	fx("GBP", "JPY", "英镑/日元", "ying bang ri yuan"),
	fx("EUR", "GBP", "欧元/英镑", "ou yuan ying bang"),
	fx("AUD", "JPY", "澳元/日元", "ao yuan ri yuan"),
	fx("EUR", "CHF", "欧元/瑞郎", "ou yuan rui lang"),
	fx("EUR", "AUD", "欧元/澳元", "ou yuan ao yuan"),
	fx("CAD", "JPY", "加元/日元", "jia yuan ri yuan"),
	fx("CHF", "JPY", "瑞郎/日元", "rui lang ri yuan"),
	fx("USD", "CNH", "美元/离岸人民币", "mei yuan li an ren min bi"),
	fx("USD", "CNY", "美元/人民币", "mei yuan ren min bi"),
	fx("USD", "HKD", "美元/港元", "mei yuan gang yuan"),
}

// fx is a currency pair, read as EUR/USD and found by its name with or
// without the slash (欧元美元). Its prices step by a tenth of a pip: five
// decimals, a yen pair's three, the onshore yuan's four as it is quoted.
func fx(base, quote, name, pinyin string) Instrument {
	tick := 0.00001
	switch quote {
	case "JPY":
		tick = 0.001
	case "CNY":
		tick = 0.0001
	}
	return Instrument{ID: "FX:" + base + quote, Code: base + "/" + quote, Name: name, Alias: strings.ReplaceAll(name, "/", ""), Pinyin: pinyin,
		Ticker: base + quote + "=X", Tick: tick, FX: true, Loc: london, Venue: "外汇"}
}

func cnIndex(ex, code, name, pinyin string) Instrument {
	in := cnStock(ex, code)
	in.Name, in.Pinyin, in.Index = name, pinyin, true
	return in
}

func hkIndex(code, secid, name, pinyin string) Instrument {
	in := hkStock(code)
	in.Ticker, in.Name, in.Pinyin, in.Index, in.Tick = secid, name, pinyin, true, 0.01
	return in
}

// cnStock is an A share, or an index, of Shanghai (SSE) or Shenzhen (SZSE):
// Eastmoney's 1.600795 or 0.000001.
func cnStock(ex, code string) Instrument {
	in := Instrument{ID: ex + ":" + code, Code: code, Ticker: "1." + code, Tick: 0.01, Loc: shanghai, Opens: hm(9, 30), Venue: "上交所"}
	if ex == "SZSE" {
		in.Ticker, in.Venue = "0."+code, "深交所"
	}
	return in
}

// hkStock is a Hong Kong share, Eastmoney's 116.00700.
func hkStock(code string) Instrument {
	return Instrument{ID: "HKEX:" + code, Code: code, Ticker: "116." + code, Tick: 0.001, Loc: hongKong, Opens: hm(9, 30), Venue: "港交所"}
}

// krStock is a share of Korea's main board, Yahoo's 000660.KS. Its prices
// step by won.
func krStock(code string) Instrument {
	return Instrument{ID: "KRX:" + code, Code: code, Ticker: code + ".KS", Tick: 1, Loc: seoul, Opens: hm(9, 0),
		Delay: 20 * time.Minute, Venue: "韩国交易所"}
}

// listedOf is the market id names among these.
func listedOf(id string) (Instrument, bool) {
	if i := slices.IndexFunc(Indices, func(in Instrument) bool { return in.ID == id }); i >= 0 {
		return Indices[i], true
	}
	ex, code, _ := strings.Cut(id, ":")
	switch {
	case ex == "SSE" && digits(code, 6), ex == "SZSE" && digits(code, 6) && strings.ContainsAny(code[:1], "0123"):
		return cnStock(ex, code), true
	case ex == "HKEX" && digits(code, 5):
		return hkStock(code), true
	case ex == "KRX" && digits(code, 6):
		return krStock(code), true
	}
	return Instrument{}, false
}

func digits(s string, n int) bool {
	return len(s) == n && !strings.ContainsFunc(s, func(r rune) bool { return r < '0' || r > '9' })
}

// IsEastmoney is whether OpenBar's name for a market is one Eastmoney has
// the bars of: an A share's, a Hong Kong share's, or one of their indices.
func IsEastmoney(id string) bool {
	return strings.HasPrefix(id, "SSE:") || strings.HasPrefix(id, "SZSE:") || strings.HasPrefix(id, "HKEX:")
}

// IsListed is whether a market is one of an exchange's, a future, a stock
// or an index, rather than Binance's.
func IsListed(id string) bool { return IsYahoo(id) || IsEastmoney(id) }

// session is when a market trades on a day, from its open: the morning's and
// the afternoon's, as times of day on its exchange's clock.
type session struct{ open, close time.Duration }

// sessions are the parts of in's trading day: A shares break for lunch from
// 11:30 to 13:00, Hong Kong's from 12:00; a currency pair trades the whole
// day.
func (in Instrument) sessions() []session {
	switch {
	case in.FX:
		return []session{{0, 24 * time.Hour}}
	case in.Loc == shanghai:
		return []session{{hm(9, 30), hm(11, 30)}, {hm(13, 0), hm(15, 0)}}
	case in.Loc == hongKong:
		return []session{{hm(9, 30), hm(12, 0)}, {hm(13, 0), hm(16, 0)}}
	}
	return nil
}

// Minutes is how long in's trading day is: A shares' 240 minutes, Hong
// Kong's 330, a US stock's regular 390, a currency pair's 1440; 0 where
// OpenBar does not know it.
func (in Instrument) Minutes() int {
	if in.Loc == nil && !in.Future && !in.Index {
		return 390
	}
	var d time.Duration
	for _, s := range in.sessions() {
		d += s.close - s.open
	}
	return int(d / time.Minute)
}

// hourStarts are the times of day in's hour bars begin: an hour from each
// part's open, the last of a part shorter (Hong Kong's 11:30, which ends at
// noon); every fourth of them begins a 4-hour bar.
func (in Instrument) hourStarts(n int) []time.Duration {
	var out []time.Duration
	for _, s := range in.sessions() {
		for t := s.open; t < s.close; t += time.Hour {
			out = append(out, t)
		}
	}
	var every []time.Duration
	for i := 0; i < len(out); i += n {
		every = append(every, out[i])
	}
	return every
}
