// Package app coordinates MonitorKit's interactive terminal experience.
package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/Snail-one/MonitorKit/internal/manager"
	"github.com/Snail-one/MonitorKit/internal/ui"
	"github.com/Snail-one/MonitorKit/internal/version"
)

type App struct {
	manager *manager.Manager
	ui      *ui.UI
}

type componentView struct {
	name        string
	label       string
	description string
	configPath  string
}

var componentViews = map[string]componentView{
	"prometheus": {
		name:        "prometheus",
		label:       "Prometheus",
		description: "汇聚并保存服务器性能指标",
		configPath:  "/etc/prometheus/prometheus.yml",
	},
	"loki": {
		name:        "loki",
		label:       "Loki",
		description: "接收并保存探针发送的系统日志",
		configPath:  "/etc/loki/loki.yml",
	},
}

func New(mgr *manager.Manager, terminal *ui.UI) *App {
	return &App{manager: mgr, ui: terminal}
}

func (a *App) Run(ctx context.Context) error {
	for {
		a.ui.Clear()
		statuses, err := a.statuses(ctx)
		if err != nil {
			return err
		}
		privileged := os.Geteuid() == 0
		access := "只读模式"
		if privileged {
			access = "管理模式"
		}
		a.ui.Home("服务器可观测性控制台 · "+version.Version, access, privileged)
		a.ui.OptionLive("1", componentViews["prometheus"].label, statusHint(statuses["prometheus"]), statusActive(statuses["prometheus"]))
		a.ui.OptionLive("2", componentViews["loki"].label, statusHint(statuses["loki"]), statusActive(statuses["loki"]))
		a.ui.Option("3", "探针接入", "Shell")
		a.ui.ExitOption("退出")
		a.ui.Blank()

		choice, err := a.ui.Ask("请选择")
		if err != nil {
			return err
		}
		switch strings.ToLower(choice) {
		case "0", "q", "exit":
			return nil
		case "1":
			if err := a.componentMenu(ctx, componentViews["prometheus"]); err != nil {
				return err
			}
		case "2":
			if err := a.componentMenu(ctx, componentViews["loki"]); err != nil {
				return err
			}
		case "3":
			if err := a.probeMenu(ctx); err != nil {
				return err
			}
		default:
			a.ui.InvalidChoice()
			a.ui.Pause()
		}
	}
}

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
		a.ui.Option("1", "配置管理", configHint)
		a.ui.Option("2", "安装或更新", "最新/指定版本")
		a.ui.Option("3", "卸载程序", "保留数据")
		a.ui.Option("4", "彻底清理", "删除数据")
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
			if err := a.installVersionMenu(ctx, component); err != nil {
				return err
			}
		case "3":
			a.uninstall(ctx, component, false)
		case "4":
			a.uninstall(ctx, component, true)
		default:
			a.ui.InvalidChoice()
			a.ui.Pause()
		}
	}
}

func (a *App) installVersionMenu(ctx context.Context, component componentView) error {
	for {
		a.ui.Clear()
		a.ui.Title(component.label, "配置管理", "安装或更新")
		a.ui.Card(ui.Neutral, "选择目标版本",
			ui.Field{Label: "最新稳定版", Value: "自动查询 GitHub Release 的最新正式版本"},
			ui.Field{Label: "指定版本", Value: "输入 x.y.z 版本号安装或降级"},
			ui.Field{Label: "保留内容", Value: "现有配置、监听端口、mTLS 和独立开关状态"},
		)
		a.ui.Blank()
		a.ui.Option("1", "安装最新稳定版", "推荐")
		a.ui.Option("2", "安装指定版本", "")
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
			a.install(ctx, component, "latest")
			return nil
		case "2":
			wantedVersion, err := a.ui.Ask("输入版本号，例如 1.2.3")
			if err != nil {
				return err
			}
			if wantedVersion == "" {
				a.ui.Card(ui.Warning, "版本号不能为空",
					ui.Field{Label: "正确格式", Value: "x.y.z，例如 3.7.2"},
				)
				a.ui.Pause()
				continue
			}
			a.install(ctx, component, wantedVersion)
			return nil
		default:
			a.ui.InvalidChoice()
			a.ui.Pause()
		}
	}
}

func (a *App) configurationMenu(ctx context.Context, component componentView) error {
	for {
		configuration, err := a.manager.Configuration(component.name)
		if err != nil {
			return err
		}
		a.ui.Clear()
		a.ui.Title(component.label, "配置管理")
		mtlsStatus := "未启用（HTTP）"
		if configuration.MTLSEnabled {
			mtlsStatus = "已启用（HTTPS，验证客户端证书）"
		}
		configFields := []ui.Field{
			{Label: "主配置", Value: configuration.Path},
			{Label: "监听端口", Value: portText(configuration.ListenPort)},
		}
		if component.name == "loki" {
			configFields = append(configFields, ui.Field{Label: "gRPC 端口", Value: grpcPortText(configuration.GRPCListenPort), Detail: "仅本机内部通信，探针无需填写"})
		}
		configFields = append(configFields,
			ui.Field{Label: "mTLS", Value: mtlsStatus},
			ui.Field{Label: "证书目录", Value: configuration.TLSDir},
		)
		if component.name == "prometheus" {
			configFields = append(configFields,
				ui.Field{Label: "远程写入接收", Value: enabledText(configuration.RemoteWriteEnabled), Detail: "独立开关；HTTP 模式开启时会显示明文传输警告"},
				ui.Field{Label: "指标大小", Value: dataUsageText(configuration.DataUsage), Detail: dataUsageDetail(configuration.DataUsage)},
				ui.Field{Label: "数据存储设置", Value: metricSettingsSummary(configuration.MetricSettings), Detail: metricSettingsDetail(configuration.MetricSettings)},
			)
		}
		if component.name == "loki" {
			configFields = append(configFields,
				ui.Field{Label: "日志大小", Value: dataUsageText(configuration.DataUsage), Detail: dataUsageDetail(configuration.DataUsage)},
				ui.Field{Label: "数据存储设置", Value: logSettingsSummary(configuration.LogSettings), Detail: logSettingsDetail(configuration.LogSettings)},
			)
		}
		a.ui.Card(ui.Neutral, component.label+"配置", configFields...)
		a.ui.Blank()
		editor, editorReady := editorValue()
		a.ui.OptionLive("1", "编辑主配置", editor, editorReady)
		a.ui.Option("2", "校验当前配置", "")
		a.ui.Option("3", "重启并应用配置", "")
		a.ui.OptionLive("4", "修改监听端口", portText(configuration.ListenPort), configuration.ListenPort > 0)
		a.ui.OptionState("5", "配置或更新 mTLS", configuration.MTLSEnabled)
		if component.name == "prometheus" {
			remoteWriteLabel := "开启远程写入"
			if configuration.RemoteWriteEnabled {
				remoteWriteLabel = "关闭远程写入"
			}
			a.ui.OptionLive("6", "数据存储设置", metricSettingsBadge(configuration.MetricSettings), !configuration.MetricSettings.IsDefault())
			a.ui.OptionState("7", remoteWriteLabel, configuration.RemoteWriteEnabled)
		}
		if component.name == "loki" {
			a.ui.OptionLive("6", "数据存储设置", logRetentionBadge(configuration.LogSettings), configuration.LogSettings.Retention > 0)
			a.ui.OptionLive("7", "修改 gRPC 端口", grpcPortText(configuration.GRPCListenPort), true)
		}
		if configuration.MTLSEnabled {
			hint := "保留证书"
			if component.name == "prometheus" && configuration.RemoteWriteEnabled {
				hint = "远程写入转为 HTTP"
			}
			a.ui.Option("8", "关闭 mTLS", hint)
		}
		a.ui.ExitOption("返回" + component.label)
		a.ui.Blank()

		choice, err := a.ui.Ask("请选择")
		if err != nil {
			return err
		}
		switch strings.ToLower(choice) {
		case "0", "q", "exit":
			return nil
		case "1":
			a.editComponentConfig(ctx, component)
		case "2":
			a.validateComponentConfig(ctx, component)
		case "3":
			a.restartComponent(ctx, component)
		case "4":
			a.changeComponentPort(ctx, component, configuration.ListenPort)
		case "5":
			a.configureComponentMTLS(ctx, component)
		case "6":
			switch component.name {
			case "prometheus":
				if err := a.prometheusMetricSettingsMenu(ctx, component); err != nil {
					return err
				}
			case "loki":
				if err := a.lokiLogSettingsMenu(ctx, component); err != nil {
					return err
				}
			default:
				a.ui.InvalidChoice()
				a.ui.Pause()
			}
		case "7":
			if component.name == "prometheus" {
				a.toggleRemoteWrite(ctx, component, configuration)
			} else if component.name == "loki" {
				a.changeLokiGRPCPort(ctx, component, configuration.GRPCListenPort)
			} else {
				a.ui.InvalidChoice()
				a.ui.Pause()
			}
		case "8":
			if configuration.MTLSEnabled {
				a.disableComponentMTLS(ctx, component)
			} else {
				a.ui.InvalidChoice()
				a.ui.Pause()
			}
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

func (a *App) toggleRemoteWrite(ctx context.Context, component componentView, configuration manager.Configuration) {
	enable := !configuration.RemoteWriteEnabled
	if enable && !configuration.MTLSEnabled {
		a.ui.Card(ui.Warning, "Prometheus 远程写入将使用 HTTP",
			ui.Field{Label: "风险", Value: "指标内容和请求凭据不会被 TLS 加密，网络中的其他设备可能读取或篡改数据"},
			ui.Field{Label: "暴露范围", Value: "接收接口监听当前服务地址；请使用防火墙限制为可信探针 IP"},
			ui.Field{Label: "推荐方案", Value: "返回配置菜单启用 mTLS 后再开放远程写入"},
		)
	}
	action := "开启"
	if !enable {
		action = "关闭"
	}
	prompt := "确认" + action + " Prometheus 远程写入接收接口"
	if enable && !configuration.MTLSEnabled {
		prompt = "确认接受明文传输风险并通过 HTTP 开启 Prometheus 远程写入"
	}
	confirmed, err := a.ui.Confirm(prompt)
	if err != nil || !confirmed {
		return
	}
	err = a.ui.During("正在"+action+" Prometheus 远程写入", func() error {
		return a.manager.SetRemoteWrite(ctx, component.name, enable)
	})
	if err != nil {
		a.operationError("Prometheus 远程写入"+action+"失败", err)
		return
	}
	result := "已关闭"
	fields := []ui.Field{{Label: "状态", Value: result}}
	if enable {
		result = "已开启（HTTP，未加密）"
		address := componentAddress(configuration.MTLSEnabled, configuration.ListenPort) + "/api/v1/write"
		accessRequirement := "建议使用防火墙仅允许可信探针访问"
		if configuration.MTLSEnabled {
			result = "已开启（mTLS）"
			accessRequirement = "客户端必须提供受信任的 mTLS 证书"
		}
		fields = []ui.Field{
			{Label: "状态", Value: result},
			{Label: "接收地址", Value: address},
			{Label: "访问要求", Value: accessRequirement},
		}
	}
	a.ui.Card(ui.Success, "Prometheus 远程写入"+action+"完成", fields...)
	a.ui.Pause()
}

func (a *App) changeComponentPort(ctx context.Context, component componentView, currentPort int) {
	value, err := a.ui.Ask("输入新端口（1024-65535，输入 r 随机生成）")
	if err != nil {
		return
	}
	var port int
	if strings.EqualFold(strings.TrimSpace(value), "r") {
		port, err = a.manager.RandomListenPort(component.name)
	} else {
		port, err = strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			err = fmt.Errorf("请输入 1024-65535 之间的端口，或输入 r")
		}
	}
	if err != nil {
		a.operationError("无法设置"+component.label+"端口", err)
		return
	}
	confirmed, err := a.ui.Confirm(fmt.Sprintf("确认将 %s 监听端口从 %s 修改为 %d", component.label, portText(currentPort), port))
	if err != nil || !confirmed {
		return
	}
	err = a.ui.During("正在修改 "+component.label+" 监听端口", func() error {
		return a.manager.ChangeListenPort(ctx, component.name, port)
	})
	if err != nil {
		a.operationError(component.label+"端口修改失败", err)
		return
	}
	configuration, err := a.manager.Configuration(component.name)
	if err != nil {
		a.operationError(component.label+"端口已修改，但读取配置失败", err)
		return
	}
	a.ui.Card(ui.Success, component.label+"监听端口已修改",
		ui.Field{Label: "新端口", Value: strconv.Itoa(configuration.ListenPort)},
		ui.Field{Label: "服务地址", Value: componentAddress(configuration.MTLSEnabled, configuration.ListenPort)},
		ui.Field{Label: "提示", Value: "请同步更新探针中的中心地址"},
	)
	a.ui.Pause()
}

func (a *App) changeLokiGRPCPort(ctx context.Context, component componentView, currentPort int) {
	value, err := a.ui.Ask("输入新 gRPC 端口（1024-65535，输入 r 随机生成）")
	if err != nil {
		return
	}
	var port int
	if strings.EqualFold(strings.TrimSpace(value), "r") {
		port, err = a.manager.RandomGRPCPort()
	} else {
		port, err = strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			err = fmt.Errorf("请输入 1024-65535 之间的端口，或输入 r")
		}
	}
	if err != nil {
		a.operationError("无法设置 Loki gRPC 端口", err)
		return
	}
	confirmed, err := a.ui.Confirm(fmt.Sprintf("确认将 Loki gRPC 端口从 %s 修改为 %d", grpcPortText(currentPort), port))
	if err != nil || !confirmed {
		return
	}
	err = a.ui.During("正在修改 Loki gRPC 端口", func() error {
		return a.manager.ChangeGRPCListenPort(ctx, component.name, port)
	})
	if err != nil {
		a.operationError("Loki gRPC 端口修改失败", err)
		return
	}
	configuration, err := a.manager.Configuration(component.name)
	if err != nil {
		a.operationError("Loki gRPC 端口已修改，但读取配置失败", err)
		return
	}
	a.ui.Card(ui.Success, "Loki gRPC 端口已修改",
		ui.Field{Label: "新端口", Value: strconv.Itoa(configuration.GRPCListenPort)},
		ui.Field{Label: "监听地址", Value: "127.0.0.1:" + strconv.Itoa(configuration.GRPCListenPort)},
		ui.Field{Label: "提示", Value: "仅供 Loki 内部通信，探针和 Grafana 无需修改"},
	)
	a.ui.Pause()
}

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
		a.ui.Option("3", "恢复默认", "不限制保留")
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
	a.ui.Option("3", "30 天", "推荐")
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
		ui.Field{Label: "保留期", Value: "不限制，已有日志不会被自动删除"},
		ui.Field{Label: "摄入限制", Value: "使用 Loki 默认值：4 MB/s、突发 6 MB、单行 256 KB"},
	)
	confirmed, err := a.ui.Confirm("确认恢复默认数据存储设置")
	if err != nil || !confirmed {
		return
	}
	a.applyLokiLogSettings(ctx, manager.LokiLogSettings{}, "正在恢复 Loki 默认数据存储设置")
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

func (a *App) editComponentConfig(ctx context.Context, component componentView) {
	editor, err := selectTextEditor()
	if err != nil {
		a.operationError("无法编辑"+component.label+"配置", err)
		return
	}
	confirmed, err := a.ui.Confirm("使用 " + filepathBase(editor) + " 编辑配置，保存后自动校验并应用")
	if err != nil || !confirmed {
		return
	}
	err = a.manager.EditConfig(ctx, component.name, func(path string) error {
		a.ui.Card(ui.Neutral, "正在编辑"+component.label,
			ui.Field{Label: "编辑器", Value: filepathBase(editor)},
			ui.Field{Label: "配置文件", Value: path},
			ui.Field{Label: "安全机制", Value: "校验失败即时清理修改并恢复原配置"},
		)
		return openTextEditor(ctx, editor, path)
	})
	if err != nil {
		a.operationError(component.label+"配置未应用", err)
		return
	}
	a.ui.Card(ui.Success, component.label+"配置已应用",
		ui.Field{Label: "配置文件", Value: component.configPath},
		ui.Field{Label: "校验", Value: "通过"},
	)
	a.ui.Pause()
}

func (a *App) validateComponentConfig(ctx context.Context, component componentView) {
	err := a.ui.During("正在校验"+component.label+"配置", func() error {
		return a.manager.ValidateConfig(ctx, component.name)
	})
	if err != nil {
		a.operationError(component.label+"配置校验失败", err)
		return
	}
	a.ui.Card(ui.Success, component.label+"配置校验通过", ui.Field{Label: "配置文件", Value: component.configPath})
	a.ui.Pause()
}

func (a *App) restartComponent(ctx context.Context, component componentView) {
	confirmed, err := a.ui.Confirm("确认校验配置并重启" + component.label)
	if err != nil || !confirmed {
		return
	}
	err = a.ui.During("正在应用"+component.label+"配置", func() error {
		return a.manager.Restart(ctx, component.name)
	})
	if err != nil {
		a.operationError(component.label+"配置应用失败", err)
		return
	}
	a.ui.Card(ui.Success, component.label+"配置已应用", ui.Field{Label: "服务", Value: "已重启"})
	a.ui.Pause()
}

func (a *App) configureComponentMTLS(ctx context.Context, component componentView) {
	editor, err := selectTextEditor()
	if err != nil {
		a.operationError("无法配置 "+component.label+" mTLS", err)
		return
	}
	configuration, err := a.manager.Configuration(component.name)
	if err != nil {
		a.operationError("无法读取 "+component.label+" mTLS 配置", err)
		return
	}
	tlsDir := configuration.TLSDir
	a.ui.Clear()
	a.ui.Title(component.label, "mTLS", "准备证书")
	a.ui.Card(ui.Neutral, "需要准备 3 个 PEM 文件",
		ui.Field{
			Label:  "1. 客户端根 CA",
			Value:  tlsDir + "/client-ca.crt",
			Detail: "填写签发 Alloy 客户端证书的 CA 公共证书；不要填写 Alloy 客户端证书或任何私钥",
		},
		ui.Field{
			Label:  "2. 完整服务端证书",
			Value:  tlsDir + "/server.crt",
			Detail: "填写完整证书链；必须包含 BEGIN/END CERTIFICATE，SAN 包含探针访问时使用的域名或 IP",
		},
		ui.Field{
			Label:  "3. 服务端私钥",
			Value:  tlsDir + "/server.key",
			Detail: "填写与 server.crt 匹配且未加密的完整 PEM 私钥；不要填写 CA 私钥",
		},
		ui.Field{
			Label:  "编辑方式",
			Value:  "程序将依次打开 3 次 " + filepathBase(editor),
			Detail: "每次先显示文件用途；确认后按回车打开编辑器，粘贴完整 PEM 内容并保存退出",
		},
		ui.Field{
			Label:  "安全校验",
			Value:  "证书格式、证书与私钥匹配关系、服务配置全部通过后才会重启",
			Detail: "任一文件无效都会即时清理本次修改并恢复原 mTLS 配置",
		},
	)
	if component.name == "prometheus" {
		a.ui.Card(ui.Neutral, "Prometheus 远程写入使用独立开关",
			ui.Field{Label: "当前操作", Value: "只配置 mTLS，不会自动开放 /api/v1/write"},
			ui.Field{Label: "后续操作", Value: "返回配置菜单单独开启远程写入；HTTP 也可开启但会警告明文风险"},
		)
	}
	confirmed, err := a.ui.Confirm("已准备好上述 3 个 PEM 文件，开始配置")
	if err != nil || !confirmed {
		return
	}
	step := 0
	err = a.manager.ConfigureMTLS(ctx, component.name, func(file manager.TLSFile) error {
		step++
		a.ui.Clear()
		a.ui.Title(component.label, "mTLS")
		a.ui.Card(ui.Neutral, fmt.Sprintf("第 %d/3 步 · 编辑%s", step, file.Label),
			ui.Field{Label: "文件", Value: file.Path},
			ui.Field{Label: "填写内容", Value: file.Description},
			ui.Field{Label: "操作", Value: "粘贴或替换为完整 PEM 内容，然后保存并退出编辑器"},
			ui.Field{Label: "编辑器", Value: filepathBase(editor)},
		)
		if err := a.ui.Wait("确认填写要求后，按回车打开 " + filepathBase(editor)); err != nil {
			return err
		}
		return openTextEditor(ctx, editor, file.Path)
	})
	if err != nil {
		a.operationError(component.label+" mTLS 配置失败", err)
		return
	}
	resultFields := []ui.Field{
		ui.Field{Label: "协议", Value: "HTTPS"},
		ui.Field{Label: "客户端认证", Value: "RequireAndVerifyClientCert"},
		ui.Field{Label: "访问要求", Value: "Web/API 请求同样需要受信任的客户端证书"},
		ui.Field{Label: "更新策略", Value: "组件更新时保留 mTLS"},
	}
	if component.name == "prometheus" {
		resultFields = append(resultFields, ui.Field{Label: "远程写入", Value: "保持独立开关状态；请在配置菜单中单独开启"})
	}
	a.ui.Card(ui.Success, component.label+" mTLS 已启用", resultFields...)
	a.ui.Pause()
}

func (a *App) disableComponentMTLS(ctx context.Context, component componentView) {
	prompt := "确认关闭 " + component.label + " mTLS 并恢复 HTTP（证书保留）"
	configuration, err := a.manager.Configuration(component.name)
	if err != nil {
		a.operationError("无法读取 "+component.label+" 配置", err)
		return
	}
	if component.name == "prometheus" && configuration.RemoteWriteEnabled {
		a.ui.Card(ui.Warning, "关闭 mTLS 后远程写入将继续开放",
			ui.Field{Label: "协议变化", Value: "HTTPS + mTLS → HTTP 明文"},
			ui.Field{Label: "风险", Value: "指标和请求内容将不再加密，请通过防火墙限制可信探针 IP"},
			ui.Field{Label: "证书", Value: "现有证书会保留，可随时重新启用 mTLS"},
		)
		prompt = "确认接受明文传输风险并关闭 Prometheus mTLS"
	}
	confirmed, err := a.ui.Confirm(prompt)
	if err != nil || !confirmed {
		return
	}
	err = a.ui.During("正在关闭 "+component.label+" mTLS", func() error {
		return a.manager.DisableMTLS(ctx, component.name)
	})
	if err != nil {
		a.operationError(component.label+" mTLS 关闭失败", err)
		return
	}
	fields := []ui.Field{
		ui.Field{Label: "当前协议", Value: "HTTP"},
		ui.Field{Label: "证书", Value: "已保留，可再次启用"},
	}
	if component.name == "prometheus" {
		remoteWriteResult := "保持关闭"
		if configuration.RemoteWriteEnabled {
			remoteWriteResult = "继续开放（HTTP，未加密）"
		}
		fields = append(fields, ui.Field{Label: "远程写入", Value: remoteWriteResult})
	}
	a.ui.Card(ui.Success, component.label+" mTLS 已关闭", fields...)
	a.ui.Pause()
}

func selectTextEditor() (string, error) {
	for _, name := range []string{"vim", "nano", "vi"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("未找到 vim、nano 或 vi，请先安装任意一个编辑器")
}

func editorValue() (string, bool) {
	editor, err := selectTextEditor()
	if err != nil {
		return "未安装", false
	}
	return filepathBase(editor), true
}

func openTextEditor(ctx context.Context, editor, path string) error {
	inputInfo, err := os.Stdin.Stat()
	if err != nil || inputInfo.Mode()&os.ModeCharDevice == 0 {
		return fmt.Errorf("当前没有可用的交互终端，无法打开 %s", filepathBase(editor))
	}
	command := exec.CommandContext(ctx, editor, path)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}

func filepathBase(path string) string {
	parts := strings.FieldsFunc(path, func(char rune) bool { return char == '/' || char == '\\' })
	if len(parts) == 0 {
		return path
	}
	return parts[len(parts)-1]
}

func (a *App) install(ctx context.Context, component componentView, version string) {
	confirmed, err := a.ui.Confirm("确认安装或更新" + component.label)
	if err != nil || !confirmed {
		return
	}
	status, err := a.installWithProgress(ctx, component, version)
	if err != nil {
		a.operationError(component.label+"部署失败", err)
		return
	}
	a.ui.Card(ui.Success, component.label+"已就绪",
		ui.Field{Label: "版本", Value: status.Version},
		ui.Field{Label: "服务", Value: serviceText(status.ServiceState)},
		ui.Field{Label: "访问", Value: a.componentAddress(component)},
	)
	a.ui.Pause()
}

func (a *App) uninstall(ctx context.Context, component componentView, purge bool) {
	prompt := "确认卸载" + component.label
	if purge {
		prompt = "确认永久删除" + component.label + "的程序、配置和数据"
	}
	confirmed, err := a.ui.Confirm(prompt)
	if err != nil || !confirmed {
		return
	}
	label := "正在卸载" + component.label
	if purge {
		label = "正在彻底清理" + component.label
	}
	err = a.ui.During(label, func() error {
		return a.manager.Uninstall(ctx, component.name, purge)
	})
	if err != nil {
		a.operationError(component.label+"卸载失败", err)
		return
	}
	detail := "配置和历史数据已保留"
	if purge {
		detail = "程序、配置和历史数据均已删除"
	}
	a.ui.Card(ui.Success, component.label+"已卸载", ui.Field{Label: "结果", Value: detail})
	a.ui.Pause()
}

func (a *App) installWithProgress(ctx context.Context, component componentView, wantedVersion string) (manager.Status, error) {
	target := wantedVersion
	if wantedVersion == "latest" {
		target = "最新稳定版"
	}
	a.ui.Blank()
	a.ui.Card(ui.Neutral, "部署"+component.label,
		ui.Field{Label: "目标版本", Value: target},
	)
	a.ui.Blank()
	status, err := a.manager.InstallWithProgress(ctx, component.name, wantedVersion, func(progress manager.InstallProgress) {
		if progress.Downloading {
			a.ui.DownloadProgress(progress.Downloaded, progress.DownloadTotal, progress.Detail, progress.DownloadDone)
			return
		}
		a.ui.Progress(progress.Step, progress.Total, progress.Message, progress.Detail)
	})
	a.ui.FinishProgressLine()
	a.ui.Blank()
	return status, err
}

func (a *App) probeMenu(ctx context.Context) error {
	for {
		a.ui.Clear()
		a.ui.Title("探针接入")
		a.ui.Card(ui.Neutral, "管理其他服务器上的探针接入配置",
			ui.Field{Label: "统一采集", Value: "Grafana Alloy", Detail: "主动向 Prometheus/Loki 推送，不加入抓取目标清单"},
			ui.Field{Label: "轻量指标", Value: "node_exporter", Detail: "由中心 Prometheus 主动抓取，需要添加目标地址"},
			ui.Field{Label: "注意", Value: "两种方案不要在同一服务器重复安装"},
		)
		a.ui.Blank()
		a.ui.Option("1", "添加探针", "接入配置")
		a.ui.Option("2", "管理当前探针", "node_exporter")
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
			if err := a.addProbeMenu(ctx); err != nil {
				return err
			}
		case "2":
			if err := a.manageProbesMenu(ctx); err != nil {
				return err
			}
		default:
			a.ui.InvalidChoice()
			a.ui.Pause()
		}
	}
}

func (a *App) addProbeMenu(ctx context.Context) error {
	for {
		a.ui.Clear()
		a.ui.Title("探针接入", "添加探针")
		a.ui.Option("1", "配置 Grafana Alloy", "主动推送")
		a.ui.Option("2", "添加 node_exporter 接入", "Prometheus 抓取")
		a.ui.ExitOption("返回探针接入")
		a.ui.Blank()
		choice, err := a.ui.Ask("请选择")
		if err != nil {
			return err
		}
		switch strings.ToLower(choice) {
		case "0", "q", "exit":
			return nil
		case "1":
			a.showAlloyAccessCard(ctx)
		case "2":
			a.addNodeExporterProbe(ctx)
		default:
			a.ui.InvalidChoice()
			a.ui.Pause()
		}
	}
}

func (a *App) showAlloyAccessCard(ctx context.Context) {
	promStatus, promErr := a.manager.Status(ctx, "prometheus")
	lokiStatus, lokiErr := a.manager.Status(ctx, "loki")
	promConfig, promConfigErr := a.manager.Configuration("prometheus")
	lokiConfig, lokiConfigErr := a.manager.Configuration("loki")
	if err := errors.Join(promErr, lokiErr, promConfigErr, lokiConfigErr); err != nil {
		a.operationError("无法读取中心端口配置", err)
		return
	}
	promAddress := "Prometheus 未安装"
	if promStatus.Installed {
		promAddress = componentAddress(promConfig.MTLSEnabled, promConfig.ListenPort)
	}
	lokiAddress := "Loki 未安装"
	if lokiStatus.Installed {
		lokiAddress = componentAddress(lokiConfig.MTLSEnabled, lokiConfig.ListenPort)
	}
	promReady := "未就绪"
	if promStatus.Installed && promConfig.RemoteWriteEnabled {
		promReady = "已就绪（HTTP 明文远程写入）"
		if promConfig.MTLSEnabled {
			promReady = "已就绪（mTLS + 远程写入）"
		}
	}
	a.ui.Card(ui.Neutral, "Grafana Alloy 统一探针安装命令",
		ui.Field{Value: "curl -fsSL https://raw.githubusercontent.com/Snail-one/MonitorKit/main/scripts/probes/alloy/install.sh | sudo bash"},
		ui.Field{Label: "Prometheus 根地址", Value: promAddress, Detail: "当前监听端口：" + portText(promConfig.ListenPort)},
		ui.Field{Label: "Prometheus 接收状态", Value: promReady},
		ui.Field{Label: "Loki 根地址", Value: lokiAddress, Detail: "当前监听端口：" + portText(lokiConfig.ListenPort)},
		ui.Field{Label: "采集内容", Value: "CPU、内存、磁盘、网络指标与 systemd journal 日志"},
		ui.Field{Label: "填写方式", Value: "将“服务器地址”替换为中心服务器实际 IP 或域名，端口使用上方数值"},
	)
	a.ui.Pause()
}

func (a *App) addNodeExporterProbe(ctx context.Context) {
	if os.Geteuid() != 0 {
		a.ui.Card(ui.Warning, "添加探针配置需要管理模式", ui.Field{Label: "运行方式", Value: "sudo monitorkit"})
		a.ui.Pause()
		return
	}
	status, err := a.manager.Status(ctx, "prometheus")
	if err != nil {
		a.operationError("无法检查 Prometheus", err)
		return
	}
	if !status.Installed {
		a.ui.Card(ui.Warning, "Prometheus 尚未安装", ui.Field{Label: "下一步", Value: "先安装 Prometheus，再添加 node_exporter 抓取目标"})
		a.ui.Pause()
		return
	}
	name, err := a.ui.Ask("探针名称，例如 web-01")
	if err != nil {
		return
	}
	host, err := a.ui.Ask("探针服务器 IP 或域名")
	if err != nil {
		return
	}
	portValue, err := a.ui.Ask("node_exporter 端口，直接回车使用 9100")
	if err != nil {
		return
	}
	port := 9100
	if portValue != "" {
		port, err = strconv.Atoi(portValue)
		if err != nil {
			a.operationError("端口格式无效", errors.New("请输入 1-65535 之间的数字"))
			return
		}
	}
	mtls, cancelled, err := a.chooseNodeExporterTransport()
	if err != nil || cancelled {
		return
	}
	serverName := ""
	if mtls {
		serverName, err = a.ui.Ask("TLS server_name，直接回车使用探针地址")
		if err != nil {
			return
		}
		if serverName == "" {
			serverName = host
		}
	}
	confirmed, err := a.ui.Confirm(fmt.Sprintf("确认添加 node_exporter 目标 %s:%d", host, port))
	if err != nil || !confirmed {
		return
	}
	var editor string
	if mtls {
		editor, err = selectTextEditor()
		if err != nil {
			a.operationError("无法配置探针 mTLS", err)
			return
		}
	}
	step := 0
	added, err := a.manager.AddNodeExporterProbe(ctx, manager.Probe{Name: name, Host: host, Port: port, MTLS: mtls, ServerName: serverName}, func(file manager.ProbeTLSFile) error {
		step++
		a.ui.Clear()
		a.ui.Title("探针接入", "node_exporter", "mTLS")
		a.ui.Card(ui.Neutral, fmt.Sprintf("第 %d/3 步 · %s", step, file.Label),
			ui.Field{Label: "文件", Value: file.Path},
			ui.Field{Label: "填写内容", Value: file.Description},
			ui.Field{Label: "编辑器", Value: filepathBase(editor)},
		)
		if err := a.ui.Wait("确认填写要求后，按回车打开 " + filepathBase(editor)); err != nil {
			return err
		}
		return openTextEditor(ctx, editor, file.Path)
	})
	if err != nil {
		a.operationError("node_exporter 接入配置未添加", err)
		return
	}
	mode, installArgument := "HTTP", "http"
	if added.MTLS {
		mode, installArgument = "HTTPS + mTLS", "mtls"
	}
	a.ui.Card(ui.Success, "node_exporter 接入配置已添加",
		ui.Field{Label: "名称", Value: added.Name},
		ui.Field{Label: "抓取目标", Value: probeDisplayTarget(added)},
		ui.Field{Label: "连接方式", Value: mode},
		ui.Field{Label: "远程服务器安装命令", Value: "curl -fsSL https://raw.githubusercontent.com/Snail-one/MonitorKit/main/scripts/probes/node_exporter/install.sh | sudo bash -s -- " + installArgument},
		ui.Field{Label: "应用结果", Value: "Prometheus 配置已校验并重新加载"},
	)
	a.ui.Pause()
}

func (a *App) chooseNodeExporterTransport() (mtls, cancelled bool, err error) {
	a.ui.Clear()
	a.ui.Title("探针接入", "node_exporter", "连接方式")
	a.ui.Option("1", "HTTPS + mTLS", "推荐")
	a.ui.Option("2", "HTTP", "无认证")
	a.ui.ExitOption("取消添加")
	a.ui.Blank()
	choice, err := a.ui.Ask("请选择")
	if err != nil {
		return false, false, err
	}
	switch strings.ToLower(choice) {
	case "1":
		return true, false, nil
	case "2":
		return false, false, nil
	case "0", "q", "exit":
		return false, true, nil
	default:
		a.ui.InvalidChoice()
		a.ui.Pause()
		return false, true, nil
	}
}

func (a *App) manageProbesMenu(ctx context.Context) error {
	if os.Geteuid() != 0 {
		a.ui.Card(ui.Warning, "管理探针配置需要管理模式", ui.Field{Label: "运行方式", Value: "sudo monitorkit"})
		a.ui.Pause()
		return nil
	}
	for {
		probes, err := a.manager.ListProbes()
		if err != nil {
			return err
		}
		a.ui.Clear()
		a.ui.Title("探针接入", "管理当前探针")
		if len(probes) == 0 {
			a.ui.Card(ui.Neutral, "暂无受管 node_exporter 接入配置",
				ui.Field{Label: "添加方式", Value: "返回后选择“添加探针”"},
				ui.Field{Label: "Alloy", Value: "主动推送，不会出现在 Prometheus 抓取目标清单"},
			)
			a.ui.Pause()
			return nil
		}
		for index, probe := range probes {
			a.ui.OptionLive(strconv.Itoa(index+1), probe.Name+" · "+probeDisplayTarget(probe), probeEnabledText(probe.Enabled), probe.Enabled)
		}
		a.ui.ExitOption("返回探针接入")
		a.ui.Blank()
		choice, err := a.ui.Ask("选择要管理的探针")
		if err != nil {
			return err
		}
		if choice == "0" || strings.EqualFold(choice, "q") || strings.EqualFold(choice, "exit") {
			return nil
		}
		index, err := strconv.Atoi(choice)
		if err != nil || index < 1 || index > len(probes) {
			a.ui.InvalidChoice()
			a.ui.Pause()
			continue
		}
		if err := a.manageProbeConfig(ctx, probes[index-1]); err != nil {
			return err
		}
	}
}

func (a *App) manageProbeConfig(ctx context.Context, probe manager.Probe) error {
	for {
		probes, err := a.manager.ListProbes()
		if err != nil {
			return err
		}
		found := false
		for _, current := range probes {
			if current.ID == probe.ID {
				probe, found = current, true
				break
			}
		}
		if !found {
			return nil
		}
		a.ui.Clear()
		a.ui.Title("探针接入", "管理当前探针", probe.Name)
		transport := "HTTP"
		if probe.MTLS {
			transport = "HTTPS + mTLS"
		}
		a.ui.Card(ui.Neutral, "node_exporter 接入配置",
			ui.Field{Label: "名称", Value: probe.Name},
			ui.Field{Label: "抓取目标", Value: probeDisplayTarget(probe)},
			ui.Field{Label: "连接方式", Value: transport},
			ui.Field{Label: "配置状态", Value: enabledText(probe.Enabled)},
			ui.Field{Label: "配置来源", Value: "/etc/prometheus/probes/inventory.json"},
		)
		a.ui.Blank()
		a.ui.Option("1", "修改名称、地址和端口", "")
		toggleLabel := "停用抓取"
		if !probe.Enabled {
			toggleLabel = "启用抓取"
		}
		a.ui.Option("2", toggleLabel, "")
		if probe.MTLS {
			a.ui.Option("3", "更新 mTLS 客户端证书", "3 个 PEM 文件")
		}
		a.ui.Option("4", "删除接入配置", "不卸载远程探针")
		a.ui.ExitOption("返回探针列表")
		a.ui.Blank()
		choice, err := a.ui.Ask("请选择")
		if err != nil {
			return err
		}
		switch strings.ToLower(choice) {
		case "0", "q", "exit":
			return nil
		case "1":
			a.updateProbeEndpoint(ctx, probe)
		case "2":
			a.setProbeEnabled(ctx, probe, !probe.Enabled)
		case "3":
			if probe.MTLS {
				a.updateProbeTLS(ctx, probe)
			} else {
				a.ui.InvalidChoice()
				a.ui.Pause()
			}
		case "4":
			if a.deleteProbe(ctx, probe) {
				return nil
			}
		default:
			a.ui.InvalidChoice()
			a.ui.Pause()
		}
	}
}

func (a *App) updateProbeEndpoint(ctx context.Context, probe manager.Probe) {
	name, err := a.ui.Ask("探针名称，直接回车保持 " + probe.Name)
	if err != nil {
		return
	}
	if name == "" {
		name = probe.Name
	}
	host, err := a.ui.Ask("探针地址，直接回车保持 " + probe.Host)
	if err != nil {
		return
	}
	if host == "" {
		host = probe.Host
	}
	portValue, err := a.ui.Ask("端口，直接回车保持 " + strconv.Itoa(probe.Port))
	if err != nil {
		return
	}
	port := probe.Port
	if portValue != "" {
		port, err = strconv.Atoi(portValue)
		if err != nil {
			a.operationError("端口格式无效", errors.New("请输入 1-65535 之间的数字"))
			return
		}
	}
	serverName := probe.ServerName
	if probe.MTLS {
		value, err := a.ui.Ask("TLS server_name，直接回车保持 " + valueOr(serverName, probe.Host))
		if err != nil {
			return
		}
		if value != "" {
			serverName = value
		}
	}
	if err := a.manager.UpdateProbe(ctx, probe.ID, name, host, port, serverName); err != nil {
		a.operationError("探针接入配置修改失败", err)
		return
	}
	a.ui.Card(ui.Success, "探针接入配置已修改",
		ui.Field{Label: "目标", Value: net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(port))},
		ui.Field{Label: "Prometheus", Value: "配置已校验并重新加载"},
	)
	a.ui.Pause()
}

func (a *App) setProbeEnabled(ctx context.Context, probe manager.Probe, enabled bool) {
	action := "停用"
	if enabled {
		action = "启用"
	}
	confirmed, err := a.ui.Confirm("确认" + action + "探针 " + probe.Name)
	if err != nil || !confirmed {
		return
	}
	if err := a.manager.SetProbeEnabled(ctx, probe.ID, enabled); err != nil {
		a.operationError(action+"探针失败", err)
		return
	}
	a.ui.Card(ui.Success, "探针已"+action, ui.Field{Label: "Prometheus", Value: "配置已校验并重新加载"})
	a.ui.Pause()
}

func (a *App) updateProbeTLS(ctx context.Context, probe manager.Probe) {
	editor, err := selectTextEditor()
	if err != nil {
		a.operationError("无法更新探针证书", err)
		return
	}
	confirmed, err := a.ui.Confirm("确认按 CA 证书 → 完整客户端证书 → 私钥的顺序更新 3 个 Prometheus 抓取 PEM 文件")
	if err != nil || !confirmed {
		return
	}
	step := 0
	err = a.manager.ConfigureProbeTLS(ctx, probe.ID, func(file manager.ProbeTLSFile) error {
		step++
		a.ui.Clear()
		a.ui.Title("探针接入", probe.Name, "更新 mTLS")
		a.ui.Card(ui.Neutral, fmt.Sprintf("第 %d/3 步 · %s", step, file.Label),
			ui.Field{Label: "文件", Value: file.Path},
			ui.Field{Label: "填写内容", Value: file.Description},
		)
		if err := a.ui.Wait("确认填写要求后，按回车打开 " + filepathBase(editor)); err != nil {
			return err
		}
		return openTextEditor(ctx, editor, file.Path)
	})
	if err != nil {
		a.operationError("探针 mTLS 证书更新失败", err)
		return
	}
	a.ui.Card(ui.Success, "探针 mTLS 证书已更新", ui.Field{Label: "校验", Value: "CA、客户端证书和私钥均通过"})
	a.ui.Pause()
}

func (a *App) deleteProbe(ctx context.Context, probe manager.Probe) bool {
	confirmed, err := a.ui.Confirm("确认从 Prometheus 删除探针接入配置 " + probe.Name)
	if err != nil || !confirmed {
		return false
	}
	if err := a.manager.DeleteProbe(ctx, probe.ID); err != nil {
		a.operationError("删除探针接入配置失败", err)
		return false
	}
	a.ui.Card(ui.Success, "探针接入配置已删除",
		ui.Field{Label: "远程服务器", Value: "未执行卸载，node_exporter 仍保持原状态"},
		ui.Field{Label: "Prometheus", Value: "已停止抓取该目标"},
	)
	a.ui.Pause()
	return true
}

func probeDisplayTarget(probe manager.Probe) string {
	scheme := "http"
	if probe.MTLS {
		scheme = "https"
	}
	return scheme + "://" + net.JoinHostPort(strings.Trim(probe.Host, "[]"), strconv.Itoa(probe.Port)) + "/metrics"
}

func (a *App) statuses(ctx context.Context) (map[string]manager.Status, error) {
	statuses := make(map[string]manager.Status, len(manager.ComponentNames()))
	for _, name := range manager.ComponentNames() {
		status, err := a.manager.Status(ctx, name)
		if err != nil {
			return nil, err
		}
		statuses[name] = status
	}
	return statuses, nil
}

func statusHint(status manager.Status) string {
	if !status.Installed {
		return "未安装"
	}
	if status.ServiceState == "active" {
		return "运行中"
	}
	if status.ServiceState == "staged" {
		return "待启用"
	}
	return "已停止"
}

func statusActive(status manager.Status) bool {
	return status.Installed && status.ServiceState == "active"
}

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
