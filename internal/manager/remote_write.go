package manager

import (
	"context"
	"errors"
	"fmt"
	"os"
)

const remoteWriteMarkerName = "remote-write.enabled"

// SetRemoteWrite controls Prometheus's remote-write receiver independently
// from TLS configuration. The presentation layer is responsible for warning
// users before exposing the receiver over plain HTTP.
func (m *Manager) SetRemoteWrite(ctx context.Context, name string, enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	spec, configPath, err := m.configTargetLocked(name)
	if err != nil {
		return err
	}
	if spec.name != "prometheus" {
		return errors.New("远程写入接收开关仅适用于 Prometheus")
	}
	mtlsEnabled := mtlsEnabledLocked(m, spec.name)
	if enabled == remoteWriteEnabledLocked(m) {
		return nil
	}
	listenPort, err := m.ensureListenPortLocked(spec.name)
	if err != nil {
		return err
	}
	unitPath := m.path("/etc/systemd/system/prometheus.service")
	markerPath := m.remoteWriteMarkerPath()
	snapshots, err := snapshotFiles([]string{unitPath, markerPath})
	if err != nil {
		return err
	}
	rollback := func(cause error) error {
		restoreErr := restoreSnapshots(snapshots)
		if m.isLiveRoot() {
			_ = run(ctx, "systemctl", "daemon-reload")
			_ = run(ctx, "systemctl", "restart", "prometheus.service")
		}
		if restoreErr != nil {
			return fmt.Errorf("%v；恢复原远程写入状态失败：%w", cause, restoreErr)
		}
		return cause
	}

	unit, err := m.componentUnitLocked(spec, mtlsEnabled, enabled, listenPort)
	if err != nil {
		return rollback(err)
	}
	if err := atomicWrite(unitPath, []byte(unit), 0644); err != nil {
		return rollback(err)
	}
	if enabled {
		if err := atomicWrite(markerPath, []byte("enabled\n"), 0640); err != nil {
			return rollback(err)
		}
	} else if err := os.Remove(markerPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return rollback(err)
	}
	if !m.isLiveRoot() {
		return nil
	}
	if err := spec.validate(ctx, configPath); err != nil {
		return rollback(fmt.Errorf("Prometheus 配置校验失败：%w", err))
	}
	if mtlsEnabled {
		if err := m.validateMTLSConfigLocked(ctx, spec, configPath); err != nil {
			return rollback(err)
		}
	}
	if err := run(ctx, "systemctl", "daemon-reload"); err != nil {
		return rollback(err)
	}
	if err := run(ctx, "systemctl", "restart", "prometheus.service"); err != nil {
		return rollback(fmt.Errorf("应用远程写入状态失败：%w", err))
	}
	return nil
}

func (m *Manager) remoteWriteMarkerPath() string {
	return m.path("/etc/prometheus/" + remoteWriteMarkerName)
}

func remoteWriteEnabledLocked(m *Manager) bool {
	_, err := os.Stat(m.remoteWriteMarkerPath())
	return err == nil
}

func managedRemoteWriteEnabled(m *Manager, name string) bool {
	return name == "prometheus" && remoteWriteEnabledLocked(m)
}
