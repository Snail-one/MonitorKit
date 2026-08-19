package app

import (
	"context"

	"github.com/Snail-one/MonitorKit/internal/manager"
	"github.com/Snail-one/MonitorKit/internal/ui"
)

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
