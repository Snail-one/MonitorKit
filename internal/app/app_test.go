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
	for _, want := range []string{"MonitorKit", "Prometheus", "Loki", "探针接入"} {
		if !strings.Contains(got, want) {
			t.Errorf("menu does not contain %q:\n%s", want, got)
		}
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
