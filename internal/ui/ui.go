// Package ui contains the terminal presentation primitives used by MonitorKit.
// It deliberately has no monitoring-domain dependencies so the visual system
// can evolve independently from component lifecycle logic.
package ui

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

const (
	reset  = "\033[0m"
	bold   = "\033[1m"
	dim    = "\033[2m"
	orange = "\033[38;5;208m"
	blue   = "\033[38;5;75m"
	green  = "\033[38;5;78m"
	yellow = "\033[38;5;220m"
	red    = "\033[38;5;203m"
	gray   = "\033[38;5;245m"
	line   = 52
)

type Tone uint8

const (
	Neutral Tone = iota
	Success
	Warning
	Danger
)

type Field struct {
	Label  string
	Value  string
	Detail string
}

type UI struct {
	reader             *bufio.Reader
	out                io.Writer
	color              bool
	interactive        bool
	progressLineActive bool
}

func New(input io.Reader, output io.Writer) *UI {
	return &UI{
		reader:      bufio.NewReader(input),
		out:         output,
		color:       colorEnabled(output),
		interactive: isTerminal(output),
	}
}

func (u *UI) Clear() {
	if u.interactive && !strings.EqualFold(os.Getenv("TERM"), "dumb") {
		fmt.Fprint(u.out, "\033[2J\033[H\033[3J")
	}
}

func (u *UI) Home(subtitle, access string, privileged bool) {
	fmt.Fprintln(u.out, u.paint(orange, "╭─")+" "+u.paint(bold+orange, "MonitorKit"))
	fmt.Fprintln(u.out, u.paint(orange, "│")+" "+u.paint(orange, strings.TrimSpace(subtitle))+"  "+u.Badge(access, privileged))
	fmt.Fprintln(u.out, u.paint(orange, "╰"+strings.Repeat("─", line)))
	fmt.Fprintln(u.out)
}

func (u *UI) Title(parts ...string) {
	clean := []string{"MonitorKit"}
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			clean = append(clean, part)
		}
	}
	fmt.Fprintln(u.out, u.paint(orange, "╭─")+" "+u.paint(bold+orange, strings.Join(clean, "  ›  ")))
	fmt.Fprintln(u.out, u.paint(orange, "╰"+strings.Repeat("─", line)))
	fmt.Fprintln(u.out)
}

func (u *UI) Option(key, label, hint string) {
	if hint = strings.TrimSpace(hint); hint == "" {
		u.writeOption(key, label, "")
		return
	}
	u.writeOption(key, label, u.paint(gray, "-- "+hint))
}

func (u *UI) OptionValue(key, label, value string, positive bool) {
	u.writeOption(key, label, u.Badge(value, positive))
}

func (u *UI) OptionState(key, label string, enabled bool) {
	u.writeOption(key, label, u.StateBadge(enabled))
}

func (u *UI) OptionLive(key, label, value string, active bool) {
	u.writeOption(key, label, u.LiveBadge(value, active))
}

func (u *UI) writeOption(key, label, suffix string) {
	keyText := u.paint(bold+blue, fmt.Sprintf("%-4s", strings.TrimSpace(key)))
	if strings.TrimSpace(suffix) == "" {
		fmt.Fprintf(u.out, "  %s %s\n", keyText, label)
		return
	}
	fmt.Fprintf(u.out, "  %s %s%s\n", keyText, pad(label, 22), suffix)
}

func (u *UI) ExitOption(label string) {
	fmt.Fprintf(u.out, "  %s %s\n", u.paint(bold+yellow, "0/q "), u.paint(dim, label))
}

func (u *UI) Blank() { fmt.Fprintln(u.out) }

// Progress prints a durable installation phase. Unlike the animated spinner,
// completed lines remain visible so long downloads still have useful context.
func (u *UI) Progress(step, total int, message, detail string) {
	marker := u.paint(bold+orange, fmt.Sprintf("[%d/%d]", step, total))
	fmt.Fprintf(u.out, "%s %s\n", marker, strings.TrimSpace(message))
	if detail = strings.TrimSpace(detail); detail != "" {
		fmt.Fprintf(u.out, "      %s\n", u.paint(gray, detail))
	}
}

// DownloadProgress renders a real byte-based progress bar when attached to a
// terminal. Redirected output only records the final result to stay readable.
func (u *UI) DownloadProgress(downloaded, total int64, name string, done bool) {
	name = strings.TrimSpace(name)
	if !u.interactive {
		if done {
			fmt.Fprintf(u.out, "[下载完成] %s · %s\n", name, formatBytes(downloaded))
		}
		return
	}

	const width = 24
	filled := 0
	percentText := " --%"
	sizeText := formatBytes(downloaded)
	if total > 0 {
		percent := downloaded * 100 / total
		if percent > 100 {
			percent = 100
		}
		filled = int(percent) * width / 100
		percentText = fmt.Sprintf("%3d%%", percent)
		sizeText += " / " + formatBytes(total)
	} else if done {
		filled = width
		percentText = "完成"
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	fmt.Fprintf(u.out, "\r\033[2K%s %s  %s  %s", u.paint(orange, bar), percentText, sizeText, u.paint(gray, name))
	u.progressLineActive = true
	if done {
		fmt.Fprintln(u.out)
		u.progressLineActive = false
	}
}

func (u *UI) FinishProgressLine() {
	if u.progressLineActive {
		fmt.Fprintln(u.out)
		u.progressLineActive = false
	}
}

func (u *UI) Ask(prompt string) (string, error) {
	prompt = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(prompt), "："))
	fmt.Fprint(u.out, u.paint(bold+orange, "❯ ")+u.paint(bold, prompt+"：")+" ")
	value, err := u.reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

func (u *UI) Confirm(prompt string) (bool, error) {
	answer, err := u.Ask(prompt + " [y/N]")
	if err != nil {
		return false, err
	}
	return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes"), nil
}

func (u *UI) Pause() {
	_ = u.Wait("按回车继续")
}

// Wait keeps important instructions visible until the user explicitly
// acknowledges them. It is used before handing control to a full-screen editor.
func (u *UI) Wait(prompt string) error {
	fmt.Fprintln(u.out)
	fmt.Fprint(u.out, u.paint(dim, strings.TrimSpace(prompt)+"…")+" ")
	_, err := u.reader.ReadString('\n')
	return err
}

func (u *UI) InvalidChoice() {
	fmt.Fprintln(u.out, u.paint(yellow, "选项不存在，请重新选择"))
}

func (u *UI) Badge(text string, positive bool) string {
	style := yellow
	if positive {
		style = green
	}
	return u.paint(style, "["+strings.TrimSpace(text)+"]")
}

func (u *UI) StateBadge(enabled bool) string {
	if enabled {
		return u.LiveBadge("已开启", true)
	}
	return u.LiveBadge("已关闭", false)
}

func (u *UI) LiveBadge(text string, active bool) string {
	style := gray
	if active {
		style = orange
	}
	return u.paint(style, "["+strings.TrimSpace(text)+"]")
}

func (u *UI) Card(tone Tone, title string, fields ...Field) {
	titleStyle := bold + orange
	switch tone {
	case Success:
		titleStyle = bold + green
	case Warning:
		titleStyle = bold + yellow
	case Danger:
		titleStyle = bold + red
	}
	fmt.Fprintln(u.out, u.paint(bold+orange, "╭─ MonitorKit"))
	fmt.Fprintln(u.out, u.paint(orange, "│")+" "+u.paint(titleStyle, strings.TrimSpace(title)))
	for _, field := range fields {
		label := strings.TrimSpace(field.Label)
		value := strings.TrimSpace(field.Value)
		if label == "" {
			fmt.Fprintln(u.out, u.paint(orange, "│")+" "+value)
		} else {
			fmt.Fprintln(u.out, u.paint(orange, "│")+" "+u.paint(blue, label+"：")+value)
		}
		if detail := strings.TrimSpace(field.Detail); detail != "" {
			fmt.Fprintln(u.out, u.paint(orange, "│")+"   "+u.paint(gray, detail))
		}
	}
	fmt.Fprintln(u.out, u.paint(orange, "╰"+strings.Repeat("─", line)))
}

func (u *UI) Muted(text string) string   { return u.paint(gray, text) }
func (u *UI) Success(text string) string { return u.paint(green, text) }
func (u *UI) Warning(text string) string { return u.paint(yellow, text) }

func (u *UI) paint(style, text string) string {
	if !u.color {
		return text
	}
	return style + text + reset
}

func colorEnabled(output io.Writer) bool {
	if _, disabled := os.LookupEnv("NO_COLOR"); disabled || strings.EqualFold(os.Getenv("TERM"), "dumb") {
		return false
	}
	if forced := os.Getenv("CLICOLOR_FORCE"); forced != "" && forced != "0" {
		return true
	}
	return isTerminal(output)
}

func isTerminal(output io.Writer) bool {
	file, ok := output.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func pad(text string, width int) string {
	remaining := width - displayWidth(text)
	if remaining < 1 {
		remaining = 1
	}
	return text + strings.Repeat(" ", remaining)
}

func displayWidth(text string) int {
	width := 0
	inEscape := false
	for len(text) > 0 {
		r, size := utf8.DecodeRuneInString(text)
		text = text[size:]
		if inEscape {
			if r == 'm' {
				inEscape = false
			}
			continue
		}
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if r >= 0x2e80 {
			width += 2
		} else {
			width++
		}
	}
	return width
}

func formatBytes(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	value := float64(size)
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	for _, unit := range units {
		value /= 1024
		if value < 1024 || unit == units[len(units)-1] {
			return fmt.Sprintf("%.1f %s", value, unit)
		}
	}
	return fmt.Sprintf("%d B", size)
}
