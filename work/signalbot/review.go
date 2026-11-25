package signalbot

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ReviewAt is when, in Beijing, yesterday's signals come back.
const ReviewAt = 9

// review sends yesterday's signals once the morning comes: what was chosen,
// how each went, and the charts of how they went. reviewed.txt keeps the
// last day sent, so a restart does not send it twice.
func (b *Bot) review(ctx context.Context) error {
	now := b.Now().In(beijing)
	if now.Hour() < ReviewAt {
		return nil
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, beijing)
	day := today.AddDate(0, 0, -1)
	mark := filepath.Join(b.dir, "reviewed.txt")
	if last, _ := os.ReadFile(mark); strings.TrimSpace(string(last)) >= day.Format("2006-01-02") {
		return nil
	}
	var rs []*Record
	for _, r := range load(b.dir, day) {
		if r.At.Before(today) && r.Msg != 0 {
			rs = append(rs, r)
		}
	}
	if len(rs) > 0 {
		if err := b.api.sendText(ctx, b.chat, Review(day, rs)); err != nil {
			return err
		}
		if b.Chart != nil {
			if err := b.charts(ctx, rs); err != nil {
				log.Printf("review charts: %v", err)
			}
		}
	}
	return os.WriteFile(mark, []byte(day.Format("2006-01-02")), 0o600)
}

// Review is the day's signals in words: each, then what the owner's picks
// made against the ones they let go.
func Review(day time.Time, rs []*Record) string {
	var sb strings.Builder
	n := map[string]int{}
	for _, r := range rs {
		n[r.Choice]++
	}
	fmt.Fprintf(&sb, "昨天（%s）的信号复盘\n共 %d 条：下单 %d、不下 %d、稍等 %d、过期 %d\n", day.Format("01-02"), len(rs), n["do"], n["skip"], n["wait"], n["expired"])
	var took, left []float64
	open := 0
	for _, r := range rs {
		side := map[bool]string{false: "做多", true: "做空"}[r.Short]
		fmt.Fprintf(&sb, "\n%s %s · ", r.At.In(beijing).Format("15:04"), side)
		fmt.Fprint(&sb, map[string]string{"do": "你：下单", "skip": "你：不下", "wait": "你：稍等", "expired": "没回", "": "没回"}[r.Choice])
		if r.Note != "" {
			fmt.Fprintf(&sb, "（%s）", r.Note)
		}
		switch {
		case r.Paper != nil && r.Paper.Exit != nil:
			x := R(r.Short, r.Paper.Fill, r.Stop, *r.Paper.Exit)
			took = append(took, x)
			fmt.Fprintf(&sb, " → 模拟单%s", outcome(r.Short, r.Paper.Fill, r.Stop, r.Paper.At, *r.Paper.Exit))
		case r.Paper != nil:
			open++
			sb.WriteString(" → 模拟单还拿着")
		case r.Plan != nil:
			left = append(left, R(r.Short, r.Entry, r.Stop, *r.Plan))
			fmt.Fprintf(&sb, " → 照计划：%s", outcome(r.Short, r.Entry, r.Stop, r.At, *r.Plan))
		default:
			open++
			sb.WriteString(" → 还没走完")
		}
	}
	sum := func(xs []float64) (s float64) {
		for _, x := range xs {
			s += x
		}
		return
	}
	sb.WriteString("\n")
	if len(took) > 0 {
		fmt.Fprintf(&sb, "\n你下的 %d 笔：合计 %+.1fR", len(took), sum(took))
	}
	if len(left) > 0 {
		fmt.Fprintf(&sb, "\n你没下的 %d 条，照计划做合计 %+.1fR", len(left), sum(left))
	}
	if open > 0 {
		fmt.Fprintf(&sb, "\n还有 %d 条没走完，走完了会在原消息上写结果", open)
	}
	return sb.String()
}

// charts sends how each went: the plan with the bars after it, up to its end
// and a little more, ten to an album.
func (b *Bot) charts(ctx context.Context, rs []*Record) error {
	tmp, err := os.MkdirTemp("", "review")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	var ps []photo
	for _, r := range rs {
		end, label := b.Now(), "还没走完"
		if r.Plan != nil {
			end = r.Plan.At
			label = "照计划：" + outcome(r.Short, r.Entry, r.Stop, r.At, *r.Plan)
		}
		if p := r.Paper; p != nil && p.Exit != nil {
			end = p.Exit.At
			label = "模拟单" + outcome(r.Short, p.Fill, r.Stop, p.At, *p.Exit)
		}
		// 90 bars before the signal, all after it up to the end and 12 more
		after := int(end.Sub(r.At)/(5*time.Minute)) + 12
		stop := min(r.At.Add(time.Duration(after)*5*time.Minute).UnixMilli(), b.Now().UnixMilli())
		bars, err := b.Perp.Klines(ctx, r.Symbol+".P", "5m", stop, min(90+after, 1500))
		if err != nil {
			return err
		}
		file := filepath.Join(tmp, r.ID+".png")
		if err := b.Chart(bars, r.Signal, label, file); err != nil {
			return err
		}
		ps = append(ps, photo{file, fmt.Sprintf("%s %s", r.At.In(beijing).Format("15:04"), label)})
	}
	for len(ps) > 0 {
		n := min(len(ps), 10)
		var err error
		if n == 1 {
			_, err = b.api.sendPhoto(ctx, b.chat, ps[0].path, ps[0].caption, nil)
		} else {
			err = b.api.sendAlbum(ctx, b.chat, ps[:n])
		}
		if err != nil {
			return err
		}
		ps = ps[n:]
	}
	return nil
}
