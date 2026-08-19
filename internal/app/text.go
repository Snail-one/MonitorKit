package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Snail-one/MonitorKit/internal/manager"
	"github.com/Snail-one/MonitorKit/internal/ui"
)

func (a *App) operationError(title string, err error) {
	a.ui.Card(ui.Danger, title, ui.Field{Label: "原因", Value: err.Error()}, ui.Field{Label: "状态", Value: "未继续执行后续操作"})
	a.ui.Pause()
}

func installedText(status manager.Status) string {
	if status.Installed {
		return "已安装"
	}
	return "未安装"
}

func bootEnabledText(enabled bool) string {
	if enabled {
		return "已开启"
	}
	return "未开启"
}

func serviceText(state string) string {
	switch state {
	case "active":
		return "运行中"
	case "staged":
		return "已写入暂存目录"
	case "not-installed":
		return "未安装"
	case "inactive", "failed":
		return "已停止"
	default:
		return valueOr(state, "未知")
	}
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func (a *App) componentAddress(component componentView) string {
	configuration, err := a.manager.Configuration(component.name)
	if err != nil {
		return "—"
	}
	return componentAddress(configuration.MTLSEnabled, configuration.ListenPort)
}

func componentAddress(mtls bool, port int) string {
	if port == 0 {
		return "安装时随机生成"
	}
	protocol := "http"
	if mtls {
		protocol = "https"
	}
	return fmt.Sprintf("%s://服务器地址:%d", protocol, port)
}

func portText(port int) string {
	if port == 0 {
		return "安装时随机生成"
	}
	return strconv.Itoa(port)
}

func grpcPortText(port int) string {
	if port == 0 {
		return "默认 9095"
	}
	return strconv.Itoa(port)
}

func enabledText(enabled bool) string {
	if enabled {
		return "已开启"
	}
	return "已关闭"
}

func metricSettingsSummary(settings manager.PrometheusStorageSettings) string {
	return metricRetentionText(settings) + " · " + metricSizeText(settings)
}

func metricSettingsDetail(settings manager.PrometheusStorageSettings) string {
	if settings.Unlimited && settings.SizeBytes == 0 {
		return "未限制时间和磁盘，/var/lib/prometheus 会持续增长"
	}
	return "时间与磁盘上限同时生效，先到先清理"
}

func metricSettingsBadge(settings manager.PrometheusStorageSettings) string {
	if settings.SizeBytes == 0 {
		return metricRetentionBadge(settings)
	}
	return metricRetentionBadge(settings) + " · " + metricSizeText(settings)
}

func metricRetentionBadge(settings manager.PrometheusStorageSettings) string {
	if settings.Unlimited {
		return "不限制"
	}
	if settings.Retention <= 0 {
		return "15 天"
	}
	return formatRetentionChoice(settings.Retention)
}

func metricRetentionText(settings manager.PrometheusStorageSettings) string {
	if settings.Unlimited {
		return "不限制"
	}
	if settings.Retention <= 0 {
		return "15 天（默认）"
	}
	return formatRetentionChoice(settings.Retention)
}

func metricRetentionDetail(settings manager.PrometheusStorageSettings) string {
	if settings.Unlimited {
		return "不再按天数删除指标"
	}
	if settings.Retention <= 0 {
		return "未写入自定义时间参数，使用 Prometheus 默认 15 天"
	}
	return "已写入 prometheus.service 的 --storage.tsdb.retention.time"
}

func metricSizeText(settings manager.PrometheusStorageSettings) string {
	if settings.SizeBytes <= 0 {
		return "不限制"
	}
	return formatMetricSize(settings.SizeBytes)
}

func formatMetricSize(bytes int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
		tb = 1024 * gb
	)
	switch {
	case bytes%tb == 0:
		return fmt.Sprintf("%d TB", bytes/tb)
	case bytes%gb == 0:
		return fmt.Sprintf("%d GB", bytes/gb)
	case bytes%mb == 0:
		return fmt.Sprintf("%d MB", bytes/mb)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func dataUsageText(usage manager.DataUsage) string {
	if !usage.Exists {
		return "尚无数据"
	}
	return formatStorageUsage(usage.Bytes)
}

func dataUsageDetail(usage manager.DataUsage) string {
	path := strings.TrimSpace(usage.Path)
	if path == "" {
		return "统计组件数据目录"
	}
	return "统计目录：" + path
}

func formatStorageUsage(bytes int64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	value := float64(bytes)
	for _, unit := range []string{"KB", "MB", "GB", "TB"} {
		value /= 1024
		if value < 1024 || unit == "TB" {
			if value == float64(int64(value)) {
				return fmt.Sprintf("%d %s", int64(value), unit)
			}
			return fmt.Sprintf("%.1f %s", value, unit)
		}
	}
	return fmt.Sprintf("%d B", bytes)
}

func logSettingsSummary(settings manager.LokiLogSettings) string {
	return logRetentionText(settings) + " · " + logIngestionText(settings)
}

func logSettingsDetail(settings manager.LokiLogSettings) string {
	if settings.Retention == 0 {
		return "未设置保留期，磁盘会持续增长"
	}
	if !settings.RetentionDeletes {
		return "已写入保留期，但尚未启用 Compactor 删除；保存一次数据存储设置即可启用"
	}
	return "过期日志由 Loki Compactor 删除"
}

func logRetentionBadge(settings manager.LokiLogSettings) string {
	if settings.Retention <= 0 {
		return "不限制"
	}
	return formatRetentionChoice(settings.Retention)
}

func logRetentionText(settings manager.LokiLogSettings) string {
	if settings.Retention <= 0 {
		return "不限制"
	}
	text := formatRetentionChoice(settings.Retention)
	if settings.Retention == manager.DefaultLokiRetention && settings.RetentionDeletes {
		return text + "（默认）"
	}
	if !settings.RetentionDeletes {
		return text + "（未启用删除）"
	}
	return text
}

func logRetentionDetail(settings manager.LokiLogSettings) string {
	if settings.Retention <= 0 {
		return "Loki 不会自动删除历史日志"
	}
	if !settings.RetentionDeletes {
		return "请通过本菜单重新保存保留期，以启用 Compactor"
	}
	return "Compactor 会按保留期清理 /var/lib/loki"
}

func logIngestionText(settings manager.LokiLogSettings) string {
	if settings.IngestionRateMB == 0 && settings.IngestionBurstMB == 0 && settings.MaxLineSizeKB == 0 {
		return "默认 4 MB/s"
	}
	return fmt.Sprintf("%s · %s", logIngestionRateText(settings), logLineSizeText(settings))
}

func logIngestionRateText(settings manager.LokiLogSettings) string {
	if settings.IngestionRateMB == 0 {
		return "4 MB/s（默认）"
	}
	return fmt.Sprintf("%d MB/s", settings.IngestionRateMB)
}

func logIngestionBurstText(settings manager.LokiLogSettings) string {
	if settings.IngestionBurstMB == 0 {
		return "6 MB（默认）"
	}
	return fmt.Sprintf("%d MB", settings.IngestionBurstMB)
}

func logLineSizeText(settings manager.LokiLogSettings) string {
	if settings.MaxLineSizeKB == 0 {
		return "256 KB（默认）"
	}
	return fmt.Sprintf("%d KB", settings.MaxLineSizeKB)
}

func formatRetentionChoice(retention time.Duration) string {
	if retention <= 0 {
		return "不限制"
	}
	if retention%(24*time.Hour) == 0 {
		return fmt.Sprintf("%d 天", retention/(24*time.Hour))
	}
	if retention%time.Hour == 0 {
		return fmt.Sprintf("%d 小时", retention/time.Hour)
	}
	return retention.String()
}

func parseOptionalPositiveInt(value string, fallback, minimum, maximum int) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		if fallback < minimum || fallback > maximum {
			return 0, fmt.Errorf("请输入 %d-%d 之间的整数", minimum, maximum)
		}
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < minimum || parsed > maximum {
		return 0, fmt.Errorf("请输入 %d-%d 之间的整数", minimum, maximum)
	}
	return parsed, nil
}

func probeEnabledText(enabled bool) string {
	if enabled {
		return "已启用"
	}
	return "已停用"
}
