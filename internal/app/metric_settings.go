package app

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/Snail-one/MonitorKit/internal/manager"
	"github.com/Snail-one/MonitorKit/internal/ui"
)

func (a *App) prometheusMetricSettingsMenu(ctx context.Context, component componentView) error {
	for {
		configuration, err := a.manager.Configuration(component.name)
		if err != nil {
			return err
		}
		settings := configuration.MetricSettings
		a.ui.Clear()
		a.ui.Title(component.label, "配置管理", "数据存储设置")
		a.ui.Card(ui.Neutral, "当前 Prometheus 存储策略",
			ui.Field{Label: "指标大小", Value: dataUsageText(configuration.DataUsage), Detail: dataUsageDetail(configuration.DataUsage)},
			ui.Field{Label: "保留期", Value: metricRetentionText(settings), Detail: metricRetentionDetail(settings)},
			ui.Field{Label: "磁盘上限", Value: metricSizeText(settings), Detail: "时间和磁盘上限同时生效，先到的先清理"},
		)
		a.ui.Blank()
		a.ui.OptionLive("1", "设置保留期", metricRetentionBadge(settings), !settings.IsDefault() && (settings.Unlimited || settings.Retention > 0))
		a.ui.OptionLive("2", "设置磁盘上限", metricSizeText(settings), settings.SizeBytes > 0)
		a.ui.Option("3", "恢复默认", "15 天，无磁盘上限")
		a.ui.ExitOption("返回配置管理")
		a.ui.Blank()
		choice, err := a.ui.Ask("请选择")
		if err != nil {
			return err
		}
		switch strings.ToLower(choice) {
		case "0", "q", "exit":
			return nil
		case "1":
			a.changePrometheusRetention(ctx, settings)
		case "2":
			a.changePrometheusSize(ctx, settings)
		case "3":
			a.resetPrometheusMetricSettings(ctx)
		default:
			a.ui.InvalidChoice()
			a.ui.Pause()
		}
	}
}

func (a *App) changePrometheusRetention(ctx context.Context, current manager.PrometheusStorageSettings) {
	a.ui.Clear()
	a.ui.Title("Prometheus", "配置管理", "数据存储设置", "保留期")
	a.ui.Card(ui.Neutral, "按时间删除过期指标",
		ui.Field{Label: "默认值", Value: "15 天（Prometheus 上游默认）"},
		ui.Field{Label: "不限制", Value: "只按磁盘上限清理；两者都关闭时磁盘会持续增长"},
		ui.Field{Label: "当前值", Value: metricRetentionText(current)},
	)
	a.ui.Blank()
	a.ui.Option("1", "7 天", "")
	a.ui.Option("2", "15 天", "默认")
	a.ui.Option("3", "30 天", "")
	a.ui.Option("4", "90 天", "")
	a.ui.Option("5", "自定义天数", "1-3650")
	a.ui.Option("6", "不限制", "需配合磁盘上限")
	a.ui.ExitOption("返回数据存储设置")
	a.ui.Blank()
	choice, err := a.ui.Ask("请选择")
	if err != nil {
		return
	}
	next := current
	switch strings.ToLower(choice) {
	case "0", "q", "exit":
		return
	case "1":
		next.Unlimited = false
		next.Retention = 7 * 24 * time.Hour
	case "2":
		next.Unlimited = false
		next.Retention = 0
	case "3":
		next.Unlimited = false
		next.Retention = 30 * 24 * time.Hour
	case "4":
		next.Unlimited = false
		next.Retention = 90 * 24 * time.Hour
	case "5":
		daysValue, err := a.ui.Ask("保留天数（1-3650）")
		if err != nil {
			return
		}
		days, err := strconv.Atoi(strings.TrimSpace(daysValue))
		if err != nil || days < 1 || days > 3650 {
			a.operationError("无法设置指标保留期", errors.New("请输入 1-3650 之间的整数天数"))
			return
		}
		next.Unlimited = false
		next.Retention = time.Duration(days) * 24 * time.Hour
	case "6":
		next.Unlimited = true
		next.Retention = 0
	default:
		a.ui.InvalidChoice()
		a.ui.Pause()
		return
	}
	if next.Unlimited && next.SizeBytes == 0 {
		a.ui.Card(ui.Warning, "关闭时间保留且未设置磁盘上限",
			ui.Field{Label: "影响", Value: "/var/lib/prometheus 会持续增长，直到磁盘耗尽"},
			ui.Field{Label: "建议", Value: "同时设置磁盘上限，或保留至少 15 天"},
		)
	}
	prompt := "确认将指标保留期设置为" + metricRetentionText(next)
	if next.Unlimited {
		prompt = "确认关闭时间保留并不再按天数删除指标"
	}
	confirmed, err := a.ui.Confirm(prompt)
	if err != nil || !confirmed {
		return
	}
	a.applyPrometheusMetricSettings(ctx, next, "正在更新 Prometheus 指标保留期")
}

func (a *App) changePrometheusSize(ctx context.Context, current manager.PrometheusStorageSettings) {
	a.ui.Clear()
	a.ui.Title("Prometheus", "配置管理", "数据存储设置", "磁盘上限")
	a.ui.Card(ui.Neutral, "按占用空间删除最旧指标",
		ui.Field{Label: "默认值", Value: "不限制"},
		ui.Field{Label: "当前值", Value: metricSizeText(current)},
		ui.Field{Label: "单位", Value: "GiB，1024 MB = 1 GB"},
	)
	a.ui.Blank()
	a.ui.Option("1", "10 GB", "")
	a.ui.Option("2", "20 GB", "")
	a.ui.Option("3", "50 GB", "")
	a.ui.Option("4", "100 GB", "")
	a.ui.Option("5", "自定义 GB", "1-1048576")
	a.ui.Option("6", "不限制", "只按保留期清理")
	a.ui.ExitOption("返回数据存储设置")
	a.ui.Blank()
	choice, err := a.ui.Ask("请选择")
	if err != nil {
		return
	}
	next := current
	switch strings.ToLower(choice) {
	case "0", "q", "exit":
		return
	case "1":
		next.SizeBytes = 10 * 1024 * 1024 * 1024
	case "2":
		next.SizeBytes = 20 * 1024 * 1024 * 1024
	case "3":
		next.SizeBytes = 50 * 1024 * 1024 * 1024
	case "4":
		next.SizeBytes = 100 * 1024 * 1024 * 1024
	case "5":
		sizeValue, err := a.ui.Ask("磁盘上限 GB（1-1048576）")
		if err != nil {
			return
		}
		gigs, err := strconv.Atoi(strings.TrimSpace(sizeValue))
		if err != nil || gigs < 1 || gigs > 1048576 {
			a.operationError("无法设置磁盘上限", errors.New("请输入 1-1048576 之间的整数 GB"))
			return
		}
		next.SizeBytes = int64(gigs) * 1024 * 1024 * 1024
	case "6":
		next.SizeBytes = 0
	default:
		a.ui.InvalidChoice()
		a.ui.Pause()
		return
	}
	if next.Unlimited && next.SizeBytes == 0 {
		a.ui.Card(ui.Warning, "关闭磁盘上限且未设置保留期",
			ui.Field{Label: "影响", Value: "/var/lib/prometheus 会持续增长，直到磁盘耗尽"},
		)
	}
	prompt := "确认将指标磁盘上限设置为" + metricSizeText(next)
	if next.SizeBytes == 0 {
		prompt = "确认关闭指标磁盘上限"
	}
	confirmed, err := a.ui.Confirm(prompt)
	if err != nil || !confirmed {
		return
	}
	a.applyPrometheusMetricSettings(ctx, next, "正在更新 Prometheus 磁盘上限")
}

func (a *App) resetPrometheusMetricSettings(ctx context.Context) {
	a.ui.Card(ui.Warning, "将恢复 Prometheus 默认存储策略",
		ui.Field{Label: "保留期", Value: "15 天"},
		ui.Field{Label: "磁盘上限", Value: "不限制"},
	)
	confirmed, err := a.ui.Confirm("确认恢复默认数据存储设置")
	if err != nil || !confirmed {
		return
	}
	a.applyPrometheusMetricSettings(ctx, manager.PrometheusStorageSettings{}, "正在恢复 Prometheus 默认数据存储设置")
}

func (a *App) applyPrometheusMetricSettings(ctx context.Context, settings manager.PrometheusStorageSettings, progress string) {
	err := a.ui.During(progress, func() error {
		return a.manager.ApplyPrometheusStorageSettings(ctx, settings)
	})
	if err != nil {
		a.operationError("Prometheus 数据存储设置未应用", err)
		return
	}
	configuration, err := a.manager.Configuration("prometheus")
	if err != nil {
		a.operationError("数据存储设置已写入，但读取配置失败", err)
		return
	}
	a.ui.Card(ui.Success, "Prometheus 数据存储设置已应用",
		ui.Field{Label: "保留期", Value: metricRetentionText(configuration.MetricSettings), Detail: metricRetentionDetail(configuration.MetricSettings)},
		ui.Field{Label: "磁盘上限", Value: metricSizeText(configuration.MetricSettings)},
		ui.Field{Label: "生效方式", Value: "已写入 prometheus.service 并重启"},
	)
	a.ui.Pause()
}
