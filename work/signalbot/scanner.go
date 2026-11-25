package signalbot

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"time"

	"candle/market"
)

const (
	perpID = "ETHUSDC.P"
	// SendFrom is the hour, Beijing, before which nothing is sent: the
	// owner sleeps.
	SendFrom = 9
	// FallbackFrom is the hour from which a day without a clean signal yet
	// gets the next setup, faults and all, so that each day has one.
	FallbackFrom = 18
)

// Scanner looks at each 5m bar of ETHUSDC.P as it closes and puts the
// setups worth sending in the inbox, chart and Claude's reason with them.
type Scanner struct {
	Dir   string
	Perp  market.Perp
	Chart Chart
	Coach Coach
	Now   func() time.Time
}

// Run scans a few seconds after each 5m bar closes, until ctx ends.
func (sc *Scanner) Run(ctx context.Context) {
	for {
		now := sc.Now()
		next := now.Truncate(5 * time.Minute).Add(5*time.Minute + 5*time.Second)
		select {
		case <-ctx.Done():
			return
		case <-time.After(next.Sub(now)):
		}
		if err := sc.Once(ctx, false); err != nil {
			log.Printf("scan: %v", err)
		}
	}
}

// Once scans the bars up to the one that just closed and sends a setup on
// it if it is clean, or the day's fallback. test sends the latest setup
// whatever its time, to try the whole way through.
func (sc *Scanner) Once(ctx context.Context, test bool) error {
	b, err := sc.Perp.Klines(ctx, perpID, "5m", 0, 1500)
	if err != nil {
		return err
	}
	now := sc.Now()
	for len(b) > 0 && b[len(b)-1].T+5*60_000 > now.UnixMilli() {
		b = b[:len(b)-1] // still open
	}
	ss := Scan(b)
	if len(ss) == 0 {
		return nil
	}
	s := ss[len(ss)-1]
	kind := fmt.Sprintf("趋势回调 · %d段", s.Legs)
	switch {
	case test:
		kind = "测试 · " + kind
	case s.Bar != b[len(b)-1].T || now.In(beijing).Hour() < SendFrom:
		return nil
	case len(s.Faults) > 0:
		if now.In(beijing).Hour() < FallbackFrom || sc.sentToday(now) {
			return nil
		}
		kind = "保底 · " + kind
	}
	return sc.push(ctx, b, s, kind, test)
}

// sentToday is whether a signal went out today, or waits in the inbox.
func (sc *Scanner) sentToday(now time.Time) bool {
	if fs, _ := filepath.Glob(filepath.Join(sc.Dir, "inbox", "*.json")); len(fs) > 0 {
		return true
	}
	for _, r := range load(sc.Dir, midnight(now)) {
		if r.Msg != 0 {
			return true
		}
	}
	return false
}

func midnight(t time.Time) time.Time {
	t = t.In(beijing)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, beijing)
}

// push draws s and leaves it in the inbox, for the bot to send at once; then
// has Claude read the chart, some 15 s, and leaves the reason beside it
// (inbox/<id>.reason), for the bot to add to the message. The chart goes
// straight to sent/, where it stays while Claude reads it. Files are written
// whole, so the bot never reads half of one.
func (sc *Scanner) push(ctx context.Context, b []market.Bar, s Setup, kind string, test bool) error {
	now := sc.Now()
	at := time.UnixMilli(s.Bar + 5*60_000)
	sig := Signal{
		ID: at.In(beijing).Format("20060102-1504"), Symbol: "ETHUSDC", Short: s.Short,
		Entry: s.Entry, Stop: s.Stop, Target: s.Target, Qty: Risk / math.Abs(s.Entry-s.Stop), Risk: Risk,
		Setup: kind, At: at, Bar: s.Bar, Faults: s.Faults,
		Warnings: redLines(now, load(sc.Dir, midnight(now))),
	}
	if test {
		sig.ID += "-test" + now.In(beijing).Format("150405")
	}
	i := len(b) - 1
	for b[i].T != s.Bar {
		i--
	}
	inbox := filepath.Join(sc.Dir, "inbox")
	os.MkdirAll(inbox, 0o700)
	os.MkdirAll(filepath.Join(sc.Dir, "sent"), 0o700)
	sig.Image = filepath.Join(sc.Dir, "sent", sig.ID+".png")
	if err := sc.Chart(b[max(i-85, 0):i+1], sig, "", sig.Image); err != nil {
		return fmt.Errorf("chart: %w", err)
	}
	data, err := json.MarshalIndent(sig, "", "  ")
	if err != nil {
		return err
	}
	if err := writeWhole(filepath.Join(inbox, sig.ID+".json"), data); err != nil {
		return err
	}
	t0 := time.Now()
	reason, err := sc.Coach.Reason(ctx, sig, sig.Image)
	if err != nil {
		log.Printf("%s: %v", sig.ID, err)
		reason = "（这条 Claude 没看成图，只有计划）"
	}
	log.Printf("%s: 收盘后 %v 交给机器人，Claude 又用了 %v", sig.ID, t0.Sub(at).Round(100*time.Millisecond), time.Since(t0).Round(100*time.Millisecond))
	return writeWhole(filepath.Join(inbox, sig.ID+".reason"), []byte(reason))
}

func writeWhole(file string, data []byte) error {
	if err := os.WriteFile(file+".tmp", data, 0o600); err != nil {
		return err
	}
	return os.Rename(file+".tmp", file)
}

// redLines are the owner's red lines a trade now would cross, as the
// journal's 00_当前作战红线 has them; their paper trades of today count.
func redLines(now time.Time, today []*Record) []string {
	t := now.In(beijing)
	var w []string
	if t.Weekday() == time.Thursday && t.Hour() >= 20 {
		w = append(w, "周四 20 点后：白天有利润就关盘（红线 2）")
	}
	if m := t.Hour()*60 + t.Minute(); m >= 21*60 && m < 22*60+30 {
		w = append(w, "21:00–22:30 美盘冷冻期，不开新仓")
	}
	n := 0
	var stop time.Time
	for _, r := range today {
		if p := r.Paper; p != nil {
			n++
			if p.Exit != nil && p.Exit.Kind == "loss" && p.Exit.At.After(stop) {
				stop = p.Exit.At
			}
		}
	}
	if n >= 2 {
		w = append(w, fmt.Sprintf("今天已经下了 %d 笔，每天不超过 2 笔", n))
	}
	if d := now.Sub(stop); !stop.IsZero() && d < 20*time.Minute {
		w = append(w, fmt.Sprintf("上一笔止损才 %d 分钟，冷却 20 分钟（红线 3）", int(d.Minutes())))
	}
	return w
}
