package ui

import (
	"bytes"
	"strings"
	"testing"
)

func TestPlainOutputHasStableHierarchy(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var output bytes.Buffer
	terminal := New(strings.NewReader("1\n"), &output)
	terminal.Home("监控中心", "只读", false)
	terminal.Option("1", "指标服务", terminal.Badge("运行中", true))
	terminal.ExitOption("退出")
	answer, err := terminal.Ask("选择")
	if err != nil {
		t.Fatal(err)
	}
	if answer != "1" {
		t.Fatalf("answer = %q", answer)
	}
	got := output.String()
	for _, want := range []string{"╭─ MonitorKit", "监控中心", "指标服务", "[运行中]", "0/q", "❯ 选择："} {
		if !strings.Contains(got, want) {
			t.Errorf("output does not contain %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\033[") {
		t.Fatalf("plain output contains ANSI: %q", got)
	}
}

func TestDisplayWidthHandlesChineseAndANSI(t *testing.T) {
	if got := displayWidth("服务A"); got != 5 {
		t.Fatalf("displayWidth = %d, want 5", got)
	}
	if got := displayWidth("\033[32m正常\033[0m"); got != 4 {
		t.Fatalf("colored displayWidth = %d, want 4", got)
	}
}
