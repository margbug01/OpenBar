package signalbot

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"candle/market"
)

// The owner's fees on Binance, from their fills: a market order (entry, the
// stop, a close at the time limit) pays taker, the target's limit order
// maker.
const (
	takerFee = 0.0004
	makerFee = 0
	// Risk is what a paper trade loses at its stop, before fees: under the
	// owner's cap of 30 U a trade.
	Risk = 20.0
	// Hold is how long a trade may run before it is closed at the market,
	// as the lab measured them.
	Hold = 24 * time.Hour
)

// Record is a signal and all that came of it; sent/<id>.json keeps it.
type Record struct {
	Signal
	Msg     int64     `json:"msg,omitempty"`     // the Telegram message
	Caption string    `json:"caption,omitempty"` // the plan as first sent
	Until   time.Time `json:"until"`
	Choice  string    `json:"choice,omitempty"` // do, skip, wait, expired; "" while it waits
	Note    string    `json:"note,omitempty"`   // the owner's words on it
	Plan    *Exit     `json:"plan,omitempty"`   // how the plan itself ended, whatever the choice
	Paper   *Paper    `json:"paper,omitempty"`  // the paper order 下单 placed
}

// Paper is a market order filled at the price of the moment 下单 was pressed,
// with the plan's stop and target.
type Paper struct {
	Fill float64   `json:"fill"`
	At   time.Time `json:"at"`
	Qty  float64   `json:"qty"`
	Exit *Exit     `json:"exit,omitempty"`
}

// Exit is how a trade ended: win at the target, loss at the stop, timeout
// at the close after Hold.
type Exit struct {
	Kind  string    `json:"kind"`
	Price float64   `json:"price"`
	At    time.Time `json:"at"`
}

func (r *Record) dir() float64 {
	if r.Short {
		return -1
	}
	return 1
}

// done is whether nothing more can happen to it.
func (r *Record) done() bool {
	return r.Choice != "" && r.Plan != nil && (r.Paper == nil || r.Paper.Exit != nil)
}

// R is a trade's result in what its stop risked, after fees.
func R(short bool, entry, stop float64, x Exit) float64 {
	dir := 1.0
	if short {
		dir = -1
	}
	fee := takerFee * entry
	if x.Kind == "win" {
		fee += makerFee * x.Price
	} else {
		fee += takerFee * x.Price
	}
	return ((x.Price-entry)*dir - fee) / math.Abs(entry-stop)
}

// follow walks closed 1m bars from the minute after from: the stop or the
// target, whichever the price reaches first (the stop when one bar reaches
// both, as nothing tells which came first), or the close once Hold is up.
// false while none of these has happened.
func follow(short bool, stop, target float64, from time.Time, bars []market.Bar) (Exit, bool) {
	start := from.Truncate(time.Minute).Add(time.Minute).UnixMilli()
	limit := from.Add(Hold).UnixMilli()
	for _, b := range bars {
		if b.T < start {
			continue
		}
		at := time.UnixMilli(b.T + 60_000)
		hitStop, hitTarget := b.L <= stop, b.H >= target
		if short {
			hitStop, hitTarget = b.H >= stop, b.L <= target
		}
		switch {
		case hitStop:
			return Exit{"loss", stop, at}, true
		case hitTarget:
			return Exit{"win", target, at}, true
		case b.T+60_000 >= limit:
			return Exit{"timeout", b.C, at}, true
		}
	}
	return Exit{}, false
}

// outcome says an exit in words: 止盈 +1.9R（47 分钟后）.
func outcome(short bool, entry, stop float64, from time.Time, x Exit) string {
	kind := map[string]string{"win": "止盈", "loss": "止损", "timeout": "24 小时没走完，平在"}[x.Kind]
	return fmt.Sprintf("%s %+.1fR（%s后）", kind, R(short, entry, stop, x), span(x.At.Sub(from)))
}

func span(d time.Duration) string {
	if d < time.Hour {
		return fmt.Sprintf("%d 分钟", int(d.Minutes()))
	}
	return fmt.Sprintf("%.1f 小时", d.Hours())
}

// load reads the records in dir/sent sent since since, oldest first.
func load(dir string, since time.Time) []*Record {
	files, _ := filepath.Glob(filepath.Join(dir, "sent", "*.json"))
	var out []*Record
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		r := &Record{}
		if json.Unmarshal(data, r) != nil || r.At.Before(since) {
			continue
		}
		if r.ID == "" {
			r.ID = strings.TrimSuffix(filepath.Base(f), ".json")
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}

// save writes r whole, so a reader never sees half of it.
func save(dir string, r *Record) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	file := filepath.Join(dir, "sent", r.ID+".json")
	if err := os.WriteFile(file+".tmp", data, 0o600); err != nil {
		return err
	}
	return os.Rename(file+".tmp", file)
}
