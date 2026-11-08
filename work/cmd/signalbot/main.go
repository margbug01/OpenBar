// Command signalbot runs the Telegram bot that pushes trade signals to the
// owner (package signalbot). Its files are in ~/.config/openbar:
// telegram.token (0600), chat.json, inbox/, sent/ and log.jsonl.
//
//	signalbot              run until interrupted
//	signalbot demo <png>   put a sample signal with that chart in the inbox
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"candle/market"
	"candle/signalbot"
)

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatal(err)
	}
	dir := filepath.Join(home, ".config", "openbar")
	if len(os.Args) == 3 && os.Args[1] == "demo" {
		if err := demo(dir, os.Args[2]); err != nil {
			log.Fatal(err)
		}
		return
	}
	// Telegram takes the system proxy from mainland China, as Binance does.
	client := &http.Client{Timeout: 60 * time.Second, Transport: &http.Transport{Proxy: market.SystemProxy}}
	b, err := signalbot.New(dir, client)
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	log.Print("signalbot running")
	b.Run(ctx)
}

func demo(dir, png string) error {
	data, err := os.ReadFile(png)
	if err != nil {
		return err
	}
	id := "demo-" + time.Now().Format("150405")
	inbox := filepath.Join(dir, "inbox")
	if err := os.MkdirAll(inbox, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(inbox, id+".png"), data, 0o600); err != nil {
		return err
	}
	s := signalbot.Signal{ID: id, Symbol: "ETHUSDC", Entry: 2421.36, Stop: 2404.65, Target: 2454.78, Qty: 1.197, Risk: 20,
		Setup: "趋势回调", Reason: "示例信号，不是真的：拉升后第一次回踩 EMA20，回调缩量。", Image: id + ".png", At: time.Now()}
	j, _ := json.MarshalIndent(s, "", "  ")
	// the json last: the bot takes a signal once its json is there
	if err := os.WriteFile(filepath.Join(inbox, id+".json.tmp"), j, 0o600); err != nil {
		return err
	}
	if err := os.Rename(filepath.Join(inbox, id+".json.tmp"), filepath.Join(inbox, id+".json")); err != nil {
		return err
	}
	fmt.Println("放进收件箱了:", id)
	return nil
}
