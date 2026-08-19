package app

import (
	"context"
	"strings"

	"github.com/Snail-one/MonitorKit/internal/ui"
)

func (a *App) componentMenu(ctx context.Context, component componentView) error {
	for {
		a.ui.Clear()
		status, err := a.manager.Status(ctx, component.name)
		if err != nil {
			return err
		}
		configuration, err := a.manager.Configuration(component.name)
		if err != nil {
			return err
		}
		transport := "HTTP"
		if configuration.MTLSEnabled {
			transport = "HTTPS + mTLS"
		}
		a.ui.Title(component.label)
		fields := []ui.Field{
			ui.Field{Label: "安装状态", Value: installedText(status)},
			ui.Field{Label: "服务状态", Value: serviceText(status.ServiceState)},
			ui.Field{Label: "当前版本", Value: valueOr(status.Version, "—")},
			ui.Field{Label: "配置文件", Value: component.configPath},
			ui.Field{Label: "监听端口", Value: portText(configuration.ListenPort)},
		}
		if component.name == "loki" {
			fields = append(fields, ui.Field{Label: "gRPC 端口", Value: grpcPortText(configuration.GRPCListenPort), Detail: "仅本机内部通信，探针无需填写"})
		}
		fields = append(fields, ui.Field{Label: "服务地址", Value: componentAddress(configuration.MTLSEnabled, configuration.ListenPort)})
		if component.name == "prometheus" {
			fields = append(fields,
				ui.Field{Label: "远程写入", Value: enabledText(configuration.RemoteWriteEnabled), Detail: "接收地址：/api/v1/write；mTLS 推荐，HTTP 需确认风险"},
				ui.Field{Label: "指标大小", Value: dataUsageText(configuration.DataUsage), Detail: dataUsageDetail(configuration.DataUsage)},
				ui.Field{Label: "数据存储设置", Value: metricSettingsSummary(configuration.MetricSettings), Detail: metricSettingsDetail(configuration.MetricSettings)},
			)
		}
		if component.name == "loki" {
			fields = append(fields,
				ui.Field{Label: "日志大小", Value: dataUsageText(configuration.DataUsage), Detail: dataUsageDetail(configuration.DataUsage)},
				ui.Field{Label: "数据存储设置", Value: logSettingsSummary(configuration.LogSettings), Detail: logSettingsDetail(configuration.LogSettings)},
			)
		}
		fields = append(fields, ui.Field{Label: "传输安全", Value: transport})
		a.ui.Card(ui.Neutral, component.description, fields...)
		a.ui.Blank()
		configHint := "请先安装"
		if status.Installed {
			configHint = "编辑/校验/mTLS"
		}
		serviceHint := "请先安装"
		if status.Installed {
			serviceHint = "启动/停止/重启"
		}
		a.ui.Option("1", "配置管理", configHint)
		a.ui.Option("2", "服务管理", serviceHint)
		a.ui.Option("3", "安装或更新", "最新/指定版本")
		a.ui.Option("4", "卸载程序", "保留数据")
		a.ui.Option("5", "彻底清理", "删除数据")
		a.ui.ExitOption("返回总览")
		a.ui.Blank()

		choice, err := a.ui.Ask("请选择")
		if err != nil {
			return err
		}
		switch strings.ToLower(choice) {
		case "0", "q", "exit":
			return nil
		case "1":
			if !status.Installed {
				a.unavailableConfigAction(component)
				continue
			}
			if err := a.configurationMenu(ctx, component); err != nil {
				return err
			}
		case "2":
			if !status.Installed {
				a.unavailableConfigAction(component)
				continue
			}
			if err := a.serviceMenu(ctx, component); err != nil {
				return err
			}
		case "3":
			if err := a.installVersionMenu(ctx, component); err != nil {
				return err
			}
		case "4":
			a.uninstall(ctx, component, false)
		case "5":
			a.uninstall(ctx, component, true)
		default:
			a.ui.InvalidChoice()
			a.ui.Pause()
		}
	}
}

func (a *App) unavailableConfigAction(component componentView) {
	a.ui.Card(ui.Warning, component.label+"尚未安装",
		ui.Field{Label: "下一步", Value: "请先选择“安装或更新”"},
	)
	a.ui.Pause()
}
