package manager

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseDefaultLokiConfigHasUnlimitedLogs(t *testing.T) {
	settings, err := parseLokiLogSettings([]byte(lokiConfig("/var/lib/loki", 3100)))
	if err != nil {
		t.Fatal(err)
	}
	if settings != (LokiLogSettings{}) {
		t.Fatalf("default settings = %+v, want zero value", settings)
	}
}

func TestApplyLokiRetentionEnablesCompactor(t *testing.T) {
	updated, err := applyLokiLogSettings([]byte(lokiConfig("/var/lib/loki", 3100)), LokiLogSettings{
		Retention: 30 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := string(updated)
	for _, want := range []string{
		"allow_structured_metadata: true",
		"retention_period: 30d",
		"max_query_lookback: 30d",
		"retention_enabled: true",
		"working_directory: /var/lib/loki/compactor",
		"delete_request_store: filesystem",
		"compaction_interval: 10m",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("updated config missing %q:\n%s", want, got)
		}
	}
	if strings.Count(got, "limits_config:") != 1 {
		t.Fatalf("duplicate limits_config:\n%s", got)
	}
}

func TestApplyLokiLogSettingsPreservesCustomKeys(t *testing.T) {
	original := `auth_enabled: false

server:
  http_listen_port: 31876

common:
  path_prefix: /data/loki

limits_config:
  allow_structured_metadata: true
  volume_enabled: true
  retention_stream:
    - selector: '{job="keep"}'
      period: 90d

schema_config:
  configs:
    - from: 2024-04-01
      store: tsdb
`
	updated, err := applyLokiLogSettings([]byte(original), LokiLogSettings{
		Retention:        7 * 24 * time.Hour,
		IngestionRateMB:  8,
		IngestionBurstMB: 16,
		MaxLineSizeKB:    512,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := string(updated)
	for _, want := range []string{
		"volume_enabled: true",
		"retention_stream:",
		`selector: '{job="keep"}'`,
		"allow_structured_metadata: true",
		"retention_period: 7d",
		"ingestion_rate_mb: 8",
		"ingestion_burst_size_mb: 16",
		"max_line_size: 512KB",
		"working_directory: /data/loki/compactor",
		"schema_config:",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("updated config missing %q:\n%s", want, got)
		}
	}
}

func TestApplyUnlimitedRetentionRemovesDeletes(t *testing.T) {
	enabled, err := applyLokiLogSettings([]byte(lokiConfig("/var/lib/loki", 3100)), LokiLogSettings{
		Retention:        15 * 24 * time.Hour,
		IngestionRateMB:  10,
		IngestionBurstMB: 20,
		MaxLineSizeKB:    1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	cleared, err := applyLokiLogSettings(enabled, LokiLogSettings{})
	if err != nil {
		t.Fatal(err)
	}
	got := string(cleared)
	for _, banned := range []string{
		"retention_period:",
		"max_query_lookback:",
		"ingestion_rate_mb:",
		"ingestion_burst_size_mb:",
		"max_line_size:",
	} {
		if strings.Contains(got, banned) {
			t.Errorf("cleared config still has %s:\n%s", banned, got)
		}
	}
	if !strings.Contains(got, "retention_enabled: false") {
		t.Fatalf("compactor deletes were not disabled:\n%s", got)
	}
	if !strings.Contains(got, "allow_structured_metadata: true") {
		t.Fatalf("structured metadata flag was removed:\n%s", got)
	}
}

func TestParseLokiLogSettingsReadsExistingValues(t *testing.T) {
	config := `
limits_config:
  retention_period: 168h
  ingestion_rate_mb: 12
  ingestion_burst_size_mb: 24
  max_line_size: 1MB
compactor:
  retention_enabled: true
`
	settings, err := parseLokiLogSettings([]byte(config))
	if err != nil {
		t.Fatal(err)
	}
	want := LokiLogSettings{
		Retention:        7 * 24 * time.Hour,
		RetentionDeletes: true,
		IngestionRateMB:  12,
		IngestionBurstMB: 24,
		MaxLineSizeKB:    1024,
	}
	if settings != want {
		t.Fatalf("settings = %+v, want %+v", settings, want)
	}
}

func TestValidateLokiLogSettingsRejectsShortRetention(t *testing.T) {
	err := validateLokiLogSettings(LokiLogSettings{Retention: 12 * time.Hour})
	if err == nil || !strings.Contains(err.Error(), "24 小时") {
		t.Fatalf("error = %v", err)
	}
}

func TestApplyLokiLogSettingsWritesStagedConfig(t *testing.T) {
	mgr, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := stageComponentConfig(t, mgr, "loki", lokiConfig("/var/lib/loki", 3100))
	if err := mgr.ApplyLokiLogSettings(context.Background(), LokiLogSettings{Retention: 90 * 24 * time.Hour}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "retention_period: 90d") {
		t.Fatalf("staged config missing retention:\n%s", content)
	}
	configuration, err := mgr.Configuration("loki")
	if err != nil {
		t.Fatal(err)
	}
	if configuration.LogSettings.Retention != 90*24*time.Hour {
		t.Fatalf("configuration retention = %s", configuration.LogSettings.Retention)
	}
	if !configuration.LogSettings.RetentionDeletes {
		t.Fatal("configuration does not report retention deletes")
	}
}

func TestConfigurationLogSettingsIgnoreMissingLokiFile(t *testing.T) {
	mgr, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	configuration, err := mgr.Configuration("loki")
	if err != nil {
		t.Fatal(err)
	}
	if configuration.LogSettings != (LokiLogSettings{}) {
		t.Fatalf("missing config settings = %+v", configuration.LogSettings)
	}
}

func TestFormatAndParseRoundTrip(t *testing.T) {
	for _, value := range []time.Duration{
		24 * time.Hour,
		7 * 24 * time.Hour,
		30 * 24 * time.Hour,
		12 * time.Hour,
	} {
		parsed, err := parsePromDuration(formatLokiDuration(value))
		if err != nil {
			t.Fatalf("parse %s: %v", value, err)
		}
		if parsed != value {
			t.Fatalf("round trip %s = %s", value, parsed)
		}
	}
}

func TestParseByteSizeKB(t *testing.T) {
	for _, test := range []struct {
		input string
		want  int
	}{
		{input: "256KB", want: 256},
		{input: "1MB", want: 1024},
		{input: "2048", want: 2},
	} {
		got, err := parseByteSizeKB(test.input)
		if err != nil {
			t.Fatalf("parse %s: %v", test.input, err)
		}
		if got != test.want {
			t.Fatalf("parse %s = %d, want %d", test.input, got, test.want)
		}
	}
}

func TestApplyLokiLogSettingsLeavesFileMode(t *testing.T) {
	mgr, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := stageComponentConfig(t, mgr, "loki", lokiConfig("/var/lib/loki", 3100))
	if err := os.Chmod(path, 0640); err != nil {
		t.Fatal(err)
	}
	if err := mgr.ApplyLokiLogSettings(context.Background(), LokiLogSettings{Retention: 7 * 24 * time.Hour}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0640 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	if _, err := os.Stat(filepath.Join(mgr.root, "etc/loki/loki.yml")); err != nil {
		t.Fatal(err)
	}
}
