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
	for _, want := range []string{"MonitorKit", "指标中心", "日志中心", "部署监控栈", "探针接入", "管理接口"} {
		if !strings.Contains(got, want) {
			t.Errorf("menu does not contain %q:\n%s", want, got)
		}
	}
}

func TestReadyCount(t *testing.T) {
	statuses := map[string]manager.Status{
		"prometheus": {Installed: true, ServiceState: "active"},
		"loki":       {Installed: true, ServiceState: "inactive"},
	}
	if got := readyCount(statuses); got != 1 {
		t.Fatalf("readyCount = %d, want 1", got)
	}
}

func TestComponentAddressFollowsMTLSState(t *testing.T) {
	component := componentViews["loki"]
	if got := componentAddress(component, false); got != "http://服务器地址:3100" {
		t.Fatalf("plain address = %q", got)
	}
	if got := componentAddress(component, true); got != "https://服务器地址:3100" {
		t.Fatalf("mTLS address = %q", got)
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
