package main

import (
	"os"
	"strings"
	"testing"
)

func TestVersionAndHelpSkipRootCheck(t *testing.T) {
	if err := run([]string{"--version"}); err != nil {
		t.Fatalf("--version: %v", err)
	}
	if err := run([]string{"help"}); err != nil {
		t.Fatalf("help: %v", err)
	}
}

func TestRunRequiresRoot(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("当前已是 root，无法验证拒绝非特权启动")
	}
	for _, args := range [][]string{
		nil,
		{"status"},
		{"install", "loki"},
		{"start", "loki"},
		{"update"},
	} {
		err := run(args)
		if err == nil || !strings.Contains(err.Error(), "需要 root 权限") {
			t.Fatalf("args %v: error = %v", args, err)
		}
	}
}

func TestRequireRoot(t *testing.T) {
	err := requireRoot()
	if os.Geteuid() == 0 {
		if err != nil {
			t.Fatalf("root requireRoot() = %v", err)
		}
		return
	}
	if err == nil || !strings.Contains(err.Error(), "sudo monitorkit") {
		t.Fatalf("non-root requireRoot() = %v", err)
	}
}
