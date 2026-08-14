package manager

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const mtlsMarkerName = "mtls.enabled"

type Configuration struct {
	Name        string
	Path        string
	TLSDir      string
	MTLSEnabled bool
	ListenPort  int
}

type TLSFile struct {
	Label       string
	Path        string
	Description string
}

type EditFunc func(path string) error
type TLSEditFunc func(TLSFile) error

type fileSnapshot struct {
	path    string
	exists  bool
	content []byte
	mode    os.FileMode
}

func (m *Manager) Configuration(name string) (Configuration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	spec, err := lookup(name)
	if err != nil {
		return Configuration{}, err
	}
	name = spec.name
	listenPort, _, err := m.configuredListenPortLocked(name)
	if err != nil {
		return Configuration{}, err
	}
	return Configuration{
		Name:        name,
		Path:        m.path("/etc/" + name + "/" + name + ".yml"),
		TLSDir:      m.path("/etc/" + name + "/tls"),
		MTLSEnabled: mtlsEnabledLocked(m, name),
		ListenPort:  listenPort,
	}, nil
}

func (m *Manager) EditConfig(ctx context.Context, name string, edit EditFunc) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if edit == nil {
		return errors.New("配置编辑器不能为空")
	}
	spec, configPath, err := m.configTargetLocked(name)
	if err != nil {
		return err
	}
	original, err := snapshotFile(configPath)
	if err != nil {
		return err
	}
	if err := edit(configPath); err != nil {
		_ = restoreSnapshot(original)
		return fmt.Errorf("编辑器退出异常，原配置已恢复：%w", err)
	}
	if err := os.Chmod(configPath, original.mode.Perm()); err != nil {
		_ = restoreSnapshot(original)
		return fmt.Errorf("恢复配置文件权限：%w", err)
	}
	if err := m.fixConfigOwnershipLocked(ctx, spec, configPath); err != nil {
		_ = restoreSnapshot(original)
		return err
	}
	if err := m.validateAndApplyLocked(ctx, spec, configPath); err != nil {
		if restoreErr := restoreSnapshot(original); restoreErr != nil {
			return fmt.Errorf("新配置无效（%v），且恢复原配置失败：%w", err, restoreErr)
		}
		_ = m.fixConfigOwnershipLocked(ctx, spec, configPath)
		if m.isLiveRoot() {
			_ = m.validateAndApplyLocked(ctx, spec, configPath)
		}
		return fmt.Errorf("新配置未通过校验或应用，已即时清理并恢复原配置：%w", err)
	}
	return nil
}

func (m *Manager) ValidateConfig(ctx context.Context, name string) error {
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
		return fmt.Errorf("%s 配置校验失败：%w", name, err)
	}
	if mtlsEnabledLocked(m, name) {
		return m.validateMTLSConfigLocked(ctx, spec, configPath)
	}
	return nil
}

func (m *Manager) Restart(ctx context.Context, name string) error {
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
		return fmt.Errorf("%s 配置校验失败：%w", name, err)
	}
	if mtlsEnabledLocked(m, name) {
		if err := m.validateMTLSConfigLocked(ctx, spec, configPath); err != nil {
			return err
		}
	}
	return run(ctx, "systemctl", "restart", name+".service")
}

func (m *Manager) ConfigureMTLS(ctx context.Context, name string, edit TLSEditFunc) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if edit == nil {
		return errors.New("证书编辑器不能为空")
	}
	if _, err := exec.LookPath("openssl"); err != nil {
		return errors.New("配置 mTLS 需要 openssl，请先安装后重试")
	}
	spec, configPath, err := m.configTargetLocked(name)
	if err != nil {
		return err
	}
	files := m.tlsFilesLocked(name)
	markerPath := m.mtlsMarkerPath(name)
	unitPath := m.path("/etc/systemd/system/" + name + ".service")
	listenPort, err := m.ensureListenPortLocked(name)
	if err != nil {
		return err
	}
	managedPaths := []string{unitPath, markerPath}
	for _, file := range files {
		managedPaths = append(managedPaths, file.Path)
	}
	if name == "prometheus" {
		managedPaths = append(managedPaths, m.path("/etc/prometheus/web.yml"))
	}
	snapshots, err := snapshotFiles(managedPaths)
	if err != nil {
		return err
	}
	rollback := func(cause error, restart bool) error {
		restoreErr := restoreSnapshots(snapshots)
		if m.isLiveRoot() {
			if ownershipErr := m.fixTLSOwnershipLocked(ctx, spec, files); ownershipErr != nil {
				restoreErr = errors.Join(restoreErr, ownershipErr)
			}
			if name == "prometheus" {
				webConfigPath := m.path("/etc/prometheus/web.yml")
				if _, statErr := os.Stat(webConfigPath); statErr == nil {
					if ownershipErr := m.fixConfigOwnershipLocked(ctx, spec, webConfigPath); ownershipErr != nil {
						restoreErr = errors.Join(restoreErr, ownershipErr)
					}
				}
			}
		}
		if m.isLiveRoot() && restart {
			_ = run(ctx, "systemctl", "daemon-reload")
			_ = run(ctx, "systemctl", "restart", name+".service")
		}
		if restoreErr != nil {
			return fmt.Errorf("%v；恢复原 mTLS 配置失败：%w", cause, restoreErr)
		}
		return cause
	}

	if err := os.MkdirAll(filepath.Dir(markerPath), 0750); err != nil {
		return err
	}
	if err := os.Chmod(filepath.Dir(markerPath), 0750); err != nil {
		return err
	}
	for _, file := range files {
		if err := ensurePrivateFile(file.Path); err != nil {
			return rollback(err, false)
		}
		if err := edit(file); err != nil {
			return rollback(fmt.Errorf("编辑%s失败：%w", file.Label, err), false)
		}
		if err := os.Chmod(file.Path, 0640); err != nil {
			return rollback(err, false)
		}
	}
	if err := m.fixTLSOwnershipLocked(ctx, spec, files); err != nil {
		return rollback(err, false)
	}
	if err := validateTLSMaterial(ctx, files); err != nil {
		return rollback(err, false)
	}
	if name == "prometheus" {
		if err := atomicWrite(m.path("/etc/prometheus/web.yml"), []byte(prometheusWebConfig()), 0640); err != nil {
			return rollback(err, false)
		}
		if err := m.fixConfigOwnershipLocked(ctx, spec, m.path("/etc/prometheus/web.yml")); err != nil {
			return rollback(err, false)
		}
	}
	if m.isLiveRoot() {
		if err := spec.validate(ctx, configPath); err != nil {
			return rollback(fmt.Errorf("%s 主配置校验失败：%w", name, err), false)
		}
		if err := m.validateMTLSConfigLocked(ctx, spec, configPath); err != nil {
			return rollback(err, false)
		}
	}
	if err := atomicWrite(unitPath, []byte(spec.unit(true, listenPort)), 0644); err != nil {
		return rollback(err, false)
	}
	if err := atomicWrite(markerPath, []byte("enabled\n"), 0640); err != nil {
		return rollback(err, false)
	}
	if m.isLiveRoot() {
		if err := run(ctx, "systemctl", "daemon-reload"); err != nil {
			return rollback(err, true)
		}
		if err := run(ctx, "systemctl", "restart", name+".service"); err != nil {
			return rollback(fmt.Errorf("启用 mTLS 后服务启动失败：%w", err), true)
		}
	}
	return nil
}

func (m *Manager) DisableMTLS(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	spec, _, err := m.configTargetLocked(name)
	if err != nil {
		return err
	}
	unitPath := m.path("/etc/systemd/system/" + name + ".service")
	markerPath := m.mtlsMarkerPath(name)
	listenPort, err := m.ensureListenPortLocked(name)
	if err != nil {
		return err
	}
	snapshots, err := snapshotFiles([]string{unitPath, markerPath})
	if err != nil {
		return err
	}
	if err := atomicWrite(unitPath, []byte(spec.unit(false, listenPort)), 0644); err != nil {
		return err
	}
	if err := os.Remove(markerPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		_ = restoreSnapshots(snapshots)
		return err
	}
	if m.isLiveRoot() {
		if err := run(ctx, "systemctl", "daemon-reload"); err != nil {
			_ = restoreSnapshots(snapshots)
			return err
		}
		if err := run(ctx, "systemctl", "restart", name+".service"); err != nil {
			_ = restoreSnapshots(snapshots)
			_ = run(ctx, "systemctl", "daemon-reload")
			_ = run(ctx, "systemctl", "restart", name+".service")
			return fmt.Errorf("关闭 mTLS 后服务启动失败，已恢复原配置：%w", err)
		}
	}
	return nil
}

func (m *Manager) configTargetLocked(name string) (componentSpec, string, error) {
	spec, err := lookup(name)
	if err != nil {
		return componentSpec{}, "", err
	}
	if err := m.requireSystemAccess(); err != nil {
		return componentSpec{}, "", err
	}
	configPath := m.path("/etc/" + name + "/" + name + ".yml")
	if _, err := os.Stat(configPath); errors.Is(err, os.ErrNotExist) {
		return componentSpec{}, "", fmt.Errorf("%s 配置文件不存在，请先安装组件", name)
	} else if err != nil {
		return componentSpec{}, "", err
	}
	if err := removeRejectedConfigs(configPath); err != nil {
		return componentSpec{}, "", fmt.Errorf("清理历史无效配置：%w", err)
	}
	return spec, configPath, nil
}

func (m *Manager) validateAndApplyLocked(ctx context.Context, spec componentSpec, configPath string) error {
	if !m.isLiveRoot() {
		return nil
	}
	if err := spec.validate(ctx, configPath); err != nil {
		return err
	}
	if mtlsEnabledLocked(m, spec.name) {
		if err := m.validateMTLSConfigLocked(ctx, spec, configPath); err != nil {
			return err
		}
	}
	action := "restart"
	if spec.name == "prometheus" {
		action = "reload"
	}
	if err := run(ctx, "systemctl", action, spec.name+".service"); err != nil && action == "reload" {
		return run(ctx, "systemctl", "restart", spec.name+".service")
	} else {
		return err
	}
}

func (m *Manager) validateMTLSConfigLocked(ctx context.Context, spec componentSpec, configPath string) error {
	if spec.name == "prometheus" {
		return run(ctx, "/usr/local/bin/promtool", "check", "web-config", "/etc/prometheus/web.yml")
	}
	args := []string{"-config.file=" + configPath, "-verify-config=true"}
	args = append(args, lokiTLSArguments()...)
	return run(ctx, "/usr/local/bin/loki", args...)
}

func (m *Manager) fixConfigOwnershipLocked(ctx context.Context, spec componentSpec, path string) error {
	if !m.isLiveRoot() {
		return nil
	}
	return run(ctx, "chown", "root:"+spec.user, path)
}

func (m *Manager) fixTLSOwnershipLocked(ctx context.Context, spec componentSpec, files []TLSFile) error {
	if !m.isLiveRoot() {
		return nil
	}
	owner := "root:" + spec.user
	if err := run(ctx, "chown", owner, filepath.Dir(files[0].Path)); err != nil {
		return err
	}
	for _, file := range files {
		if _, err := os.Stat(file.Path); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		if err := run(ctx, "chown", owner, file.Path); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) tlsFilesLocked(name string) []TLSFile {
	tlsDir := m.path("/etc/" + name + "/tls")
	return []TLSFile{
		{Label: "服务端证书", Path: filepath.Join(tlsDir, "server.crt"), Description: "完整 PEM 服务端证书或证书链；SAN 必须包含探针访问中心端时使用的域名或 IP"},
		{Label: "服务端私钥", Path: filepath.Join(tlsDir, "server.key"), Description: "与 server.crt 匹配的完整、未加密 PEM 私钥；不要填写 CA 私钥"},
		{Label: "客户端根 CA", Path: filepath.Join(tlsDir, "client-ca.crt"), Description: "签发 Alloy 客户端证书的 CA 公共证书；不要填写客户端证书、客户端私钥或 CA 私钥"},
	}
}

func (m *Manager) mtlsMarkerPath(name string) string {
	return m.path("/etc/" + name + "/tls/" + mtlsMarkerName)
}

func mtlsEnabledLocked(m *Manager, name string) bool {
	_, err := os.Stat(m.mtlsMarkerPath(name))
	return err == nil
}

func prometheusWebConfig() string {
	return `tls_server_config:
  cert_file: /etc/prometheus/tls/server.crt
  key_file: /etc/prometheus/tls/server.key
  client_auth_type: RequireAndVerifyClientCert
  client_ca_file: /etc/prometheus/tls/client-ca.crt
  min_version: TLS12
`
}

func lokiTLSArguments() []string {
	return []string{
		"-server.http-tls-cert-path=/etc/loki/tls/server.crt",
		"-server.http-tls-key-path=/etc/loki/tls/server.key",
		"-server.http-tls-client-auth=RequireAndVerifyClientCert",
		"-server.http-tls-ca-path=/etc/loki/tls/client-ca.crt",
	}
}

func validateTLSMaterial(ctx context.Context, files []TLSFile) error {
	for _, file := range files {
		info, err := os.Stat(file.Path)
		if err != nil {
			return fmt.Errorf("读取%s：%w", file.Label, err)
		}
		if info.Size() == 0 {
			return fmt.Errorf("%s为空：%s", file.Label, file.Path)
		}
	}
	if err := run(ctx, "openssl", "x509", "-in", files[0].Path, "-noout"); err != nil {
		return fmt.Errorf("服务端证书格式无效：%w", err)
	}
	if err := run(ctx, "openssl", "pkey", "-in", files[1].Path, "-noout", "-passin", "pass:"); err != nil {
		return fmt.Errorf("服务端私钥无效或已加密：%w", err)
	}
	if err := run(ctx, "openssl", "x509", "-in", files[2].Path, "-noout"); err != nil {
		return fmt.Errorf("客户端根 CA 格式无效：%w", err)
	}
	certPublicKey, err := commandOutput(ctx, "openssl", "x509", "-in", files[0].Path, "-pubkey", "-noout")
	if err != nil {
		return err
	}
	privatePublicKey, err := commandOutput(ctx, "openssl", "pkey", "-in", files[1].Path, "-pubout", "-passin", "pass:")
	if err != nil {
		return err
	}
	if !bytes.Equal(bytes.TrimSpace(certPublicKey), bytes.TrimSpace(privatePublicKey)) {
		return errors.New("服务端证书与私钥不匹配")
	}
	return nil
}

func commandOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("执行 %s 失败：%s", name, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func ensurePrivateFile(path string) error {
	file, err := os.OpenFile(path, os.O_CREATE, 0640)
	if err != nil {
		return err
	}
	return file.Close()
}

func snapshotFile(path string) (fileSnapshot, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return fileSnapshot{path: path}, nil
	}
	if err != nil {
		return fileSnapshot{}, err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return fileSnapshot{}, err
	}
	return fileSnapshot{path: path, exists: true, content: content, mode: info.Mode().Perm()}, nil
}

func snapshotFiles(paths []string) ([]fileSnapshot, error) {
	snapshots := make([]fileSnapshot, 0, len(paths))
	for _, path := range paths {
		snapshot, err := snapshotFile(path)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snapshot)
	}
	return snapshots, nil
}

func restoreSnapshot(snapshot fileSnapshot) error {
	if !snapshot.exists {
		if err := os.Remove(snapshot.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	return atomicWrite(snapshot.path, snapshot.content, snapshot.mode)
}

func restoreSnapshots(snapshots []fileSnapshot) error {
	var restoredErr error
	for _, snapshot := range snapshots {
		if err := restoreSnapshot(snapshot); err != nil {
			restoredErr = errors.Join(restoredErr, err)
		}
	}
	return restoredErr
}

func removeRejectedConfigs(configPath string) error {
	directory := filepath.Dir(configPath)
	prefix := filepath.Base(configPath) + ".rejected-"
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), prefix) || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if err := os.Remove(filepath.Join(directory, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}
