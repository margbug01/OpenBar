package signalbot

import (
	"math"

	"candle/market"
)

// Setup is a pullback in a trend the scanner found: a trend making new highs
// (lows), a push, a pullback down to EMA20, and the first bar that closes
// back the trend's way. The entry is that bar's close, the stop the
// pullback's extreme less the pullback once more, the target 2R.
//
// The lab (zz_lab_test.go, six months of ETHUSDC.P 5m) found no rule that
// makes these pay, but four that mark the ones not to take; Faults names
// those a setup trips, empty for a clean one.
type Setup struct {
	Bar                 int64 // the signal bar's open time, ms
	Short               bool
	Entry, Stop, Target float64
	Legs                int     // the pullback's legs, 1 or 2
	Retrace             float64 // the pullback over the push
	Faults              []string
}

// Scan finds the setups in closed 5m bars, oldest first. A push's extreme
// gives one setup at most.
func Scan(b []market.Bar) []Setup {
	e, atr := ema(b, 20), wilderATR(b, 14)
	var out []Setup
	used := map[bool]int{false: -1, true: -1}
	for i := 300; i < len(b); i++ {
		for _, short := range []bool{false, true} {
			dir := 1.0
			hi := func(j int) float64 { return b[j].H }
			lo := func(j int) float64 { return b[j].L }
			if short {
				dir = -1
				hi, lo = lo, hi
			}
			// a bar closing the trend's way, beyond a rising EMA20
			if (b[i].C-b[i].O)*dir <= 0 || (b[i].C-e[i])*dir <= 0 || (e[i]-e[i-10])*dir <= 0 {
				continue
			}
			// the push's extreme in the 15 bars before this one
			h := i - 15
			for j := i - 15; j < i; j++ {
				if (hi(j)-hi(h))*dir > 0 {
					h = j
				}
			}
			if h >= i-1 || h == used[short] || !extreme(b, h, 60, hi, dir) {
				continue
			}
			// the pullback's extreme m, and this the first bar back since
			m := h + 1
			for j := h + 1; j <= i; j++ {
				if (lo(j)-lo(m))*dir < 0 {
					m = j
				}
			}
			first := true
			for j := m + 1; j < i; j++ {
				if (b[j].C-b[j].O)*dir > 0 {
					first = false
				}
			}
			if !first || (lo(m)-e[m])*dir > 0.0015*e[m] {
				continue // not the first bar back, or the pullback stopped short of EMA20
			}
			st := h
			for j := max(h-40, 0); j < h; j++ {
				if (lo(j)-lo(st))*dir < 0 {
					st = j
				}
			}
			push, pb := (hi(h)-lo(st))*dir, (hi(h)-lo(m))*dir
			if push <= 0 || pb <= 0 {
				continue
			}
			s := Setup{Bar: b[i].T, Short: short, Entry: b[i].C, Stop: lo(m) - dir*pb, Retrace: pb / push, Legs: 1}
			s.Target = s.Entry + 2*(s.Entry-s.Stop)
			for j := h + 1; j < m; j++ {
				if (b[j].C-b[j].O)*dir > 0 {
					s.Legs = 2
				}
			}
			big := false
			for j := i - 24; j <= i; j++ {
				if b[j].H-b[j].L > 3*atr[j-1] {
					big = true
				}
			}
			if big {
				s.Faults = append(s.Faults, "近两小时有大K线")
			}
			if efficiency(b, i, 48, dir) < 0.08 {
				s.Faults = append(s.Faults, "4小时在拉锯")
			}
			if s.Retrace > 0.5 {
				s.Faults = append(s.Faults, "回撤过半")
			}
			if touches(b, e, i, lo, dir) >= 3 {
				s.Faults = append(s.Faults, "第3次及以后回踩EMA20")
			}
			out = append(out, s)
			used[short] = h
		}
	}
	return out
}

// extreme is whether bar h's hi beats the n bars before it.
func extreme(b []market.Bar, h, n int, hi func(int) float64, dir float64) bool {
	for j := h - n; j < h; j++ {
		if (hi(j)-hi(h))*dir >= 0 {
			return false
		}
	}
	return true
}

// touches counts the pullbacks to EMA20 since price last closed across it,
// this one included.
func touches(b []market.Bar, e []float64, i int, lo func(int) float64, dir float64) int {
	n := 0
	for j := i - 1; j > i-200; j-- {
		if (b[j].C-e[j])*dir < 0 {
			break
		}
		if (lo(j)-e[j])*dir < 0.0006*e[j] && (lo(j-1)-e[j-1])*dir >= 0.0006*e[j-1] {
			n++
		}
	}
	return max(n, 1)
}

// efficiency is Kaufman's efficiency ratio over n bars ending at i, signed by
// dir: 1 a straight line the trend's way, near 0 a chop.
func efficiency(b []market.Bar, i, n int, dir float64) float64 {
	if i-n < 1 {
		return 0
	}
	path := 0.0
	for j := i - n + 1; j <= i; j++ {
		path += math.Abs(b[j].C - b[j-1].C)
	}
	if path == 0 {
		return 0
	}
	return (b[i].C - b[i-n].C) * dir / path
}

// ema starts from the SMA of the first n closes, as TradingView does.
func ema(b []market.Bar, n int) []float64 {
	e := make([]float64, len(b))
	k := 2 / float64(n+1)
	s := 0.0
	for i := range b {
		switch {
		case i < n-1:
			s += b[i].C
			e[i] = math.NaN()
		case i == n-1:
			s += b[i].C
			e[i] = s / float64(n)
		default:
			e[i] = b[i].C*k + e[i-1]*(1-k)
		}
	}
	return e
}

func wilderATR(b []market.Bar, n int) []float64 {
	a := make([]float64, len(b))
	for i := range b {
		tr := b[i].H - b[i].L
		if i > 0 {
			tr = math.Max(tr, math.Max(math.Abs(b[i].H-b[i-1].C), math.Abs(b[i].L-b[i-1].C)))
			a[i] = (a[i-1]*float64(n-1) + tr) / float64(n)
		} else {
			a[i] = tr
		}
	}
	return a
}
