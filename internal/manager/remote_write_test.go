package manager

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func stagePrometheusService(t *testing.T, mgr *Manager, mtls, remoteWrite bool) string {
	t.Helper()
	const port = 19090
	stageComponentConfig(t, mgr, "prometheus", prometheusConfig("/var/lib/prometheus", port))
	unitPath := mgr.path("/etc/systemd/system/prometheus.service")
	if err := os.MkdirAll(filepath.Dir(unitPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unitPath, []byte(prometheusUnit(mtls, remoteWrite, port)), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mgr.listenPortPath("prometheus"), []byte("19090\n"), 0640); err != nil {
		t.Fatal(err)
	}
	return unitPath
}

func TestRemoteWriteCannotBeEnabledWithoutMTLS(t *testing.T) {
	mgr, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	unitPath := stagePrometheusService(t, mgr, false, false)
	err = mgr.SetRemoteWrite(context.Background(), "prometheus", true)
	if err == nil || !strings.Contains(err.Error(), "必须先配置并启用 mTLS") {
		t.Fatalf("SetRemoteWrite error = %v", err)
	}
	if _, err := os.Stat(mgr.remoteWriteMarkerPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("remote-write marker exists: %v", err)
	}
	unit, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(unit), "--web.enable-remote-write-receiver") {
		t.Fatal("remote write was enabled without mTLS")
	}
}

func TestRemoteWriteHasIndependentManagedSwitch(t *testing.T) {
	mgr, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	unitPath := stagePrometheusService(t, mgr, true, false)
	markerPath := mgr.mtlsMarkerPath("prometheus")
	if err := os.MkdirAll(filepath.Dir(markerPath), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(markerPath, []byte("enabled\n"), 0640); err != nil {
		t.Fatal(err)
	}

	if err := mgr.SetRemoteWrite(context.Background(), "prometheus", true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(mgr.remoteWriteMarkerPath()); err != nil {
		t.Fatalf("remote-write marker missing: %v", err)
	}
	unit, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(unit), "--web.enable-remote-write-receiver") || !strings.Contains(string(unit), "--web.config.file") {
		t.Fatalf("enabled unit is incomplete:\n%s", unit)
	}

	if err := mgr.SetRemoteWrite(context.Background(), "prometheus", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(mgr.remoteWriteMarkerPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("remote-write marker still exists: %v", err)
	}
	unit, err = os.ReadFile(unitPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(unit), "--web.enable-remote-write-receiver") {
		t.Fatalf("remote write still enabled:\n%s", unit)
	}
	if !strings.Contains(string(unit), "--web.config.file") {
		t.Fatal("disabling remote write unexpectedly disabled mTLS")
	}
}
