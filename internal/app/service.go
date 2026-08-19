package app

import (
	"context"
	"strings"

	"github.com/Snail-one/MonitorKit/internal/ui"
)

func (a *App) serviceMenu(ctx context.Context, component componentView) error {
	for {
		status, err := a.manager.Status(ctx, component.name)
		if err != nil {
			return err
		}
		a.ui.Clear()
		a.ui.Title(component.label, "服务管理")
		a.ui.Card(ui.Neutral, "手动控制 systemd 服务",
			ui.Field{Label: "安装状态", Value: installedText(status)},
			ui.Field{Label: "服务状态", Value: serviceText(status.ServiceState)},
			ui.Field{Label: "开机自启", Value: bootEnabledText(status.BootEnabled)},
			ui.Field{Label: "说明", Value: "启动会立即运行并开启开机自启；停止会关掉进程并取消开机自启"},
		)
		a.ui.Blank()
		a.ui.OptionLive("1", "启动服务", serviceText(status.ServiceState), status.ServiceState == "active")
		a.ui.OptionLive("2", "停止服务", bootEnabledText(status.BootEnabled), status.BootEnabled)
		a.ui.Option("3", "重启服务", "先校验配置")
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
			a.controlComponentService(ctx, component, "start")
		case "2":
			a.controlComponentService(ctx, component, "stop")
		case "3":
			a.controlComponentService(ctx, component, "restart")
		default:
			a.ui.InvalidChoice()
			a.ui.Pause()
		}
	}
}

func (a *App) controlComponentService(ctx context.Context, component componentView, action string) {
	var (
		prompt  string
		working string
		failed  string
		done    string
		result  string
		run     func() error
	)
	switch action {
	case "start":
		prompt = "确认启动" + component.label + "并开启开机自启"
		working = "正在启动" + component.label
		failed = component.label + "启动失败"
		done = component.label + "已启动"
		result = "运行中，已开启开机自启"
		run = func() error { return a.manager.Start(ctx, component.name) }
	case "stop":
		prompt = "确认停止" + component.label + "并关闭开机自启"
		working = "正在停止" + component.label
		failed = component.label + "停止失败"
		done = component.label + "已停止"
		result = "已停止，已关闭开机自启"
		run = func() error { return a.manager.Stop(ctx, component.name) }
	default:
		prompt = "确认校验配置并重启" + component.label
		working = "正在重启" + component.label
		failed = component.label + "重启失败"
		done = component.label + "已重启"
		result = "运行中"
		run = func() error { return a.manager.Restart(ctx, component.name) }
	}
	confirmed, err := a.ui.Confirm(prompt)
	if err != nil || !confirmed {
		return
	}
	err = a.ui.During(working, run)
	if err != nil {
		a.operationError(failed, err)
		return
	}
	a.ui.Card(ui.Success, done, ui.Field{Label: "服务", Value: result})
	a.ui.Pause()
}
