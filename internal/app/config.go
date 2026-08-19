package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/Snail-one/MonitorKit/internal/manager"
	"github.com/Snail-one/MonitorKit/internal/ui"
)

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
		a.ui.Option("9", "重置配置", "恢复程序默认主配置")
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
		case "9":
			a.resetComponentConfig(ctx, component)
		default:
			a.ui.InvalidChoice()
			a.ui.Pause()
		}
	}
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

func (a *App) resetComponentConfig(ctx context.Context, component componentView) {
	a.ui.Clear()
	a.ui.Title(component.label, "配置管理", "重置配置")
	fields := []ui.Field{
		{Label: "将覆盖", Value: component.configPath + " 与 systemd unit"},
		{Label: "生成方式", Value: "与当前程序首次安装相同的默认配置模板"},
		{Label: "保留", Value: "监听端口、gRPC 端口、mTLS 证书、远程写入开关、存储设置文件、探针清单、历史数据"},
		{Label: "恢复默认", Value: "主配置里的手工修改；Loki 保留期回到 30 天，摄入限制回到上游默认"},
	}
	if component.name == "prometheus" {
		fields = append(fields, ui.Field{Label: "探针", Value: "受管 node_exporter 接入会写回新配置"})
	}
	a.ui.Card(ui.Warning, "用程序内置默认配置替换当前主配置", fields...)
	a.ui.Blank()
	confirmed, err := a.ui.Confirm("确认重置" + component.label + "配置")
	if err != nil || !confirmed {
		return
	}
	err = a.ui.During("正在重置"+component.label+"配置", func() error {
		return a.manager.ResetConfig(ctx, component.name)
	})
	if err != nil {
		a.operationError(component.label+"配置重置失败", err)
		return
	}
	a.ui.Card(ui.Success, component.label+"配置已重置为程序默认",
		ui.Field{Label: "配置文件", Value: component.configPath},
		ui.Field{Label: "提示", Value: "服务若未运行，请到服务管理中启动或重启"},
	)
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
