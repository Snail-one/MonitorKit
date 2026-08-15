package manager

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultPrometheusUnitHasNoRetentionFlags(t *testing.T) {
	unit := prometheusUnit(false, false, 19090)
	if strings.Contains(unit, "--storage.tsdb.retention.") {
		t.Fatalf("default unit unexpectedly sets retention:\n%s", unit)
	}
}

func TestPrometheusUnitIncludesStorageFlags(t *testing.T) {
	unit := prometheusUnitWithStorage(false, true, 19090, PrometheusStorageSettings{
		Retention: 30 * 24 * time.Hour,
		SizeBytes: 20 * 1024 * 1024 * 1024,
	})
	for _, want := range []string{
		"--storage.tsdb.retention.time=30d",
		"--storage.tsdb.retention.size=20GB",
		"--web.enable-remote-write-receiver",
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("unit missing %q:\n%s", want, unit)
		}
	}
}

func TestApplyPrometheusStorageSettingsWritesUnitAndFile(t *testing.T) {
	mgr, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stageComponentConfig(t, mgr, "prometheus", prometheusConfig("/var/lib/prometheus", 19090))
	unitPath := mgr.path("/etc/systemd/system/prometheus.service")
	if err := os.MkdirAll(filepath.Dir(unitPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unitPath, []byte(prometheusUnit(false, false, 19090)), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mgr.listenPortPath("prometheus"), []byte("19090\n"), 0640); err != nil {
		t.Fatal(err)
	}

	settings := PrometheusStorageSettings{Retention: 30 * 24 * time.Hour, SizeBytes: 10 * 1024 * 1024 * 1024}
	if err := mgr.ApplyPrometheusStorageSettings(context.Background(), settings); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(mgr.prometheusStorageSettingsPath())
	if err != nil {
		t.Fatal(err)
	}
	got := string(content)
	if !strings.Contains(got, "retention_time=30d") || !strings.Contains(got, "retention_size=10GB") {
		t.Fatalf("settings file = %q", got)
	}
	unit, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(unit), "--storage.tsdb.retention.time=30d") || !strings.Contains(string(unit), "--storage.tsdb.retention.size=10GB") {
		t.Fatalf("unit missing retention flags:\n%s", unit)
	}
	configuration, err := mgr.Configuration("prometheus")
	if err != nil {
		t.Fatal(err)
	}
	if configuration.MetricSettings != settings {
		t.Fatalf("configuration = %+v, want %+v", configuration.MetricSettings, settings)
	}
}

func TestApplyDefaultPrometheusStorageSettingsRemovesCustomFlags(t *testing.T) {
	mgr, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stageComponentConfig(t, mgr, "prometheus", prometheusConfig("/var/lib/prometheus", 19090))
	unitPath := mgr.path("/etc/systemd/system/prometheus.service")
	if err := os.MkdirAll(filepath.Dir(unitPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mgr.listenPortPath("prometheus"), []byte("19090\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := mgr.ApplyPrometheusStorageSettings(context.Background(), PrometheusStorageSettings{Unlimited: true}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.ApplyPrometheusStorageSettings(context.Background(), PrometheusStorageSettings{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(mgr.prometheusStorageSettingsPath()); !os.IsNotExist(err) {
		t.Fatalf("default settings file still exists: %v", err)
	}
	unit, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(unit), "--storage.tsdb.retention.") {
		t.Fatalf("default unit still has retention flags:\n%s", unit)
	}
}

func TestRemoteWritePreservesPrometheusStorageSettings(t *testing.T) {
	mgr, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stageComponentConfig(t, mgr, "prometheus", prometheusConfig("/var/lib/prometheus", 19090))
	unitPath := mgr.path("/etc/systemd/system/prometheus.service")
	if err := os.MkdirAll(filepath.Dir(unitPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mgr.listenPortPath("prometheus"), []byte("19090\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := mgr.ApplyPrometheusStorageSettings(context.Background(), PrometheusStorageSettings{Retention: 7 * 24 * time.Hour}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SetRemoteWrite(context.Background(), "prometheus", true); err != nil {
		t.Fatal(err)
	}
	unit, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(unit)
	if !strings.Contains(got, "--storage.tsdb.retention.time=7d") {
		t.Fatalf("remote write dropped retention:\n%s", got)
	}
	if !strings.Contains(got, "--web.enable-remote-write-receiver") {
		t.Fatalf("remote write flag missing:\n%s", got)
	}
}

func TestParsePrometheusStorageSettingsFromUnitFallback(t *testing.T) {
	mgr, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	unitPath := mgr.path("/etc/systemd/system/prometheus.service")
	if err := os.MkdirAll(filepath.Dir(unitPath), 0755); err != nil {
		t.Fatal(err)
	}
	unit := prometheusUnitWithStorage(false, false, 19090, PrometheusStorageSettings{
		Unlimited: true,
		SizeBytes: 5 * 1024 * 1024 * 1024,
	})
	if err := os.WriteFile(unitPath, []byte(unit), 0644); err != nil {
		t.Fatal(err)
	}
	settings, err := mgr.prometheusStorageSettingsLocked()
	if err != nil {
		t.Fatal(err)
	}
	want := PrometheusStorageSettings{Unlimited: true, SizeBytes: 5 * 1024 * 1024 * 1024}
	if settings != want {
		t.Fatalf("settings = %+v, want %+v", settings, want)
	}
}

func TestValidatePrometheusStorageSettingsRejectsShortRetention(t *testing.T) {
	err := validatePrometheusStorageSettings(PrometheusStorageSettings{Retention: 12 * time.Hour})
	if err == nil || !strings.Contains(err.Error(), "24 小时") {
		t.Fatalf("error = %v", err)
	}
}

func TestParseAndFormatPrometheusSize(t *testing.T) {
	for _, test := range []struct {
		input string
		want  int64
		text  string
	}{
		{input: "20GB", want: 20 * 1024 * 1024 * 1024, text: "20GB"},
		{input: "1024MB", want: 1024 * 1024 * 1024, text: "1GB"},
		{input: "1TB", want: 1024 * 1024 * 1024 * 1024, text: "1TB"},
	} {
		got, err := parsePrometheusSize(test.input)
		if err != nil {
			t.Fatalf("parse %s: %v", test.input, err)
		}
		if got != test.want {
			t.Fatalf("parse %s = %d, want %d", test.input, got, test.want)
		}
		if formatted := formatPrometheusSize(got); formatted != test.text {
			t.Fatalf("format %s = %s, want %s", test.input, formatted, test.text)
		}
	}
}
