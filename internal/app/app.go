// Package app coordinates MonitorKit's interactive terminal experience.
package app

import (
	"context"
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
