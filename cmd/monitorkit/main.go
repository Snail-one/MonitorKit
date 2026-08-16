package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/Snail-one/MonitorKit/internal/app"
	"github.com/Snail-one/MonitorKit/internal/manager"
	"github.com/Snail-one/MonitorKit/internal/selfupdate"
	"github.com/Snail-one/MonitorKit/internal/ui"
	"github.com/Snail-one/MonitorKit/internal/version"
)

const usage = `MonitorKit 中心端管理程序

除 --version 和 --help 外，启动时必须具有 root 权限。

用法：
  sudo monitorkit                                    # 交互式管理界面
  monitorkit --version
  sudo monitorkit update                             # 更新管理程序自身
  sudo monitorkit uninstall                          # 卸载管理程序自身
  sudo monitorkit install <prometheus|loki> [--version latest]
  sudo monitorkit uninstall <prometheus|loki> [--purge]
  sudo monitorkit start <prometheus|loki>
  sudo monitorkit stop <prometheus|loki>
  sudo monitorkit disable <prometheus|loki>
  sudo monitorkit restart <prometheus|loki>
  sudo monitorkit reset-config <prometheus|loki>
  sudo monitorkit status [prometheus|loki]

环境变量：
  MONITORKIT_ROOT    安装根目录，默认 /（主要用于测试或离线打包）
  GITHUB_TOKEN       可选，提升 GitHub API 请求限额
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("操作失败", "error", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "--version", "-v", "version":
			fmt.Println(version.Info())
			return nil
		case "help", "-h", "--help":
			fmt.Print(usage)
			return nil
		}
	}
	if err := requireRoot(); err != nil {
		return err
	}

	if len(args) > 0 {
		switch args[0] {
		case "update":
			fmt.Printf("安装脚本：%s\n", selfupdate.InstallScriptURL)
			return selfupdate.Run(context.Background())
		case "uninstall":
			if len(args) == 1 {
				fmt.Printf("卸载脚本：%s\n", selfupdate.InstallScriptURL)
				return selfupdate.Run(context.Background(), "--uninstall")
			}
		}
	}

	root := envOr("MONITORKIT_ROOT", "/")
	mgr, err := manager.New(root)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		return app.New(mgr, ui.New(os.Stdin, os.Stdout)).Run(context.Background())
	}

	switch args[0] {
	case "install":
		fs := flag.NewFlagSet("install", flag.ContinueOnError)
		version := fs.String("version", "latest", "安装版本（latest 或 x.y.z）")
		component, rest, err := componentArg(args[1:])
		if err != nil {
			return err
		}
		if err := fs.Parse(rest); err != nil {
			return err
		}
		result, err := mgr.Install(context.Background(), component, *version)
		if err != nil {
			return err
		}
		if result.GRPCListenPort > 0 {
			fmt.Printf("%s %s 安装完成，服务未启动，状态：%s，监听端口：%d，gRPC 端口：%d\n", result.Name, result.Version, result.ServiceState, result.ListenPort, result.GRPCListenPort)
			return nil
		}
		fmt.Printf("%s %s 安装完成，服务未启动，状态：%s，监听端口：%d\n", result.Name, result.Version, result.ServiceState, result.ListenPort)
		return nil
	case "start", "stop", "disable", "restart":
		component, _, err := componentArg(args[1:])
		if err != nil {
			return err
		}
		switch args[0] {
		case "start":
			err = mgr.Start(context.Background(), component)
		case "stop":
			err = mgr.Stop(context.Background(), component)
		case "disable":
			err = mgr.DisableBoot(context.Background(), component)
		default:
			err = mgr.Restart(context.Background(), component)
		}
		if err != nil {
			return err
		}
		fmt.Printf("%s 已%s\n", component, map[string]string{"start": "启动并开启开机自启", "stop": "停止", "disable": "关闭开机自启", "restart": "重启"}[args[0]])
		return nil
	case "reset-config":
		component, _, err := componentArg(args[1:])
		if err != nil {
			return err
		}
		if err := mgr.ResetConfig(context.Background(), component); err != nil {
			return err
		}
		fmt.Printf("%s 配置已重置为程序默认\n", component)
		return nil
	case "uninstall":
		fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
		purge := fs.Bool("purge", false, "同时删除配置和数据")
		component, rest, err := componentArg(args[1:])
		if err != nil {
			return err
		}
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if err := mgr.Uninstall(context.Background(), component, *purge); err != nil {
			return err
		}
		fmt.Printf("%s 已卸载（清理数据：%t）\n", component, *purge)
		return nil
	case "status":
		if len(args) > 2 {
			return fmt.Errorf("status 最多接受一个组件名")
		}
		names := manager.ComponentNames()
		if len(args) == 2 {
			names = []string{args[1]}
		}
		for _, name := range names {
			status, err := mgr.Status(context.Background(), name)
			if err != nil {
				return err
			}
			boot := "off"
			if status.BootEnabled {
				boot = "on"
			}
			if status.GRPCListenPort > 0 {
				fmt.Printf("%-10s installed=%-5t service=%s boot=%s version=%s port=%s grpc=%s\n", status.Name, status.Installed, status.ServiceState, boot, status.Version, displayPort(status.ListenPort), displayPort(status.GRPCListenPort))
				continue
			}
			fmt.Printf("%-10s installed=%-5t service=%s boot=%s version=%s port=%s\n", status.Name, status.Installed, status.ServiceState, boot, status.Version, displayPort(status.ListenPort))
		}
		return nil
	default:
		return fmt.Errorf("未知命令 %q\n\n%s", args[0], usage)
	}
}

func requireRoot() error {
	if os.Geteuid() == 0 {
		return nil
	}
	return fmt.Errorf("需要 root 权限，请使用 sudo monitorkit")
}

func componentArg(args []string) (string, []string, error) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return "", nil, fmt.Errorf("缺少组件名（prometheus 或 loki）")
	}
	return args[0], args[1:], nil
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func displayPort(port int) string {
	if port == 0 {
		return "-"
	}
	return fmt.Sprint(port)
}
