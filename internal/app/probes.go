package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/Snail-one/MonitorKit/internal/manager"
	"github.com/Snail-one/MonitorKit/internal/ui"
)

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
		ui.Field{Value: "curl -fsSL https://raw.githubusercontent.com/Snail-one/MonitorKit/main/scripts/probes/alloy/install.sh | sudo bash", Detail: "默认安装 Release DEB/RPM，也可选择 Grafana 官方软件源"},
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
