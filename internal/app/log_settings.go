package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Snail-one/MonitorKit/internal/manager"
	"github.com/Snail-one/MonitorKit/internal/ui"
)

func (a *App) lokiLogSettingsMenu(ctx context.Context, component componentView) error {
	for {
		configuration, err := a.manager.Configuration(component.name)
		if err != nil {
			return err
		}
		settings := configuration.LogSettings
		a.ui.Clear()
		a.ui.Title(component.label, "配置管理", "数据存储设置")
		a.ui.Card(ui.Neutral, "当前 Loki 日志策略",
			ui.Field{Label: "日志大小", Value: dataUsageText(configuration.DataUsage), Detail: dataUsageDetail(configuration.DataUsage)},
			ui.Field{Label: "保留期", Value: logRetentionText(settings), Detail: logRetentionDetail(settings)},
			ui.Field{Label: "摄入速率", Value: logIngestionRateText(settings)},
			ui.Field{Label: "突发大小", Value: logIngestionBurstText(settings)},
			ui.Field{Label: "单行上限", Value: logLineSizeText(settings)},
		)
		a.ui.Blank()
		a.ui.OptionLive("1", "设置保留期", logRetentionBadge(settings), settings.Retention > 0)
		a.ui.Option("2", "设置摄入限制", logIngestionText(settings))
		a.ui.Option("3", "恢复默认", "30 天")
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
			a.changeLokiRetention(ctx, settings)
		case "2":
			a.changeLokiIngestion(ctx, settings)
		case "3":
			a.resetLokiLogSettings(ctx)
		default:
			a.ui.InvalidChoice()
			a.ui.Pause()
		}
	}
}

func (a *App) changeLokiRetention(ctx context.Context, current manager.LokiLogSettings) {
	a.ui.Clear()
	a.ui.Title("Loki", "配置管理", "数据存储设置", "保留期")
	a.ui.Card(ui.Neutral, "过期日志由 Loki Compactor 删除",
		ui.Field{Label: "最短时间", Value: "24 小时"},
		ui.Field{Label: "不限制", Value: "日志会一直写入 /var/lib/loki，直到磁盘耗尽"},
		ui.Field{Label: "当前值", Value: logRetentionText(current)},
	)
	a.ui.Blank()
	a.ui.Option("1", "7 天", "")
	a.ui.Option("2", "15 天", "")
	a.ui.Option("3", "30 天", "默认")
	a.ui.Option("4", "90 天", "")
	a.ui.Option("5", "自定义天数", "1-3650")
	a.ui.Option("6", "不限制", "磁盘持续增长")
	a.ui.ExitOption("返回数据存储设置")
	a.ui.Blank()
	choice, err := a.ui.Ask("请选择")
	if err != nil {
		return
	}
	var retention time.Duration
	switch strings.ToLower(choice) {
	case "0", "q", "exit":
		return
	case "1":
		retention = 7 * 24 * time.Hour
	case "2":
		retention = 15 * 24 * time.Hour
	case "3":
		retention = 30 * 24 * time.Hour
	case "4":
		retention = 90 * 24 * time.Hour
	case "5":
		daysValue, err := a.ui.Ask("保留天数（1-3650）")
		if err != nil {
			return
		}
		days, err := strconv.Atoi(strings.TrimSpace(daysValue))
		if err != nil || days < 1 || days > 3650 {
			a.operationError("无法设置日志保留期", errors.New("请输入 1-3650 之间的整数天数"))
			return
		}
		retention = time.Duration(days) * 24 * time.Hour
	case "6":
		retention = 0
	default:
		a.ui.InvalidChoice()
		a.ui.Pause()
		return
	}
	if retention == 0 {
		a.ui.Card(ui.Warning, "关闭保留期后日志将无限保存",
			ui.Field{Label: "影响", Value: "/var/lib/loki 会持续增长，需要自行监控磁盘"},
			ui.Field{Label: "已有数据", Value: "已经写入的日志不会因为本次设置被删除"},
		)
	}
	prompt := "确认将日志保留期设置为" + formatRetentionChoice(retention)
	if retention == 0 {
		prompt = "确认关闭日志保留期并不再自动删除过期日志"
	}
	confirmed, err := a.ui.Confirm(prompt)
	if err != nil || !confirmed {
		return
	}
	next := current
	next.Retention = retention
	a.applyLokiLogSettings(ctx, next, "正在更新 Loki 日志保留期")
}

func (a *App) changeLokiIngestion(ctx context.Context, current manager.LokiLogSettings) {
	rateDefault := current.IngestionRateMB
	if rateDefault == 0 {
		rateDefault = 4
	}
	burstDefault := current.IngestionBurstMB
	if burstDefault == 0 {
		burstDefault = 6
	}
	lineDefault := current.MaxLineSizeKB
	if lineDefault == 0 {
		lineDefault = 256
	}
	a.ui.Clear()
	a.ui.Title("Loki", "配置管理", "数据存储设置", "摄入限制")
	a.ui.Card(ui.Neutral, "限制单机 Loki 接收日志的速度和单行大小",
		ui.Field{Label: "摄入速率", Value: fmt.Sprintf("%d MB/s", rateDefault), Detail: "超过后新日志会被拒绝，直到速率回落"},
		ui.Field{Label: "突发大小", Value: fmt.Sprintf("%d MB", burstDefault), Detail: "必须不小于摄入速率"},
		ui.Field{Label: "单行上限", Value: fmt.Sprintf("%d KB", lineDefault), Detail: "超长 journal 行会被 Loki 拒绝"},
	)
	rateValue, err := a.ui.Ask(fmt.Sprintf("摄入速率 MB/s（1-1024，回车使用 %d）", rateDefault))
	if err != nil {
		return
	}
	rate, err := parseOptionalPositiveInt(rateValue, rateDefault, 1, 1024)
	if err != nil {
		a.operationError("无法设置摄入速率", err)
		return
	}
	burstValue, err := a.ui.Ask(fmt.Sprintf("突发大小 MB（%d-2048，回车使用 %d）", rate, burstDefault))
	if err != nil {
		return
	}
	if burstDefault < rate {
		burstDefault = rate
	}
	burst, err := parseOptionalPositiveInt(burstValue, burstDefault, rate, 2048)
	if err != nil {
		a.operationError("无法设置突发大小", err)
		return
	}
	lineValue, err := a.ui.Ask(fmt.Sprintf("单行上限 KB（1-16384，回车使用 %d）", lineDefault))
	if err != nil {
		return
	}
	lineSize, err := parseOptionalPositiveInt(lineValue, lineDefault, 1, 16384)
	if err != nil {
		a.operationError("无法设置单行上限", err)
		return
	}
	confirmed, err := a.ui.Confirm(fmt.Sprintf("确认将摄入限制设置为 %d MB/s、突发 %d MB、单行 %d KB", rate, burst, lineSize))
	if err != nil || !confirmed {
		return
	}
	next := current
	next.IngestionRateMB = rate
	next.IngestionBurstMB = burst
	next.MaxLineSizeKB = lineSize
	a.applyLokiLogSettings(ctx, next, "正在更新 Loki 摄入限制")
}

func (a *App) resetLokiLogSettings(ctx context.Context) {
	a.ui.Card(ui.Warning, "将恢复首次安装时的日志策略",
		ui.Field{Label: "保留期", Value: "30 天，Compactor 会删除过期日志"},
		ui.Field{Label: "摄入限制", Value: "使用 Loki 默认值：4 MB/s、突发 6 MB、单行 256 KB"},
	)
	confirmed, err := a.ui.Confirm("确认恢复默认数据存储设置")
	if err != nil || !confirmed {
		return
	}
	a.applyLokiLogSettings(ctx, manager.DefaultLokiLogSettings(), "正在恢复 Loki 默认数据存储设置")
}

func (a *App) applyLokiLogSettings(ctx context.Context, settings manager.LokiLogSettings, progress string) {
	err := a.ui.During(progress, func() error {
		return a.manager.ApplyLokiLogSettings(ctx, settings)
	})
	if err != nil {
		a.operationError("Loki 数据存储设置未应用", err)
		return
	}
	configuration, err := a.manager.Configuration("loki")
	if err != nil {
		a.operationError("数据存储设置已写入，但读取配置失败", err)
		return
	}
	a.ui.Card(ui.Success, "Loki 数据存储设置已应用",
		ui.Field{Label: "保留期", Value: logRetentionText(configuration.LogSettings), Detail: logRetentionDetail(configuration.LogSettings)},
		ui.Field{Label: "摄入限制", Value: logIngestionText(configuration.LogSettings)},
		ui.Field{Label: "配置文件", Value: "/etc/loki/loki.yml"},
	)
	a.ui.Pause()
}
