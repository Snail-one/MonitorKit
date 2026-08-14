package manager

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractPrometheusTarGz(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "prometheus.tar.gz")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)
	files := map[string]string{
		"prometheus-1.2.3.linux-amd64/prometheus": "prometheus-bin",
		"prometheus-1.2.3.linux-amd64/promtool":   "promtool-bin",
		"prometheus-1.2.3.linux-amd64/NOTICE":     "ignored",
	}
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(dir, "out")
	if err := os.Mkdir(out, 0755); err != nil {
		t.Fatal(err)
	}
	found, err := extractBinaries(archivePath, out, []string{"prometheus", "promtool"})
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"prometheus": "prometheus-bin", "promtool": "promtool-bin"} {
		got, err := os.ReadFile(found[name])
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Fatalf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestExtractLokiZipRenamesBinary(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "loki.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(file)
	entry, err := zw.Create("loki-linux-amd64")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("loki-bin")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(dir, "out")
	if err := os.Mkdir(out, 0755); err != nil {
		t.Fatal(err)
	}
	found, err := extractBinaries(archivePath, out, []string{"loki"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(found["loki"])
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "loki-bin" {
		t.Fatalf("loki = %q", got)
	}
}

func TestValidVersion(t *testing.T) {
	for _, test := range []struct {
		value string
		want  bool
	}{
		{"1.2.3", true},
		{"01.2.3", true},
		{"v1.2.3", false},
		{"1.2", false},
		{"1.2.3-beta", false},
		{"../1.2.3", false},
	} {
		if got := validVersion(test.value); got != test.want {
			t.Errorf("validVersion(%q) = %t, want %t", test.value, got, test.want)
		}
	}
}

func TestPrometheusRemoteWriteIsIndependentFromMTLS(t *testing.T) {
	if strings.Contains(prometheusUnit(false, false, 19090), "--web.enable-remote-write-receiver") {
		t.Fatal("plain HTTP Prometheus unit enables the remote-write receiver")
	}
	if strings.Contains(prometheusUnit(true, false, 19090), "--web.enable-remote-write-receiver") {
		t.Fatal("mTLS alone unexpectedly enables the remote-write receiver")
	}
	if !strings.Contains(prometheusUnit(false, true, 19090), "--web.enable-remote-write-receiver") {
		t.Fatal("plain HTTP Prometheus unit does not enable the managed remote-write receiver")
	}
	if !strings.Contains(prometheusUnit(true, true, 19090), "--web.enable-remote-write-receiver") {
		t.Fatal("mTLS Prometheus unit does not enable the remote-write receiver")
	}
}

func TestComponentUnitsPreserveManagedMTLS(t *testing.T) {
	if !strings.Contains(prometheusUnit(true, false, 19090), "--web.config.file=/etc/prometheus/web.yml") {
		t.Fatal("Prometheus mTLS unit does not use the managed web config")
	}
	for _, want := range []string{"-server.http-tls-cert-path=", "-server.http-tls-client-auth=RequireAndVerifyClientCert"} {
		if !strings.Contains(lokiUnit(true, false, 13100), want) {
			t.Fatalf("Loki mTLS unit does not contain %q", want)
		}
	}
	if strings.Contains(prometheusUnit(false, false, 19090), "--web.config.file=") || strings.Contains(lokiUnit(false, false, 13100), "http-tls") {
		t.Fatal("plain HTTP units unexpectedly contain mTLS arguments")
	}
}
