# MonitorKit 架构

MonitorKit 分为中心端和探针端。中心服务器运行 Go 管理程序，负责 Prometheus 与 Loki 的生命周期；被监控服务器不运行管理程序，所有探针均由独立 Shell 脚本安装。

## 分层与目录

```text
cmd/monitorkit/               程序入口与 CLI 参数
internal/app/                 交互菜单与业务流程编排
internal/manager/             组件生命周期、组件注册、下载和安装基础设施
internal/ui/                  终端视觉组件、状态徽标与操作反馈
scripts/probes/<name>/        探针安装脚本，每个探针一个目录
docs/                         架构和运维文档
.github/workflows/            CI 与 GitHub Release 发布流程
```

`internal/app` 只负责编排交互流程，`internal/ui` 不依赖监控业务。组件元数据统一注册在 `internal/manager/spec.go`，下载、SHA-256 校验、压缩包处理以及安全配置编辑是公共能力。

node_exporter 使用 Prometheus 拉取模型：中心端探针清单保存在 `/etc/prometheus/probes/inventory.json`，每个启用目标渲染为独立的受管 `scrape_config`，因此可以为每台服务器设置不同地址、端口和 mTLS 文件。Alloy 使用主动推送模型，不生成抓取目标；界面读取 Prometheus/Loki 当前端口和安全状态作为安装参数提示。

配置编辑通过受限的 `vim → nano → vi` 编辑器选择进入：Manager 在编辑期间保存原内容，编辑后调用组件自己的校验器，并在校验成功后 reload/restart。失败修改即时清理，活动配置自动回滚。mTLS 与 Prometheus 远程写入分别使用受管状态标记生成组件 unit，因此二进制更新不会丢失独立开关状态。远程写入在 HTTPS/mTLS 和 HTTP 下都能独立启用；交互层会对 HTTP 明文传输显示风险说明并要求再次确认。

## 扩展中心组件

1. 在 `internal/manager/spec.go` 的注册表增加组件定义，包括仓库、发布资源命名、二进制、默认配置、配置校验与 systemd unit；组件列表会自动生成。
2. 如组件使用新的压缩格式，在 `archive.go` 增加对应解压器。
3. 增加配置和归档回归测试。

## 扩展探针

每个探针必须位于 `scripts/probes/<name>/install.sh`，脚本应至少支持安装、更新、`uninstall` 和 `purge`。探针脚本自行处理架构检测、校验、systemd 服务和重复执行，不依赖中心端 Go 二进制。

- node_exporter 是只采集主机指标的轻量方案。
- Grafana Alloy 是指标与日志统一采集方案，通过 `prometheus.exporter.unix` 采集主机指标，同时读取 systemd journal。
- 两种方案在单台被监控服务器上二选一，避免重复上报同一组主机指标。

## 安全边界

- 发布包必须通过 GitHub Release 提供的 SHA-256 digest 校验。
- Prometheus 与 Loki 可强制验证 Alloy 客户端证书；所有交互按 CA 证书、完整证书链、私钥的固定顺序录入，并在启用前通过 OpenSSL 校验。
- 默认卸载保留配置与数据，只有显式 `purge` 才清理。
