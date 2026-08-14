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
│   └── ui/                        # 终端视觉组件与操作反馈
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

交互界面提供中心组件状态总览、Prometheus/Loki 独立管理和探针接入命令。终端支持颜色时会显示状态徽标和动态操作反馈；设置 `NO_COLOR=1` 可关闭颜色。

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

## 安装探针

node_exporter 指标探针：

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/MonitorKit/main/scripts/probes/node_exporter/install.sh | sudo bash
```

Grafana Alloy 指标与日志统一探针：

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/MonitorKit/main/scripts/probes/alloy/install.sh | \
  sudo bash
```

脚本会交互询问 Prometheus、Loki 根地址（包含中心界面显示的随机端口），以及两个中心是否启用 mTLS。通过管道执行时会从 `/dev/tty` 读取输入，不会与下载脚本使用的标准输入冲突。

两种探针按项目需求二选一：只需要主机指标时安装 node_exporter；同时需要指标和日志时安装 Alloy。Alloy 已内置 Unix 主机指标采集，同一服务器不应再重复安装 node_exporter。

## 中心组件配置管理

Prometheus 和 Loki 的管理菜单均提供“配置管理”：

- 按 `vim → nano → vi` 的顺序自动选择已安装的编辑器。
- 编辑前保留原内容，保存后使用 `promtool` 或 Loki 自身执行配置校验。
- 校验成功后自动 reload/restart；失败时即时清理无效修改并恢复原配置，不生成残留文件。
- 配置操作及组件安装/更新会清理旧版本遗留的同名 `.rejected-*` 普通文件。
- 可以单独校验当前配置或重启服务应用配置。
- 首次安装会分别生成 `10000-59999` 范围内的可用随机监听端口，并写入 `/etc/prometheus/listen.port` 或 `/etc/loki/listen.port`；更新时保持不变。
- 可以输入指定端口或重新随机生成端口；变更会检查端口占用，失败时恢复原配置和原端口。修改后需要同步更新探针中心地址及防火墙规则。
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

Alloy 连接已启用 mTLS 的中心端时，在安装交互中为对应中心选择 `y`，然后依次输入：

```text
中心 HTTPS 根地址
服务端 CA 证书路径
Alloy 客户端证书路径
Alloy 客户端私钥路径
证书中的 TLS server_name
```

Prometheus 与 Loki 独立询问，只有启用 mTLS 的中心才要求填写证书。无人值守场景仍可使用脚本帮助中列出的环境变量。

中心端选择“配置或更新 mTLS”后，会先完整显示三个目标文件、每个文件应填写的 PEM 内容和禁止填写的内容。确认已准备证书后，程序逐个显示当前步骤；每一步只有在用户按回车后才会打开检测到的 `vim`、`nano` 或 `vi`。证书格式、证书与私钥匹配关系及服务配置全部校验通过后才会重启。

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
