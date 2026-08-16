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
sudo install -m 0755 bin/monitorkit /usr/local/sbin/monitorkit
```

也可以运行 `make build`。发布构建脚本默认生成带版本和架构的 `dist/monitorkit_linux_<arch>_<version>`，并支持通过 `OUTPUT` 自定义本地输出路径：

```bash
VERSION=v1.0.0 GOARCH=amd64 ./scripts/build_linux.sh
```

启动交互式管理界面：

```bash
sudo monitorkit
```

除 `--version` 和 `--help` 外，启动时会检测 root 权限；普通用户运行会直接退出。交互界面提供中心组件状态总览、Prometheus/Loki 独立管理和探针接入命令。终端支持颜色时会显示状态徽标和动态操作反馈；设置 `NO_COLOR=1` 可关闭颜色。

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

直接使用 CLI 安装中心组件。安装或更新只会写入程序、配置和 systemd 单元，**不会自动启动服务**：

```bash
sudo monitorkit install prometheus
sudo monitorkit install loki
sudo monitorkit start prometheus
sudo monitorkit start loki
sudo monitorkit status
```

`start` 会 `systemctl enable --now`：立即运行，并开启开机自启。`stop` 会 `systemctl disable --now`：停止进程并取消开机自启。

```bash
sudo monitorkit stop loki
sudo monitorkit restart prometheus
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

Grafana Alloy 指标与日志统一探针：

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/MonitorKit/main/scripts/probes/alloy/install.sh | \
  sudo bash
```

node_exporter 指标探针：

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/MonitorKit/main/scripts/probes/node_exporter/install.sh | sudo bash
```

脚本会先要求填写唯一的监控节点名称，再分别询问 Prometheus、Loki 是否启用 mTLS，首次配置均默认推荐启用。两者都启用 mTLS 时，可选择录入一套共享证书或分别配置；共享模式只录入一次 CA、完整客户端证书和私钥，校验后仍分别写入 `prometheus-*` 与 `loki-*` 受管文件，两个服务的地址和 `server_name` 始终分别设置。指标统一写入 `job="alloy-one"`，节点名称会同时写入指标的 `instance`、`host` 标签和日志的 `host` 标签。journal 日志还会带上 `unit`（systemd 服务名）和 `ident`，Grafana 可按主机或服务筛选。系统指标原生提供的 `nodename` 保持真实主机名，不会被脚本覆盖。启用 mTLS 时，裸 `IP:端口` 自动补为 `https://` 并填写客户端证书；选择不启用时会先警告明文传输风险，再次确认后裸地址才补为 `http://`。通过管道执行时会从 `/dev/tty` 读取输入，不会与下载脚本使用的标准输入冲突。

Alloy 已安装时，再次运行同一命令会进入维护菜单，可选择更新软件包、仅重新配置、查看状态、普通卸载或彻底清理。“仅重新配置”可修改节点名称；节点名称持久化在 `/etc/alloy/monitor.name`。交互配置会按 `vim → nano → vi` 自动选择编辑器，直接填写 `/etc/alloy/tls/` 中的受管 PEM 文件；配置和服务启动失败会恢复操作前的配置、节点名称与证书，不保留无效副本。

也可以直接执行维护动作：

```bash
sudo bash scripts/probes/alloy/install.sh reconfigure
sudo bash scripts/probes/alloy/install.sh status
sudo bash scripts/probes/alloy/install.sh uninstall
sudo bash scripts/probes/alloy/install.sh purge
```

两种探针按项目需求二选一：只需要主机指标时安装 node_exporter；同时需要指标和日志时安装 Alloy。Alloy 已内置 Unix 主机指标采集，同一服务器不应再重复安装 node_exporter。

“探针接入”菜单提供两个中心端操作：

- “添加探针”可以添加其他服务器上的 node_exporter。填写名称、IP/域名、端口和 HTTP/mTLS 连接方式后，MonitorKit 会在 `prometheus.yml` 的受管区域生成独立 scrape job，使用 `promtool` 校验并 reload。mTLS 模式还会逐个编辑和校验 node_exporter 服务端 CA、Prometheus 客户端证书及私钥。
- “管理当前探针”读取 `/etc/prometheus/probes/inventory.json`，可以修改目标名称、地址、端口和 TLS server_name，启停抓取、更新 mTLS 证书或删除接入配置。删除只停止 Prometheus 抓取，不会远程卸载 node_exporter。
- Alloy 主动推送，不加入 Prometheus 抓取目标清单。Alloy 安装卡片会直接显示当前 Prometheus、Loki 根地址、随机监听端口和接收状态，脚本交互时可照此填写。

Prometheus 生成的 node_exporter mTLS 抓取配置使用官方 [`scrape_config`](https://prometheus.io/docs/prometheus/latest/configuration/configuration/) 的 `static_configs` 和 `tls_config`（`ca_file`、`cert_file`、`key_file`、`server_name`）。每个 mTLS 探针独立保存证书，允许不同服务器使用不同 CA 或 server_name。

## 中心组件配置管理

Prometheus 和 Loki 的管理菜单均提供“配置管理”：

- 按 `vim → nano → vi` 的顺序自动选择已安装的编辑器。
- 编辑前保留原内容，保存后使用 `promtool` 或 Loki 自身执行配置校验。
- 校验成功后，仅当服务已在运行时才 reload/restart；服务处于停止状态时只写入配置，不会自动拉起。失败时即时清理无效修改并恢复原配置，不生成残留文件。
- “重置配置”会用当前程序内置的默认模板重写主配置和 systemd unit，便于吃到后续版本的默认配置变更。监听端口、gRPC 端口、mTLS 证书、远程写入开关、存储设置文件、探针清单和历史数据会保留；主配置里的手工修改以及 Loki 保留期/摄入限制会回到程序默认。Prometheus 受管探针会重新写入新配置。
- 组件菜单提供“服务管理”，可手动启动、停止或重启。安装与更新不会自动启动；“启动”会开启开机自启并立即运行；“停止”会关掉进程并取消开机自启。
- 配置操作及组件安装/更新会清理旧版本遗留的同名 `.rejected-*` 普通文件。
- 可以单独校验当前配置或重启服务应用配置。
- 首次安装会分别生成 `10000-59999` 范围内的可用随机监听端口，并写入 `/etc/prometheus/listen.port` 或 `/etc/loki/listen.port`；Loki 还会为内部 gRPC 再生成一个随机端口，写入 `/etc/loki/grpc.port` 和 `loki.yml`，并绑定 `127.0.0.1`。更新时这些端口保持不变。
- 可以输入指定端口或重新随机生成端口；Loki 的配置菜单可分别修改 HTTP 监听端口和 gRPC 端口。变更会检查端口占用，失败时恢复原配置和原端口。修改 HTTP 端口后需要同步更新探针中心地址及防火墙规则；gRPC 仅供 Loki 内部通信，探针无需修改。
- 可以配置、更新或关闭服务端 mTLS；关闭和普通卸载均保留证书，`purge` 才会删除。
- Prometheus 的远程写入接收使用独立开关且默认关闭。mTLS 模式可直接确认开启；HTTP 模式也允许开启，但界面会警告指标与请求未加密并要求再次确认。关闭 mTLS 会保留远程写入开关状态，已开放的接收端会转为 HTTP。
- Prometheus 和 Loki 的配置菜单都提供“数据存储设置”。Prometheus 可选择 7/15/30/90 天、自定义天数或不限制保留期，并可设置 10/20/50/100 GB 或自定义磁盘上限。时间和磁盘上限同时生效，先到先清理。设置会写入 `/etc/prometheus/storage.settings` 和 `prometheus.service` 的 `--storage.tsdb.retention.time` / `--storage.tsdb.retention.size`；更新、改端口、开关 mTLS 或远程写入时都会保留。恢复默认后回到上游 15 天、无磁盘上限。首次安装不写自定义参数。
- Loki 的“数据存储设置”可选择 7/15/30/90 天、自定义天数或不限制保留期，并可设置摄入速率、突发大小和单行上限。设置保留期时会写入 `limits_config.retention_period` 并启用 Compactor 过期删除。首次安装和“重置配置”默认 30 天并打开过期删除；菜单里“恢复默认”同样回到 30 天。最短保留期为 24 小时。
- 组件更新会读取受管 mTLS 状态，不会把 HTTPS 配置覆盖回 HTTP。

中心端 mTLS 需要依次填写：

```text
/etc/prometheus/tls/ 或 /etc/loki/tls/
├── client-ca.crt    # 1. 签发 Alloy 客户端证书的 CA 公共证书
├── server.crt       # 2. 完整服务端证书或证书链，SAN 包含中心端域名或 IP
├── server.key       # 3. 匹配的未加密服务端私钥
└── mtls.enabled     # MonitorKit 管理的启用状态
```

Prometheus 还会生成 `/etc/prometheus/web.yml`。证书和私钥会通过 OpenSSL 检查格式与匹配关系，服务配置通过校验后才会重启。
启用 mTLS 后，该端口上的 Web UI、查询 API 和健康检查都会要求受信任的客户端证书；`/api/v1/write` 只有再打开“远程写入”独立开关后才存在。

Alloy 为 Prometheus 或 Loki 选择 mTLS 时，需要填写以下信息。交互模式直接编辑受管路径，无需先把证书放到其他临时路径：

```text
中心 HTTPS 根地址
服务端 CA 证书内容
Alloy 客户端证书内容
Alloy 客户端私钥内容
证书中的 TLS server_name
```

所有证书录入流程统一使用 `CA 证书 → 完整证书或证书链 → 私钥` 的顺序；中心端服务、Alloy 和 node_exporter 均遵循这一顺序。

Prometheus 与 Loki 的证书分别填写。安装 Alloy 前，必须在中心端 Prometheus 配置菜单中单独开启“远程写入”；中心端是否启用 mTLS 必须与 Alloy 的选择一致。HTTP 模式不会要求证书，但数据未经 TLS 加密，应使用防火墙限制可信探针 IP。无人值守场景仍可使用脚本帮助中列出的环境变量。

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
