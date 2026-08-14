// Package app coordinates MonitorKit's interactive terminal experience.
package app

import (
	"context"
	"fmt"
	"os"
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
		a.ui.Title(component.label)
		a.ui.Card(ui.Neutral, component.description,
			ui.Field{Label: "安装状态", Value: installedText(status)},
			ui.Field{Label: "服务状态", Value: serviceText(status.ServiceState)},
			ui.Field{Label: "当前版本", Value: valueOr(status.Version, "—")},
			ui.Field{Label: "配置文件", Value: component.configPath},
			ui.Field{Label: "服务地址", Value: component.address},
		)
		a.ui.Blank()
		a.ui.Option("1", "安装或更新", a.ui.Badge("最新稳定版", true))
		a.ui.Option("2", "安装指定版本", "")
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
			a.uninstall(ctx, component, false)
		case "4":
			a.uninstall(ctx, component, true)
		default:
			a.ui.InvalidChoice()
			a.ui.Pause()
		}
	}
}

func (a *App) install(ctx context.Context, component componentView, version string) {
	confirmed, err := a.ui.Confirm("确认安装或更新" + component.label)
	if err != nil || !confirmed {
		return
	}
	var status manager.Status
	err = a.ui.During("正在部署"+component.label, func() error {
		var operationErr error
		status, operationErr = a.manager.Install(ctx, component.name, version)
		return operationErr
	})
	if err != nil {
		a.operationError(component.label+"部署失败", err)
		return
	}
	a.ui.Card(ui.Success, component.label+"已就绪",
		ui.Field{Label: "版本", Value: status.Version},
		ui.Field{Label: "服务", Value: serviceText(status.ServiceState)},
		ui.Field{Label: "访问", Value: component.address},
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
		err := a.ui.During("正在部署"+component.label, func() error {
			_, operationErr := a.manager.Install(ctx, name, "latest")
			return operationErr
		})
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
