package market

import (
	"context"
	"math"
	"net/http"
	"os"
	"slices"
	"testing"
	"time"
)

// TestYahooLive asks Yahoo itself, through the system proxy. CANDLE_LIVE=1
// turns it on.
func TestYahooLive(t *testing.T) {
	if os.Getenv("CANDLE_LIVE") == "" {
		t.Skip("CANDLE_LIVE is not set")
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = SystemProxy
	y := Yahoo{HTTP: &http.Client{Transport: tr, Timeout: 30 * time.Second}}
	ctx := context.Background()
	onTick := func(bars []Bar, tick float64) bool {
		for _, b := range bars {
			for _, p := range []float64{b.O, b.H, b.L, b.C} {
				if q := p / tick; math.Abs(q-math.Round(q)) > 1e-6 {
					return false
				}
			}
		}
		return true
	}
	for _, c := range []struct{ id, iv string }{{"CME:NQ", "5m"}, {"CME:MNQ", "1m"}, {"CME:MNQ", "4h"}, {"CME:ES", "1h"}, {"US:AAPL", "1h"}, {"US:AAPL", "4h"}, {"CME:MES", "1d"}, {"US:AAPL", "1w"}} {
		t0 := time.Now()
		bars, err := y.Klines(ctx, c.id, c.iv, 0, 1000)
		if err != nil {
			t.Fatal(c, err)
		}
		in, _ := InstrumentOf(c.id)
		last := bars[len(bars)-1]
		t.Logf("%s %s: %d bars in %v, %s .. %s, last %+v", c.id, c.iv, len(bars), time.Since(t0).Round(time.Millisecond),
			time.UnixMilli(bars[0].T).In(newYork).Format("01-02 15:04"), time.UnixMilli(last.T).In(newYork).Format("01-02 15:04"), last)
		// A stock has two 4-hour bars a day, and hours go back 730 days.
		if len(bars) != 1000 && !(c.iv == "4h" && !in.Future && len(bars) > 900) {
			t.Fatalf("%v: %d bars", c, len(bars))
		}
		if in.Future && c.iv != "1d" && !onTick(bars, in.Tick) {
			t.Fatalf("%v: prices off the %v tick", c, in.Tick)
		}
		for i := 1; i < len(bars); i++ {
			if bars[i].T <= bars[i-1].T {
				t.Fatalf("%v: bar %d out of order", c, i)
			}
		}
		older, err := y.Klines(ctx, c.id, c.iv, bars[0].T-1, 1000)
		if err != nil {
			t.Fatal(c, "older", err)
		}
		if len(older) > 0 && older[len(older)-1].T >= bars[0].T {
			t.Fatalf("%v: pages overlap", c)
		}
		if len(older) > 0 {
			t.Logf("   older page: %d bars from %s", len(older), time.UnixMilli(older[0].T).In(newYork).Format("2006-01-02 15:04"))
		} else if len(bars) == 1000 {
			t.Fatalf("%v: no older page after a full one", c)
		}
	}
	// The regular hours: hours from 9:30, quarters within 9:30–16:15.
	rth := y
	rth.RTH = true
	for _, iv := range []string{"1h", "15m", "4h"} {
		bars, err := rth.Klines(ctx, "CME:NQ", iv, 0, 1000)
		if err != nil || len(bars) == 0 {
			t.Fatalf("RTH %s: %d, %v", iv, len(bars), err)
		}
		for _, b := range bars {
			if !InRTH(b.T) || iv != "15m" && sinceMidnight(b.T)%3_600_000 != 1_800_000 {
				t.Fatalf("RTH %s bar at %s", iv, time.UnixMilli(b.T).In(newYork).Format("01-02 Mon 15:04"))
			}
		}
		last := bars[len(bars)-1]
		t.Logf("RTH %s: %d bars, %s .. %s, last %+v", iv, len(bars), time.UnixMilli(bars[0].T).In(newYork).Format("01-02 15:04"), time.UnixMilli(last.T).In(newYork).Format("01-02 15:04"), last)
	}
	syms, err := y.Symbols(ctx, "CME:MNQ", "US:AAPL")
	if err != nil || len(syms) != 2 || syms[1].Tick != 0.01 || syms[1].Desc == "" {
		t.Fatalf("symbols: %+v, %v", syms, err)
	}
	t.Logf("symbols: %+v", syms)
	q, err := y.Quotes(ctx, []string{"CME:NQ", "CME:MNQ", "US:AAPL", "US:QQQ"})
	if err != nil || len(q) != 4 {
		t.Fatalf("quotes: %+v, %v", q, err)
	}
	t.Logf("quotes: %+v", q)
	found, err := y.Search(ctx, "apple")
	if err != nil || !slices.ContainsFunc(found, func(in Instrument) bool { return in.ID == "US:AAPL" }) {
		t.Fatalf("search: %+v, %v", found, err)
	}
	t.Logf("search apple: %d, first %+v", len(found), found[0])
	ctx2, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	n := 0
	y.Stream(ctx2, "CME:NQ", "1m", func(b Bar, _ bool, at int64) {
		n++
		if n <= 4 {
			t.Logf("stream: %s %+v (%.1f min behind)", time.UnixMilli(b.T).Format("15:04"), b, float64(at-b.T)/60000)
		}
	})
	if n < 4 {
		t.Fatalf("stream gave %d bars in 12s", n)
	}
}

// TestYahooForexLive asks Yahoo for EUR/USD through the system proxy: its
// minutes, hours, 4 hours, days and weeks, its quote and its stream.
// Days open at midnight in London, none on a weekend, 4 hours at 0:00, 4:00…
// there; no volume. CANDLE_LIVE=1 turns it on.
func TestYahooForexLive(t *testing.T) {
	if os.Getenv("CANDLE_LIVE") == "" {
		t.Skip("CANDLE_LIVE is not set")
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = SystemProxy
	y := Yahoo{HTTP: &http.Client{Transport: tr, Timeout: 30 * time.Second}}
	ctx := context.Background()
	in, _ := InstrumentOf("FX:EURUSD")
	at := func(ms int64) string { return time.UnixMilli(ms).In(london).Format("Mon 01-02 15:04") }
	var price float64 // the newest hour's close
	for _, iv := range []string{"1m", "5m", "1h", "4h", "1d", "1w"} {
		bars, err := y.Klines(ctx, in.ID, iv, 0, 1000)
		if err != nil || len(bars) == 0 {
			t.Fatalf("%s: %d bars, %v", iv, len(bars), err)
		}
		var vol float64
		for i, b := range bars {
			vol += b.V
			if i > 0 && b.T <= bars[i-1].T {
				t.Fatalf("%s: bar %d out of order", iv, i)
			}
			l := time.UnixMilli(b.T).In(london)
			switch {
			case iv == "1d" && (in.SessionStart(b.T) != b.T || l.Weekday() == time.Saturday || l.Weekday() == time.Sunday):
				t.Fatalf("a day opening %s", at(b.T))
			case iv == "4h" && (l.Hour()%4 != 0 || l.Minute() != 0):
				t.Fatalf("4 hours opening %s", at(b.T))
			}
		}
		last := bars[len(bars)-1]
		if iv == "1h" {
			price = last.C
		}
		t.Logf("%s: %d bars, %s .. %s London, volume %v, last %+v", iv, len(bars), at(bars[0].T), at(last.T), vol, last)
	}
	syms, err := y.Symbols(ctx, "FX:EURUSD", "FX:USDJPY")
	if err != nil || len(syms) != 2 || syms[0].Tick != 0.00001 || syms[1].Tick != 0.001 {
		t.Fatalf("symbols: %+v, %v", syms, err)
	}
	q, err := y.Quotes(ctx, []string{"FX:EURUSD", "FX:USDJPY", "FX:USDCNH", "FX:USDCNY", "FX:USDHKD"})
	if err != nil || len(q) != 5 || q[0].Symbol != "FX:EURUSD" || q[0].Last != price {
		t.Fatalf("quotes, EUR/USD's newest hour closing at %v: %+v, %v", price, q, err)
	}
	t.Logf("quotes: %+v", q)
	ctx2, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	n := 0
	y.Stream(ctx2, "FX:EURUSD", "1m", func(b Bar, _ bool, at int64) {
		n++
		if n <= 2 {
			t.Logf("stream: %s %+v (%.1f min behind)", time.UnixMilli(b.T).In(london).Format("Mon 15:04"), b, float64(at-b.T)/60000)
		}
	})
	if n == 0 {
		t.Fatal("the stream gave no bars")
	}
}
