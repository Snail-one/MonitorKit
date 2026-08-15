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

var (
	prometheusUnitPortPattern   = regexp.MustCompile(`--web\.listen-address=[^\s:]+:(\d+)`)
	prometheusConfigPortPattern = regexp.MustCompile(`127\.0\.0\.1:(\d+)`)
	lokiUnitPortPattern         = regexp.MustCompile(`-server\.http-listen-port=(\d+)`)
	lokiConfigPortPattern       = regexp.MustCompile(`(?m)^(\s*http_listen_port:\s*)(\d+)(\s*(?:#.*)?)$`)
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
	if err := m.ensurePortCanBeUsedLocked(spec.name, port); err != nil {
		return err
	}

	unitPath := m.path("/etc/systemd/system/" + spec.name + ".service")
	portPath := m.listenPortPath(spec.name)
	snapshots, err := snapshotFiles([]string{configPath, unitPath, portPath})
	if err != nil {
		return err
	}
	rollback := func(cause error) error {
		restoreErr := restoreSnapshots(snapshots)
		if m.isLiveRoot() {
			_ = m.fixConfigOwnershipLocked(ctx, spec, configPath)
			_ = run(ctx, "systemctl", "daemon-reload")
			_ = run(ctx, "systemctl", "restart", spec.name+".service")
		}
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
	if err := atomicWrite(unitPath, []byte(m.componentUnitLocked(spec, mtlsEnabled, managedRemoteWriteEnabled(m, spec.name), port)), 0644); err != nil {
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
	if err := run(ctx, "systemctl", "daemon-reload"); err != nil {
		return rollback(err)
	}
	if err := run(ctx, "systemctl", "restart", spec.name+".service"); err != nil {
		return rollback(fmt.Errorf("新端口启动失败，已恢复原端口：%w", err))
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
	return m.randomListenPortLocked(name)
}

func (m *Manager) listenPortPath(name string) string {
	return m.path("/etc/" + name + "/listen.port")
}

func (m *Manager) ensureListenPortLocked(name string) (int, error) {
	port, found, err := m.configuredListenPortLocked(name)
	if err != nil {
		return 0, err
	}
	if !found {
		port, err = m.randomListenPortLocked(name)
		if err != nil {
			return 0, err
		}
	}
	if err := atomicWrite(m.listenPortPath(name), []byte(strconv.Itoa(port)+"\n"), 0640); err != nil {
		return 0, fmt.Errorf("保存 %s 监听端口：%w", name, err)
	}
	return port, nil
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

func (m *Manager) randomListenPortLocked(name string) (int, error) {
	span := int64(randomPortEnd - randomPortStart + 1)
	for attempt := 0; attempt < 128; attempt++ {
		randomValue, err := rand.Int(rand.Reader, big.NewInt(span))
		if err != nil {
			return 0, fmt.Errorf("生成随机端口：%w", err)
		}
		port := randomPortStart + int(randomValue.Int64())
		if m.ensurePortCanBeUsedLocked(name, port) == nil {
			return port, nil
		}
	}
	return 0, errors.New("无法生成可用随机端口，请检查本机端口占用")
}

func (m *Manager) ensurePortCanBeUsedLocked(name string, port int) error {
	if err := validateListenPort(port); err != nil {
		return err
	}
	for _, other := range ComponentNames() {
		if other == name {
			continue
		}
		otherPort, found, err := m.configuredListenPortLocked(other)
		if err != nil {
			return err
		}
		if found && otherPort == port {
			return fmt.Errorf("端口 %d 已由 %s 使用", port, other)
		}
	}
	// Staged installations use an isolated filesystem root and do not start a
	// service on this host, so checking the host network namespace is misleading.
	if !m.isLiveRoot() {
		return nil
	}
	listener, err := net.Listen("tcp4", fmt.Sprintf("0.0.0.0:%d", port))
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
