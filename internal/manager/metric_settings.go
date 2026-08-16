package manager

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	prometheusStorageSettingsName     = "storage.settings"
	minimumPrometheusRetention        = 24 * time.Hour
	maximumPrometheusRetention        = 3650 * 24 * time.Hour
	minimumPrometheusRetentionSize    = 1024 * 1024
	maximumPrometheusRetentionSize    = 1024 * 1024 * 1024 * 1024 * 1024 // 1 PB
	prometheusRetentionSizeMultiplier = 1024
)

var (
	prometheusRetentionTimePattern = regexp.MustCompile(`--storage\.tsdb\.retention\.time=(\S+)`)
	prometheusRetentionSizePattern = regexp.MustCompile(`--storage\.tsdb\.retention\.size=(\S+)`)
	prometheusSizePattern          = regexp.MustCompile(`(?i)^(\d+)\s*(ei?b?|pi?b?|ti?b?|gi?b?|mi?b?|ki?b?|b)?$`)
)

// PrometheusStorageSettings is the TSDB retention policy written into the
// Prometheus systemd unit. The zero value means Prometheus defaults: 15 days
// and no disk-size cap.
type PrometheusStorageSettings struct {
	Retention time.Duration
	Unlimited bool
	SizeBytes int64
}

func (s PrometheusStorageSettings) IsDefault() bool {
	return !s.Unlimited && s.Retention == 0 && s.SizeBytes == 0
}

// ApplyPrometheusStorageSettings writes time and size retention into the
// managed settings file and regenerates prometheus.service. Restoring the
// zero value removes the custom flags so Prometheus uses its 15-day default.
func (m *Manager) ApplyPrometheusStorageSettings(ctx context.Context, settings PrometheusStorageSettings) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.applyPrometheusStorageSettingsLocked(ctx, settings)
}

func (m *Manager) applyPrometheusStorageSettingsLocked(ctx context.Context, settings PrometheusStorageSettings) error {
	if err := validatePrometheusStorageSettings(settings); err != nil {
		return err
	}
	spec, configPath, err := m.configTargetLocked("prometheus")
	if err != nil {
		return err
	}
	current, err := m.prometheusStorageSettingsLocked()
	if err != nil {
		return err
	}
	if current == settings {
		return nil
	}
	listenPort, err := m.ensureListenPortLocked(spec.name)
	if err != nil {
		return err
	}
	unitPath := m.path("/etc/systemd/system/prometheus.service")
	settingsPath := m.prometheusStorageSettingsPath()
	snapshots, err := snapshotFiles([]string{unitPath, settingsPath})
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
			return fmt.Errorf("%v；恢复原数据存储设置失败：%w", cause, restoreErr)
		}
		return cause
	}
	if settings.IsDefault() {
		if err := os.Remove(settingsPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return rollback(err)
		}
	} else if err := atomicWrite(settingsPath, []byte(encodePrometheusStorageSettings(settings)), 0640); err != nil {
		return rollback(err)
	}
	unit := prometheusUnitWithStorage(mtlsEnabledLocked(m, spec.name), remoteWriteEnabledLocked(m), listenPort, settings)
	if err := atomicWrite(unitPath, []byte(unit), 0644); err != nil {
		return rollback(err)
	}
	if !m.isLiveRoot() {
		return nil
	}
	if err := spec.validate(ctx, configPath); err != nil {
		return rollback(fmt.Errorf("修改数据存储设置后的配置校验失败：%w", err))
	}
	if mtlsEnabledLocked(m, spec.name) {
		if err := m.validateMTLSConfigLocked(ctx, spec, configPath); err != nil {
			return rollback(err)
		}
	}
	if err := run(ctx, "systemctl", "daemon-reload"); err != nil {
		return rollback(err)
	}
	if err := run(ctx, "systemctl", "restart", "prometheus.service"); err != nil {
		return rollback(fmt.Errorf("应用数据存储设置后服务启动失败，已恢复原配置：%w", err))
	}
	return nil
}

func (m *Manager) componentUnitLocked(spec componentSpec, mtls, remoteWrite bool, port int) (string, error) {
	if spec.name == "prometheus" {
		settings, err := m.prometheusStorageSettingsLocked()
		if err != nil {
			settings = PrometheusStorageSettings{}
		}
		return prometheusUnitWithStorage(mtls, remoteWrite, port, settings), nil
	}
	grpcPort, _, err := m.configuredGRPCPortLocked()
	if err != nil {
		return "", err
	}
	return lokiUnit(mtls, port, grpcPort), nil
}

func (m *Manager) prometheusStorageSettingsLocked() (PrometheusStorageSettings, error) {
	settingsPath := m.prometheusStorageSettingsPath()
	content, err := os.ReadFile(settingsPath)
	if err == nil {
		return parsePrometheusStorageSettings(content)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return PrometheusStorageSettings{}, err
	}
	unit, unitErr := os.ReadFile(m.path("/etc/systemd/system/prometheus.service"))
	if errors.Is(unitErr, os.ErrNotExist) {
		return PrometheusStorageSettings{}, nil
	}
	if unitErr != nil {
		return PrometheusStorageSettings{}, unitErr
	}
	return parsePrometheusUnitStorageSettings(unit)
}

func (m *Manager) prometheusStorageSettingsPath() string {
	return m.path("/etc/prometheus/" + prometheusStorageSettingsName)
}

func prometheusUnit(mtls, remoteWrite bool, port int) string {
	return prometheusUnitWithStorage(mtls, remoteWrite, port, PrometheusStorageSettings{})
}

func prometheusUnitWithStorage(mtls, remoteWrite bool, port int, storage PrometheusStorageSettings) string {
	webConfigArgument := ""
	remoteWriteArgument := ""
	if mtls {
		webConfigArgument = " --web.config.file=/etc/prometheus/web.yml"
	}
	if remoteWrite {
		remoteWriteArgument = " --web.enable-remote-write-receiver"
	}
	return fmt.Sprintf(`[Unit]
Description=Prometheus monitoring server
Wants=network-online.target
After=network-online.target

[Service]
User=prometheus
Group=prometheus
Type=simple
ExecStart=/usr/local/bin/prometheus --config.file=/etc/prometheus/prometheus.yml --storage.tsdb.path=/var/lib/prometheus --web.listen-address=0.0.0.0:%d%s%s%s
ExecReload=/bin/kill -HUP $MAINPID
Restart=on-failure
RestartSec=5s
NoNewPrivileges=true
ProtectSystem=full
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/var/lib/prometheus

[Install]
WantedBy=multi-user.target
`, port, remoteWriteArgument, webConfigArgument, prometheusStorageArguments(storage))
}

func prometheusStorageArguments(settings PrometheusStorageSettings) string {
	var args string
	if settings.Unlimited {
		args += " --storage.tsdb.retention.time=0s"
	} else if settings.Retention > 0 {
		args += " --storage.tsdb.retention.time=" + formatLokiDuration(settings.Retention)
	}
	if settings.SizeBytes > 0 {
		args += " --storage.tsdb.retention.size=" + formatPrometheusSize(settings.SizeBytes)
	}
	return args
}

func encodePrometheusStorageSettings(settings PrometheusStorageSettings) string {
	var lines []string
	if settings.Unlimited {
		lines = append(lines, "retention_time=0s")
	} else if settings.Retention > 0 {
		lines = append(lines, "retention_time="+formatLokiDuration(settings.Retention))
	}
	if settings.SizeBytes > 0 {
		lines = append(lines, "retention_size="+formatPrometheusSize(settings.SizeBytes))
	}
	return strings.Join(lines, "\n") + "\n"
}

func parsePrometheusStorageSettings(content []byte) (PrometheusStorageSettings, error) {
	var settings PrometheusStorageSettings
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return PrometheusStorageSettings{}, fmt.Errorf("数据存储设置无效：%s", line)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch key {
		case "retention_time":
			if err := applyPrometheusRetentionTime(&settings, value); err != nil {
				return PrometheusStorageSettings{}, err
			}
		case "retention_size":
			size, err := parsePrometheusSize(value)
			if err != nil {
				return PrometheusStorageSettings{}, fmt.Errorf("解析 retention_size：%w", err)
			}
			settings.SizeBytes = size
		default:
			return PrometheusStorageSettings{}, fmt.Errorf("未知的数据存储设置：%s", key)
		}
	}
	return settings, validatePrometheusStorageSettings(settings)
}

func parsePrometheusUnitStorageSettings(unit []byte) (PrometheusStorageSettings, error) {
	var settings PrometheusStorageSettings
	if match := prometheusRetentionTimePattern.FindSubmatch(unit); len(match) == 2 {
		if err := applyPrometheusRetentionTime(&settings, string(match[1])); err != nil {
			return PrometheusStorageSettings{}, err
		}
	}
	if match := prometheusRetentionSizePattern.FindSubmatch(unit); len(match) == 2 {
		size, err := parsePrometheusSize(string(match[1]))
		if err != nil {
			return PrometheusStorageSettings{}, fmt.Errorf("解析 retention.size：%w", err)
		}
		settings.SizeBytes = size
	}
	return settings, validatePrometheusStorageSettings(settings)
}

func applyPrometheusRetentionTime(settings *PrometheusStorageSettings, value string) error {
	retention, err := parsePromDuration(value)
	if err != nil {
		return fmt.Errorf("解析 retention_time：%w", err)
	}
	if retention == 0 {
		settings.Unlimited = true
		settings.Retention = 0
		return nil
	}
	settings.Unlimited = false
	settings.Retention = retention
	return nil
}

func validatePrometheusStorageSettings(settings PrometheusStorageSettings) error {
	if settings.Unlimited && settings.Retention != 0 {
		return errors.New("不限制保留期时不能同时指定天数")
	}
	if settings.Retention < 0 {
		return errors.New("保留期不能为负数")
	}
	if settings.Retention > 0 && settings.Retention < minimumPrometheusRetention {
		return errors.New("Prometheus 最短保留期为 24 小时")
	}
	if settings.Retention > maximumPrometheusRetention {
		return errors.New("保留期不能超过 3650 天")
	}
	if settings.SizeBytes < 0 {
		return errors.New("磁盘上限不能为负数")
	}
	if settings.SizeBytes > 0 && settings.SizeBytes < minimumPrometheusRetentionSize {
		return errors.New("磁盘上限至少 1 MB")
	}
	if settings.SizeBytes > maximumPrometheusRetentionSize {
		return errors.New("磁盘上限不能超过 1 PB")
	}
	return nil
}

func parsePrometheusSize(value string) (int64, error) {
	value = strings.TrimSpace(unquoteYAML(value))
	if value == "" || value == "0" {
		return 0, nil
	}
	match := prometheusSizePattern.FindStringSubmatch(value)
	if match == nil {
		return 0, fmt.Errorf("无效大小 %q", value)
	}
	amount, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("无效大小 %q", value)
	}
	unit := strings.ToLower(match[2])
	unit = strings.TrimSuffix(unit, "b")
	unit = strings.TrimSuffix(unit, "i")
	var multiplier int64 = 1
	switch unit {
	case "", "b":
		multiplier = 1
	case "k":
		multiplier = prometheusRetentionSizeMultiplier
	case "m":
		multiplier = prometheusRetentionSizeMultiplier * prometheusRetentionSizeMultiplier
	case "g":
		multiplier = prometheusRetentionSizeMultiplier * prometheusRetentionSizeMultiplier * prometheusRetentionSizeMultiplier
	case "t":
		multiplier = prometheusRetentionSizeMultiplier * prometheusRetentionSizeMultiplier * prometheusRetentionSizeMultiplier * prometheusRetentionSizeMultiplier
	case "p":
		multiplier = prometheusRetentionSizeMultiplier * prometheusRetentionSizeMultiplier * prometheusRetentionSizeMultiplier * prometheusRetentionSizeMultiplier * prometheusRetentionSizeMultiplier
	case "e":
		multiplier = prometheusRetentionSizeMultiplier * prometheusRetentionSizeMultiplier * prometheusRetentionSizeMultiplier * prometheusRetentionSizeMultiplier * prometheusRetentionSizeMultiplier * prometheusRetentionSizeMultiplier
	default:
		return 0, fmt.Errorf("无效大小 %q", value)
	}
	if amount > 0 && multiplier > (1<<62)/amount {
		return 0, fmt.Errorf("无效大小 %q", value)
	}
	return amount * multiplier, nil
}

func formatPrometheusSize(bytes int64) string {
	if bytes <= 0 {
		return "0B"
	}
	unit := "B"
	value := bytes
	for _, next := range []struct {
		name string
		size int64
	}{
		{name: "KB", size: prometheusRetentionSizeMultiplier},
		{name: "MB", size: prometheusRetentionSizeMultiplier * prometheusRetentionSizeMultiplier},
		{name: "GB", size: prometheusRetentionSizeMultiplier * prometheusRetentionSizeMultiplier * prometheusRetentionSizeMultiplier},
		{name: "TB", size: prometheusRetentionSizeMultiplier * prometheusRetentionSizeMultiplier * prometheusRetentionSizeMultiplier * prometheusRetentionSizeMultiplier},
		{name: "PB", size: prometheusRetentionSizeMultiplier * prometheusRetentionSizeMultiplier * prometheusRetentionSizeMultiplier * prometheusRetentionSizeMultiplier * prometheusRetentionSizeMultiplier},
	} {
		if bytes%next.size != 0 {
			break
		}
		unit = next.name
		value = bytes / next.size
	}
	return strconv.FormatInt(value, 10) + unit
}
