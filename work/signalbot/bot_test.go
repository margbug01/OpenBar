package signalbot

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"candle/market"
)

type call struct {
	method string
	params map[string]any
}

// fakeTelegram answers like the Bot API and keeps what it was asked.
func fakeTelegram(t *testing.T) (*httptest.Server, func() []call) {
	var mu sync.Mutex
	var calls []call
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		p := map[string]any{}
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
			r.ParseMultipartForm(1 << 20)
			for k, v := range r.MultipartForm.Value {
				p[k] = v[0]
			}
			if _, _, err := r.FormFile("photo"); err == nil {
				p["photo"] = true
			}
		} else {
			json.NewDecoder(r.Body).Decode(&p)
		}
		mu.Lock()
		calls = append(calls, call{method, p})
		mu.Unlock()
		res := any(true)
		if method == "sendPhoto" {
			res = map[string]any{"message_id": 42, "chat": map[string]any{"id": 111}}
		}
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": res})
	}))
	t.Cleanup(srv.Close)
	return srv, func() []call {
		mu.Lock()
		defer mu.Unlock()
		out := calls
		calls = nil
		return out
	}
}

func msg(chat int64, text string) update {
	m := &message{Text: text}
	m.Chat.ID = chat
	m.From.ID = chat
	return update{Message: m}
}

func press(from int64, data string) update {
	var u update
	u.Callback = &struct {
		ID      string   `json:"id"`
		From    user     `json:"from"`
		Message *message `json:"message"`
		Data    string   `json:"data"`
	}{ID: "cb", From: user{ID: from}, Data: data}
	return u
}

// fakePerp answers /fapi/v1/klines from bars, by endTime and limit.
func fakePerp(t *testing.T, bars *[]market.Bar) market.Perp {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		end, _ := strconv.ParseInt(r.URL.Query().Get("endTime"), 10, 64)
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		var rows [][]any
		for _, b := range *bars {
			if end == 0 || b.T <= end {
				f := func(x float64) string { return strconv.FormatFloat(x, 'f', -1, 64) }
				rows = append(rows, []any{b.T, f(b.O), f(b.H), f(b.L), f(b.C), f(b.V)})
			}
		}
		json.NewEncoder(w).Encode(rows[max(len(rows)-limit, 0):])
	}))
	t.Cleanup(srv.Close)
	return market.Perp{HTTP: srv.Client(), Base: srv.URL}
}

// minute is a 1m bar opening at t.
func minute(t time.Time, o, h, l, c float64) market.Bar {
	return market.Bar{T: t.UnixMilli(), O: o, H: h, L: l, C: c, V: 1}
}

func putSignal(t *testing.T, dir, id string, at time.Time) {
	os.WriteFile(filepath.Join(dir, "inbox", id+".png"), []byte("png"), 0o600)
	j, _ := json.Marshal(Signal{ID: id, Symbol: "ETHUSDC", Entry: 2421.36, Stop: 2404.65, Target: 2454.78, Image: id + ".png", At: at})
	os.WriteFile(filepath.Join(dir, "inbox", id+".json"), j, 0o600)
}

func record(t *testing.T, dir, id string) *Record {
	for _, r := range load(dir, time.Time{}) {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("no record %s", id)
	return nil
}

func TestBot(t *testing.T) {
	srv, calls := fakeTelegram(t)
	dir := t.TempDir()
	now := time.Date(2026, 11, 9, 14, 0, 0, 0, beijing)
	bars := []market.Bar{minute(now.Add(-time.Minute), 2421, 2423, 2420, 2422.5)}
	b := newBot(dir, srv.URL+"/botX", srv.Client())
	b.Perp = fakePerp(t, &bars)
	b.Now = func() time.Time { return now }
	b.checked = now // no following and no review here
	ctx := context.Background()

	// nothing goes out before the owner says /start; strangers are ignored
	putSignal(t, dir, "s1", now)
	b.tick(ctx)
	b.handle(ctx, msg(222, "hello"))
	if c := calls(); len(c) != 0 {
		t.Fatalf("before /start: %v", c)
	}
	b.handle(ctx, msg(111, "/start"))
	b.handle(ctx, msg(222, "/start"))
	c := calls()
	if len(c) != 1 || c[0].params["chat_id"] != float64(111) {
		t.Fatalf("pairing: %v", c)
	}
	if b2 := newBot(dir, srv.URL+"/botX", srv.Client()); b2.chat != 111 {
		t.Fatalf("chat.json not kept: %d", b2.chat)
	}

	// the inbox goes out as a photo with three buttons, kept in sent/
	b.tick(ctx)
	c = calls()
	if len(c) != 1 || c[0].method != "sendPhoto" || c[0].params["photo"] != true || !strings.Contains(c[0].params["reply_markup"].(string), "do:s1") {
		t.Fatalf("send: %v", c)
	}
	if !strings.Contains(c[0].params["caption"].(string), "止损 2404.65") {
		t.Fatalf("caption: %s", c[0].params["caption"])
	}
	if r := record(t, dir, "s1"); r.Msg != 42 {
		t.Fatalf("record: %+v", r)
	}

	// a stranger's press does nothing; the owner's 下单 fills a paper trade
	// at the last price, sized to lose Risk at the stop, and shows it
	b.handle(ctx, press(222, "do:s1"))
	b.handle(ctx, press(111, "do:s1"))
	b.handle(ctx, press(111, "skip:s1"))
	r := record(t, dir, "s1")
	if r.Choice != "do" || r.Paper == nil || r.Paper.Fill != 2422.5 || math.Abs(r.Paper.Qty*(2422.5-2404.65)-Risk) > 1e-9 {
		t.Fatalf("paper: %+v %+v", r, r.Paper)
	}
	edited := false
	for _, x := range calls() {
		if x.method == "editMessageCaption" {
			edited = strings.Contains(x.params["caption"].(string), "模拟成交 2422.50")
		}
	}
	if !edited {
		t.Fatal("the message does not show the fill")
	}

	// 不下 asks why and keeps the answer on the signal
	putSignal(t, dir, "s2", now)
	b.tick(ctx)
	calls()
	b.handle(ctx, press(111, "skip:s2"))
	if c := calls(); !strings.Contains(c[len(c)-1].params["text"].(string), "为啥不下") {
		t.Fatalf("skip did not ask why: %v", c)
	}
	b.handle(ctx, msg(111, "VWAP 下面不追"))
	if r := record(t, dir, "s2"); r.Choice != "skip" || r.Note != "VWAP 下面不追" {
		t.Fatalf("skip note: %+v", r)
	}

	// a price past the stop opens nothing, and says so
	putSignal(t, dir, "s3", now)
	b.tick(ctx)
	calls()
	bars = append(bars, minute(now, 2410, 2410, 2400, 2401))
	b.handle(ctx, press(111, "do:s3"))
	if r := record(t, dir, "s3"); r.Paper != nil {
		t.Fatalf("filled past the stop: %+v", r.Paper)
	}
	if c := calls(); !strings.Contains(fmt.Sprint(c), "已经过了止损") {
		t.Fatalf("no word on the refused order: %v", c)
	}

	// unanswered for 10 minutes it expires, and a late press is turned down
	putSignal(t, dir, "s4", now)
	b.tick(ctx)
	calls()
	now = now.Add(10*time.Minute + time.Second)
	b.tick(ctx)
	b.handle(ctx, press(111, "do:s4"))
	if r := record(t, dir, "s4"); r.Choice != "expired" {
		t.Fatalf("expiry: %+v", r)
	}
	for _, x := range calls() {
		if x.method == "answerCallbackQuery" && x.params["text"] != "这个信号已经结束了" {
			t.Fatalf("late press answered %v", x.params["text"])
		}
	}

	// a restart picks the open ones up again
	if b2 := newBot(dir, srv.URL+"/botX", srv.Client()); b2.open["s1"] == nil || b2.open["s2"] == nil {
		t.Fatalf("open after restart: %v", b2.open)
	}
}

// Every signal is followed to its end: the paper trade of one taken, the
// plan of one let go; the owner hears of the paper trade, and the message
// of the other shows how it would have gone.
func TestFollow(t *testing.T) {
	srv, calls := fakeTelegram(t)
	dir := t.TempDir()
	at := time.Date(2026, 11, 9, 14, 0, 0, 0, beijing)
	now := at
	var bars []market.Bar
	b := newBot(dir, srv.URL+"/botX", srv.Client())
	b.chat, b.Perp, b.Now = 111, fakePerp(t, &bars), func() time.Time { return now }
	b.checked = now.Add(24 * time.Hour) // the test follows by hand
	ctx := context.Background()
	putSignal(t, dir, "taken", at)
	putSignal(t, dir, "left", at)
	b.tick(ctx)
	bars = []market.Bar{minute(at, 2421, 2422, 2420, 2421.36)}
	now = at.Add(time.Minute)
	b.handle(ctx, press(111, "do:taken"))
	b.handle(ctx, press(111, "skip:left"))
	calls()

	// the plan, from the signal's close, is stopped in the next minute; the
	// paper trade filled in that minute counts from the one after, and
	// reaches the target
	bars = append(bars,
		minute(at.Add(1*time.Minute), 2421, 2430, 2400, 2425),
		minute(at.Add(2*time.Minute), 2425, 2455, 2424, 2450),
		minute(at.Add(3*time.Minute), 2450, 2451, 2449, 2450))
	now = at.Add(3*time.Minute + 30*time.Second) // the last bar still open
	b.follow(ctx)
	taken, left := record(t, dir, "taken"), record(t, dir, "left")
	if taken.Paper.Exit == nil || taken.Paper.Exit.Kind != "win" || taken.Plan.Kind != "loss" || left.Plan == nil || left.Plan.Kind != "loss" {
		t.Fatalf("taken %+v %+v, left %+v", taken.Paper, taken.Plan, left.Plan)
	}
	if len(b.open) != 0 {
		t.Fatalf("still open: %v", b.open)
	}
	var told, shown bool
	for _, c := range calls() {
		told = told || c.method == "sendMessage" && strings.Contains(c.params["text"].(string), "模拟单止盈 +1.9R")
		shown = shown || c.method == "editMessageCaption" && strings.Contains(c.params["caption"].(string), "照计划做的话：止损 -1.1R")
	}
	if !told || !shown {
		t.Fatalf("told %v, shown %v", told, shown)
	}
}

// A plan that ends while the owner still decides changes nothing they see:
// the buttons stay and the outcome waits for their choice, here the expiry.
func TestFollowWhileDeciding(t *testing.T) {
	srv, calls := fakeTelegram(t)
	dir := t.TempDir()
	at := time.Date(2026, 11, 9, 14, 0, 0, 0, beijing)
	now := at
	bars := []market.Bar{minute(at.Add(time.Minute), 2421, 2422, 2400, 2405)}
	b := newBot(dir, srv.URL+"/botX", srv.Client())
	b.chat, b.Perp, b.Now = 111, fakePerp(t, &bars), func() time.Time { return now }
	ctx := context.Background()
	putSignal(t, dir, "s", at)
	b.checked = now // sent, not followed yet
	b.tick(ctx)
	calls()
	now = at.Add(3 * time.Minute)
	b.follow(ctx)
	if r := record(t, dir, "s"); r.Plan == nil || r.Plan.Kind != "loss" {
		t.Fatalf("plan: %+v", r.Plan)
	}
	if c := calls(); len(c) != 0 {
		t.Fatalf("the message changed while the owner decides: %v", c)
	}
	now = at.Add(11 * time.Minute)
	b.tick(ctx)
	c := calls()
	if len(c) != 1 || !strings.Contains(c[0].params["caption"].(string), "已过期，没下单\n→ 照计划做的话：止损") {
		t.Fatalf("expiry: %v", c)
	}
}

func TestExit(t *testing.T) {
	at := time.Date(2026, 11, 9, 14, 0, 0, 0, beijing)
	bar := func(m int, h, l, c float64) market.Bar {
		return minute(at.Add(time.Duration(m)*time.Minute), 100, h, l, c)
	}
	for _, tc := range []struct {
		name  string
		short bool
		bars  []market.Bar
		want  string
	}{
		{"nothing yet", false, []market.Bar{bar(1, 101, 99.5, 100)}, ""},
		{"both in one bar: the stop", false, []market.Bar{bar(1, 103, 98, 100)}, "loss"},
		{"short target", true, []market.Bar{bar(1, 100.5, 97.9, 98)}, "win"},
		{"a day without either", false, []market.Bar{bar(1, 101, 99.5, 100), bar(24*60-1, 101, 99.5, 100.5)}, "timeout"},
	} {
		stop, target := 99.0, 102.0
		if tc.short {
			stop, target = 101, 98
		}
		x, ok := follow(tc.short, stop, target, at, tc.bars)
		if ok != (tc.want != "") || x.Kind != tc.want {
			t.Errorf("%s: %+v %v", tc.name, x, ok)
		}
	}
	// fees: taker in and at the stop, maker at the target
	if r := R(false, 100, 99, Exit{Kind: "win", Price: 102}); math.Abs(r-(2-0.04)) > 1e-9 {
		t.Errorf("win R %v", r)
	}
	if r := R(false, 100, 99, Exit{Kind: "loss", Price: 99}); math.Abs(r-(-1-0.0796)) > 1e-9 {
		t.Errorf("loss R %v", r)
	}
}

func TestRedLines(t *testing.T) {
	thu := time.Date(2026, 11, 26, 21, 10, 0, 0, beijing)
	stopped := &Record{Paper: &Paper{At: thu.Add(-time.Hour), Exit: &Exit{Kind: "loss", At: thu.Add(-5 * time.Minute)}}}
	got := strings.Join(redLines(thu, []*Record{stopped, {Paper: &Paper{}}}), "\n")
	for _, want := range []string{"周四 20 点后", "冷冻期", "已经下了 2 笔", "止损才 5 分钟"} {
		if !strings.Contains(got, want) {
			t.Errorf("lacks %q:\n%s", want, got)
		}
	}
	if w := redLines(time.Date(2026, 11, 25, 14, 0, 0, 0, beijing), nil); len(w) != 0 {
		t.Errorf("a Wednesday afternoon: %v", w)
	}
}

func TestReview(t *testing.T) {
	at := time.Date(2026, 11, 24, 14, 0, 0, 0, beijing)
	win := &Exit{Kind: "win", Price: 102, At: at.Add(47 * time.Minute)}
	loss := &Exit{Kind: "loss", Price: 99, At: at.Add(20 * time.Minute)}
	rs := []*Record{
		{Signal: Signal{Entry: 100, Stop: 99, At: at}, Choice: "do", Plan: win, Paper: &Paper{Fill: 100, At: at, Exit: win}},
		{Signal: Signal{Entry: 100, Stop: 101, Short: true, At: at.Add(time.Hour)}, Choice: "skip", Note: "逆着 1 小时", Plan: &Exit{Kind: "win", Price: 98, At: at.Add(2 * time.Hour)}},
		{Signal: Signal{Entry: 100, Stop: 99, At: at.Add(2 * time.Hour)}, Choice: "expired", Plan: loss},
	}
	got := Review(at, rs)
	for _, want := range []string{"共 3 条：下单 1、不下 1", "14:00 做多 · 你：下单 → 模拟单止盈 +2.0R（47 分钟后）", "（逆着 1 小时）", "你下的 1 笔：合计 +2.0R", "你没下的 2 条，照计划做合计 +0.9R"} {
		if !strings.Contains(got, want) {
			t.Errorf("lacks %q:\n%s", want, got)
		}
	}
}

func TestCaption(t *testing.T) {
	until := time.Date(2026, 11, 9, 14, 10, 0, 0, beijing)
	got := Caption(Signal{Symbol: "ETHUSDC", Short: true, Entry: 2000, Stop: 2010, Target: 1980, Qty: 2, Risk: 20, Setup: "趋势回调"}, until)
	for _, want := range []string{"做空 ETHUSDC", "止损 2010.00（+0.50%）", "止盈 1980.00（-1.00%，1:2.0）", "打到止损亏 20.0 U", "14:10 前有效"} {
		if !strings.Contains(got, want) {
			t.Errorf("caption lacks %q:\n%s", want, got)
		}
	}
}
