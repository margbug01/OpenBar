// Package signalbot pushes trade signals to the owner on Telegram, a chart
// and a plan with three buttons: 下单, 不下, 稍等. It takes signals from a
// folder (inbox/<id>.json, the chart beside it), so whatever finds them only
// writes files; only the owner, the first to send /start, is answered.
//
// This step only records the owner's choice (log.jsonl): 下单 places
// nothing yet.
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
)

// Signal is a trade the scanner proposes.
type Signal struct {
	ID     string    `json:"id"`
	Symbol string    `json:"symbol"` // ETHUSDC
	Short  bool      `json:"short"`
	Entry  float64   `json:"entry"`
	Stop   float64   `json:"stop"`
	Target float64   `json:"target"`
	Qty    float64   `json:"qty"`    // in the base asset
	Risk   float64   `json:"risk"`   // what the stop loses, in the quote asset
	Setup  string    `json:"setup"`  // 趋势回调
	Reason string    `json:"reason"` // the AI's one or two sentences
	Image  string    `json:"image"`  // the chart, beside the json unless absolute
	At     time.Time `json:"at"`     // when it was found
}

// Decision is the owner's answer to a signal, as log.jsonl keeps it.
type Decision struct {
	ID     string    `json:"id"`
	Choice string    `json:"choice"` // do, skip, wait, expired, note
	Note   string    `json:"note,omitempty"`
	At     time.Time `json:"at"`
}

type sent struct {
	sig     Signal
	msg     int64
	caption string
	until   time.Time
	done    bool
}

// Bot runs in one process: it alone may poll Telegram for updates.
type Bot struct {
	api     api
	dir     string // the token, chat.json, inbox/, sent/, log.jsonl
	TTL     time.Duration
	Now     func() time.Time
	chat    int64
	open    map[string]*sent // by signal id
	waiting string           // the signal 稍等 waits on a reason for
	offset  int64
}

// New reads the token in dir/telegram.token. client carries the proxy.
func New(dir string, client *http.Client) (*Bot, error) {
	tok, err := os.ReadFile(filepath.Join(dir, "telegram.token"))
	if err != nil {
		return nil, err
	}
	return newBot(dir, "https://api.telegram.org/bot"+strings.TrimSpace(string(tok)), client), nil
}

func newBot(dir, base string, client *http.Client) *Bot {
	b := &Bot{api: api{base: base, http: client}, dir: dir, TTL: 10 * time.Minute, Now: time.Now, open: map[string]*sent{}}
	if data, err := os.ReadFile(filepath.Join(dir, "chat.json")); err == nil {
		var c struct{ Chat int64 }
		json.Unmarshal(data, &c)
		b.chat = c.Chat
	}
	for _, d := range []string{"inbox", "sent"} {
		os.MkdirAll(filepath.Join(dir, d), 0o700)
	}
	return b
}

// Run polls until ctx ends, sending what comes into the inbox and expiring
// what nobody answered.
func (b *Bot) Run(ctx context.Context) error {
	for ctx.Err() == nil {
		b.tick(ctx)
		us, err := b.api.updates(ctx, b.offset, 5)
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
			b.offset = u.ID + 1
			b.handle(ctx, u)
		}
	}
	return ctx.Err()
}

// tick sends the inbox and expires signals past their time.
func (b *Bot) tick(ctx context.Context) {
	if b.chat != 0 {
		files, _ := filepath.Glob(filepath.Join(b.dir, "inbox", "*.json"))
		sort.Strings(files)
		for _, f := range files {
			if err := b.send(ctx, f); err != nil {
				log.Printf("%s: %v", filepath.Base(f), err)
			}
		}
	}
	now := b.Now()
	for _, s := range b.open {
		if !s.done && now.After(s.until) {
			s.done = true
			b.api.editCaption(ctx, b.chat, s.msg, s.caption+"\n\n→ 已过期，没下单", nil)
			b.record(Decision{ID: s.sig.ID, Choice: "expired", At: now})
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
	img := s.Image
	if img == "" {
		os.Rename(file, file+".bad")
		return errors.New("no chart")
	}
	if !filepath.IsAbs(img) {
		img = filepath.Join(filepath.Dir(file), img)
	}
	until := b.Now().Add(b.TTL)
	caption := Caption(s, until)
	kb := &keyboard{Rows: [][]button{{
		{Text: "下单", Data: "do:" + s.ID}, {Text: "稍等", Data: "wait:" + s.ID}, {Text: "不下", Data: "skip:" + s.ID},
	}}}
	m, err := b.api.sendPhoto(ctx, b.chat, img, caption, kb)
	if err != nil {
		return err
	}
	b.open[s.ID] = &sent{sig: s, msg: m.ID, caption: caption, until: until}
	os.Rename(file, filepath.Join(b.dir, "sent", filepath.Base(file)))
	if filepath.Dir(img) == filepath.Dir(file) {
		os.Rename(img, filepath.Join(b.dir, "sent", filepath.Base(img)))
	}
	return nil
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
		fmt.Fprintf(&sb, "\n仓位 %g · 打到止损亏 %.1f U", s.Qty, s.Risk)
	}
	if s.Reason != "" {
		fmt.Fprintf(&sb, "\n\n%s", s.Reason)
	}
	fmt.Fprintf(&sb, "\n\n%s 前有效", until.Format("15:04"))
	return sb.String()
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
	s := b.open[id]
	if s == nil || s.done {
		b.api.answer(ctx, cb.ID, "这个信号已经结束了")
		return
	}
	now := b.Now()
	if now.After(s.until) {
		b.api.answer(ctx, cb.ID, "已过期")
		return
	}
	label := map[string]string{
		"do":   "→ 你选了：下单（这一步还没接币安，只记下了）",
		"skip": "→ 你选了：不下",
		"wait": "→ 你选了：稍等，接着看",
	}[choice]
	if label == "" {
		return
	}
	s.done = true
	b.record(Decision{ID: id, Choice: choice, At: now})
	b.api.answer(ctx, cb.ID, "")
	b.api.editCaption(ctx, b.chat, s.msg, s.caption+"\n\n"+label, nil)
	if choice == "wait" {
		b.waiting = id
		b.api.sendText(ctx, b.chat, "等什么？回一句话就行，比如「像假突破，等下一根」。不用回也可以。")
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
		n := 0
		for _, s := range b.open {
			if !s.done {
				n++
			}
		}
		b.api.sendText(ctx, from, fmt.Sprintf("在跑。等你回复的信号 %d 个。", n))
	case b.waiting != "":
		b.record(Decision{ID: b.waiting, Choice: "note", Note: m.Text, At: b.Now()})
		b.waiting = ""
		b.api.sendText(ctx, from, "记下了，会带着这句话接着看。")
	default:
		b.record(Decision{Choice: "note", Note: m.Text, At: b.Now()})
		b.api.sendText(ctx, from, "收到，记下了。")
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
