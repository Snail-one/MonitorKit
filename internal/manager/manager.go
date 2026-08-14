// Package manager implements the central server's component lifecycle.
// Component-specific metadata lives in spec.go; transport code must only use
// the exported Manager methods so new components do not leak into the API.
package manager

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Manager struct {
	root   string
	client *http.Client
	mu     sync.Mutex
}

type Status struct {
	Name         string `json:"name"`
	Installed    bool   `json:"installed"`
	Version      string `json:"version,omitempty"`
	ServiceState string `json:"service_state"`
}

func New(root string) (*Manager, error) {
	if root == "" {
		return nil, errors.New("安装根目录不能为空")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("解析安装根目录：%w", err)
	}
	abs = filepath.Clean(abs)
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("读取安装根目录：%w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("安装根目录不是目录：%s", abs)
	}
	return &Manager{
		root: abs,
		client: &http.Client{
			Timeout: 10 * time.Minute,
		},
	}, nil
}

func ComponentNames() []string {
	definitions := registeredSpecs()
	names := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		names = append(names, definition.name)
	}
	return names
}

func (m *Manager) Install(ctx context.Context, name, wantedVersion string) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	spec, err := lookup(name)
	if err != nil {
		return Status{}, err
	}
	if err := m.requireSystemAccess(); err != nil {
		return Status{}, err
	}
	arch, err := platformArch()
	if err != nil {
		return Status{}, err
	}
	version, asset, err := m.resolveRelease(ctx, spec, wantedVersion, arch)
	if err != nil {
		return Status{}, err
	}

	tempDir, err := os.MkdirTemp("", "snailmon-"+name+"-")
	if err != nil {
		return Status{}, fmt.Errorf("创建临时目录：%w", err)
	}
	defer os.RemoveAll(tempDir)
	archivePath := filepath.Join(tempDir, asset.Name)
	if err := m.download(ctx, asset, archivePath); err != nil {
		return Status{}, err
	}
	binaries, err := extractBinaries(archivePath, tempDir, spec.binaries)
	if err != nil {
		return Status{}, fmt.Errorf("解压 %s：%w", asset.Name, err)
	}

	configDir := m.path("/etc/" + name)
	dataDir := m.path("/var/lib/" + name)
	binDir := m.path("/usr/local/bin")
	unitDir := m.path("/etc/systemd/system")
	for _, dir := range []string{configDir, dataDir, binDir, unitDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return Status{}, fmt.Errorf("创建目录 %s：%w", dir, err)
		}
	}

	if m.isLiveRoot() {
		if err := ensureSystemUser(ctx, spec.user); err != nil {
			return Status{}, err
		}
	}
	for _, binary := range spec.binaries {
		if err := installFile(binaries[binary], filepath.Join(binDir, binary), 0755); err != nil {
			return Status{}, fmt.Errorf("安装 %s：%w", binary, err)
		}
	}
	configPath := filepath.Join(configDir, name+".yml")
	if _, err := os.Stat(configPath); errors.Is(err, os.ErrNotExist) {
		if err := atomicWrite(configPath, []byte(spec.config("/var/lib/"+name)), 0640); err != nil {
			return Status{}, fmt.Errorf("写入默认配置：%w", err)
		}
	} else if err != nil {
		return Status{}, fmt.Errorf("检查配置文件：%w", err)
	}
	unitPath := filepath.Join(unitDir, name+".service")
	if err := atomicWrite(unitPath, []byte(spec.unit()), 0644); err != nil {
		return Status{}, fmt.Errorf("写入 systemd 单元：%w", err)
	}

	if m.isLiveRoot() {
		if err := run(ctx, "chown", "-R", "root:"+spec.user, configDir); err != nil {
			return Status{}, err
		}
		if err := run(ctx, "chown", "-R", spec.user+":"+spec.user, dataDir); err != nil {
			return Status{}, err
		}
		if spec.validate != nil {
			if err := spec.validate(ctx, configPath); err != nil {
				return Status{}, fmt.Errorf("%s 配置校验失败：%w", name, err)
			}
		}
		if err := run(ctx, "systemctl", "daemon-reload"); err != nil {
			return Status{}, err
		}
		if err := run(ctx, "systemctl", "enable", "--now", name+".service"); err != nil {
			return Status{}, err
		}
	}
	return m.statusLocked(ctx, name, version)
}

func (m *Manager) Uninstall(ctx context.Context, name string, purge bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	spec, err := lookup(name)
	if err != nil {
		return err
	}
	if err := m.requireSystemAccess(); err != nil {
		return err
	}
	if m.isLiveRoot() {
		_ = run(ctx, "systemctl", "disable", "--now", name+".service")
	}
	for _, binary := range spec.binaries {
		if err := removeFile(m.path("/usr/local/bin/" + binary)); err != nil {
			return err
		}
	}
	if err := removeFile(m.path("/etc/systemd/system/" + name + ".service")); err != nil {
		return err
	}
	if purge {
		for _, dir := range []string{m.path("/etc/" + name), m.path("/var/lib/" + name)} {
			if err := os.RemoveAll(dir); err != nil {
				return fmt.Errorf("删除 %s：%w", dir, err)
			}
		}
	}
	if m.isLiveRoot() {
		return run(ctx, "systemctl", "daemon-reload")
	}
	return nil
}

func (m *Manager) Status(ctx context.Context, name string) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.statusLocked(ctx, name, "")
}

func (m *Manager) statusLocked(ctx context.Context, name, knownVersion string) (Status, error) {
	spec, err := lookup(name)
	if err != nil {
		return Status{}, err
	}
	binaryPath := m.path("/usr/local/bin/" + spec.binaries[0])
	status := Status{Name: name, ServiceState: "not-installed"}
	if _, err := os.Stat(binaryPath); errors.Is(err, os.ErrNotExist) {
		return status, nil
	} else if err != nil {
		return Status{}, fmt.Errorf("检查 %s：%w", binaryPath, err)
	}
	status.Installed = true
	status.Version = knownVersion
	if status.Version == "" {
		status.Version = binaryVersion(ctx, binaryPath)
	}
	if !m.isLiveRoot() {
		status.ServiceState = "staged"
		return status, nil
	}
	cmd := exec.CommandContext(ctx, "systemctl", "is-active", name+".service")
	output, err := cmd.Output()
	status.ServiceState = strings.TrimSpace(string(output))
	if status.ServiceState == "" {
		status.ServiceState = "inactive"
	}
	if err != nil && status.ServiceState == "active" {
		status.ServiceState = "unknown"
	}
	return status, nil
}

func lookup(name string) (componentSpec, error) {
	for _, spec := range registeredSpecs() {
		if spec.name == strings.ToLower(name) {
			return spec, nil
		}
	}
	return componentSpec{}, fmt.Errorf("不支持的组件 %q，可用组件：%s", name, strings.Join(ComponentNames(), ", "))
}

func (m *Manager) path(absolute string) string {
	return filepath.Join(m.root, strings.TrimPrefix(filepath.Clean(absolute), string(filepath.Separator)))
}

func (m *Manager) isLiveRoot() bool { return m.root == string(filepath.Separator) }

func (m *Manager) requireSystemAccess() error {
	if m.isLiveRoot() && os.Geteuid() != 0 {
		return errors.New("安装或卸载系统服务需要 root 权限")
	}
	return nil
}

func ensureSystemUser(ctx context.Context, name string) error {
	if exec.CommandContext(ctx, "getent", "group", name).Run() != nil {
		if err := run(ctx, "groupadd", "--system", name); err != nil {
			return err
		}
	}
	if exec.CommandContext(ctx, "id", "-u", name).Run() == nil {
		return nil
	}
	return run(ctx, "useradd", "--system", "--gid", name, "--no-create-home", "--shell", "/usr/sbin/nologin", name)
}

func run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("执行 %s 失败：%s", name, message)
	}
	return nil
}

func binaryVersion(ctx context.Context, path string) string {
	cmd := exec.CommandContext(ctx, path, "--version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "unknown"
	}
	line := strings.SplitN(strings.TrimSpace(string(output)), "\n", 2)[0]
	if len(line) > 160 {
		line = line[:160]
	}
	return line
}

func installFile(source, destination string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	temp, err := os.CreateTemp(filepath.Dir(destination), ".snailmon-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if _, err := io.Copy(temp, input); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, destination)
}

func atomicWrite(destination string, content []byte, mode os.FileMode) error {
	temp, err := os.CreateTemp(filepath.Dir(destination), ".snailmon-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if _, err := temp.Write(content); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, destination)
}

func removeFile(path string) error {
	err := os.Remove(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("删除 %s：%w", path, err)
	}
	return nil
}
