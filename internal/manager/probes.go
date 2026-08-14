package manager

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	probeInventoryVersion = 1
	probeBlockBegin       = "# BEGIN MONITORKIT MANAGED PROBES"
	probeBlockEnd         = "# END MONITORKIT MANAGED PROBES"
)

var (
	probeHostnamePattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?$`)
	probeIDPattern       = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,72}$`)
)

type Probe struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Enabled    bool   `json:"enabled"`
	MTLS       bool   `json:"mtls"`
	ServerName string `json:"server_name,omitempty"`
	CreatedAt  string `json:"created_at"`
}

type ProbeTLSFile struct {
	Label       string
	Path        string
	Description string
}

type ProbeTLSEditFunc func(ProbeTLSFile) error

type probeInventory struct {
	Version int     `json:"version"`
	Probes  []Probe `json:"probes"`
}

func (m *Manager) ListProbes() ([]Probe, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	inventory, err := m.readProbeInventoryLocked()
	if err != nil {
		return nil, err
	}
	return append([]Probe(nil), inventory.Probes...), nil
}

func (m *Manager) AddNodeExporterProbe(ctx context.Context, probe Probe, editTLS ProbeTLSEditFunc) (Probe, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	spec, configPath, err := m.configTargetLocked("prometheus")
	if err != nil {
		return Probe{}, fmt.Errorf("添加 node_exporter 接入前必须先安装 Prometheus：%w", err)
	}
	probe.Type = "node_exporter"
	probe.Name = strings.TrimSpace(probe.Name)
	probe.Host = normalizeProbeHost(probe.Host)
	probe.ServerName = strings.TrimSpace(probe.ServerName)
	probe.Enabled = true
	if err := validateProbe(probe); err != nil {
		return Probe{}, err
	}
	if probe.MTLS && editTLS == nil {
		return Probe{}, errors.New("mTLS 探针需要配置 Prometheus 客户端证书")
	}
	inventory, err := m.readProbeInventoryLocked()
	if err != nil {
		return Probe{}, err
	}
	for _, current := range inventory.Probes {
		if strings.EqualFold(current.Name, probe.Name) {
			return Probe{}, fmt.Errorf("探针名称已存在：%s", probe.Name)
		}
		if current.Host == probe.Host && current.Port == probe.Port {
			return Probe{}, fmt.Errorf("探针目标已存在：%s", probeTarget(current))
		}
	}
	probe.ID, err = newProbeID(probe.Name)
	if err != nil {
		return Probe{}, err
	}
	probe.CreatedAt = time.Now().UTC().Format(time.RFC3339)

	paths := []string{configPath, m.probeInventoryPath()}
	if probe.MTLS {
		for _, file := range m.probeTLSFilesLocked(probe.ID) {
			paths = append(paths, file.Path)
		}
	}
	snapshots, err := snapshotFiles(paths)
	if err != nil {
		return Probe{}, err
	}
	rollback := func(cause error) (Probe, error) {
		restoreErr := restoreSnapshots(snapshots)
		_ = os.Remove(m.probeTLSDir(probe.ID))
		if m.isLiveRoot() {
			_ = m.fixConfigOwnershipLocked(ctx, spec, configPath)
			_ = run(ctx, "systemctl", "reload", "prometheus.service")
		}
		if restoreErr != nil {
			return Probe{}, fmt.Errorf("%v；恢复原探针配置失败：%w", cause, restoreErr)
		}
		return Probe{}, cause
	}

	if probe.MTLS {
		files := m.probeTLSFilesLocked(probe.ID)
		if err := os.MkdirAll(m.probeTLSDir(probe.ID), 0750); err != nil {
			return rollback(err)
		}
		for _, file := range files {
			if err := ensurePrivateFile(file.Path); err != nil {
				return rollback(err)
			}
			if err := editTLS(file); err != nil {
				return rollback(fmt.Errorf("编辑%s失败：%w", file.Label, err))
			}
			if err := os.Chmod(file.Path, 0640); err != nil {
				return rollback(err)
			}
		}
		if err := validateProbeTLSMaterial(ctx, files); err != nil {
			return rollback(err)
		}
	}

	inventory.Probes = append(inventory.Probes, probe)
	if err := m.applyProbeInventoryLocked(ctx, spec, configPath, inventory); err != nil {
		return rollback(err)
	}
	return probe, nil
}

func (m *Manager) UpdateProbe(ctx context.Context, id, name, host string, port int, serverName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	spec, configPath, err := m.configTargetLocked("prometheus")
	if err != nil {
		return err
	}
	inventory, err := m.readProbeInventoryLocked()
	if err != nil {
		return err
	}
	index := probeIndex(inventory.Probes, id)
	if index < 0 {
		return fmt.Errorf("未找到探针：%s", id)
	}
	updated := inventory.Probes[index]
	updated.Name = strings.TrimSpace(name)
	updated.Host = normalizeProbeHost(host)
	updated.Port = port
	updated.ServerName = strings.TrimSpace(serverName)
	if err := validateProbe(updated); err != nil {
		return err
	}
	for i, current := range inventory.Probes {
		if i == index {
			continue
		}
		if strings.EqualFold(current.Name, updated.Name) {
			return fmt.Errorf("探针名称已存在：%s", updated.Name)
		}
		if current.Host == updated.Host && current.Port == updated.Port {
			return fmt.Errorf("探针目标已存在：%s", probeTarget(current))
		}
	}
	inventory.Probes[index] = updated
	return m.applyProbeInventoryWithRollbackLocked(ctx, spec, configPath, inventory)
}

func (m *Manager) SetProbeEnabled(ctx context.Context, id string, enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	spec, configPath, err := m.configTargetLocked("prometheus")
	if err != nil {
		return err
	}
	inventory, err := m.readProbeInventoryLocked()
	if err != nil {
		return err
	}
	index := probeIndex(inventory.Probes, id)
	if index < 0 {
		return fmt.Errorf("未找到探针：%s", id)
	}
	inventory.Probes[index].Enabled = enabled
	return m.applyProbeInventoryWithRollbackLocked(ctx, spec, configPath, inventory)
}

func (m *Manager) DeleteProbe(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	spec, configPath, err := m.configTargetLocked("prometheus")
	if err != nil {
		return err
	}
	inventory, err := m.readProbeInventoryLocked()
	if err != nil {
		return err
	}
	index := probeIndex(inventory.Probes, id)
	if index < 0 {
		return fmt.Errorf("未找到探针：%s", id)
	}
	probe := inventory.Probes[index]
	inventory.Probes = append(inventory.Probes[:index], inventory.Probes[index+1:]...)
	if err := m.applyProbeInventoryWithRollbackLocked(ctx, spec, configPath, inventory); err != nil {
		return err
	}
	if err := os.RemoveAll(m.probeTLSDir(probe.ID)); err != nil {
		return fmt.Errorf("探针已从 Prometheus 移除，但删除证书目录失败：%w", err)
	}
	return nil
}

func (m *Manager) ConfigureProbeTLS(ctx context.Context, id string, edit ProbeTLSEditFunc) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if edit == nil {
		return errors.New("证书编辑器不能为空")
	}
	spec, configPath, err := m.configTargetLocked("prometheus")
	if err != nil {
		return err
	}
	inventory, err := m.readProbeInventoryLocked()
	if err != nil {
		return err
	}
	index := probeIndex(inventory.Probes, id)
	if index < 0 {
		return fmt.Errorf("未找到探针：%s", id)
	}
	if !inventory.Probes[index].MTLS {
		return errors.New("该探针使用 HTTP，没有 mTLS 证书配置")
	}
	files := m.probeTLSFilesLocked(id)
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.Path)
	}
	snapshots, err := snapshotFiles(paths)
	if err != nil {
		return err
	}
	rollback := func(cause error) error {
		if restoreErr := restoreSnapshots(snapshots); restoreErr != nil {
			return fmt.Errorf("%v；恢复原探针证书失败：%w", cause, restoreErr)
		}
		return cause
	}
	for _, file := range files {
		if err := edit(file); err != nil {
			return rollback(err)
		}
		if err := os.Chmod(file.Path, 0640); err != nil {
			return rollback(err)
		}
	}
	if err := validateProbeTLSMaterial(ctx, files); err != nil {
		return rollback(err)
	}
	if m.isLiveRoot() {
		if err := run(ctx, "chown", "-R", "root:prometheus", m.path("/etc/prometheus/probes")); err != nil {
			return rollback(err)
		}
		if err := spec.validate(ctx, configPath); err != nil {
			return rollback(err)
		}
		if err := run(ctx, "systemctl", "reload", "prometheus.service"); err != nil {
			return rollback(err)
		}
	}
	return nil
}

func (m *Manager) applyProbeInventoryWithRollbackLocked(ctx context.Context, spec componentSpec, configPath string, inventory probeInventory) error {
	snapshots, err := snapshotFiles([]string{configPath, m.probeInventoryPath()})
	if err != nil {
		return err
	}
	if err := m.applyProbeInventoryLocked(ctx, spec, configPath, inventory); err != nil {
		if restoreErr := restoreSnapshots(snapshots); restoreErr != nil {
			return fmt.Errorf("%v；恢复原探针配置失败：%w", err, restoreErr)
		}
		if m.isLiveRoot() {
			_ = m.fixConfigOwnershipLocked(ctx, spec, configPath)
			_ = run(ctx, "systemctl", "reload", "prometheus.service")
		}
		return err
	}
	return nil
}

func (m *Manager) applyProbeInventoryLocked(ctx context.Context, spec componentSpec, configPath string, inventory probeInventory) error {
	currentConfig, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	rendered, err := renderProbeScrapeConfigs(currentConfig, inventory.Probes)
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(inventory, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if err := os.MkdirAll(filepath.Dir(m.probeInventoryPath()), 0750); err != nil {
		return err
	}
	if err := atomicWrite(m.probeInventoryPath(), encoded, 0640); err != nil {
		return err
	}
	if err := atomicWrite(configPath, rendered, 0640); err != nil {
		return err
	}
	if !m.isLiveRoot() {
		return nil
	}
	if err := run(ctx, "chown", "-R", "root:prometheus", m.path("/etc/prometheus/probes")); err != nil {
		return err
	}
	if err := m.fixConfigOwnershipLocked(ctx, spec, configPath); err != nil {
		return err
	}
	if err := spec.validate(ctx, configPath); err != nil {
		return fmt.Errorf("生成的 Prometheus 探针配置校验失败：%w", err)
	}
	if err := run(ctx, "systemctl", "reload", "prometheus.service"); err != nil {
		return run(ctx, "systemctl", "restart", "prometheus.service")
	}
	return nil
}

func (m *Manager) readProbeInventoryLocked() (probeInventory, error) {
	inventory := probeInventory{Version: probeInventoryVersion, Probes: []Probe{}}
	content, err := os.ReadFile(m.probeInventoryPath())
	if errors.Is(err, os.ErrNotExist) {
		return inventory, nil
	}
	if err != nil {
		return probeInventory{}, err
	}
	if err := json.Unmarshal(content, &inventory); err != nil {
		return probeInventory{}, fmt.Errorf("探针清单格式无效：%w", err)
	}
	if inventory.Version != probeInventoryVersion {
		return probeInventory{}, fmt.Errorf("不支持的探针清单版本：%d", inventory.Version)
	}
	seenIDs := make(map[string]struct{}, len(inventory.Probes))
	seenNames := make(map[string]struct{}, len(inventory.Probes))
	seenTargets := make(map[string]struct{}, len(inventory.Probes))
	for _, probe := range inventory.Probes {
		if !probeIDPattern.MatchString(probe.ID) {
			return probeInventory{}, fmt.Errorf("探针清单包含无效 ID：%q", probe.ID)
		}
		if probe.Type != "node_exporter" {
			return probeInventory{}, fmt.Errorf("探针清单包含不支持的类型：%q", probe.Type)
		}
		if err := validateProbe(probe); err != nil {
			return probeInventory{}, fmt.Errorf("探针 %s 配置无效：%w", probe.ID, err)
		}
		nameKey := strings.ToLower(probe.Name)
		targetKey := probeTarget(probe)
		if _, exists := seenIDs[probe.ID]; exists {
			return probeInventory{}, fmt.Errorf("探针清单包含重复 ID：%s", probe.ID)
		}
		if _, exists := seenNames[nameKey]; exists {
			return probeInventory{}, fmt.Errorf("探针清单包含重复名称：%s", probe.Name)
		}
		if _, exists := seenTargets[targetKey]; exists {
			return probeInventory{}, fmt.Errorf("探针清单包含重复目标：%s", targetKey)
		}
		seenIDs[probe.ID] = struct{}{}
		seenNames[nameKey] = struct{}{}
		seenTargets[targetKey] = struct{}{}
	}
	return inventory, nil
}

func (m *Manager) probeInventoryPath() string {
	return m.path("/etc/prometheus/probes/inventory.json")
}

func (m *Manager) probeTLSDir(id string) string {
	return m.path("/etc/prometheus/probes/" + id)
}

func (m *Manager) probeTLSFilesLocked(id string) []ProbeTLSFile {
	dir := m.probeTLSDir(id)
	return []ProbeTLSFile{
		{Label: "node_exporter 服务端 CA", Path: filepath.Join(dir, "ca.crt"), Description: "签发 node_exporter 服务端证书的 CA 公共证书"},
		{Label: "Prometheus 完整客户端证书", Path: filepath.Join(dir, "client.crt"), Description: "由 node_exporter 信任的客户端 CA 签发的完整 Prometheus 客户端证书或证书链"},
		{Label: "Prometheus 客户端私钥", Path: filepath.Join(dir, "client.key"), Description: "与 client.crt 匹配的未加密 PEM 私钥"},
	}
}

func renderProbeScrapeConfigs(config []byte, probes []Probe) ([]byte, error) {
	lines := strings.Split(strings.ReplaceAll(string(config), "\r\n", "\n"), "\n")
	lines, err := removeManagedProbeBlock(lines)
	if err != nil {
		return nil, err
	}
	enabled := make([]Probe, 0, len(probes))
	for _, probe := range probes {
		if probe.Enabled {
			enabled = append(enabled, probe)
		}
	}
	if len(enabled) == 0 {
		return []byte(strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"), nil
	}
	scrapeIndex := -1
	for index, line := range lines {
		if line == "scrape_configs: []" || line == "scrape_configs: {}" {
			lines[index] = "scrape_configs:"
			scrapeIndex = index
			break
		}
		if line == "scrape_configs:" {
			scrapeIndex = index
			break
		}
	}
	if scrapeIndex < 0 {
		return nil, errors.New("Prometheus 主配置缺少根级 scrape_configs，无法加入受管探针")
	}
	insertIndex := len(lines)
	for index := scrapeIndex + 1; index < len(lines); index++ {
		line := lines[index]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if line[0] != ' ' && line[0] != '\t' {
			insertIndex = index
			break
		}
	}
	block := []string{"  " + probeBlockBegin}
	for _, probe := range enabled {
		block = append(block, renderProbeJob(probe)...)
	}
	block = append(block, "  "+probeBlockEnd)
	result := append([]string{}, lines[:insertIndex]...)
	result = append(result, block...)
	result = append(result, lines[insertIndex:]...)
	return []byte(strings.TrimRight(strings.Join(result, "\n"), "\n") + "\n"), nil
}

func removeManagedProbeBlock(lines []string) ([]string, error) {
	start, end := -1, -1
	for index, line := range lines {
		switch strings.TrimSpace(line) {
		case probeBlockBegin:
			if start >= 0 {
				return nil, errors.New("Prometheus 配置包含重复的 MonitorKit 探针配置块")
			}
			start = index
		case probeBlockEnd:
			if start < 0 || end >= 0 {
				return nil, errors.New("Prometheus 配置中的 MonitorKit 探针结束标记无效")
			}
			end = index
		}
	}
	if start < 0 && end < 0 {
		return lines, nil
	}
	if start < 0 || end < start {
		return nil, errors.New("Prometheus 配置中的 MonitorKit 探针配置块不完整")
	}
	return append(append([]string{}, lines[:start]...), lines[end+1:]...), nil
}

func renderProbeJob(probe Probe) []string {
	target := probeTarget(probe)
	lines := []string{
		"  - job_name: " + strconv.Quote("monitorkit-node-"+probe.ID),
		"    metrics_path: /metrics",
	}
	if probe.MTLS {
		base := "/etc/prometheus/probes/" + probe.ID
		lines = append(lines,
			"    scheme: https",
			"    tls_config:",
			"      ca_file: "+base+"/ca.crt",
			"      cert_file: "+base+"/client.crt",
			"      key_file: "+base+"/client.key",
			"      min_version: TLS12",
		)
		if probe.ServerName != "" {
			lines = append(lines, "      server_name: "+strconv.Quote(probe.ServerName))
		}
	} else {
		lines = append(lines, "    scheme: http")
	}
	return append(lines,
		"    static_configs:",
		"      - targets: ["+strconv.Quote(target)+"]",
		"        labels:",
		"          monitorkit_probe: "+strconv.Quote(probe.Name),
	)
}

func validateProbe(probe Probe) error {
	if probe.Name == "" {
		return errors.New("探针名称不能为空")
	}
	if len([]rune(probe.Name)) > 64 {
		return errors.New("探针名称不能超过 64 个字符")
	}
	if !validProbeHost(probe.Host) {
		return fmt.Errorf("探针地址无效：%s", probe.Host)
	}
	if probe.Port < 1 || probe.Port > 65535 {
		return errors.New("探针端口必须在 1 到 65535 之间")
	}
	if probe.MTLS && probe.ServerName != "" && !validProbeHost(probe.ServerName) {
		return fmt.Errorf("TLS server_name 无效：%s", probe.ServerName)
	}
	return nil
}

func normalizeProbeHost(host string) string {
	host = strings.TrimSpace(host)
	return strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
}

func validProbeHost(host string) bool {
	if _, err := netip.ParseAddr(host); err == nil {
		return true
	}
	return len(host) <= 253 && probeHostnamePattern.MatchString(host) && !strings.Contains(host, "..")
}

func probeTarget(probe Probe) string {
	return net.JoinHostPort(probe.Host, strconv.Itoa(probe.Port))
}

func probeIndex(probes []Probe, id string) int {
	for index, probe := range probes {
		if probe.ID == id {
			return index
		}
	}
	return -1
}

func newProbeID(name string) (string, error) {
	var random [4]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	slug := strings.ToLower(name)
	slug = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		slug = "node"
	}
	if len(slug) > 32 {
		slug = strings.Trim(slug[:32], "-")
	}
	return slug + "-" + hex.EncodeToString(random[:]), nil
}

func validateProbeTLSMaterial(ctx context.Context, files []ProbeTLSFile) error {
	for _, file := range files {
		info, err := os.Stat(file.Path)
		if err != nil {
			return fmt.Errorf("读取%s：%w", file.Label, err)
		}
		if info.Size() == 0 {
			return fmt.Errorf("%s为空", file.Label)
		}
	}
	if err := run(ctx, "openssl", "x509", "-in", files[0].Path, "-noout"); err != nil {
		return fmt.Errorf("node_exporter 服务端 CA 格式无效：%w", err)
	}
	if err := run(ctx, "openssl", "x509", "-in", files[1].Path, "-noout"); err != nil {
		return fmt.Errorf("Prometheus 客户端证书格式无效：%w", err)
	}
	if err := run(ctx, "openssl", "pkey", "-in", files[2].Path, "-noout", "-passin", "pass:"); err != nil {
		return fmt.Errorf("Prometheus 客户端私钥无效或已加密：%w", err)
	}
	certPublicKey, err := commandOutput(ctx, "openssl", "x509", "-in", files[1].Path, "-pubkey", "-noout")
	if err != nil {
		return err
	}
	keyPublicKey, err := commandOutput(ctx, "openssl", "pkey", "-in", files[2].Path, "-pubout", "-passin", "pass:")
	if err != nil {
		return err
	}
	if !bytes.Equal(bytes.TrimSpace(certPublicKey), bytes.TrimSpace(keyPublicKey)) {
		return errors.New("Prometheus 客户端证书与私钥不匹配")
	}
	return nil
}
