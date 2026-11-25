// Package signalbot trains the owner on trade signals over Telegram. A
// scanner (Scanner) finds pullbacks on ETHUSDC.P 5m, draws them and has
// Claude write why they are good or bad against the owner's own record; the
// bot (Bot) sends each as a chart and a plan with three buttons, 下单, 不下,
// 稍等. 下单 opens a paper trade at the price of the moment; every signal,
// taken or not, is then followed on the real prices to its stop or target,
// and each morning brings back yesterday's, chart by chart.
//
// The two meet in a folder: the scanner writes inbox/<id>.json and its chart,
// the bot sends them and keeps each as sent/<id>.json (a Record). Only the
// owner, the first to send /start, is answered.
package signalbot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"candle/market"
)

// Signal is a trade the scanner proposes.
type Signal struct {
	ID       string    `json:"id"`
	Symbol   string    `json:"symbol"` // ETHUSDC
	Short    bool      `json:"short"`
	Entry    float64   `json:"entry"`
	Stop     float64   `json:"stop"`
	Target   float64   `json:"target"`
	Qty      float64   `json:"qty"`                // in the base asset
	Risk     float64   `json:"risk"`               // what the stop loses, in the quote asset
	Setup    string    `json:"setup"`              // 趋势回调
	Reason   string    `json:"reason"`             // Claude's two or three sentences
	Image    string    `json:"image"`              // the chart, beside the json unless absolute
	At       time.Time `json:"at"`                 // the signal bar's close
	Bar      int64     `json:"bar,omitempty"`      // the signal bar's open time, ms
	Faults   []string  `json:"faults,omitempty"`   // the lab's 不做 rules it trips
	Warnings []string  `json:"warnings,omitempty"` // the owner's red lines it crosses
}

// Decision is a note of the owner's not on any signal, as log.jsonl keeps it.
type Decision struct {
	ID     string    `json:"id"`
	Choice string    `json:"choice"`
	Note   string    `json:"note,omitempty"`
	At     time.Time `json:"at"`
}

// Chart draws bars as OpenBar does, EMA20 and VWAP on them and the signal's
// plan; a label is written over the plan, for a chart of how it went.
type Chart func(bars []market.Bar, s Signal, label, file string) error

// Bot runs in one process: it alone may poll Telegram for updates.
type Bot struct {
	api     api
	dir     string // the token, chat.json, inbox/, sent/, log.jsonl
	Perp    market.Perp
	Chart   Chart
	TTL     time.Duration
	Now     func() time.Time
	chat    int64
	open    map[string]*Record // by signal id, till done
	waiting string             // the signal whose why the owner is asked
	checked time.Time          // when the open ones were last followed
}

// beijing is the owner's clock: their days, red lines and hours are in it.
var beijing = time.FixedZone("CST", 8*3600)

// New reads the token in dir/telegram.token. client carries the proxy.
func New(dir string, client *http.Client) (*Bot, error) {
	tok, err := os.ReadFile(filepath.Join(dir, "telegram.token"))
	if err != nil {
		return nil, err
	}
	return newBot(dir, "https://api.telegram.org/bot"+strings.TrimSpace(string(tok)), client), nil
}

func newBot(dir, base string, client *http.Client) *Bot {
	b := &Bot{api: api{base: base, http: client}, dir: dir, TTL: 10 * time.Minute, Now: time.Now, open: map[string]*Record{}}
	if data, err := os.ReadFile(filepath.Join(dir, "chat.json")); err == nil {
		var c struct{ Chat int64 }
		json.Unmarshal(data, &c)
		b.chat = c.Chat
	}
	for _, d := range []string{"inbox", "sent"} {
		os.MkdirAll(filepath.Join(dir, d), 0o700)
	}
	for _, r := range load(dir, time.Time{}) {
		if !r.done() && r.Msg != 0 {
			b.open[r.ID] = r
		}
	}
	return b
}

// Run polls until ctx ends, sending what comes into the inbox, following the
// open signals on the market and bringing back yesterday's in the morning.
func (b *Bot) Run(ctx context.Context) error {
	var offset int64
	for ctx.Err() == nil {
		b.tick(ctx)
		us, err := b.api.updates(ctx, offset, 5)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			log.Print(err)
			select {
			case <-ctx.Done():
			case <-time.After(5 * time.Second):
			}
			continue
		}
		for _, u := range us {
			offset = u.ID + 1
			b.handle(ctx, u)
		}
	}
	return ctx.Err()
}

// tick sends the inbox, expires signals past their time and, once a minute,
// follows the open ones and sends the morning's review when it is due.
func (b *Bot) tick(ctx context.Context) {
	if b.chat == 0 {
		return
	}
	files, _ := filepath.Glob(filepath.Join(b.dir, "inbox", "*.json"))
	sort.Strings(files)
	for _, f := range files {
		if err := b.send(ctx, f); err != nil {
			log.Printf("%s: %v", filepath.Base(f), err)
		}
	}
	reasons, _ := filepath.Glob(filepath.Join(b.dir, "inbox", "*.reason"))
	for _, f := range reasons {
		b.reason(ctx, f)
	}
	now := b.Now()
	for _, r := range b.open {
		if r.Choice == "" && now.After(r.Until) {
			r.Choice = "expired"
			b.update(ctx, r)
		}
	}
	if now.Sub(b.checked) >= time.Minute {
		b.checked = now
		b.follow(ctx)
		if err := b.review(ctx); err != nil {
			log.Printf("review: %v", err)
		}
	}
}

func (b *Bot) send(ctx context.Context, file string) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	var s Signal
	if err := json.Unmarshal(data, &s); err != nil {
		os.Rename(file, file+".bad")
		return err
	}
	if s.ID == "" {
		s.ID = strings.TrimSuffix(filepath.Base(file), ".json")
	}
	if s.At.IsZero() {
		s.At = b.Now()
	}
	img := s.Image
	if img == "" {
		os.Rename(file, file+".bad")
		return errors.New("no chart")
	}
	if !filepath.IsAbs(img) {
		img = filepath.Join(filepath.Dir(file), img)
	}
	r := &Record{Signal: s, Until: b.Now().Add(b.TTL)}
	r.Caption = Caption(s, r.Until)
	m, err := b.api.sendPhoto(ctx, b.chat, img, clip(r.Caption), buttons(s.ID))
	if err != nil {
		return err
	}
	r.Msg = m.ID
	if filepath.Dir(img) == filepath.Dir(file) {
		os.Rename(img, filepath.Join(b.dir, "sent", filepath.Base(img)))
	}
	if err := save(b.dir, r); err != nil {
		return err
	}
	os.Remove(file)
	b.open[s.ID] = r
	return nil
}

func buttons(id string) *keyboard {
	return &keyboard{Rows: [][]button{{
		{Text: "下单", Data: "do:" + id}, {Text: "稍等", Data: "wait:" + id}, {Text: "不下", Data: "skip:" + id},
	}}}
}

// reason adds Claude's reason, come after the signal, to its message. One
// for a signal not sent yet waits for it.
func (b *Bot) reason(ctx context.Context, file string) {
	id := strings.TrimSuffix(filepath.Base(file), ".reason")
	if _, err := os.Stat(filepath.Join(b.dir, "inbox", id+".json")); err == nil {
		return
	}
	r := b.open[id]
	if r == nil { // done already
		for _, x := range load(b.dir, time.Time{}) {
			if x.ID == id {
				r = x
			}
		}
	}
	data, err := os.ReadFile(file)
	if err == nil && r != nil {
		r.Reason = strings.TrimSpace(string(data))
		r.Caption = Caption(r.Signal, r.Until)
		b.update(ctx, r)
	}
	os.Remove(file)
}

// Caption is the plan as the message shows it.
func Caption(s Signal, until time.Time) string {
	side, dir := "做多", 1.0
	if s.Short {
		side, dir = "做空", -1
	}
	pct := func(p float64) string { return fmt.Sprintf("%+.2f%%", (p-s.Entry)/s.Entry*100) }
	rr := 0.0
	if risk := math.Abs(s.Entry - s.Stop); risk > 0 {
		rr = (s.Target - s.Entry) * dir / risk
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s %s 永续 · 5分", side, s.Symbol)
	if s.Setup != "" {
		fmt.Fprintf(&sb, " · %s", s.Setup)
	}
	fmt.Fprintf(&sb, "\n入场 %.2f\n止损 %.2f（%s）\n止盈 %.2f（%s，1:%.1f）", s.Entry, s.Stop, pct(s.Stop), s.Target, pct(s.Target), rr)
	if s.Qty > 0 {
		fmt.Fprintf(&sb, "\n仓位 %.3f · 打到止损亏 %.1f U", s.Qty, s.Risk)
	}
	if len(s.Faults) > 0 {
		fmt.Fprintf(&sb, "\n\n回测里「不做」的情况，这条碰到了：%s", strings.Join(s.Faults, "、"))
	}
	for _, w := range s.Warnings {
		fmt.Fprintf(&sb, "\n⚠️ %s", w)
	}
	if s.Reason != "" {
		fmt.Fprintf(&sb, "\n\n%s", s.Reason)
	} else {
		sb.WriteString("\n\nClaude 在看图，十几秒后补在这里")
	}
	fmt.Fprintf(&sb, "\n\n%s 前有效", until.In(beijing).Format("15:04"))
	return sb.String()
}

// text is the message as it stands: the plan, what the owner chose and how
// it went.
func (r *Record) text() string {
	var sb strings.Builder
	sb.WriteString(r.Caption)
	switch r.Choice {
	case "do":
		if p := r.Paper; p != nil {
			fmt.Fprintf(&sb, "\n\n→ 下单：模拟成交 %.2f，%.3f ETH", p.Fill, p.Qty)
			if p.Exit != nil {
				fmt.Fprintf(&sb, "\n→ %s", outcome(r.Short, p.Fill, r.Stop, p.At, *p.Exit))
			}
		} else {
			sb.WriteString("\n\n→ 你选了：下单，但没成交（见下面的消息）")
		}
	case "skip":
		sb.WriteString("\n\n→ 你选了：不下")
	case "wait":
		sb.WriteString("\n\n→ 你选了：稍等")
	case "expired":
		sb.WriteString("\n\n→ 已过期，没下单")
	}
	if r.Note != "" {
		fmt.Fprintf(&sb, "（%s）", r.Note)
	}
	if r.Plan != nil && r.Paper == nil && r.Choice != "" {
		fmt.Fprintf(&sb, "\n→ 照计划做的话：%s", outcome(r.Short, r.Entry, r.Stop, r.At, *r.Plan))
	}
	return clip(sb.String())
}

// clip keeps a caption under Telegram's 1024 characters.
func clip(s string) string {
	if rs := []rune(s); len(rs) > 1024 {
		return string(rs[:1023]) + "…"
	}
	return s
}

// update saves r and shows it on its message, buttons gone once chosen.
func (b *Bot) update(ctx context.Context, r *Record) {
	if err := save(b.dir, r); err != nil {
		log.Print(err)
	}
	var kb *keyboard
	if r.Choice == "" {
		kb = buttons(r.ID)
	}
	if err := b.api.editCaption(ctx, b.chat, r.Msg, r.text(), kb); err != nil {
		log.Printf("%s: %v", r.ID, err)
	}
	if r.done() {
		delete(b.open, r.ID)
	}
}

func (b *Bot) handle(ctx context.Context, u update) {
	if m := u.Message; m != nil {
		b.message(ctx, m)
		return
	}
	cb := u.Callback
	if cb == nil {
		return
	}
	if b.chat == 0 || cb.From.ID != b.chat {
		b.api.answer(ctx, cb.ID, "")
		return
	}
	choice, id, _ := strings.Cut(cb.Data, ":")
	r := b.open[id]
	if r == nil || r.Choice != "" {
		b.api.answer(ctx, cb.ID, "这个信号已经结束了")
		return
	}
	if b.Now().After(r.Until) {
		b.api.answer(ctx, cb.ID, "已过期")
		return
	}
	ask := map[string]string{
		"skip": "为啥不下？一句话就行，不回也没关系。",
		"wait": "等什么？回一句话就行，比如「像假突破，等下一根」。不用回也可以。",
	}
	if choice != "do" && ask[choice] == "" {
		return
	}
	r.Choice = choice
	b.api.answer(ctx, cb.ID, "")
	if choice == "do" {
		if err := b.fill(ctx, r); err != nil {
			b.api.sendText(ctx, b.chat, "没成交："+err.Error())
		}
	}
	b.update(ctx, r)
	if ask[choice] != "" {
		b.waiting = id
		b.api.sendText(ctx, b.chat, ask[choice])
	}
}

// fill opens r's paper trade at the last price, sized so its stop loses
// Risk; a price already past the stop or the target opens nothing.
func (b *Bot) fill(ctx context.Context, r *Record) error {
	bars, err := b.Perp.Klines(ctx, r.Symbol+".P", "1m", 0, 1)
	if err != nil || len(bars) == 0 {
		return fmt.Errorf("拿不到现价：%v", err)
	}
	p := bars[len(bars)-1].C
	switch {
	case (p-r.Stop)*r.dir() <= 0:
		return fmt.Errorf("现价 %.2f 已经过了止损", p)
	case (r.Target-p)*r.dir() <= 0:
		return fmt.Errorf("现价 %.2f 已经过了止盈", p)
	}
	r.Paper = &Paper{Fill: p, At: b.Now(), Qty: Risk / math.Abs(p-r.Stop)}
	return nil
}

// follow takes the open signals along the 1m bars since they were sent: the
// plan of each, and the paper trade of those taken.
func (b *Bot) follow(ctx context.Context) {
	now := b.Now()
	for _, r := range b.open {
		if r.Plan != nil && (r.Paper == nil || r.Paper.Exit != nil) {
			continue
		}
		end := min(r.At.Add(Hold+time.Minute).UnixMilli(), now.UnixMilli())
		bars, err := b.Perp.Klines(ctx, r.Symbol+".P", "1m", end, 1500)
		if err != nil {
			log.Printf("follow %s: %v", r.ID, err)
			return
		}
		for len(bars) > 0 && bars[len(bars)-1].T+60_000 > now.UnixMilli() {
			bars = bars[:len(bars)-1] // still open
		}
		changed := false
		if r.Plan == nil {
			if x, ok := follow(r.Short, r.Stop, r.Target, r.At, bars); ok {
				r.Plan, changed = &x, true
			}
		}
		if p := r.Paper; p != nil && p.Exit == nil {
			if x, ok := follow(r.Short, r.Stop, r.Target, p.At, bars); ok {
				p.Exit, changed = &x, true
				res := R(r.Short, p.Fill, r.Stop, x)
				b.api.reply(ctx, b.chat, r.Msg, fmt.Sprintf("模拟单%s，%+.1f U", outcome(r.Short, p.Fill, r.Stop, p.At, x), res*Risk))
			}
		}
		switch {
		case changed && r.Choice == "":
			save(b.dir, r) // the owner is still deciding: the message keeps its buttons and tells nothing yet
		case changed:
			b.update(ctx, r)
		}
	}
}

func (b *Bot) message(ctx context.Context, m *message) {
	from := m.Chat.ID
	if b.chat == 0 {
		if strings.HasPrefix(m.Text, "/start") {
			b.chat = from
			data, _ := json.Marshal(map[string]any{"Chat": from, "User": m.From.Username, "At": b.Now()})
			os.WriteFile(filepath.Join(b.dir, "chat.json"), data, 0o600)
			b.api.sendText(ctx, from, "连上了。之后信号只发给你，别人发什么我都不理。")
		}
		return
	}
	if from != b.chat {
		return // only the owner
	}
	switch {
	case strings.HasPrefix(m.Text, "/start"):
		b.api.sendText(ctx, from, "已经连着了。")
	case m.Text == "/status":
		wait, trades := 0, 0
		for _, r := range b.open {
			if r.Choice == "" {
				wait++
			}
			if r.Paper != nil && r.Paper.Exit == nil {
				trades++
			}
		}
		b.api.sendText(ctx, from, fmt.Sprintf("在跑。等你回复的信号 %d 个，拿着的模拟单 %d 笔。", wait, trades))
	case b.waiting != "":
		r := b.open[b.waiting]
		if r == nil { // done while the owner wrote: read it back to add the note
			for _, x := range load(b.dir, time.Time{}) {
				if x.ID == b.waiting {
					r = x
				}
			}
		}
		b.waiting = ""
		if r != nil {
			r.Note = m.Text
			b.update(ctx, r)
		}
		b.api.sendText(ctx, from, "记下了。")
	default:
		b.record(Decision{Choice: "note", Note: m.Text, At: b.Now()})
		b.api.sendText(ctx, from, "收到，记下了，之后看图会参考。")
	}
}

func (b *Bot) record(d Decision) {
	f, err := os.OpenFile(filepath.Join(b.dir, "log.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		log.Print(err)
		return
	}
	defer f.Close()
	json.NewEncoder(f).Encode(d)
}
