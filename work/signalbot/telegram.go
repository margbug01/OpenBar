package signalbot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
)

// api is the little of Telegram's Bot API the bot uses.
type api struct {
	base string // https://api.telegram.org/bot<token>
	http *http.Client
}

type update struct {
	ID       int64    `json:"update_id"`
	Message  *message `json:"message"`
	Callback *struct {
		ID      string   `json:"id"`
		From    user     `json:"from"`
		Message *message `json:"message"`
		Data    string   `json:"data"`
	} `json:"callback_query"`
}

type message struct {
	ID   int64 `json:"message_id"`
	Chat struct {
		ID int64 `json:"id"`
	} `json:"chat"`
	From user   `json:"from"`
	Text string `json:"text"`
}

type user struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

type button struct {
	Text string `json:"text"`
	Data string `json:"callback_data"`
}

type keyboard struct {
	Rows [][]button `json:"inline_keyboard"`
}

// call posts params as JSON and decodes the result into out.
func (a api) call(ctx context.Context, method string, params, out any) error {
	body, err := json.Marshal(params)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.base+"/"+method, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return a.do(req, method, out)
}

func (a api) do(req *http.Request, method string, out any) error {
	resp, err := a.http.Do(req)
	if err != nil {
		return fmt.Errorf("telegram %s: %w", method, redact(err))
	}
	defer resp.Body.Close()
	var r struct {
		OK          bool            `json:"ok"`
		Description string          `json:"description"`
		Result      json.RawMessage `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&r); err != nil {
		return fmt.Errorf("telegram %s: HTTP %d: %w", method, resp.StatusCode, err)
	}
	if !r.OK {
		return fmt.Errorf("telegram %s: %s", method, r.Description)
	}
	if out != nil {
		return json.Unmarshal(r.Result, out)
	}
	return nil
}

// redact keeps the token out of errors: url.Error carries the address.
func redact(err error) error {
	if u, ok := err.(interface{ Unwrap() error }); ok {
		if inner := u.Unwrap(); inner != nil {
			return inner
		}
	}
	return err
}

// sendPhoto uploads the picture at path with a caption and buttons.
func (a api) sendPhoto(ctx context.Context, chat int64, path, caption string, kb *keyboard) (message, error) {
	fields := map[string]string{"chat_id": fmt.Sprint(chat), "caption": caption}
	if kb != nil {
		j, _ := json.Marshal(kb)
		fields["reply_markup"] = string(j)
	}
	var m message
	return m, a.upload(ctx, "sendPhoto", fields, map[string]string{"photo": path}, &m)
}

// photo is one picture of an album.
type photo struct {
	path, caption string
}

// sendAlbum sends up to 10 pictures as one message.
func (a api) sendAlbum(ctx context.Context, chat int64, ps []photo) error {
	type media struct {
		Type    string `json:"type"`
		Media   string `json:"media"`
		Caption string `json:"caption,omitempty"`
	}
	var ms []media
	files := map[string]string{}
	for i, p := range ps {
		name := fmt.Sprintf("p%d", i)
		ms = append(ms, media{"photo", "attach://" + name, p.caption})
		files[name] = p.path
	}
	j, _ := json.Marshal(ms)
	return a.upload(ctx, "sendMediaGroup", map[string]string{"chat_id": fmt.Sprint(chat), "media": string(j)}, files, nil)
}

// upload posts fields and the files at their paths, by field name.
func (a api) upload(ctx context.Context, method string, fields, files map[string]string, out any) error {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		w.WriteField(k, v)
	}
	for name, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		part, err := w.CreateFormFile(name, filepath.Base(path))
		if err != nil {
			return err
		}
		part.Write(data)
	}
	w.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.base+"/"+method, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	return a.do(req, method, out)
}

func (a api) sendText(ctx context.Context, chat int64, text string) error {
	return a.call(ctx, "sendMessage", map[string]any{"chat_id": chat, "text": text}, nil)
}

// reply sends text as an answer to message to, quoting it.
func (a api) reply(ctx context.Context, chat, to int64, text string) error {
	return a.call(ctx, "sendMessage", map[string]any{"chat_id": chat, "text": text,
		"reply_parameters": map[string]any{"message_id": to, "allow_sending_without_reply": true}}, nil)
}

// editCaption replaces a photo's caption and buttons; kb nil takes them away.
func (a api) editCaption(ctx context.Context, chat, msg int64, caption string, kb *keyboard) error {
	p := map[string]any{"chat_id": chat, "message_id": msg, "caption": caption}
	if kb != nil {
		p["reply_markup"] = kb
	} else {
		p["reply_markup"] = keyboard{Rows: [][]button{}}
	}
	return a.call(ctx, "editMessageCaption", p, nil)
}

func (a api) answer(ctx context.Context, id, text string) error {
	return a.call(ctx, "answerCallbackQuery", map[string]any{"callback_query_id": id, "text": text}, nil)
}

func (a api) updates(ctx context.Context, offset int64, wait int) ([]update, error) {
	var us []update
	err := a.call(ctx, "getUpdates", map[string]any{"offset": offset, "timeout": wait,
		"allowed_updates": []string{"message", "callback_query"}}, &us)
	return us, err
}
