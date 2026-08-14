package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Snail-one/MonitorKit/internal/app"
	"github.com/Snail-one/MonitorKit/internal/manager"
	"github.com/Snail-one/MonitorKit/internal/selfupdate"
	api "github.com/Snail-one/MonitorKit/internal/server"
	"github.com/Snail-one/MonitorKit/internal/ui"
	"github.com/Snail-one/MonitorKit/internal/version"
)

const usage = `MonitorKit 中心端管理程序

用法：
  monitorkit                                    # 交互式管理界面
  monitorkit --version
  sudo monitorkit update                       # 更新管理程序自身
  sudo monitorkit uninstall                    # 卸载管理程序自身
  monitorkit install <prometheus|loki> [--version latest]
  monitorkit uninstall <prometheus|loki> [--purge]
  monitorkit status [prometheus|loki]
  monitorkit serve [--listen 127.0.0.1:8088] [--token TOKEN]

环境变量：
  MONITORKIT_ROOT    安装根目录，默认 /（主要用于测试或离线打包）
  MONITORKIT_LISTEN  API 监听地址
  MONITORKIT_TOKEN   API Bearer Token
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
		case "update":
			if os.Geteuid() != 0 {
				return fmt.Errorf("更新管理程序需要 root 权限，请使用 sudo monitorkit update")
			}
			fmt.Printf("安装脚本：%s\n", selfupdate.InstallScriptURL)
			return selfupdate.Run(context.Background())
		case "uninstall":
			if len(args) == 1 {
				if os.Geteuid() != 0 {
					return fmt.Errorf("卸载管理程序需要 root 权限，请使用 sudo monitorkit uninstall")
				}
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
		fmt.Printf("%s %s 安装完成，服务状态：%s\n", result.Name, result.Version, result.ServiceState)
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
			fmt.Printf("%-10s installed=%-5t service=%s version=%s\n", status.Name, status.Installed, status.ServiceState, status.Version)
		}
		return nil
	case "serve":
		fs := flag.NewFlagSet("serve", flag.ContinueOnError)
		listen := fs.String("listen", envOr("MONITORKIT_LISTEN", "127.0.0.1:8088"), "API 监听地址")
		token := fs.String("token", os.Getenv("MONITORKIT_TOKEN"), "Bearer Token")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return fmt.Errorf("serve 不接受位置参数")
		}
		return serve(mgr, *listen, *token)
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	default:
		return fmt.Errorf("未知命令 %q\n\n%s", args[0], usage)
	}
}

func componentArg(args []string) (string, []string, error) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return "", nil, fmt.Errorf("缺少组件名（prometheus 或 loki）")
	}
	return args[0], args[1:], nil
}

func serve(mgr *manager.Manager, listen, token string) error {
	handler, err := api.New(mgr, token, listen)
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr:              listen,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	slog.Info("MonitorKit API 已启动", "listen", listen)
	err = server.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
