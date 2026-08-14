# SnailMon 架构

SnailMon 分为中心端和探针端。中心服务器运行 Go 管理程序，负责 Prometheus 与 Loki 的生命周期；被监控服务器不运行管理程序，所有探针均由独立 Shell 脚本安装。

## 分层与目录

```text
cmd/snailmon/                 程序入口与 CLI 参数
internal/app/                 交互菜单与业务流程编排
internal/manager/             组件生命周期、组件注册、下载和安装基础设施
internal/server/              HTTP API 与鉴权
internal/ui/                  终端视觉组件、状态徽标与操作反馈
configs/                      中心端配置示例
deploy/systemd/               中心端 systemd 部署文件
scripts/probes/<name>/        探针安装脚本，每个探针一个目录
docs/                         架构和运维文档
```

`internal/app` 只负责编排交互流程，`internal/ui` 不依赖监控业务。`internal/server` 仅依赖 `Manager` 接口，不包含 Prometheus 或 Loki 的分支逻辑。组件元数据统一注册在 `internal/manager/spec.go`，下载、SHA-256 校验和压缩包处理是公共能力。

## 扩展中心组件

1. 在 `internal/manager/spec.go` 的注册表增加组件定义，包括仓库、发布资源命名、二进制、默认配置、配置校验与 systemd unit；组件列表会自动生成。
2. 如组件使用新的压缩格式，在 `archive.go` 增加对应解压器。
3. 增加配置、归档和 API 回归测试。

API 路由以 `{name}` 作为组件参数，因此新增组件无需复制 HTTP handler。

## 扩展探针

每个探针必须位于 `scripts/probes/<name>/install.sh`，脚本应至少支持安装、更新、`uninstall` 和 `purge`。探针脚本自行处理架构检测、校验、systemd 服务和重复执行，不依赖中心端 Go 二进制。

## 安全边界

- API 只允许已注册组件和预定义动作，不接收任意 Shell 命令。
- 默认监听 `127.0.0.1:8088`；监听公网或内网地址时强制要求 Bearer Token。
- 发布包必须通过 GitHub Release 提供的 SHA-256 digest 校验。
- 默认卸载保留配置与数据，只有显式 `purge` 才清理。
