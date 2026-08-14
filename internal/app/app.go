// Package app coordinates MonitorKit's interactive terminal experience.
package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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
	address     string
	configPath  string
}

var componentViews = map[string]componentView{
	"prometheus": {
		name:        "prometheus",
		label:       "指标中心",
		description: "汇聚并保存服务器性能指标",
		address:     "http://服务器地址:9090",
		configPath:  "/etc/prometheus/prometheus.yml",
	},
	"loki": {
		name:        "loki",
		label:       "日志中心",
		description: "接收并保存探针发送的系统日志",
		address:     "http://服务器地址:3100",
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
		ready := readyCount(statuses)
		a.ui.Option("1", "部署监控栈", a.ui.Badge(fmt.Sprintf("就绪 %d/2", ready), ready == 2))
		a.ui.Option("2", "指标中心", a.statusBadge(statuses["prometheus"]))
		a.ui.Option("3", "日志中心", a.statusBadge(statuses["loki"]))
		a.ui.Option("4", "探针接入", a.ui.Badge("Shell", true))
		a.ui.Option("5", "管理接口", a.ui.Badge("HTTP API", true))
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
			a.deployStack(ctx)
		case "2":
			if err := a.componentMenu(ctx, componentViews["prometheus"]); err != nil {
				return err
			}
		case "3":
			if err := a.componentMenu(ctx, componentViews["loki"]); err != nil {
				return err
			}
		case "4":
			if err := a.probeMenu(); err != nil {
				return err
			}
		case "5":
			a.apiInfo()
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
		a.ui.Card(ui.Neutral, component.description,
			ui.Field{Label: "安装状态", Value: installedText(status)},
			ui.Field{Label: "服务状态", Value: serviceText(status.ServiceState)},
			ui.Field{Label: "当前版本", Value: valueOr(status.Version, "—")},
			ui.Field{Label: "配置文件", Value: component.configPath},
			ui.Field{Label: "服务地址", Value: componentAddress(component, configuration.MTLSEnabled)},
			ui.Field{Label: "传输安全", Value: transport},
		)
		a.ui.Blank()
		a.ui.Option("1", "安装或更新", a.ui.Badge("最新稳定版", true))
		a.ui.Option("2", "安装指定版本", "")
		configBadge := a.ui.Badge("先安装", false)
		if status.Installed {
			configBadge = a.ui.Badge("编辑/校验/mTLS", true)
		}
		a.ui.Option("3", "配置管理", configBadge)
		a.ui.Option("4", "卸载程序", a.ui.Badge("保留数据", true))
		a.ui.Option("5", "彻底清理", a.ui.Badge("删除数据", false))
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
			a.install(ctx, component, "latest")
		case "2":
			version, err := a.ui.Ask("输入版本号，例如 1.2.3")
			if err != nil {
				return err
			}
			if version == "" {
				a.ui.InvalidChoice()
				a.ui.Pause()
				continue
			}
			a.install(ctx, component, version)
		case "3":
			if !status.Installed {
				a.ui.Card(ui.Warning, component.label+"尚未安装",
					ui.Field{Label: "下一步", Value: "请先选择安装或更新，再进入配置管理"},
				)
				a.ui.Pause()
				continue
			}
			if err := a.configurationMenu(ctx, component); err != nil {
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
		a.ui.Card(ui.Neutral, component.label+"配置",
			ui.Field{Label: "主配置", Value: configuration.Path},
			ui.Field{Label: "mTLS", Value: mtlsStatus},
			ui.Field{Label: "证书目录", Value: configuration.TLSDir},
		)
		a.ui.Blank()
		a.ui.Option("1", "编辑主配置", a.ui.Badge("vim/nano/vi", true))
		a.ui.Option("2", "校验当前配置", "")
		a.ui.Option("3", "重启并应用配置", "")
		a.ui.Option("4", "配置或更新 mTLS", a.ui.Badge("证书校验", true))
		if configuration.MTLSEnabled {
			a.ui.Option("5", "关闭 mTLS", a.ui.Badge("保留证书", false))
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
			a.configureComponentMTLS(ctx, component)
		case "5":
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
	confirmed, err := a.ui.Confirm("确认配置 mTLS 并依次编辑服务端证书、私钥和客户端 CA")
	if err != nil || !confirmed {
		return
	}
	err = a.manager.ConfigureMTLS(ctx, component.name, func(file manager.TLSFile) error {
		a.ui.Clear()
		a.ui.Title(component.label, "mTLS")
		a.ui.Card(ui.Neutral, "编辑"+file.Label,
			ui.Field{Label: "文件", Value: file.Path},
			ui.Field{Label: "要求", Value: file.Description},
			ui.Field{Label: "编辑器", Value: filepathBase(editor)},
		)
		return openTextEditor(ctx, editor, file.Path)
	})
	if err != nil {
		a.operationError(component.label+" mTLS 配置失败", err)
		return
	}
	a.ui.Card(ui.Success, component.label+" mTLS 已启用",
		ui.Field{Label: "协议", Value: "HTTPS"},
		ui.Field{Label: "客户端认证", Value: "RequireAndVerifyClientCert"},
		ui.Field{Label: "访问要求", Value: "Web/API 请求同样需要受信任的客户端证书"},
		ui.Field{Label: "更新策略", Value: "组件更新时保留 mTLS"},
	)
	a.ui.Pause()
}

func (a *App) disableComponentMTLS(ctx context.Context, component componentView) {
	confirmed, err := a.ui.Confirm("确认关闭 " + component.label + " mTLS 并恢复 HTTP（证书保留）")
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
	a.ui.Card(ui.Success, component.label+" mTLS 已关闭",
		ui.Field{Label: "当前协议", Value: "HTTP"},
		ui.Field{Label: "证书", Value: "已保留，可再次启用"},
	)
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

func (a *App) deployStack(ctx context.Context) {
	confirmed, err := a.ui.Confirm("确认部署 Prometheus 和 Loki")
	if err != nil || !confirmed {
		return
	}
	installed := make([]string, 0, 2)
	for _, name := range manager.ComponentNames() {
		component := componentViews[name]
		_, err := a.installWithProgress(ctx, component, "latest")
		if err != nil {
			a.operationError("监控栈部署未完成", fmt.Errorf("%s：%w", component.label, err))
			return
		}
		installed = append(installed, component.label)
	}
	a.ui.Card(ui.Success, "监控中心部署完成",
		ui.Field{Label: "已启用", Value: strings.Join(installed, "、")},
		ui.Field{Label: "下一步", Value: "在目标服务器安装指标与日志探针"},
	)
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

func (a *App) probeMenu() error {
	for {
		a.ui.Clear()
		a.ui.Title("探针接入")
		a.ui.Card(ui.Neutral, "根据项目需求选择一种 Shell 探针方案",
			ui.Field{Label: "轻量指标", Value: "node_exporter", Detail: "只采集主机指标，适合不需要日志的项目"},
			ui.Field{Label: "统一采集", Value: "Grafana Alloy", Detail: "同时发送主机指标和系统日志，无需再装 node_exporter"},
			ui.Field{Label: "注意", Value: "两种方案不要在同一服务器重复安装"},
		)
		a.ui.Blank()
		a.ui.Option("1", "轻量指标探针", a.ui.Badge("node_exporter", true))
		a.ui.Option("2", "指标与日志统一探针", a.ui.Badge("Alloy", true))
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
			a.ui.Card(ui.Neutral, "node_exporter 安装命令",
				ui.Field{Value: "curl -fsSL https://raw.githubusercontent.com/Snail-one/MonitorKit/main/scripts/probes/node_exporter/install.sh | sudo bash"},
			)
			a.ui.Pause()
		case "2":
			a.ui.Card(ui.Neutral, "Grafana Alloy 统一探针安装命令",
				ui.Field{Value: "curl -fsSL https://raw.githubusercontent.com/Snail-one/MonitorKit/main/scripts/probes/alloy/install.sh | sudo PROMETHEUS_URL=http://中心服务器:9090 LOKI_URL=http://中心服务器:3100 bash"},
				ui.Field{Label: "采集内容", Value: "CPU、内存、磁盘、网络指标与 systemd journal 日志"},
			)
			a.ui.Pause()
		default:
			a.ui.InvalidChoice()
			a.ui.Pause()
		}
	}
}

func (a *App) apiInfo() {
	listen := valueOr(os.Getenv("MONITORKIT_LISTEN"), "127.0.0.1:8088")
	auth := "仅本机访问，可不配置 Token"
	if os.Getenv("MONITORKIT_TOKEN") != "" {
		auth = "Bearer Token 已配置"
	}
	a.ui.Clear()
	a.ui.Title("管理接口")
	a.ui.Card(ui.Neutral, "自动化管理入口",
		ui.Field{Label: "启动命令", Value: "sudo monitorkit serve"},
		ui.Field{Label: "监听地址", Value: listen},
		ui.Field{Label: "访问控制", Value: auth},
		ui.Field{Label: "组件接口", Value: "/api/v1/components"},
		ui.Field{Label: "健康检查", Value: "/healthz"},
	)
	a.ui.Pause()
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

func readyCount(statuses map[string]manager.Status) int {
	ready := 0
	for _, status := range statuses {
		if status.Installed && (status.ServiceState == "active" || status.ServiceState == "staged") {
			ready++
		}
	}
	return ready
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
		return component.address
	}
	return componentAddress(component, configuration.MTLSEnabled)
}

func componentAddress(component componentView, mtls bool) string {
	if mtls {
		return strings.Replace(component.address, "http://", "https://", 1)
	}
	return component.address
}
