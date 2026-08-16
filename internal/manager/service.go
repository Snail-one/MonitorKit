package manager

import (
	"context"
	"fmt"
	"os/exec"
)

// Start validates the component configuration, enables the unit for boot,
// and starts it immediately (systemctl enable --now).
func (m *Manager) Start(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	spec, configPath, err := m.configTargetLocked(name)
	if err != nil {
		return err
	}
	if !m.isLiveRoot() {
		return nil
	}
	if err := spec.validate(ctx, configPath); err != nil {
		return fmt.Errorf("%s 配置校验失败：%w", spec.name, err)
	}
	if mtlsEnabledLocked(m, spec.name) {
		if err := m.validateMTLSConfigLocked(ctx, spec, configPath); err != nil {
			return err
		}
	}
	if err := run(ctx, "systemctl", "daemon-reload"); err != nil {
		return err
	}
	return run(ctx, "systemctl", "enable", "--now", spec.name+".service")
}

// Stop disables boot start and stops the unit immediately (systemctl disable --now).
func (m *Manager) Stop(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	spec, err := lookup(name)
	if err != nil {
		return err
	}
	if err := m.requireSystemAccess(); err != nil {
		return err
	}
	if !m.isLiveRoot() {
		return nil
	}
	return run(ctx, "systemctl", "disable", "--now", spec.name+".service")
}

func (m *Manager) serviceActiveLocked(ctx context.Context, name string) bool {
	if !m.isLiveRoot() {
		return false
	}
	return exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", name+".service").Run() == nil
}

func (m *Manager) serviceBootEnabledLocked(ctx context.Context, name string) bool {
	if !m.isLiveRoot() {
		return false
	}
	return exec.CommandContext(ctx, "systemctl", "is-enabled", "--quiet", name+".service").Run() == nil
}

// applyRunningServiceLocked reloads systemd when the unit file changed.
// A stopped service stays stopped; only an already-active service is
// reloaded or restarted so configuration changes do not auto-start it.
func (m *Manager) applyRunningServiceLocked(ctx context.Context, name string, wasActive, unitChanged bool) error {
	if !m.isLiveRoot() {
		return nil
	}
	if unitChanged {
		if err := run(ctx, "systemctl", "daemon-reload"); err != nil {
			return err
		}
	}
	if !wasActive {
		return nil
	}
	action := "restart"
	if !unitChanged && name == "prometheus" {
		action = "reload"
	}
	if err := run(ctx, "systemctl", action, name+".service"); err != nil && action == "reload" {
		return run(ctx, "systemctl", "restart", name+".service")
	} else {
		return err
	}
}

func (m *Manager) restoreRunningServiceLocked(ctx context.Context, name string, wasActive bool) {
	if !m.isLiveRoot() {
		return
	}
	_ = run(ctx, "systemctl", "daemon-reload")
	if wasActive {
		_ = run(ctx, "systemctl", "restart", name+".service")
	}
}
