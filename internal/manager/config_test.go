package manager

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func stageComponentConfig(t *testing.T, mgr *Manager, name, content string) string {
	t.Helper()
	path := mgr.path("/etc/" + name + "/" + name + ".yml")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0640); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestEditConfigRestoresOriginalWhenEditorFails(t *testing.T) {
	mgr, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := stageComponentConfig(t, mgr, "prometheus", "original\n")
	err = mgr.EditConfig(context.Background(), "prometheus", func(path string) error {
		if err := os.WriteFile(path, []byte("partial edit\n"), 0600); err != nil {
			return err
		}
		return errors.New("editor failed")
	})
	if err == nil || !strings.Contains(err.Error(), "原配置已恢复") {
		t.Fatalf("EditConfig error = %v", err)
	}
	content, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != "original\n" {
		t.Fatalf("restored config = %q", content)
	}
}

func TestEditConfigAppliesValidStagedChange(t *testing.T) {
	mgr, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := stageComponentConfig(t, mgr, "loki", "original\n")
	if err := mgr.EditConfig(context.Background(), "loki", func(path string) error {
		return os.WriteFile(path, []byte("updated\n"), 0600)
	}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "updated\n" {
		t.Fatalf("updated config = %q", content)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0640 {
		t.Fatalf("config mode = %o, want 640", info.Mode().Perm())
	}
}

func TestRemoveRejectedConfigsDeletesOnlyMatchingRegularFiles(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "prometheus.yml")
	if err := os.WriteFile(configPath, []byte("config\n"), 0640); err != nil {
		t.Fatal(err)
	}
	rejectedPath := configPath + ".rejected-old"
	unrelatedPath := filepath.Join(directory, "loki.yml.rejected-old")
	for path, content := range map[string]string{rejectedPath: "invalid\n", unrelatedPath: "keep\n"} {
		if err := os.WriteFile(path, []byte(content), 0640); err != nil {
			t.Fatal(err)
		}
	}
	rejectedDirectory := configPath + ".rejected-directory"
	if err := os.Mkdir(rejectedDirectory, 0750); err != nil {
		t.Fatal(err)
	}

	if err := removeRejectedConfigs(configPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(rejectedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("matching rejected file still exists: %v", err)
	}
	for _, path := range []string{unrelatedPath, rejectedDirectory} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("unrelated path was removed: %s: %v", path, err)
		}
	}
}

func TestDisableMTLSPreservesCertificates(t *testing.T) {
	mgr, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stageComponentConfig(t, mgr, "prometheus", "global: {}\n")
	marker := mgr.mtlsMarkerPath("prometheus")
	if err := os.MkdirAll(filepath.Dir(marker), 0750); err != nil {
		t.Fatal(err)
	}
	certificate := filepath.Join(filepath.Dir(marker), "server.crt")
	if err := os.WriteFile(marker, []byte("enabled\n"), 0640); err != nil {
		t.Fatal(err)
	}
	remoteWriteMarker := mgr.remoteWriteMarkerPath()
	if err := os.WriteFile(remoteWriteMarker, []byte("enabled\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certificate, []byte("certificate fixture\n"), 0640); err != nil {
		t.Fatal(err)
	}
	unitPath := mgr.path("/etc/systemd/system/prometheus.service")
	if err := os.MkdirAll(filepath.Dir(unitPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unitPath, []byte(prometheusUnit(true, false, 19090)), 0644); err != nil {
		t.Fatal(err)
	}

	if err := mgr.DisableMTLS(context.Background(), "prometheus"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("mTLS marker still exists: %v", err)
	}
	if _, err := os.Stat(remoteWriteMarker); err != nil {
		t.Fatalf("remote-write marker was not preserved: %v", err)
	}
	if _, err := os.Stat(certificate); err != nil {
		t.Fatalf("certificate was not preserved: %v", err)
	}
	unit, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(unit), "--web.config.file") {
		t.Fatal("plain unit still enables Prometheus mTLS")
	}
	if !strings.Contains(string(unit), "--web.enable-remote-write-receiver") {
		t.Fatal("disabling mTLS unexpectedly disabled remote write")
	}
}

func TestConfigureMTLSStagesValidatedFilesAndUnits(t *testing.T) {
	if _, err := exec.LookPath("openssl"); err != nil {
		t.Skip("openssl is not installed")
	}
	fixtureDir := t.TempDir()
	certificatePath := filepath.Join(fixtureDir, "server.crt")
	keyPath := filepath.Join(fixtureDir, "server.key")
	command := exec.Command("openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes",
		"-keyout", keyPath, "-out", certificatePath, "-days", "1", "-subj", "/CN=monitor.example.com")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generate certificate: %v\n%s", err, output)
	}
	certificate, err := os.ReadFile(certificatePath)
	if err != nil {
		t.Fatal(err)
	}
	privateKey, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}

	mgr, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(mgr.path("/etc/systemd/system"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"prometheus", "loki"} {
		stageComponentConfig(t, mgr, name, "fixture: true\n")
		if err := os.WriteFile(mgr.path("/etc/systemd/system/"+name+".service"), []byte("original unit\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := mgr.ConfigureMTLS(context.Background(), name, func(file TLSFile) error {
			content := certificate
			if strings.HasSuffix(file.Path, "server.key") {
				content = privateKey
			}
			return os.WriteFile(file.Path, content, 0640)
		}); err != nil {
			t.Fatalf("ConfigureMTLS(%s): %v", name, err)
		}
		if !mtlsEnabledLocked(mgr, name) {
			t.Fatalf("%s mTLS marker was not created", name)
		}
		unit, err := os.ReadFile(mgr.path("/etc/systemd/system/" + name + ".service"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(unit), "tls") && !strings.Contains(string(unit), "web.config.file") {
			t.Fatalf("%s unit does not enable mTLS:\n%s", name, unit)
		}
	}
	if _, err := os.Stat(mgr.path("/etc/prometheus/web.yml")); err != nil {
		t.Fatalf("Prometheus web config was not generated: %v", err)
	}
}
