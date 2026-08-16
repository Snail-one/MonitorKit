package manager

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
)

const (
	minimumListenPort = 1024
	maximumListenPort = 65535
	randomPortStart   = 10000
	randomPortEnd     = 59999
)

type listenPortRole string

const (
	listenPortHTTP  listenPortRole = "http"
	listenPortGRPC  listenPortRole = "grpc"
	lokiGRPCAddress                = "127.0.0.1"
)

var (
	prometheusUnitPortPattern   = regexp.MustCompile(`--web\.listen-address=[^\s:]+:(\d+)`)
	prometheusConfigPortPattern = regexp.MustCompile(`127\.0\.0\.1:(\d+)`)
	lokiUnitPortPattern         = regexp.MustCompile(`-server\.http-listen-port=(\d+)`)
	lokiConfigPortPattern       = regexp.MustCompile(`(?m)^(\s*http_listen_port:\s*)(\d+)(\s*(?:#.*)?)$`)
	lokiUnitGRPCPortPattern     = regexp.MustCompile(`-server\.grpc-listen-port=(\d+)`)
	lokiConfigGRPCPortPattern   = regexp.MustCompile(`(?m)^(\s*grpc_listen_port:\s*)(\d+)(\s*(?:#.*)?)$`)
)

// ChangeListenPort atomically changes the externally reachable HTTP(S) port.
// The managed port file is the source of truth across updates and mTLS changes.
func (m *Manager) ChangeListenPort(ctx context.Context, name string, port int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	spec, configPath, err := m.configTargetLocked(name)
	if err != nil {
		return err
	}
	if err := validateListenPort(port); err != nil {
		return err
	}
	currentPort, err := m.ensureListenPortLocked(spec.name)
	if err != nil {
		return err
	}
	if port == currentPort {
		return nil
	}
	if err := m.ensurePortAvailableLocked(spec.name, listenPortHTTP, port); err != nil {
		return err
	}

	unitPath := m.path("/etc/systemd/system/" + spec.name + ".service")
	portPath := m.listenPortPath(spec.name)
	managedPaths := []string{configPath, unitPath, portPath}
	if spec.name == "loki" {
		managedPaths = append(managedPaths, m.grpcPortPath())
	}
	snapshots, err := snapshotFiles(managedPaths)
	if err != nil {
		return err
	}
	wasActive := m.serviceActiveLocked(ctx, spec.name)
	rollback := func(cause error) error {
		restoreErr := restoreSnapshots(snapshots)
		if m.isLiveRoot() {
			_ = m.fixConfigOwnershipLocked(ctx, spec, configPath)
		}
		m.restoreRunningServiceLocked(ctx, spec.name, wasActive)
		if restoreErr != nil {
			return fmt.Errorf("%v；恢复原端口配置失败：%w", cause, restoreErr)
		}
		return cause
	}

	config, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	updatedConfig := updateManagedConfigPort(spec.name, config, currentPort, port)
	if err := atomicWrite(configPath, updatedConfig, snapshots[0].mode); err != nil {
		return rollback(err)
	}
	mtlsEnabled := mtlsEnabledLocked(m, spec.name)
	unit, err := m.componentUnitLocked(spec, mtlsEnabled, managedRemoteWriteEnabled(m, spec.name), port)
	if err != nil {
		return rollback(err)
	}
	if err := atomicWrite(unitPath, []byte(unit), 0644); err != nil {
		return rollback(err)
	}
	if err := atomicWrite(portPath, []byte(strconv.Itoa(port)+"\n"), 0640); err != nil {
		return rollback(err)
	}
	if err := m.fixConfigOwnershipLocked(ctx, spec, configPath); err != nil {
		return rollback(err)
	}
	if !m.isLiveRoot() {
		return nil
	}
	if err := spec.validate(ctx, configPath); err != nil {
		return rollback(fmt.Errorf("修改端口后的配置校验失败：%w", err))
	}
	if mtlsEnabledLocked(m, spec.name) {
		if err := m.validateMTLSConfigLocked(ctx, spec, configPath); err != nil {
			return rollback(err)
		}
	}
	if err := m.applyRunningServiceLocked(ctx, spec.name, wasActive, true); err != nil {
		return rollback(fmt.Errorf("新端口应用失败，已恢复原端口：%w", err))
	}
	return nil
}

// ChangeGRPCListenPort atomically changes Loki's internal gRPC listen port.
func (m *Manager) ChangeGRPCListenPort(ctx context.Context, name string, port int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	spec, configPath, err := m.configTargetLocked(name)
	if err != nil {
		return err
	}
	if spec.name != "loki" {
		return errors.New("gRPC 端口仅适用于 Loki")
	}
	if err := validateListenPort(port); err != nil {
		return err
	}
	currentPort, found, err := m.configuredGRPCPortLocked()
	if err != nil {
		return err
	}
	if found && port == currentPort {
		return nil
	}
	if err := m.ensurePortAvailableLocked(spec.name, listenPortGRPC, port); err != nil {
		return err
	}

	unitPath := m.path("/etc/systemd/system/" + spec.name + ".service")
	httpPort, err := m.ensureListenPortLocked(spec.name)
	if err != nil {
		return err
	}
	portPath := m.grpcPortPath()
	snapshots, err := snapshotFiles([]string{configPath, unitPath, portPath})
	if err != nil {
		return err
	}
	wasActive := m.serviceActiveLocked(ctx, spec.name)
	rollback := func(cause error) error {
		restoreErr := restoreSnapshots(snapshots)
		if m.isLiveRoot() {
			_ = m.fixConfigOwnershipLocked(ctx, spec, configPath)
		}
		m.restoreRunningServiceLocked(ctx, spec.name, wasActive)
		if restoreErr != nil {
			return fmt.Errorf("%v；恢复原 gRPC 端口配置失败：%w", cause, restoreErr)
		}
		return cause
	}

	config, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	if err := atomicWrite(configPath, applyLokiGRPCPort(config, port), snapshots[0].mode); err != nil {
		return rollback(err)
	}
	if err := atomicWrite(portPath, []byte(strconv.Itoa(port)+"\n"), 0640); err != nil {
		return rollback(err)
	}
	unit, err := m.componentUnitLocked(spec, mtlsEnabledLocked(m, spec.name), false, httpPort)
	if err != nil {
		return rollback(err)
	}
	if err := atomicWrite(unitPath, []byte(unit), 0644); err != nil {
		return rollback(err)
	}
	if err := m.fixConfigOwnershipLocked(ctx, spec, configPath); err != nil {
		return rollback(err)
	}
	if !m.isLiveRoot() {
		return nil
	}
	if err := spec.validate(ctx, configPath); err != nil {
		return rollback(fmt.Errorf("修改 gRPC 端口后的配置校验失败：%w", err))
	}
	if mtlsEnabledLocked(m, spec.name) {
		if err := m.validateMTLSConfigLocked(ctx, spec, configPath); err != nil {
			return rollback(err)
		}
	}
	if err := m.applyRunningServiceLocked(ctx, spec.name, wasActive, true); err != nil {
		return rollback(fmt.Errorf("新 gRPC 端口应用失败，已恢复原端口：%w", err))
	}
	return nil
}

// RandomListenPort returns an available random port suitable for ChangeListenPort.
func (m *Manager) RandomListenPort(name string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := lookup(name); err != nil {
		return 0, err
	}
	return m.randomListenPortLocked(name, listenPortHTTP)
}

// RandomGRPCPort returns an available random port suitable for ChangeGRPCListenPort.
func (m *Manager) RandomGRPCPort() (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.randomListenPortLocked("loki", listenPortGRPC)
}

func (m *Manager) listenPortPath(name string) string {
	return m.path("/etc/" + name + "/listen.port")
}

func (m *Manager) grpcPortPath() string {
	return m.path("/etc/loki/grpc.port")
}

func (m *Manager) ensureListenPortLocked(name string) (int, error) {
	port, found, err := m.configuredListenPortLocked(name)
	if err != nil {
		return 0, err
	}
	if !found {
		port, err = m.randomListenPortLocked(name, listenPortHTTP)
		if err != nil {
			return 0, err
		}
	}
	if err := atomicWrite(m.listenPortPath(name), []byte(strconv.Itoa(port)+"\n"), 0640); err != nil {
		return 0, fmt.Errorf("保存 %s 监听端口：%w", name, err)
	}
	return port, nil
}

func (m *Manager) ensureGRPCPortLocked() (int, error) {
	port, found, err := m.configuredGRPCPortLocked()
	if err != nil {
		return 0, err
	}
	if !found {
		port, err = m.randomListenPortLocked("loki", listenPortGRPC)
		if err != nil {
			return 0, err
		}
	}
	if err := atomicWrite(m.grpcPortPath(), []byte(strconv.Itoa(port)+"\n"), 0640); err != nil {
		return 0, fmt.Errorf("保存 Loki gRPC 端口：%w", err)
	}
	return port, nil
}

func (m *Manager) configuredGRPCPortLocked() (int, bool, error) {
	portPath := m.grpcPortPath()
	content, err := os.ReadFile(portPath)
	if err == nil {
		port, parseErr := strconv.Atoi(strings.TrimSpace(string(content)))
		if parseErr != nil || validateListenPort(port) != nil {
			return 0, false, fmt.Errorf("gRPC 端口文件无效：%s", portPath)
		}
		return port, true, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return 0, false, err
	}

	unit, unitErr := os.ReadFile(m.path("/etc/systemd/system/loki.service"))
	if unitErr == nil {
		if match := lokiUnitGRPCPortPattern.FindSubmatch(unit); len(match) == 2 {
			port, _ := strconv.Atoi(string(match[1]))
			if validateListenPort(port) == nil {
				return port, true, nil
			}
		}
	} else if !errors.Is(unitErr, os.ErrNotExist) {
		return 0, false, unitErr
	}

	config, configErr := os.ReadFile(m.path("/etc/loki/loki.yml"))
	if configErr == nil {
		if match := lokiConfigGRPCPortPattern.FindSubmatch(config); len(match) >= 3 {
			port, _ := strconv.Atoi(string(match[2]))
			if validateListenPort(port) == nil {
				return port, true, nil
			}
		}
	} else if !errors.Is(configErr, os.ErrNotExist) {
		return 0, false, configErr
	}
	return 0, false, nil
}

func applyLokiGRPCPort(config []byte, port int) []byte {
	updated := setYAMLSectionScalar(config, "server", "grpc_listen_port", strconv.Itoa(port))
	if _, ok := yamlSectionScalar(updated, "server", "grpc_listen_address"); !ok {
		updated = setYAMLSectionScalar(updated, "server", "grpc_listen_address", lokiGRPCAddress)
	}
	return updated
}

func (m *Manager) configuredListenPortLocked(name string) (int, bool, error) {
	portPath := m.listenPortPath(name)
	content, err := os.ReadFile(portPath)
	if err == nil {
		port, parseErr := strconv.Atoi(strings.TrimSpace(string(content)))
		if parseErr != nil || validateListenPort(port) != nil {
			return 0, false, fmt.Errorf("监听端口文件无效：%s", portPath)
		}
		return port, true, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return 0, false, err
	}

	unit, unitErr := os.ReadFile(m.path("/etc/systemd/system/" + name + ".service"))
	if unitErr == nil {
		pattern := lokiUnitPortPattern
		if name == "prometheus" {
			pattern = prometheusUnitPortPattern
		}
		if match := pattern.FindSubmatch(unit); len(match) == 2 {
			port, _ := strconv.Atoi(string(match[1]))
			if validateListenPort(port) == nil {
				return port, true, nil
			}
		}
	} else if !errors.Is(unitErr, os.ErrNotExist) {
		return 0, false, unitErr
	}

	if name == "loki" {
		config, configErr := os.ReadFile(m.path("/etc/loki/loki.yml"))
		if configErr == nil {
			if match := lokiConfigPortPattern.FindSubmatch(config); len(match) >= 3 {
				port, _ := strconv.Atoi(string(match[2]))
				if validateListenPort(port) == nil {
					return port, true, nil
				}
			}
		} else if !errors.Is(configErr, os.ErrNotExist) {
			return 0, false, configErr
		}
	} else if name == "prometheus" {
		config, configErr := os.ReadFile(m.path("/etc/prometheus/prometheus.yml"))
		if configErr == nil {
			if match := prometheusConfigPortPattern.FindSubmatch(config); len(match) == 2 {
				port, _ := strconv.Atoi(string(match[1]))
				if validateListenPort(port) == nil {
					return port, true, nil
				}
			}
		} else if !errors.Is(configErr, os.ErrNotExist) {
			return 0, false, configErr
		}
	}
	return 0, false, nil
}

func (m *Manager) randomListenPortLocked(name string, role listenPortRole) (int, error) {
	span := int64(randomPortEnd - randomPortStart + 1)
	for attempt := 0; attempt < 128; attempt++ {
		randomValue, err := rand.Int(rand.Reader, big.NewInt(span))
		if err != nil {
			return 0, fmt.Errorf("生成随机端口：%w", err)
		}
		port := randomPortStart + int(randomValue.Int64())
		if m.ensurePortAvailableLocked(name, role, port) == nil {
			return port, nil
		}
	}
	return 0, errors.New("无法生成可用随机端口，请检查本机端口占用")
}

func (m *Manager) ensurePortCanBeUsedLocked(name string, port int) error {
	return m.ensurePortAvailableLocked(name, listenPortHTTP, port)
}

func (m *Manager) ensurePortAvailableLocked(name string, role listenPortRole, port int) error {
	if err := validateListenPort(port); err != nil {
		return err
	}
	for _, other := range ComponentNames() {
		otherPort, found, err := m.configuredListenPortLocked(other)
		if err != nil {
			return err
		}
		if found && otherPort == port && !(other == name && role == listenPortHTTP) {
			return fmt.Errorf("端口 %d 已由 %s 使用", port, other)
		}
	}
	grpcPort, found, err := m.configuredGRPCPortLocked()
	if err != nil {
		return err
	}
	if found && grpcPort == port && !(name == "loki" && role == listenPortGRPC) {
		return fmt.Errorf("端口 %d 已由 Loki gRPC 使用", port)
	}
	// Staged installations use an isolated filesystem root and do not start a
	// service on this host, so checking the host network namespace is misleading.
	if !m.isLiveRoot() {
		return nil
	}
	listenAddress := "0.0.0.0"
	if name == "loki" && role == listenPortGRPC {
		listenAddress = lokiGRPCAddress
	}
	listener, err := net.Listen("tcp4", fmt.Sprintf("%s:%d", listenAddress, port))
	if err != nil {
		return fmt.Errorf("端口 %d 已被占用或不可用", port)
	}
	return listener.Close()
}

func validateListenPort(port int) error {
	if port < minimumListenPort || port > maximumListenPort {
		return fmt.Errorf("监听端口必须在 %d 到 %d 之间", minimumListenPort, maximumListenPort)
	}
	return nil
}

func updateManagedConfigPort(name string, config []byte, oldPort, newPort int) []byte {
	if name == "prometheus" {
		oldAddress := []byte(fmt.Sprintf("127.0.0.1:%d", oldPort))
		newAddress := []byte(fmt.Sprintf("127.0.0.1:%d", newPort))
		return []byte(strings.ReplaceAll(string(config), string(oldAddress), string(newAddress)))
	}
	return lokiConfigPortPattern.ReplaceAll(config, []byte(fmt.Sprintf("${1}%d${3}", newPort)))
}
