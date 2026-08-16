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
	if err := os.MkdirAll(filepath.Dir(mgr.grpcPortPath()), 0755); err != nil {
		t.Fatal(err)
	}
	grpcPort, err := mgr.ensureGRPCPortLocked()
	if err != nil {
		t.Fatal(err)
	}
	if grpcPort < randomPortStart || grpcPort > randomPortEnd {
		t.Fatalf("loki grpc random port = %d", grpcPort)
	}
	if other, exists := ports[grpcPort]; exists {
		t.Fatalf("loki grpc and %s received the same port %d", other, grpcPort)
	}
	again, err := mgr.ensureGRPCPortLocked()
	if err != nil {
		t.Fatal(err)
	}
	if again != grpcPort {
		t.Fatalf("loki grpc port changed from %d to %d", grpcPort, again)
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
			if err := os.WriteFile(unitPath, []byte(spec.unit(false, false, oldPort)), 0644); err != nil {
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

func TestChangeGRPCListenPortUpdatesManagedFiles(t *testing.T) {
	mgr, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const httpPort = 31876
	const oldGRPC = 19095
	const newGRPC = 25432
	configPath := stageComponentConfig(t, mgr, "loki", lokiConfigWithGRPC("/var/lib/loki", httpPort, oldGRPC))
	unitPath := mgr.path("/etc/systemd/system/loki.service")
	if err := os.MkdirAll(filepath.Dir(unitPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unitPath, []byte(lokiUnit(false, httpPort, oldGRPC)), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mgr.listenPortPath("loki"), []byte(strconv.Itoa(httpPort)+"\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mgr.grpcPortPath(), []byte(strconv.Itoa(oldGRPC)+"\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := mgr.ChangeGRPCListenPort(context.Background(), "loki", newGRPC); err != nil {
		t.Fatal(err)
	}
	portFile, err := os.ReadFile(mgr.grpcPortPath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(portFile)) != strconv.Itoa(newGRPC) {
		t.Fatalf("grpc.port = %q", portFile)
	}
	for _, path := range []string{configPath, unitPath} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		got := string(content)
		if !strings.Contains(got, strconv.Itoa(newGRPC)) {
			t.Fatalf("%s does not contain new gRPC port:\n%s", path, got)
		}
		if strings.Contains(got, strconv.Itoa(oldGRPC)) {
			t.Fatalf("%s still contains old gRPC port:\n%s", path, got)
		}
		if !strings.Contains(got, strconv.Itoa(httpPort)) {
			t.Fatalf("%s lost HTTP port:\n%s", path, got)
		}
	}
}

func TestChangeGRPCListenPortRejectsNonLoki(t *testing.T) {
	mgr, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stageComponentConfig(t, mgr, "prometheus", prometheusConfig("/var/lib/prometheus", 19090))
	err = mgr.ChangeGRPCListenPort(context.Background(), "prometheus", 25432)
	if err == nil || !strings.Contains(err.Error(), "仅适用于 Loki") {
		t.Fatalf("error = %v", err)
	}
}

func TestChangeGRPCListenPortRejectsHTTPCollision(t *testing.T) {
	mgr, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const httpPort = 31876
	stageComponentConfig(t, mgr, "loki", lokiConfigWithGRPC("/var/lib/loki", httpPort, 19095))
	if err := os.MkdirAll(filepath.Dir(mgr.listenPortPath("loki")), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mgr.listenPortPath("loki"), []byte(strconv.Itoa(httpPort)+"\n"), 0640); err != nil {
		t.Fatal(err)
	}
	err = mgr.ChangeGRPCListenPort(context.Background(), "loki", httpPort)
	if err == nil || !strings.Contains(err.Error(), "已由 loki 使用") {
		t.Fatalf("collision error = %v", err)
	}
}

func TestChangeListenPortPreservesGRPCPort(t *testing.T) {
	mgr, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const oldHTTP = 31876
	const newHTTP = 25432
	const grpcPort = 19095
	configPath := stageComponentConfig(t, mgr, "loki", lokiConfigWithGRPC("/var/lib/loki", oldHTTP, grpcPort))
	unitPath := mgr.path("/etc/systemd/system/loki.service")
	if err := os.MkdirAll(filepath.Dir(unitPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unitPath, []byte(lokiUnit(false, oldHTTP, grpcPort)), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mgr.listenPortPath("loki"), []byte(strconv.Itoa(oldHTTP)+"\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mgr.grpcPortPath(), []byte(strconv.Itoa(grpcPort)+"\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := mgr.ChangeListenPort(context.Background(), "loki", newHTTP); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{configPath, unitPath, mgr.grpcPortPath()} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(content), strconv.Itoa(grpcPort)) {
			t.Fatalf("%s lost gRPC port:\n%s", path, content)
		}
	}
}

func TestApplyLokiGRPCPortInsertsMissingKeys(t *testing.T) {
	original := []byte(lokiConfig("/var/lib/loki", 31876))
	updated := string(applyLokiGRPCPort(original, 45231))
	for _, want := range []string{
		"http_listen_port: 31876",
		"grpc_listen_address: 127.0.0.1",
		"grpc_listen_port: 45231",
	} {
		if !strings.Contains(updated, want) {
			t.Fatalf("updated config missing %q:\n%s", want, updated)
		}
	}
}

func TestApplyLokiGRPCPortKeepsExistingAddress(t *testing.T) {
	original := []byte("server:\n  grpc_listen_address: 0.0.0.0\n  grpc_listen_port: 9095\n")
	updated := string(applyLokiGRPCPort(original, 45231))
	if !strings.Contains(updated, "grpc_listen_address: 0.0.0.0") {
		t.Fatalf("custom gRPC address was overwritten:\n%s", updated)
	}
	if !strings.Contains(updated, "grpc_listen_port: 45231") {
		t.Fatalf("gRPC port was not updated:\n%s", updated)
	}
}

func TestLegacyLokiWithoutGRPCPortIsUnset(t *testing.T) {
	mgr, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stageComponentConfig(t, mgr, "loki", lokiConfig("/var/lib/loki", 3100))
	port, found, err := mgr.configuredGRPCPortLocked()
	if err != nil {
		t.Fatal(err)
	}
	if found || port != 0 {
		t.Fatalf("legacy grpc port = %d, found=%t; want unset", port, found)
	}
}

func TestLokiUnitIncludesGRPCListenFlags(t *testing.T) {
	unit := lokiUnit(false, 31876, 45231)
	for _, want := range []string{
		"-server.http-listen-port=31876",
		"-server.grpc-listen-address=127.0.0.1",
		"-server.grpc-listen-port=45231",
	} {
		if !strings.Contains(unit, want) {
			t.Fatalf("unit missing %q:\n%s", want, unit)
		}
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
