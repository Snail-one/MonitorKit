package manager

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProbeTLSFilesUseCAChainKeyOrder(t *testing.T) {
	mgr, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	files := mgr.probeTLSFilesLocked("node-01")
	wantSuffixes := []string{"ca.crt", "client.crt", "client.key"}
	for index, suffix := range wantSuffixes {
		if !strings.HasSuffix(files[index].Path, suffix) {
			t.Errorf("probe TLS step %d = %s, want %s", index+1, files[index].Path, suffix)
		}
	}
}

func TestRenderProbeScrapeConfigsAddsAndReplacesManagedBlock(t *testing.T) {
	base := []byte(`global:
  scrape_interval: 15s

scrape_configs:
  - job_name: prometheus
    static_configs:
      - targets: ["127.0.0.1:9090"]

rule_files: []
`)
	probes := []Probe{
		{ID: "web-01-a1", Name: "Web 01", Type: "node_exporter", Host: "10.0.0.21", Port: 9100, Enabled: true},
		{ID: "db-01-b2", Name: "DB 01", Type: "node_exporter", Host: "node.example.com", Port: 9443, Enabled: true, MTLS: true, ServerName: "node.example.com"},
	}
	rendered, err := renderProbeScrapeConfigs(base, probes)
	if err != nil {
		t.Fatal(err)
	}
	text := string(rendered)
	for _, want := range []string{probeBlockBegin, "10.0.0.21:9100", "scheme: https", "ca_file: /etc/prometheus/probes/db-01-b2/ca.crt", "server_name: \"node.example.com\""} {
		if !strings.Contains(text, want) {
			t.Errorf("rendered config missing %q:\n%s", want, text)
		}
	}
	if strings.Index(text, probeBlockEnd) > strings.Index(text, "rule_files: []") {
		t.Fatal("managed jobs were inserted outside scrape_configs")
	}

	replaced, err := renderProbeScrapeConfigs(rendered, probes[:1])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(replaced), probeBlockBegin) != 1 || strings.Contains(string(replaced), "db-01-b2") {
		t.Fatalf("managed block was not replaced cleanly:\n%s", replaced)
	}
}

func TestProbeInventoryLifecycleUpdatesPrometheusConfig(t *testing.T) {
	mgr, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	configPath := stageComponentConfig(t, mgr, "prometheus", prometheusConfig("/var/lib/prometheus", 19090))
	probe, err := mgr.AddNodeExporterProbe(context.Background(), Probe{Name: "web-01", Host: "10.0.0.21", Port: 9100}, nil)
	if err != nil {
		t.Fatal(err)
	}
	listed, err := mgr.ListProbes()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != probe.ID || !listed[0].Enabled {
		t.Fatalf("listed probes = %#v", listed)
	}
	assertFileContains(t, configPath, "10.0.0.21:9100")

	if err := mgr.UpdateProbe(context.Background(), probe.ID, "web-primary", "web.example.com", 9200, ""); err != nil {
		t.Fatal(err)
	}
	assertFileContains(t, configPath, "web.example.com:9200")

	if err := mgr.SetProbeEnabled(context.Background(), probe.ID, false); err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(config), probeBlockBegin) || strings.Contains(string(config), "web.example.com:9200") {
		t.Fatalf("disabled probe remains in scrape config:\n%s", config)
	}
	listed, err = mgr.ListProbes()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Enabled {
		t.Fatalf("disabled probe inventory = %#v", listed)
	}

	if err := mgr.DeleteProbe(context.Background(), probe.ID); err != nil {
		t.Fatal(err)
	}
	listed, err = mgr.ListProbes()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Fatalf("probe inventory after delete = %#v", listed)
	}
}

func TestProbeValidationRejectsDuplicateAndUnsafeTargets(t *testing.T) {
	mgr, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stageComponentConfig(t, mgr, "prometheus", prometheusConfig("/var/lib/prometheus", 19090))
	if _, err := mgr.AddNodeExporterProbe(context.Background(), Probe{Name: "bad", Host: "http://10.0.0.1", Port: 9100}, nil); err == nil {
		t.Fatal("unsafe host was accepted")
	}
	if _, err := mgr.AddNodeExporterProbe(context.Background(), Probe{Name: "web", Host: "10.0.0.1", Port: 9100}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.AddNodeExporterProbe(context.Background(), Probe{Name: "web", Host: "10.0.0.2", Port: 9100}, nil); err == nil {
		t.Fatal("duplicate probe name was accepted")
	}
}

func TestProbeInventoryRejectsUnsafeID(t *testing.T) {
	mgr, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := mgr.probeInventoryPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	content := `{"version":1,"probes":[{"id":"../../unsafe","name":"bad","type":"node_exporter","host":"10.0.0.1","port":9100,"enabled":true,"mtls":false,"created_at":"now"}]}`
	if err := os.WriteFile(path, []byte(content), 0640); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.ListProbes(); err == nil || !strings.Contains(err.Error(), "无效 ID") {
		t.Fatalf("ListProbes error = %v", err)
	}
}

func assertFileContains(t *testing.T, path, want string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), want) {
		t.Fatalf("%s does not contain %q:\n%s", path, want, content)
	}
}
