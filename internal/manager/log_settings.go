package manager

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	minimumLokiRetention = 24 * time.Hour
	maximumLokiRetention = 3650 * 24 * time.Hour
	defaultLokiDataDir   = "/var/lib/loki"
)

var (
	yamlScalarKeyPattern = regexp.MustCompile(`^(\s*)([A-Za-z0-9_]+)\s*:\s*(.*?)\s*$`)
	promDurationPart     = regexp.MustCompile(`(?i)^(\d+)(ms|s|m|h|d|w|y)`)
	byteSizePattern      = regexp.MustCompile(`(?i)^(\d+)\s*(kib|kb|k|mib|mb|m|b)?$`)
)

// LokiLogSettings is the Loki retention and ingestion policy shown and
// written by the configuration menu. Zero ingestion fields mean “leave Loki
// defaults / omit the keys”.
type LokiLogSettings struct {
	Retention        time.Duration
	RetentionDeletes bool
	IngestionRateMB  int
	IngestionBurstMB int
	MaxLineSizeKB    int
}

// ApplyLokiLogSettings writes retention and ingestion limits into loki.yml.
// A zero retention period disables Compactor deletes. Zero ingestion fields
// remove the corresponding keys so Loki uses its built-in defaults.
func (m *Manager) ApplyLokiLogSettings(ctx context.Context, settings LokiLogSettings) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.applyLokiLogSettingsLocked(ctx, settings)
}

func (m *Manager) applyLokiLogSettingsLocked(ctx context.Context, settings LokiLogSettings) error {
	if err := validateLokiLogSettings(settings); err != nil {
		return err
	}
	spec, configPath, err := m.configTargetLocked("loki")
	if err != nil {
		return err
	}
	original, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	updated, err := applyLokiLogSettings(original, settings)
	if err != nil {
		return err
	}
	if string(updated) == string(original) {
		return nil
	}

	info, err := os.Stat(configPath)
	if err != nil {
		return err
	}
	snapshots, err := snapshotFiles([]string{configPath})
	if err != nil {
		return err
	}
	rollback := func(cause error) error {
		restoreErr := restoreSnapshots(snapshots)
		if m.isLiveRoot() {
			_ = m.fixConfigOwnershipLocked(ctx, spec, configPath)
			_ = run(ctx, "systemctl", "restart", "loki.service")
		}
		if restoreErr != nil {
			return fmt.Errorf("%v；恢复原日志设置失败：%w", cause, restoreErr)
		}
		return cause
	}
	if err := atomicWrite(configPath, updated, info.Mode().Perm()); err != nil {
		return rollback(err)
	}
	if err := m.fixConfigOwnershipLocked(ctx, spec, configPath); err != nil {
		return rollback(err)
	}
	if !m.isLiveRoot() {
		return nil
	}
	if err := spec.validate(ctx, configPath); err != nil {
		return rollback(fmt.Errorf("修改日志设置后的配置校验失败：%w", err))
	}
	if mtlsEnabledLocked(m, spec.name) {
		if err := m.validateMTLSConfigLocked(ctx, spec, configPath); err != nil {
			return rollback(err)
		}
	}
	if err := run(ctx, "systemctl", "restart", "loki.service"); err != nil {
		return rollback(fmt.Errorf("应用日志设置后服务启动失败，已恢复原配置：%w", err))
	}
	return nil
}

func (m *Manager) lokiLogSettingsLocked() (LokiLogSettings, error) {
	content, err := os.ReadFile(m.path("/etc/loki/loki.yml"))
	if errors.Is(err, os.ErrNotExist) {
		return LokiLogSettings{}, nil
	}
	if err != nil {
		return LokiLogSettings{}, err
	}
	return parseLokiLogSettings(content)
}

func parseLokiLogSettings(content []byte) (LokiLogSettings, error) {
	var settings LokiLogSettings
	if value, ok := yamlSectionScalar(content, "limits_config", "retention_period"); ok {
		retention, err := parsePromDuration(value)
		if err != nil {
			return LokiLogSettings{}, fmt.Errorf("解析 retention_period：%w", err)
		}
		settings.Retention = retention
	}
	if value, ok := yamlSectionScalar(content, "limits_config", "ingestion_rate_mb"); ok {
		rate, err := strconv.Atoi(value)
		if err != nil {
			return LokiLogSettings{}, fmt.Errorf("解析 ingestion_rate_mb：%w", err)
		}
		settings.IngestionRateMB = rate
	}
	if value, ok := yamlSectionScalar(content, "limits_config", "ingestion_burst_size_mb"); ok {
		burst, err := strconv.Atoi(value)
		if err != nil {
			return LokiLogSettings{}, fmt.Errorf("解析 ingestion_burst_size_mb：%w", err)
		}
		settings.IngestionBurstMB = burst
	}
	if value, ok := yamlSectionScalar(content, "limits_config", "max_line_size"); ok {
		size, err := parseByteSizeKB(value)
		if err != nil {
			return LokiLogSettings{}, fmt.Errorf("解析 max_line_size：%w", err)
		}
		settings.MaxLineSizeKB = size
	}
	if value, ok := yamlSectionScalar(content, "compactor", "retention_enabled"); ok {
		settings.RetentionDeletes = yamlTruthy(value)
	}
	return settings, nil
}

func applyLokiLogSettings(content []byte, settings LokiLogSettings) ([]byte, error) {
	if err := validateLokiLogSettings(settings); err != nil {
		return nil, err
	}
	updated := append([]byte(nil), content...)
	if settings.Retention > 0 {
		period := formatLokiDuration(settings.Retention)
		updated = setYAMLSectionScalar(updated, "limits_config", "retention_period", period)
		updated = setYAMLSectionScalar(updated, "limits_config", "max_query_lookback", period)
		updated = enableLokiRetentionCompactor(updated)
	} else {
		updated = removeYAMLSectionScalar(updated, "limits_config", "retention_period")
		updated = removeYAMLSectionScalar(updated, "limits_config", "max_query_lookback")
		if yamlHasSection(updated, "compactor") {
			updated = setYAMLSectionScalar(updated, "compactor", "retention_enabled", "false")
		}
	}
	if settings.IngestionRateMB > 0 {
		updated = setYAMLSectionScalar(updated, "limits_config", "ingestion_rate_mb", strconv.Itoa(settings.IngestionRateMB))
		updated = setYAMLSectionScalar(updated, "limits_config", "ingestion_burst_size_mb", strconv.Itoa(settings.IngestionBurstMB))
		updated = setYAMLSectionScalar(updated, "limits_config", "max_line_size", formatByteSizeKB(settings.MaxLineSizeKB))
	} else {
		updated = removeYAMLSectionScalar(updated, "limits_config", "ingestion_rate_mb")
		updated = removeYAMLSectionScalar(updated, "limits_config", "ingestion_burst_size_mb")
		updated = removeYAMLSectionScalar(updated, "limits_config", "max_line_size")
	}
	return updated, nil
}

func enableLokiRetentionCompactor(content []byte) []byte {
	updated := setYAMLSectionScalar(content, "compactor", "retention_enabled", "true")
	if _, ok := yamlSectionScalar(updated, "compactor", "working_directory"); !ok {
		updated = setYAMLSectionScalar(updated, "compactor", "working_directory", lokiCompactorDir(updated))
	}
	if _, ok := yamlSectionScalar(updated, "compactor", "compaction_interval"); !ok {
		updated = setYAMLSectionScalar(updated, "compactor", "compaction_interval", "10m")
	}
	if _, ok := yamlSectionScalar(updated, "compactor", "retention_delete_delay"); !ok {
		updated = setYAMLSectionScalar(updated, "compactor", "retention_delete_delay", "2h")
	}
	if _, ok := yamlSectionScalar(updated, "compactor", "delete_request_store"); !ok {
		updated = setYAMLSectionScalar(updated, "compactor", "delete_request_store", "filesystem")
	}
	return updated
}

func lokiCompactorDir(content []byte) string {
	prefix := defaultLokiDataDir
	if value, ok := yamlFindScalar(content, "path_prefix"); ok && value != "" {
		prefix = value
	}
	return filepath.ToSlash(filepath.Join(prefix, "compactor"))
}

func validateLokiLogSettings(settings LokiLogSettings) error {
	if settings.Retention < 0 {
		return errors.New("保留期不能为负数")
	}
	if settings.Retention > 0 && settings.Retention < minimumLokiRetention {
		return errors.New("Loki 最短保留期为 24 小时")
	}
	if settings.Retention > maximumLokiRetention {
		return errors.New("保留期不能超过 3650 天")
	}
	if settings.IngestionRateMB < 0 || settings.IngestionRateMB > 1024 {
		return errors.New("摄入速率必须在 1-1024 MB/s 之间，或留空使用默认值")
	}
	if settings.IngestionRateMB == 0 {
		if settings.IngestionBurstMB != 0 || settings.MaxLineSizeKB != 0 {
			return errors.New("未设置摄入速率时，突发大小和单行上限必须一并恢复默认")
		}
		return nil
	}
	if settings.IngestionBurstMB < settings.IngestionRateMB || settings.IngestionBurstMB > 2048 {
		return errors.New("突发大小必须不小于摄入速率，且不超过 2048 MB")
	}
	if settings.MaxLineSizeKB < 1 || settings.MaxLineSizeKB > 16384 {
		return errors.New("单行上限必须在 1-16384 KB 之间")
	}
	return nil
}

func parsePromDuration(value string) (time.Duration, error) {
	value = strings.TrimSpace(unquoteYAML(value))
	if value == "" || value == "0" {
		return 0, nil
	}
	var total time.Duration
	rest := value
	for rest != "" {
		match := promDurationPart.FindStringSubmatch(rest)
		if match == nil {
			return 0, fmt.Errorf("无效时长 %q", value)
		}
		amount, err := strconv.Atoi(match[1])
		if err != nil {
			return 0, fmt.Errorf("无效时长 %q", value)
		}
		var part time.Duration
		switch strings.ToLower(match[2]) {
		case "ms":
			part = time.Duration(amount) * time.Millisecond
		case "s":
			part = time.Duration(amount) * time.Second
		case "m":
			part = time.Duration(amount) * time.Minute
		case "h":
			part = time.Duration(amount) * time.Hour
		case "d":
			part = time.Duration(amount) * 24 * time.Hour
		case "w":
			part = time.Duration(amount) * 7 * 24 * time.Hour
		case "y":
			part = time.Duration(amount) * 365 * 24 * time.Hour
		}
		total += part
		rest = rest[len(match[0]):]
	}
	return total, nil
}

func formatLokiDuration(value time.Duration) string {
	if value <= 0 {
		return "0s"
	}
	if value%(24*time.Hour) == 0 {
		return strconv.Itoa(int(value/(24*time.Hour))) + "d"
	}
	if value%time.Hour == 0 {
		return strconv.Itoa(int(value/time.Hour)) + "h"
	}
	if value%time.Minute == 0 {
		return strconv.Itoa(int(value/time.Minute)) + "m"
	}
	return strconv.Itoa(int(value/time.Second)) + "s"
}

func parseByteSizeKB(value string) (int, error) {
	value = strings.TrimSpace(unquoteYAML(value))
	match := byteSizePattern.FindStringSubmatch(value)
	if match == nil {
		return 0, fmt.Errorf("无效大小 %q", value)
	}
	amount, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("无效大小 %q", value)
	}
	var bytes int64
	switch strings.ToLower(match[2]) {
	case "", "b":
		bytes = amount
	case "k", "kb", "kib":
		bytes = amount * 1024
	case "m", "mb", "mib":
		bytes = amount * 1024 * 1024
	default:
		return 0, fmt.Errorf("无效大小 %q", value)
	}
	kb := (bytes + 1023) / 1024
	if kb > int64(^uint(0)>>1) {
		return 0, fmt.Errorf("无效大小 %q", value)
	}
	return int(kb), nil
}

func formatByteSizeKB(value int) string {
	if value > 0 && value%1024 == 0 {
		return strconv.Itoa(value/1024) + "MB"
	}
	return strconv.Itoa(value) + "KB"
}

func yamlTruthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(unquoteYAML(value))) {
	case "true", "yes", "on", "1":
		return true
	default:
		return false
	}
}

func unquoteYAML(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
			return value[1 : len(value)-1]
		}
	}
	return value
}

func yamlHasSection(content []byte, section string) bool {
	_, _, found := yamlTopLevelSection(splitYAMLLines(content), section)
	return found
}

func yamlSectionScalar(content []byte, section, key string) (string, bool) {
	lines := splitYAMLLines(content)
	start, end, found := yamlTopLevelSection(lines, section)
	if !found {
		return "", false
	}
	_, value, ok := yamlSectionKey(lines, start, end, key)
	return value, ok
}

func yamlFindScalar(content []byte, key string) (string, bool) {
	for _, line := range splitYAMLLines(content) {
		indent, name, value, ok := parseYAMLScalarLine(line)
		if !ok || name != key || yamlLineIsComment(line) {
			continue
		}
		if indent == "" && value == "" {
			continue
		}
		if value != "" {
			return value, true
		}
	}
	return "", false
}

func setYAMLSectionScalar(content []byte, section, key, value string) []byte {
	lines := splitYAMLLines(content)
	start, end, found := yamlTopLevelSection(lines, section)
	if !found {
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
			lines = append(lines, "")
		}
		lines = append(lines, section+":", "  "+key+": "+value)
		return joinYAMLLines(lines)
	}
	if index, _, ok := yamlSectionKey(lines, start, end, key); ok {
		lines[index] = leadingWhitespace(lines[index]) + key + ": " + value
		return joinYAMLLines(lines)
	}
	inserted := []string{childIndent(lines, start, end) + key + ": " + value}
	updated := make([]string, 0, len(lines)+1)
	updated = append(updated, lines[:start+1]...)
	updated = append(updated, inserted...)
	updated = append(updated, lines[start+1:]...)
	return joinYAMLLines(updated)
}

func removeYAMLSectionScalar(content []byte, section, key string) []byte {
	lines := splitYAMLLines(content)
	start, end, found := yamlTopLevelSection(lines, section)
	if !found {
		return content
	}
	index, _, ok := yamlSectionKey(lines, start, end, key)
	if !ok {
		return content
	}
	updated := append([]string{}, lines[:index]...)
	updated = append(updated, lines[index+1:]...)
	return joinYAMLLines(updated)
}

func yamlTopLevelSection(lines []string, name string) (int, int, bool) {
	for index, line := range lines {
		if !isYAMLTopLevelKey(line, name) {
			continue
		}
		end := len(lines)
		for next := index + 1; next < len(lines); next++ {
			if isYAMLTopLevelLine(lines[next]) {
				end = next
				break
			}
		}
		return index, end, true
	}
	return 0, 0, false
}

func yamlSectionKey(lines []string, start, end int, key string) (int, string, bool) {
	child := childIndent(lines, start, end)
	for index := start + 1; index < end; index++ {
		indent, name, value, ok := parseYAMLScalarLine(lines[index])
		if !ok || name != key {
			continue
		}
		if child != "" && indent != child {
			continue
		}
		return index, value, true
	}
	return 0, "", false
}

func childIndent(lines []string, start, end int) string {
	for index := start + 1; index < end; index++ {
		line := lines[index]
		if yamlLineIsComment(line) || strings.TrimSpace(line) == "" {
			continue
		}
		if indent := leadingWhitespace(line); indent != "" {
			return indent
		}
	}
	return "  "
}

func parseYAMLScalarLine(line string) (indent, key, value string, ok bool) {
	if yamlLineIsComment(line) {
		return "", "", "", false
	}
	stripped, _, _ := strings.Cut(line, " #")
	match := yamlScalarKeyPattern.FindStringSubmatch(stripped)
	if match == nil {
		return "", "", "", false
	}
	return match[1], match[2], unquoteYAML(match[3]), true
}

func isYAMLTopLevelKey(line, name string) bool {
	if !isYAMLTopLevelLine(line) {
		return false
	}
	_, key, _, ok := parseYAMLScalarLine(line)
	return ok && key == name
}

func isYAMLTopLevelLine(line string) bool {
	if yamlLineIsComment(line) || strings.TrimSpace(line) == "" {
		return false
	}
	return leadingWhitespace(line) == ""
}

func yamlLineIsComment(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "#")
}

func leadingWhitespace(line string) string {
	trimmed := strings.TrimLeft(line, " \t")
	return line[:len(line)-len(trimmed)]
}

func splitYAMLLines(content []byte) []string {
	text := strings.ReplaceAll(string(content), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return strings.Split(text, "\n")
}

func joinYAMLLines(lines []string) []byte {
	return []byte(strings.Join(lines, "\n"))
}
