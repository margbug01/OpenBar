package signalbot

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Coach has Claude look at a signal's chart and say, in two or three
// sentences, what is good or bad in it against the owner's own record
// (coach.md in the folder: their red lines, what they do right and the
// mistakes they make) and what they chose on the last signals, in their own
// words. No labels: the words are how it learns what the owner wants.
type Coach struct {
	Claude string // the claude command
	Model  string
	Dir    string // the bot's folder
}

// Reason runs Claude on s and its chart; it may take a minute.
func (c Coach) Reason(ctx context.Context, s Signal, chart string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.Claude, "-p", c.prompt(s, chart), "--model", c.Model,
		"--allowedTools", "Read", "--output-format", "json", "--no-session-persistence")
	cmd.Dir = c.Dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("claude: %w", err)
	}
	var r struct {
		Result  string `json:"result"`
		IsError bool   `json:"is_error"`
	}
	if err := json.Unmarshal(out, &r); err != nil || r.IsError || r.Result == "" {
		return "", fmt.Errorf("claude: %.200s", out)
	}
	return strings.TrimSpace(r.Result), nil
}

func (c Coach) prompt(s Signal, chart string) string {
	side := map[bool]string{false: "做多", true: "做空"}[s.Short]
	var sb strings.Builder
	fmt.Fprintf(&sb, `你在帮一位交易者看 %s 永续 5 分钟图上的一条信号。对方停了一阵子交易，正在靠这些信号重新练盘感：每一条都由对方自己决定下不下。你写两三句理由给对方参考，不替对方做决定。

图：%s（用 Read 打开看）。图上是到信号这根为止的 K 线、EMA20（黄线）和 VWAP，盈亏比框是计划的入场、止损和止盈。
计划：%s，入场 %.2f，止损 %.2f，止盈 %.2f（2R）。现在是北京时间 %s %s。
扫描规则：趋势创新高（低）后回调到 EMA20，第一根收回趋势方向的 K 线收盘入场；止损在回调极值外再加一个回调幅度。
`, s.Symbol, chart, side, s.Entry, s.Stop, s.Target, weekday[s.At.In(beijing).Weekday()], s.At.In(beijing).Format("01-02 15:04"))
	if len(s.Faults) > 0 {
		fmt.Fprintf(&sb, "回测里标记「不做」的情况，这条碰到了：%s（今天没有干净的信号，这是保底推送）。\n", strings.Join(s.Faults, "、"))
	}
	if len(s.Warnings) > 0 {
		fmt.Fprintf(&sb, "按对方的红线，现在：%s。\n", strings.Join(s.Warnings, "；"))
	}
	if p, err := os.ReadFile(filepath.Join(c.Dir, "coach.md")); err == nil {
		fmt.Fprintf(&sb, "\n对方的交易习惯和红线（从对方自己的交易日志整理）：\n---\n%s\n---\n", p)
	}
	if h := history(c.Dir, s.ID, 15); h != "" {
		fmt.Fprintf(&sb, "\n对方最近对信号的选择和原话，这是理解对方想要什么的主要依据：\n%s\n", h)
	}
	sb.WriteString(`
要求：中文，不超过 120 字，纯文本，不要 markdown、不要列表、不要开场白。先说图上这笔具体好在哪或差在哪（结构、位置、K 线），再说它碰没碰到对方的红线或常犯的错。不改计划里的数字，不说「建议下单」「建议不下」。`)
	return sb.String()
}

var weekday = [...]string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}

// history is the last n signals before id and the owner's notes, a line
// each, oldest first.
func history(dir, id string, n int) string {
	var lines []string
	for _, r := range load(dir, time.Now().AddDate(0, 0, -30)) {
		if r.ID == id || r.Msg == 0 {
			continue
		}
		line := fmt.Sprintf("%s %s：%s", r.At.In(beijing).Format("01-02 15:04"), map[bool]string{false: "做多", true: "做空"}[r.Short],
			map[string]string{"do": "下单", "skip": "不下", "wait": "稍等", "expired": "没回", "": "还没回"}[r.Choice])
		if r.Note != "" {
			line += "，原话「" + r.Note + "」"
		}
		if r.Plan != nil {
			line += "；计划后来" + outcome(r.Short, r.Entry, r.Stop, r.At, *r.Plan)
		}
		lines = append(lines, line)
	}
	lines = lines[max(len(lines)-n, 0):]
	if f, err := os.Open(filepath.Join(dir, "log.jsonl")); err == nil {
		defer f.Close()
		var notes []string
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			var d Decision
			if json.Unmarshal(sc.Bytes(), &d) == nil && d.Choice == "note" && d.ID == "" && d.Note != "" {
				notes = append(notes, fmt.Sprintf("%s 随口说：「%s」", d.At.In(beijing).Format("01-02 15:04"), d.Note))
			}
		}
		lines = append(lines, notes[max(len(notes)-10, 0):]...)
	}
	return strings.Join(lines, "\n")
}
