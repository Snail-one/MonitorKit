package app

import (
	"context"
	"strings"

	"github.com/Snail-one/MonitorKit/internal/manager"
	"github.com/Snail-one/MonitorKit/internal/ui"
)

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
		ui.Field{Label: "服务", Value: serviceText(status.ServiceState), Detail: "安装或更新不会自动启动"},
		ui.Field{Label: "下一步", Value: "请到服务管理中手动启动或重启"},
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
