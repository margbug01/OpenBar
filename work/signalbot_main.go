//go:build signalbot

// The signal bot (package signalbot), built from this package so its charts
// are OpenBar's own: go build -tags signalbot -o signalbot . Its files are in
// ~/.config/openbar: telegram.token (0600), chat.json, coach.md, inbox/,
// sent/ and log.jsonl.
//
//	signalbot        run until interrupted
//	signalbot test   send the latest setup now, whatever its time
package main

import (
	"context"
	"fmt"
	"image"
	"image/png"
	"log"
	"math"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/egoist/mygo/ui"

	"candle/market"
	"candle/signalbot"
)

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatal(err)
	}
	dir := filepath.Join(home, ".config", "openbar")
	// Telegram and Binance's perpetuals take the system proxy from mainland
	// China.
	tr := &http.Transport{Proxy: market.SystemProxy}
	perp := market.Perp{HTTP: &http.Client{Timeout: 30 * time.Second, Transport: tr}}
	claude, err := exec.LookPath("claude")
	if err != nil {
		claude = "/opt/homebrew/bin/claude" // launchd's PATH has no Homebrew
	}
	sc := &signalbot.Scanner{Dir: dir, Perp: perp, Chart: drawSignal, Now: time.Now,
		Coach: signalbot.Coach{Claude: claude, Model: "alma-claude/claude-opus-5-5", Dir: dir}}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if len(os.Args) == 2 && os.Args[1] == "test" {
		if err := sc.Once(ctx, true); err != nil {
			log.Fatal(err)
		}
		fmt.Println("放进收件箱了，开着的 signalbot 几秒内发出去")
		return
	}
	b, err := signalbot.New(dir, &http.Client{Timeout: 60 * time.Second, Transport: tr})
	if err != nil {
		log.Fatal(err)
	}
	b.Perp, b.Chart = perp, drawSignal
	go sc.Run(ctx)
	log.Print("signalbot running")
	b.Run(ctx)
}

// drawSignal draws bars in OpenBar's dark theme, EMA20 and VWAP on them, and
// the signal's plan as a long or short position. Without a label it is the
// chart as the signal came, the plan stretched to the right; with one, the
// chart of how it went, the label over the plan.
func drawSignal(bars []market.Bar, s signalbot.Signal, label, file string) error {
	const id = "ETHUSDC.P"
	a := newApplication(defaultSettings(), "", market.Binance{}, func(f func()) { f() })
	a.spawn = func(func()) {}
	for _, key := range []string{"ema", "vwap"} {
		k, _ := kindOf(key)
		a.addIndicator(k)
	}
	a.indicators[0].Params[0] = 20
	a.set.Chart.BreakZone = "utc"
	a.set.FavBar = false
	a.infos[id] = market.Symbol{Name: id, Base: "ETH", Quote: "USDC", Tick: 0.01}
	a.open(id, "5m")
	a.loading, a.atStart, a.stream = false, true, live
	a.bars = bars
	const iv = 5 * 60_000
	kind := "long"
	if s.Short {
		kind = "short"
	}
	end := s.Bar + 16*iv
	a.view.spacing, a.view.offset = 8, 34
	var ds []drawing
	if label != "" {
		end = max(bars[len(bars)-1].T-12*iv, s.Bar+8*iv)
		a.view.spacing = float32(min(7, 900/float64(len(bars)+8)))
		a.view.offset = 4
		p := math.Max(s.Target, s.Stop) + math.Abs(s.Target-s.Stop)*0.22
		ds = append(ds, drawing{ID: 2, Kind: "text", Width: 2, Size: 15, Color: hexColor(ui.Hex("#e0e0e0")),
			Pts: []point{{s.Bar, p}}, Text: label})
	}
	ds = append(ds, drawing{ID: 1, Kind: kind, Width: 1, Account: 1000, Risk: 2,
		Pts: []point{{s.Bar, s.Entry}, {s.Bar, s.Target}, {s.Bar, s.Stop}, {end, s.Entry}}})
	a.set.Drawings = map[string][]drawing{id: ds}
	// the prices: the bars', the stop's and the target's, so the plan shows whole
	lo, hi := math.Min(s.Stop, s.Target), math.Max(s.Stop, s.Target)
	for _, b := range bars {
		lo, hi = math.Min(lo, b.L), math.Max(hi, b.H)
	}
	pad := (hi - lo) * 0.06
	a.scale = prices{manual: true, lo: lo - pad, hi: hi + pad}
	tt := ui.NewTester(a.render, 1320, 820)
	tt.SetDark(true)
	tt.Frame()
	tt.Frame()
	tt.Move(1319, 819)
	tt.Frame()
	f, err := os.Create(file)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, tt.Image().SubImage(image.Rect(52, 0, 1320-watchW, 820)))
}
