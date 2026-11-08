package signalbot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
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

func decisions(t *testing.T, dir string) []Decision {
	data, _ := os.ReadFile(filepath.Join(dir, "log.jsonl"))
	var ds []Decision
	for _, l := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if l == "" {
			continue
		}
		var d Decision
		if err := json.Unmarshal([]byte(l), &d); err != nil {
			t.Fatal(err)
		}
		ds = append(ds, d)
	}
	return ds
}

func putSignal(t *testing.T, dir, id string) {
	os.WriteFile(filepath.Join(dir, "inbox", id+".png"), []byte("png"), 0o600)
	j, _ := json.Marshal(Signal{ID: id, Symbol: "ETHUSDC", Entry: 2421.36, Stop: 2404.65, Target: 2454.78, Image: id + ".png"})
	os.WriteFile(filepath.Join(dir, "inbox", id+".json"), j, 0o600)
}

func TestBot(t *testing.T) {
	srv, calls := fakeTelegram(t)
	dir := t.TempDir()
	now := time.Date(2026, 11, 9, 14, 0, 0, 0, time.Local)
	b := newBot(dir, srv.URL+"/botX", srv.Client())
	b.Now = func() time.Time { return now }
	ctx := context.Background()

	// nothing goes out before the owner says /start; strangers are ignored
	putSignal(t, dir, "s1")
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

	// the inbox goes out as a photo with three buttons, then moves to sent/
	b.tick(ctx)
	c = calls()
	if len(c) != 1 || c[0].method != "sendPhoto" || c[0].params["photo"] != true || !strings.Contains(c[0].params["reply_markup"].(string), "do:s1") {
		t.Fatalf("send: %v", c)
	}
	if !strings.Contains(c[0].params["caption"].(string), "止损 2404.65") {
		t.Fatalf("caption: %s", c[0].params["caption"])
	}
	if _, err := os.Stat(filepath.Join(dir, "sent", "s1.json")); err != nil {
		t.Fatal("not moved to sent/")
	}

	// a stranger's press does nothing; the owner's 下单 is kept once
	b.handle(ctx, press(222, "do:s1"))
	b.handle(ctx, press(111, "do:s1"))
	b.handle(ctx, press(111, "skip:s1"))
	ds := decisions(t, dir)
	if len(ds) != 1 || ds[0].Choice != "do" || ds[0].ID != "s1" {
		t.Fatalf("decisions: %+v", ds)
	}
	c = calls()
	edited := false
	for _, x := range c {
		if x.method == "editMessageCaption" {
			edited = strings.Contains(x.params["caption"].(string), "下单")
		}
	}
	if !edited {
		t.Fatalf("the message does not show the choice: %v", c)
	}

	// 稍等 asks why and keeps the answer with the signal
	putSignal(t, dir, "s2")
	b.tick(ctx)
	b.handle(ctx, press(111, "wait:s2"))
	b.handle(ctx, msg(111, "像假突破，等下一根"))
	ds = decisions(t, dir)
	if last := ds[len(ds)-1]; last.ID != "s2" || last.Choice != "note" || last.Note != "像假突破，等下一根" {
		t.Fatalf("wait note: %+v", ds)
	}

	// unanswered for 10 minutes it expires, and a late press is turned down
	putSignal(t, dir, "s3")
	b.tick(ctx)
	calls()
	now = now.Add(10*time.Minute + time.Second)
	b.tick(ctx)
	b.handle(ctx, press(111, "do:s3"))
	ds = decisions(t, dir)
	if last := ds[len(ds)-1]; last.ID != "s3" || last.Choice != "expired" {
		t.Fatalf("expiry: %+v", ds)
	}
	for _, x := range calls() {
		if x.method == "answerCallbackQuery" && x.params["text"] != "这个信号已经结束了" {
			t.Fatalf("late press answered %v", x.params["text"])
		}
	}
}

func TestCaption(t *testing.T) {
	until := time.Date(2026, 11, 9, 14, 10, 0, 0, time.Local)
	got := Caption(Signal{Symbol: "ETHUSDC", Short: true, Entry: 2000, Stop: 2010, Target: 1980, Qty: 2, Risk: 20, Setup: "趋势回调"}, until)
	for _, want := range []string{"做空 ETHUSDC", "止损 2010.00（+0.50%）", "止盈 1980.00（-1.00%，1:2.0）", "打到止损亏 20.0 U", "14:10 前有效"} {
		if !strings.Contains(got, want) {
			t.Errorf("caption lacks %q:\n%s", want, got)
		}
	}
}
