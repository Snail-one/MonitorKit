package manager

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestFreshComponentsReceivePersistentRandomPorts(t *testing.T) {
	mgr, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ports := make(map[int]string)
	for _, name := range []string{"prometheus", "loki"} {
		if err := os.MkdirAll(filepath.Dir(mgr.listenPortPath(name)), 0755); err != nil {
			t.Fatal(err)
		}
		port, err := mgr.ensureListenPortLocked(name)
		if err != nil {
			t.Fatal(err)
		}
		if port < randomPortStart || port > randomPortEnd {
			t.Fatalf("%s random port = %d", name, port)
		}
		if other, exists := ports[port]; exists {
			t.Fatalf("%s and %s received the same port %d", name, other, port)
		}
		ports[port] = name
		again, err := mgr.ensureListenPortLocked(name)
		if err != nil {
			t.Fatal(err)
		}
		if again != port {
			t.Fatalf("%s port changed from %d to %d", name, port, again)
		}
	}
}

func TestChangeListenPortUpdatesManagedFiles(t *testing.T) {
	for _, name := range []string{"prometheus", "loki"} {
		t.Run(name, func(t *testing.T) {
			mgr, err := New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			const oldPort = 19090
			const newPort = 25432
			configPath := stageComponentConfig(t, mgr, name, registeredConfig(name, oldPort))
			unitPath := mgr.path("/etc/systemd/system/" + name + ".service")
			if err := os.MkdirAll(filepath.Dir(unitPath), 0755); err != nil {
				t.Fatal(err)
			}
			spec, err := lookup(name)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(unitPath, []byte(spec.unit(false, oldPort)), 0644); err != nil {
				t.Fatal(err)
			}
			if err := mgr.ChangeListenPort(context.Background(), name, newPort); err != nil {
				t.Fatal(err)
			}
			portFile, err := os.ReadFile(mgr.listenPortPath(name))
			if err != nil {
				t.Fatal(err)
			}
			if strings.TrimSpace(string(portFile)) != strconv.Itoa(newPort) {
				t.Fatalf("listen.port = %q", portFile)
			}
			for _, path := range []string{configPath, unitPath} {
				content, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(content), strconv.Itoa(newPort)) {
					t.Fatalf("%s does not contain new port:\n%s", path, content)
				}
				if strings.Contains(string(content), strconv.Itoa(oldPort)) {
					t.Fatalf("%s still contains old port:\n%s", path, content)
				}
			}
		})
	}
}

func TestListenPortValidation(t *testing.T) {
	for _, port := range []int{-1, 0, 80, 65536} {
		if validateListenPort(port) == nil {
			t.Errorf("port %d unexpectedly accepted", port)
		}
	}
	for _, port := range []int{1024, 9090, 65535} {
		if err := validateListenPort(port); err != nil {
			t.Errorf("port %d rejected: %v", port, err)
		}
	}
}

func TestLegacyConfigsPreserveDefaultPorts(t *testing.T) {
	for _, test := range []struct {
		name string
		port int
	}{
		{name: "prometheus", port: 9090},
		{name: "loki", port: 3100},
	} {
		t.Run(test.name, func(t *testing.T) {
			mgr, err := New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			stageComponentConfig(t, mgr, test.name, registeredConfig(test.name, test.port))
			port, found, err := mgr.configuredListenPortLocked(test.name)
			if err != nil {
				t.Fatal(err)
			}
			if !found || port != test.port {
				t.Fatalf("legacy port = %d, found=%t; want %d", port, found, test.port)
			}
		})
	}
}

func registeredConfig(name string, port int) string {
	if name == "prometheus" {
		return prometheusConfig("/var/lib/prometheus", port)
	}
	return lokiConfig("/var/lib/loki", port)
}
