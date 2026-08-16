package manager

import (
	"context"
	"strings"
	"testing"
)

func TestStartAndStopAreNoopsOnStagedRoot(t *testing.T) {
	mgr, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stageComponentConfig(t, mgr, "loki", lokiConfig("/var/lib/loki", 31876))
	if err := mgr.Start(context.Background(), "loki"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Stop(context.Background(), "loki"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.DisableBoot(context.Background(), "loki"); err != nil {
		t.Fatal(err)
	}
}

func TestStartRequiresInstalledConfig(t *testing.T) {
	mgr, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	err = mgr.Start(context.Background(), "loki")
	if err == nil || !strings.Contains(err.Error(), "请先安装") {
		t.Fatalf("error = %v", err)
	}
}

func TestApplyRunningServiceSkipsStoppedUnits(t *testing.T) {
	mgr, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.applyRunningServiceLocked(context.Background(), "loki", false, true); err != nil {
		t.Fatal(err)
	}
	if mgr.serviceActiveLocked(context.Background(), "loki") {
		t.Fatal("staged root unexpectedly reported an active service")
	}
}
