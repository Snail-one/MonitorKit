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
		a.ui.Option("1", componentViews["prometheus"].label, a.statusBadge(statuses["prometheus"]))
		a.ui.Option("2", componentViews["loki"].label, a.statusBadge(statuses["loki"]))
		a.ui.Option("3", "探针接入", a.ui.Badge("Shell", true))
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
			ui.Field{Label: "服务地址", Value: componentAddress(configuration.MTLSEnabled, configuration.ListenPort)},
		}
		if component.name == "prometheus" {
			fields = append(fields, ui.Field{Label: "远程写入", Value: enabledText(configuration.RemoteWriteEnabled), Detail: "接收地址：/api/v1/write；mTLS 推荐，HTTP 需确认风险"})
		}
		fields = append(fields, ui.Field{Label: "传输安全", Value: transport})
		a.ui.Card(ui.Neutral, component.description, fields...)
		a.ui.Blank()
		configBadge := a.ui.Badge("请先安装", false)
		if status.Installed {
			configBadge = a.ui.Badge("编辑/校验/mTLS", true)
		}
		a.ui.Option("1", "配置管理", configBadge)
		a.ui.Option("2", "安装或更新", a.ui.Badge("最新/指定版本", true))
		a.ui.Option("3", "卸载程序", a.ui.Badge("保留数据", true))
		a.ui.Option("4", "彻底清理", a.ui.Badge("删除数据", false))
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
		a.ui.Option("1", "安装最新稳定版", a.ui.Badge("推荐", true))
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
			{Label: "mTLS", Value: mtlsStatus},
			{Label: "证书目录", Value: configuration.TLSDir},
		}
		if component.name == "prometheus" {
			configFields = append(configFields, ui.Field{Label: "远程写入接收", Value: enabledText(configuration.RemoteWriteEnabled), Detail: "独立开关；HTTP 模式开启时会显示明文传输警告"})
		}
		a.ui.Card(ui.Neutral, component.label+"配置", configFields...)
		a.ui.Blank()
		a.ui.Option("1", "编辑主配置", a.ui.Badge("vim/nano/vi", true))
		a.ui.Option("2", "校验当前配置", "")
		a.ui.Option("3", "重启并应用配置", "")
		a.ui.Option("4", "修改监听端口", a.ui.Badge("当前 "+portText(configuration.ListenPort), true))
		a.ui.Option("5", "配置或更新 mTLS", a.ui.Badge("证书校验", true))
		if component.name == "prometheus" {
			remoteWriteLabel := "开启远程写入"
			remoteWriteBadge := a.ui.Badge("已关闭", false)
			if configuration.RemoteWriteEnabled {
				remoteWriteLabel = "关闭远程写入"
				remoteWriteBadge = a.ui.Badge("已开启", true)
			}
			a.ui.Option("6", remoteWriteLabel, remoteWriteBadge)
		}
		if configuration.MTLSEnabled && component.name == "prometheus" {
			badge := "保留证书"
			if configuration.RemoteWriteEnabled {
				badge = "远程写入转为 HTTP"
			}
			a.ui.Option("7", "关闭 mTLS", a.ui.Badge(badge, false))
		} else if configuration.MTLSEnabled {
			a.ui.Option("6", "关闭 mTLS", a.ui.Badge("保留证书", false))
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
			if component.name == "prometheus" {
				a.toggleRemoteWrite(ctx, component, configuration)
			} else if configuration.MTLSEnabled {
				a.disableComponentMTLS(ctx, component)
			} else {
				a.ui.InvalidChoice()
				a.ui.Pause()
			}
		case "7":
			if component.name == "prometheus" && configuration.MTLSEnabled {
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
			Label:  "1. 服务端证书",
			Value:  tlsDir + "/server.crt",
			Detail: "填写完整证书链；必须包含 BEGIN/END CERTIFICATE，SAN 包含探针访问时使用的域名或 IP",
		},
		ui.Field{
			Label:  "2. 服务端私钥",
			Value:  tlsDir + "/server.key",
			Detail: "填写与 server.crt 匹配且未加密的完整 PEM 私钥；不要填写 CA 私钥",
		},
		ui.Field{
			Label:  "3. 客户端根 CA",
			Value:  tlsDir + "/client-ca.crt",
			Detail: "填写签发 Alloy 客户端证书的 CA 公共证书；不要填写 Alloy 客户端证书或任何私钥",
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
		a.ui.Option("1", "添加探针", a.ui.Badge("接入配置", true))
		a.ui.Option("2", "管理当前探针", a.ui.Badge("node_exporter", true))
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
		a.ui.Option("1", "配置 Grafana Alloy", a.ui.Badge("主动推送", true))
		a.ui.Option("2", "添加 node_exporter 接入", a.ui.Badge("Prometheus 抓取", true))
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
	a.ui.Option("1", "HTTPS + mTLS", a.ui.Badge("推荐", true))
	a.ui.Option("2", "HTTP", a.ui.Badge("无认证", false))
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
			badge := a.ui.Badge("已停用", false)
			if probe.Enabled {
				badge = a.ui.Badge("已启用", true)
			}
			a.ui.Option(strconv.Itoa(index+1), probe.Name+" · "+probeDisplayTarget(probe), badge)
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
			a.ui.Option("3", "更新 mTLS 客户端证书", a.ui.Badge("3 个 PEM 文件", true))
		}
		a.ui.Option("4", "删除接入配置", a.ui.Badge("不卸载远程探针", false))
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
	confirmed, err := a.ui.Confirm("确认依次更新 3 个 Prometheus 抓取客户端 PEM 文件")
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

func (a *App) statusBadge(status manager.Status) string {
	if !status.Installed {
		return a.ui.Badge("未安装", false)
	}
	if status.ServiceState == "active" {
		return a.ui.Badge("运行中", true)
	}
	if status.ServiceState == "staged" {
		return a.ui.Badge("待启用", false)
	}
	return a.ui.Badge("已停止", false)
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

func enabledText(enabled bool) string {
	if enabled {
		return "已开启"
	}
	return "已关闭"
}
