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
	terminal.Option("1", "指标服务", "运行中")
	terminal.ExitOption("退出")
	answer, err := terminal.Ask("选择")
	if err != nil {
		t.Fatal(err)
	}
	if answer != "1" {
		t.Fatalf("answer = %q", answer)
	}
	got := output.String()
	for _, want := range []string{"╭─ MonitorKit", "监控中心", "指标服务", "-- 运行中", "0/q", "❯ 选择："} {
		if !strings.Contains(got, want) {
			t.Errorf("output does not contain %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\033[") {
		t.Fatalf("plain output contains ANSI: %q", got)
	}
}

func TestOptionRendersHintAsGrayExplanation(t *testing.T) {
	t.Setenv("CLICOLOR_FORCE", "1")
	t.Setenv("NO_COLOR", "")
	var output bytes.Buffer
	terminal := New(strings.NewReader(""), &output)
	terminal.color = true
	terminal.Option("3", "卸载程序", "保留数据")
	got := output.String()
	if !strings.Contains(got, "卸载程序") || !strings.Contains(got, "-- 保留数据") {
		t.Fatalf("option output = %q", got)
	}
	if !strings.Contains(got, gray) {
		t.Fatalf("option hint is not gray: %q", got)
	}
	if strings.Contains(got, "[保留数据]") {
		t.Fatalf("option still uses badge brackets: %q", got)
	}
}

func TestOptionValueRendersLiveDataAsBadge(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var output bytes.Buffer
	terminal := New(strings.NewReader(""), &output)
	terminal.OptionValue("4", "修改监听端口", "48680", true)
	terminal.OptionValue("5", "配置或更新 mTLS", "已关闭", false)
	got := output.String()
	for _, want := range []string{"[48680]", "[已关闭]"} {
		if !strings.Contains(got, want) {
			t.Errorf("value option does not contain %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "-- 48680") || strings.Contains(got, "-- 已关闭") {
		t.Fatalf("value option used hint dashes:\n%s", got)
	}
}

func TestOptionLiveRendersActiveStatusOrangeAndInactiveGray(t *testing.T) {
	t.Setenv("CLICOLOR_FORCE", "1")
	t.Setenv("NO_COLOR", "")
	var running bytes.Buffer
	runningUI := New(strings.NewReader(""), &running)
	runningUI.color = true
	runningUI.OptionLive("1", "Prometheus", "运行中", true)
	if got := running.String(); !strings.Contains(got, "[运行中]") || !strings.Contains(got, orange) || strings.Contains(got, gray) {
		t.Fatalf("running status = %q", got)
	}

	var missing bytes.Buffer
	missingUI := New(strings.NewReader(""), &missing)
	missingUI.color = true
	missingUI.OptionLive("2", "Loki", "未安装", false)
	if got := missing.String(); !strings.Contains(got, "[未安装]") || !strings.Contains(got, gray) || strings.Contains(got, orange) {
		t.Fatalf("missing status = %q", got)
	}
}

func TestOptionStateUsesOrangeWhenEnabledAndGrayWhenClosed(t *testing.T) {
	t.Setenv("CLICOLOR_FORCE", "1")
	t.Setenv("NO_COLOR", "")
	var closed bytes.Buffer
	closedUI := New(strings.NewReader(""), &closed)
	closedUI.color = true
	closedUI.OptionState("5", "配置或更新 mTLS", false)
	if got := closed.String(); !strings.Contains(got, "[已关闭]") || !strings.Contains(got, gray) || strings.Contains(got, orange) {
		t.Fatalf("closed state = %q", got)
	}

	var open bytes.Buffer
	openUI := New(strings.NewReader(""), &open)
	openUI.color = true
	openUI.OptionState("6", "开启远程写入", true)
	if got := open.String(); !strings.Contains(got, "[已开启]") || !strings.Contains(got, orange) || strings.Contains(got, gray) {
		t.Fatalf("open state = %q", got)
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

func TestWaitShowsReasonBeforeContinuing(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var output bytes.Buffer
	terminal := New(strings.NewReader("\n"), &output)
	if err := terminal.Wait("确认填写要求后，按回车打开 vim"); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); !strings.Contains(got, "确认填写要求后，按回车打开 vim") {
		t.Fatalf("wait output = %q", got)
	}
}

func TestProgressKeepsStepAndDownloadDetailVisible(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var output bytes.Buffer
	terminal := New(strings.NewReader(""), &output)
	terminal.Progress(3, 7, "下载并校验安装包", "v3.7.2 · prometheus-3.7.2.linux-amd64.tar.gz")

	got := output.String()
	for _, want := range []string{"[3/7]", "下载并校验安装包", "prometheus-3.7.2.linux-amd64.tar.gz"} {
		if !strings.Contains(got, want) {
			t.Errorf("progress output does not contain %q:\n%s", want, got)
		}
	}
}

func TestDownloadProgressKeepsCompletionInRedirectedOutput(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var output bytes.Buffer
	terminal := New(strings.NewReader(""), &output)
	terminal.DownloadProgress(8*1024*1024, 10*1024*1024, "prometheus.tar.gz", false)
	terminal.DownloadProgress(10*1024*1024, 10*1024*1024, "prometheus.tar.gz", true)

	got := output.String()
	for _, want := range []string{"[下载完成]", "prometheus.tar.gz", "10.0 MiB"} {
		if !strings.Contains(got, want) {
			t.Errorf("download output does not contain %q:\n%s", want, got)
		}
	}
}

func TestInteractiveDownloadProgressShowsRealPercentage(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var output bytes.Buffer
	terminal := New(strings.NewReader(""), &output)
	terminal.interactive = true
	terminal.DownloadProgress(5*1024*1024, 10*1024*1024, "prometheus.tar.gz", false)
	terminal.DownloadProgress(10*1024*1024, 10*1024*1024, "prometheus.tar.gz", true)

	got := output.String()
	for _, want := range []string{"50%", "100%", "5.0 MiB / 10.0 MiB", "████"} {
		if !strings.Contains(got, want) {
			t.Errorf("interactive download output does not contain %q:\n%s", want, got)
		}
	}
}
