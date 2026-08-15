package manager

import (
	"os"
	"path/filepath"
	"testing"
)

func TestComponentDataUsageMissingDirectory(t *testing.T) {
	mgr, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	usage, err := mgr.componentDataUsageLocked("loki")
	if err != nil {
		t.Fatal(err)
	}
	if usage.Exists || usage.Bytes != 0 {
		t.Fatalf("missing dir usage = %+v", usage)
	}
	if filepath.Base(filepath.Dir(usage.Path)) != "lib" {
		t.Fatalf("path = %s", usage.Path)
	}
}

func TestComponentDataUsageSumsFiles(t *testing.T) {
	mgr, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := mgr.path("/var/lib/loki")
	if err := os.MkdirAll(filepath.Join(dir, "chunks"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "chunks", "a"), []byte("12345"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index"), []byte("abcd"), 0640); err != nil {
		t.Fatal(err)
	}
	usage, err := mgr.componentDataUsageLocked("loki")
	if err != nil {
		t.Fatal(err)
	}
	if !usage.Exists || usage.Bytes != 9 {
		t.Fatalf("usage = %+v", usage)
	}
}

func TestLokiDataDirFollowsPathPrefix(t *testing.T) {
	mgr, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stageComponentConfig(t, mgr, "loki", "common:\n  path_prefix: /data/loki\n")
	dir, err := mgr.componentDataDirLocked("loki")
	if err != nil {
		t.Fatal(err)
	}
	if dir != mgr.path("/data/loki") {
		t.Fatalf("data dir = %s", dir)
	}
}

func TestConfigurationIncludesLokiDataUsage(t *testing.T) {
	mgr, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stageComponentConfig(t, mgr, "loki", lokiConfig("/var/lib/loki", 3100))
	dir := mgr.path("/var/lib/loki")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "chunk"), []byte("xxxxxx"), 0640); err != nil {
		t.Fatal(err)
	}
	configuration, err := mgr.Configuration("loki")
	if err != nil {
		t.Fatal(err)
	}
	if !configuration.DataUsage.Exists || configuration.DataUsage.Bytes != 6 {
		t.Fatalf("configuration usage = %+v", configuration.DataUsage)
	}
}
