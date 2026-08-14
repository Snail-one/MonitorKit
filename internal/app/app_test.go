package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Snail-one/MonitorKit/internal/manager"
	"github.com/Snail-one/MonitorKit/internal/ui"
)

func TestMainMenuExitsAndShowsMonitoringDomain(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	mgr, err := manager.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	application := New(mgr, ui.New(strings.NewReader("q\n"), &output))
	if err := application.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	for _, want := range []string{"MonitorKit", "Prometheus", "Loki", "探针接入", "[未安装]"} {
		if !strings.Contains(got, want) {
			t.Errorf("menu does not contain %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "-- 未安装") || strings.Contains(got, "-- 运行中") {
		t.Fatalf("home status still uses hint dashes:\n%s", got)
	}
}

func TestComponentMenuShowsGrayStyleHints(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	root := t.TempDir()
	absolute := filepath.Join(root, "usr/local/bin/prometheus")
	if err := os.MkdirAll(filepath.Dir(absolute), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte("installed\n"), 0755); err != nil {
		t.Fatal(err)
	}
	mgr, err := manager.New(root)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	application := New(mgr, ui.New(strings.NewReader("q\n"), &output))
	if err := application.componentMenu(context.Background(), componentViews["prometheus"]); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	for _, want := range []string{
		"配置管理", "-- 编辑/校验/mTLS",
		"安装或更新", "-- 最新/指定版本",
		"卸载程序", "-- 保留数据",
		"彻底清理", "-- 删除数据",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("component menu does not contain %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "[保留数据]") || strings.Contains(got, "[删除数据]") {
		t.Fatalf("component menu still uses badge brackets:\n%s", got)
	}
}

func TestConfigurationAndInstallAreSeparateComponentActions(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	mgr, err := manager.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	// Prometheus -> 安装或更新 -> 返回各级菜单 -> 退出。
	application := New(mgr, ui.New(strings.NewReader("1\n2\nq\nq\nq\n"), &output))
	if err := application.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	for _, want := range []string{"配置管理", "安装或更新", "安装最新稳定版", "安装指定版本", "现有配置、监听端口、mTLS 和独立开关状态"} {
		if !strings.Contains(got, want) {
			t.Errorf("nested install menu does not contain %q:\n%s", want, got)
		}
	}
}

func TestConfigurationMenuShowsPortAndMTLSStateBadges(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	root := t.TempDir()
	for path, content := range map[string]string{
		"usr/local/bin/prometheus":      "installed\n",
		"etc/prometheus/prometheus.yml": "scrape_configs: []\n",
		"etc/prometheus/listen.port":    "48680\n",
	} {
		absolute := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(absolute), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, []byte(content), 0640); err != nil {
			t.Fatal(err)
		}
	}
	mgr, err := manager.New(root)
	if err != nil {
		t.Fatal(err)
	}

	var disabled bytes.Buffer
	application := New(mgr, ui.New(strings.NewReader("q\n"), &disabled))
	if err := application.configurationMenu(context.Background(), componentViews["prometheus"]); err != nil {
		t.Fatal(err)
	}
	got := disabled.String()
	for _, want := range []string{"修改监听端口", "[48680]", "配置或更新 mTLS", "[已关闭]", "开启远程写入"} {
		if !strings.Contains(got, want) {
			t.Errorf("disabled mTLS menu does not contain %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "-- 48680") || strings.Contains(got, "-- 已关闭") {
		t.Fatalf("dynamic values should use badges, not hint dashes:\n%s", got)
	}

	marker := filepath.Join(root, "etc/prometheus/tls/mtls.enabled")
	if err := os.MkdirAll(filepath.Dir(marker), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, []byte("enabled\n"), 0640); err != nil {
		t.Fatal(err)
	}
	var enabled bytes.Buffer
	application = New(mgr, ui.New(strings.NewReader("q\n"), &enabled))
	if err := application.configurationMenu(context.Background(), componentViews["prometheus"]); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(enabled.String(), "[已开启]") {
		t.Fatalf("enabled mTLS menu does not show on state:\n%s", enabled.String())
	}
}

func TestComponentAddressFollowsMTLSState(t *testing.T) {
	if got := componentAddress(false, 23100); got != "http://服务器地址:23100" {
		t.Fatalf("plain address = %q", got)
	}
	if got := componentAddress(true, 23100); got != "https://服务器地址:23100" {
		t.Fatalf("mTLS address = %q", got)
	}
	if got := componentAddress(false, 0); got != "安装时随机生成" {
		t.Fatalf("uninstalled address = %q", got)
	}
}

func TestSelectTextEditorUsesSupportedPriority(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"nano", "vi"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("#!/bin/sh\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", directory)
	editor, err := selectTextEditor()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(editor) != "nano" {
		t.Fatalf("selected editor = %q, want nano before vi", editor)
	}
}

func TestConfigurationMenuShowsDetectedEditorBadge(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "nano"), []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)
	root := t.TempDir()
	for path, content := range map[string]string{
		"usr/local/bin/prometheus":      "installed\n",
		"etc/prometheus/prometheus.yml": "scrape_configs: []\n",
		"etc/prometheus/listen.port":    "48680\n",
	} {
		absolute := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(absolute), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, []byte(content), 0640); err != nil {
			t.Fatal(err)
		}
	}
	mgr, err := manager.New(root)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	application := New(mgr, ui.New(strings.NewReader("q\n"), &output))
	if err := application.configurationMenu(context.Background(), componentViews["prometheus"]); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if !strings.Contains(got, "编辑主配置") || !strings.Contains(got, "[nano]") {
		t.Fatalf("menu does not show detected editor:\n%s", got)
	}
	if strings.Contains(got, "vim/nano/vi") {
		t.Fatalf("menu still uses generic editor hint:\n%s", got)
	}
}

func TestAlloyAccessCardShowsCurrentCenterPorts(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	root := t.TempDir()
	for _, path := range []string{
		"usr/local/bin/prometheus",
		"usr/local/bin/loki",
		"etc/prometheus/tls/mtls.enabled",
		"etc/prometheus/remote-write.enabled",
		"etc/loki/tls/mtls.enabled",
	} {
		absolute := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(absolute), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, []byte("enabled\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	for path, port := range map[string]string{
		"etc/prometheus/listen.port": "24567\n",
		"etc/loki/listen.port":       "31876\n",
	} {
		absolute := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(absolute), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, []byte(port), 0640); err != nil {
			t.Fatal(err)
		}
	}
	mgr, err := manager.New(root)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	application := New(mgr, ui.New(strings.NewReader("\n"), &output))
	application.showAlloyAccessCard(context.Background())
	got := output.String()
	for _, want := range []string{"https://服务器地址:24567", "当前监听端口：24567", "https://服务器地址:31876", "当前监听端口：31876", "已就绪（mTLS + 远程写入）"} {
		if !strings.Contains(got, want) {
			t.Errorf("Alloy access card does not contain %q:\n%s", want, got)
		}
	}
}

func TestAlloyAccessCardShowsConfirmedHTTPRemoteWrite(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	root := t.TempDir()
	for path, content := range map[string]string{
		"usr/local/bin/prometheus":            "installed\n",
		"etc/prometheus/remote-write.enabled": "enabled\n",
		"etc/prometheus/listen.port":          "19367\n",
	} {
		absolute := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(absolute), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, []byte(content), 0755); err != nil {
			t.Fatal(err)
		}
	}
	mgr, err := manager.New(root)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	application := New(mgr, ui.New(strings.NewReader("\n"), &output))
	application.showAlloyAccessCard(context.Background())
	got := output.String()
	for _, want := range []string{"http://服务器地址:19367", "已就绪（HTTP 明文远程写入）"} {
		if !strings.Contains(got, want) {
			t.Errorf("HTTP Alloy access card does not contain %q:\n%s", want, got)
		}
	}
}

func TestHTTPRemoteWriteWarnsAndRequiresConfirmation(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	root := t.TempDir()
	for path, content := range map[string]string{
		"etc/prometheus/prometheus.yml": "scrape_configs: []\n",
		"etc/prometheus/listen.port":    "19367\n",
	} {
		absolute := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(absolute), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, []byte(content), 0640); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "etc/systemd/system"), 0755); err != nil {
		t.Fatal(err)
	}
	mgr, err := manager.New(root)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	application := New(mgr, ui.New(strings.NewReader("y\n\n"), &output))
	application.toggleRemoteWrite(context.Background(), componentViews["prometheus"], manager.Configuration{ListenPort: 19367})
	got := output.String()
	for _, want := range []string{"将使用 HTTP", "明文传输风险", "确认接受明文传输风险", "已开启（HTTP，未加密）"} {
		if !strings.Contains(got, want) {
			t.Errorf("HTTP remote-write confirmation does not contain %q:\n%s", want, got)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "etc/prometheus/remote-write.enabled")); err != nil {
		t.Fatalf("remote-write marker missing after confirmation: %v", err)
	}
	unit, err := os.ReadFile(filepath.Join(root, "etc/systemd/system/prometheus.service"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(unit), "--web.enable-remote-write-receiver") || strings.Contains(string(unit), "--web.config.file") {
		t.Fatalf("unexpected HTTP remote-write unit:\n%s", unit)
	}
}

func TestProbeMenuSeparatesAddAndManagedConfigurations(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	mgr, err := manager.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	application := New(mgr, ui.New(strings.NewReader("q\n"), &output))
	if err := application.probeMenu(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	for _, want := range []string{"添加探针", "管理当前探针", "Prometheus 主动抓取", "主动向 Prometheus/Loki 推送"} {
		if !strings.Contains(got, want) {
			t.Errorf("probe menu does not contain %q:\n%s", want, got)
		}
	}
}

func TestAddProbeMenuListsAlloyBeforeNodeExporter(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	mgr, err := manager.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	application := New(mgr, ui.New(strings.NewReader("q\n"), &output))
	if err := application.addProbeMenu(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	alloy := strings.Index(got, "配置 Grafana Alloy")
	nodeExporter := strings.Index(got, "添加 node_exporter 接入")
	if alloy < 0 || nodeExporter < 0 || alloy > nodeExporter {
		t.Fatalf("Alloy is not listed before node_exporter:\n%s", got)
	}
}
