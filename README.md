# MonitorKit

MonitorKit 是一套中心化服务器监控配置工具：中心服务器运行 Go 管理程序，负责 Prometheus 和 Loki 的安装与生命周期管理；被监控服务器上的指标、日志探针全部使用独立 `.sh` 脚本安装。

## 项目结构

```text
MonitorKit/
├── .github/workflows/             # 日常 CI 与标签发布
├── cmd/monitorkit/                # Go 中心端入口
├── internal/
│   ├── app/                       # 交互菜单与业务流程编排
│   ├── manager/                   # 组件注册、下载、校验、安装与 systemd 管理
│   ├── server/                    # HTTP API、鉴权和响应模型
│   └── ui/                        # 终端视觉组件与操作反馈
├── configs/                       # 配置示例
├── deploy/systemd/                # 中心端 systemd unit
├── scripts/
│   ├── install.sh                 # 在线安装、更新和卸载
│   ├── build_linux.sh             # Linux 发布构建
│   ├── generate_release_notes.sh  # 自动生成发布说明
│   └── probes/
│       ├── node_exporter/install.sh
│       └── alloy/install.sh
└── docs/architecture.md           # 扩展规范
```

详细的分层和扩展方式见 [架构文档](docs/architecture.md)。安装会写入哪些系统文件、各卸载模式保留哪些内容，见 [文件写入与清理边界](docs/filesystem-and-cleanup.md)。

## 构建中心端

需要 Go 1.22 或更高版本：

```bash
OUTPUT=bin/monitorkit ./scripts/build_linux.sh
sudo install -m 0755 bin/monitorkit /usr/local/bin/monitorkit
```

也可以运行 `make build`。发布构建脚本默认生成带版本和架构的 `dist/monitorkit_linux_<arch>_<version>`，并支持通过 `OUTPUT` 自定义本地输出路径：

```bash
VERSION=v1.0.0 GOARCH=amd64 ./scripts/build_linux.sh
```

启动交互式管理界面：

```bash
sudo monitorkit
```

交互界面提供中心组件状态总览、Prometheus/Loki 独立管理、探针接入命令和 HTTP API 信息。终端支持颜色时会显示状态徽标和动态操作反馈；设置 `NO_COLOR=1` 可关闭颜色。

## 在线安装、更新与卸载

一键安装或更新最新正式版：

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/MonitorKit/main/scripts/install.sh | sudo sh
```

安装后可以直接更新或卸载管理程序：

```bash
monitorkit --version
sudo monitorkit update
sudo monitorkit uninstall
```

在线卸载也可以直接调用脚本：

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/MonitorKit/main/scripts/install.sh | sudo sh -s -- --uninstall
```

自身卸载不会删除 Prometheus、Loki、配置或监控数据。完整发布流程、指定版本安装和安全校验说明见 [发布文档](docs/release.md)，所有保留项见 [清理边界文档](docs/filesystem-and-cleanup.md)。

直接使用 CLI 安装中心组件：

```bash
sudo monitorkit install prometheus
sudo monitorkit install loki
monitorkit status
```

固定版本安装：

```bash
sudo monitorkit install prometheus --version 3.13.1
sudo monitorkit install loki --version 3.7.4
```

普通卸载会保留配置和数据；彻底清理由显式参数控制：

```bash
sudo monitorkit uninstall prometheus
sudo monitorkit uninstall loki --purge
```

中心组件默认路径如下：

- 二进制：`/usr/local/bin/{prometheus,promtool,loki}`
- 配置：`/etc/prometheus/prometheus.yml`、`/etc/loki/loki.yml`
- 数据：`/var/lib/prometheus`、`/var/lib/loki`
- 服务：`prometheus.service`、`loki.service`

已有配置在更新时会保留，发布包必须通过 SHA-256 校验才会安装。
频繁查询 GitHub Release 时可设置可选的 `GITHUB_TOKEN` 以提高 API 请求限额。

## 运行管理 API

API 默认仅监听本机：

```bash
sudo monitorkit serve
```

需要监听其他网卡时必须配置 Token：

```bash
sudo MONITORKIT_LISTEN=0.0.0.0:8088 \
  MONITORKIT_TOKEN="$(openssl rand -hex 32)" \
  monitorkit serve
```

可将 [systemd unit](deploy/systemd/monitorkit.service) 安装到 `/etc/systemd/system/monitorkit.service`，并参考 [环境变量示例](configs/monitorkit.env.example) 创建 `/etc/monitorkit/monitorkit.env`。

主要接口：

```text
GET    /healthz
GET    /api/v1/components
GET    /api/v1/components/{name}
POST   /api/v1/components/{name}/install
DELETE /api/v1/components/{name}?purge=true
```

安装请求示例：

```bash
curl -X POST http://127.0.0.1:8088/api/v1/components/prometheus/install \
  -H 'Content-Type: application/json' \
  -d '{"version":"latest"}'
```

配置了 Token 时增加 `Authorization: Bearer <token>` 请求头。API 只接受注册组件和预定义动作，不提供任意命令执行能力。

## 安装探针

node_exporter 指标探针：

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/MonitorKit/main/scripts/probes/node_exporter/install.sh | sudo bash
```

Grafana Alloy 指标与日志统一探针：

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/MonitorKit/main/scripts/probes/alloy/install.sh | \
  sudo PROMETHEUS_URL=http://10.0.0.10:9090 LOKI_URL=http://10.0.0.10:3100 bash
```

两种探针按项目需求二选一：只需要主机指标时安装 node_exporter；同时需要指标和日志时安装 Alloy。Alloy 已内置 Unix 主机指标采集，同一服务器不应再重复安装 node_exporter。

## 中心组件配置管理

Prometheus 和 Loki 的管理菜单均提供“配置管理”：

- 按 `vim → nano → vi` 的顺序自动选择已安装的编辑器。
- 编辑前保留原内容，保存后使用 `promtool` 或 Loki 自身执行配置校验。
- 校验成功后自动 reload/restart；失败时即时清理无效修改并恢复原配置，不生成残留文件。
- 配置操作及组件安装/更新会清理旧版本遗留的同名 `.rejected-*` 普通文件。
- 可以单独校验当前配置或重启服务应用配置。
- 可以配置、更新或关闭服务端 mTLS；关闭和普通卸载均保留证书，`purge` 才会删除。
- 组件更新会读取受管 mTLS 状态，不会把 HTTPS 配置覆盖回 HTTP。

中心端 mTLS 需要依次填写：

```text
/etc/prometheus/tls/ 或 /etc/loki/tls/
├── server.crt       # 服务端证书，SAN 包含探针使用的中心端域名或 IP
├── server.key       # 匹配的未加密服务端私钥
├── client-ca.crt    # 签发 Alloy 客户端证书的 CA 公共证书
└── mtls.enabled     # MonitorKit 管理的启用状态
```

Prometheus 还会生成 `/etc/prometheus/web.yml`。证书和私钥会通过 OpenSSL 检查格式与匹配关系，服务配置通过校验后才会重启。
启用后该端口上的 Web UI、查询 API、健康检查和写入接口都会要求受信任的客户端证书。

Alloy 连接已启用 mTLS 的中心端时，使用 HTTPS 地址并提供客户端证书。例如两个中心使用同一套 CA 和客户端证书：

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/MonitorKit/main/scripts/probes/alloy/install.sh | \
  sudo PROMETHEUS_URL=https://monitor.example.com:9090 \
       LOKI_URL=https://monitor.example.com:3100 \
       PROMETHEUS_MTLS_CA_FILE=/root/monitor-ca.crt \
       PROMETHEUS_MTLS_CERT_FILE=/root/alloy-client.crt \
       PROMETHEUS_MTLS_KEY_FILE=/root/alloy-client.key \
       PROMETHEUS_TLS_SERVER_NAME=monitor.example.com \
       LOKI_MTLS_CA_FILE=/root/monitor-ca.crt \
       LOKI_MTLS_CERT_FILE=/root/alloy-client.crt \
       LOKI_MTLS_KEY_FILE=/root/alloy-client.key \
       LOKI_TLS_SERVER_NAME=monitor.example.com \
       bash
```

Prometheus 与 Loki 的证书参数彼此独立，只给已经启用 mTLS 的中心配置对应变量即可。

探针卸载：

```bash
sudo bash scripts/probes/node_exporter/install.sh uninstall
sudo bash scripts/probes/alloy/install.sh uninstall
```

所有探针遵循 `scripts/probes/<name>/install.sh` 目录规范，不依赖中心端 Go 程序。

## 开发检查

```bash
make check
```

该命令运行 Go 单元测试、`go vet`、Shell 语法检查和安装器离线集成测试。
