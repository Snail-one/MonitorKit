# 文件写入与卸载清理边界

本文记录 MonitorKit 管理程序、中心组件和探针在默认配置下写入的系统路径，以及不同卸载命令会删除或保留的内容。通过环境变量修改路径时，以实际配置为准。

## 清单范围

本文把落盘内容分为三类：

1. **MonitorKit 直接管理的路径**：下面的默认路径总索引完整列出；安装、更新和卸载行为由本项目代码确定。
2. **组件运行时数据子树**：Prometheus、Loki 和 Alloy 会在各自 `/var/lib/.../` 下继续生成版本相关的数据文件，本文以整个目录子树表示，目录内任意新文件均属于该组件数据。
3. **系统工具产生的副作用**：systemd、journald、用户数据库和发行版包管理器会写自己的数据库、缓存或日志。这些文件不由 MonitorKit 单独拥有，路径可能因发行版和软件包版本变化，因此单独列出，卸载不会为了清理一个组件而删除共享数据库。

除非明确写为临时文件，下面路径均可能在重启后继续存在。

## 默认持久化路径总索引

```text
/usr/local/sbin/
└── monitorkit

/usr/local/bin/
├── prometheus
├── promtool
├── loki
└── node_exporter

/etc/prometheus/
├── prometheus.yml
├── listen.port
├── remote-write.enabled                 # 仅开启远程写入接收时存在
├── storage.settings                     # 仅自定义过指标保留或磁盘上限时存在
├── web.yml                              # 配置过 Prometheus mTLS 后存在
├── tls/
│   ├── client-ca.crt
│   ├── server.crt
│   ├── server.key
│   └── mtls.enabled                     # 仅 mTLS 当前启用时存在
└── probes/
    ├── inventory.json                   # 添加过 node_exporter 接入后存在
    └── <probe-id>/                      # 仅该目标使用 mTLS 时存在
        ├── ca.crt
        ├── client.crt
        └── client.key

/etc/loki/
├── loki.yml
├── listen.port
├── grpc.port
└── tls/
    ├── client-ca.crt
    ├── server.crt
    ├── server.key
    └── mtls.enabled                     # 仅 mTLS 当前启用时存在

/etc/node_exporter/
├── web.yml                              # 启用或曾配置过 mTLS
└── tls/                                 # 启用或曾配置过 mTLS
    ├── client-ca.crt
    ├── server.crt
    └── server.key

/etc/alloy/
├── config.alloy
├── monitor.name                         # Grafana 中显示的自定义节点名称
└── tls/
    ├── prometheus-ca.crt                # Prometheus 选择 mTLS 时
    ├── prometheus-client.crt            # Prometheus 选择 mTLS 时
    ├── prometheus-client.key            # Prometheus 选择 mTLS 时
    ├── loki-ca.crt                      # Loki 选择 mTLS 时
    ├── loki-client.crt                  # Loki 选择 mTLS 时
    └── loki-client.key                  # Loki 选择 mTLS 时

/var/lib/prometheus/**                   # TSDB 块、WAL、锁和查询状态等全部数据
/var/lib/loki/**                         # chunks、rules、TSDB/WAL、索引和缓存等
/var/lib/alloy/**                        # Alloy 运行状态、队列和组件数据

/etc/systemd/system/
├── prometheus.service
├── loki.service
├── node_exporter.service
└── multi-user.target.wants/
    ├── prometheus.service -> ../prometheus.service
    ├── loki.service -> ../loki.service
    ├── node_exporter.service -> ../node_exporter.service
    └── alloy.service -> <发行版软件包提供的 alloy.service>

# Alloy 仓库配置（只会出现当前发行版对应的一组）
/etc/apt/keyrings/grafana.asc             # Debian/Ubuntu
/etc/apt/sources.list.d/grafana.list      # Debian/Ubuntu
/etc/yum.repos.d/grafana.repo             # RHEL/Fedora
/etc/zypp/repos.d/grafana.repo            # SUSE/openSUSE，文件名由 zypper 按别名生成
RPM 密钥数据库中的 Grafana 签名密钥         # RPM 系发行版
```

Alloy 二进制、软件包自带的 systemd unit 和环境文件由发行版软件包决定，不由脚本硬编码。常见路径包括 `/usr/bin/alloy`、`/usr/lib/systemd/system/alloy.service` 或 `/lib/systemd/system/alloy.service`，以及 `/etc/default/alloy` 或 `/etc/sysconfig/alloy`。实际机器上的完整软件包文件清单必须以以下命令为准：

```bash
dpkg-query -L alloy       # Debian/Ubuntu
rpm -ql alloy             # RHEL/Fedora/SUSE
systemctl cat alloy       # 查看实际加载的 unit 和环境文件
```

## 清理级别总览

| 对象 | 普通卸载 | 彻底清理 |
| --- | --- | --- |
| MonitorKit 自身 | `sudo monitorkit uninstall` | 没有额外 purge；只管理自身二进制 |
| Prometheus | `sudo monitorkit uninstall prometheus` | `sudo monitorkit uninstall prometheus --purge` |
| Loki | `sudo monitorkit uninstall loki` | `sudo monitorkit uninstall loki --purge` |
| node_exporter | `sudo bash scripts/probes/node_exporter/install.sh uninstall` | `sudo bash scripts/probes/node_exporter/install.sh purge` |
| Grafana Alloy | `sudo bash scripts/probes/alloy/install.sh uninstall` | `sudo bash scripts/probes/alloy/install.sh purge` |

普通卸载优先保留配置、证书和历史数据。只有显式执行组件或探针的 `purge` 才删除对应数据目录，但部分系统级资源仍会保留，详见下文。

## MonitorKit 管理程序

在线安装脚本默认只持久写入：

```text
/usr/local/sbin/monitorkit
```

安装期间还会使用以下临时文件，正常结束或失败退出时自动删除：

```text
${TMPDIR:-/tmp}/monitorkit-install.XXXXXXXX/
/usr/local/sbin/.monitorkit.new.<PID>
```

`MONITORKIT_INSTALL_DIR` 和 `MONITORKIT_BINARY_NAME` 可以改变实际安装位置及文件名。使用默认命令名且安装目录不是 `/usr/local/bin` 时，安装、更新和卸载还会删除旧默认路径 `/usr/local/bin/monitorkit`。

执行以下任一命令只删除管理程序二进制：

```bash
sudo monitorkit uninstall
curl -fsSL https://raw.githubusercontent.com/Snail-one/MonitorKit/main/scripts/install.sh | sudo sh -s -- --uninstall
```

自身卸载不会停止、卸载或清理：

- Prometheus 与 Loki 的服务、二进制、配置、数据和系统账号；
- 任意被监控服务器上的 node_exporter 或 Alloy；
- GitHub 下载缓存以外的任何用户文件。

## Prometheus 中心组件

安装或更新会管理：

```text
/usr/local/bin/prometheus
/usr/local/bin/promtool
/etc/prometheus/
├── prometheus.yml
├── listen.port             # 首次安装生成，记录当前随机或自定义监听端口
├── remote-write.enabled    # 仅在远程写入接收开关开启时存在
├── storage.settings        # 仅自定义过指标保留或磁盘上限时存在
├── web.yml                 # 启用 mTLS 时的受管 Web 配置
├── probes/
│   ├── inventory.json         # node_exporter 接入清单、启停状态和目标地址
│   └── <probe-id>/            # 对应探针使用 mTLS 时
│       ├── ca.crt              # node_exporter 服务端 CA
│       ├── client.crt          # Prometheus 客户端证书
│       └── client.key          # Prometheus 客户端私钥
└── tls/                    # 启用或曾启用 mTLS 时
    ├── client-ca.crt
    ├── server.crt
    ├── server.key
    └── mtls.enabled        # 仅在 mTLS 启用时存在
/var/lib/prometheus/
/etc/systemd/system/prometheus.service
/etc/systemd/system/multi-user.target.wants/prometheus.service
系统用户：prometheus
系统组：prometheus
```

`/var/lib/prometheus/` 是完整的数据边界。Prometheus 会在其中生成 TSDB 块目录、`wal/`、`chunks_head/`、`queries.active`、`lock` 等运行时内容；具体文件会随 Prometheus 版本和数据状态变化。

首次安装从 `10000-59999` 中选择一个当时可用的随机端口。更新会读取 `listen.port` 并保持端口不变，同时替换二进制和 systemd unit；已经存在的 `prometheus.yml` 会保留，不会被默认配置覆盖。存在 `mtls.enabled` 时，新 unit 会继续引用 `web.yml`，不会在更新后退回 HTTP。远程写入默认关闭，存在 `remote-write.enabled` 时 unit 会开放 `/api/v1/write`，该开关与 mTLS 独立：HTTP 模式允许开启但交互界面会警告明文风险；关闭 mTLS 不再删除远程写入标记。指标保留默认使用 Prometheus 上游的 15 天、无磁盘上限；“数据存储设置”会把自定义时间或磁盘上限写入 `storage.settings` 和 unit 的 `--storage.tsdb.retention.time` / `--storage.tsdb.retention.size`。更新、改端口、开关 mTLS 或远程写入都会读回该文件，不会丢掉自定义保留。恢复默认会删除 `storage.settings` 并去掉这些参数。

添加、修改、启停或删除 node_exporter 接入时，MonitorKit 会更新 `inventory.json` 和 `prometheus.yml` 中带有 `BEGIN/END MONITORKIT MANAGED PROBES` 标记的受管 scrape job。新配置只有通过 `promtool` 校验并成功 reload/restart 后才生效，失败会恢复清单和主配置。删除接入配置会删除该探针在中心端保存的 mTLS 文件，但不会连接远程服务器或卸载远程 node_exporter。

从配置菜单直接编辑 `prometheus.yml` 时，原配置内容只在操作期间保存。新配置通过 `promtool` 校验后，服务已运行才 reload；失败时即时清理无效修改并恢复原配置，不生成残留文件。配置操作及安装/更新会删除旧版本遗留的同名 `.rejected-*` 普通文件。

“重置配置”会按当前程序模板重写 `prometheus.yml` 和 `prometheus.service`，并保留 `listen.port`、mTLS、远程写入开关、`storage.settings` 与探针清单；受管 scrape job 会写回新配置。

普通卸载会：

- 停止并禁用 `prometheus.service`；
- 删除 `prometheus`、`promtool` 和 `prometheus.service`；
- 由 systemd 删除服务启用链接。

普通卸载会保留：

```text
/etc/prometheus/
/var/lib/prometheus/
prometheus 用户与组
```

`--purge` 会额外删除 `/etc/prometheus/` 和 `/var/lib/prometheus/`，但仍保留 `prometheus` 用户与组。管理程序无法可靠判断同名账号是否由 MonitorKit 首次创建，因此不会自动删除系统身份。

## Loki 中心组件

安装或更新会管理：

```text
/usr/local/bin/loki
/etc/loki/
├── loki.yml
├── listen.port             # 首次安装生成，记录当前随机或自定义 HTTP 监听端口
├── grpc.port               # 首次安装生成，记录当前随机或自定义 gRPC 端口
└── tls/                    # 启用或曾启用 mTLS 时
    ├── client-ca.crt
    ├── server.crt
    ├── server.key
    └── mtls.enabled        # 仅在 mTLS 启用时存在
/var/lib/loki/
├── chunks/
├── rules/
└── 运行时生成的 TSDB、WAL、索引、缓存和压缩状态
/etc/systemd/system/loki.service
/etc/systemd/system/multi-user.target.wants/loki.service
系统用户：loki
系统组：loki
```

首次安装从 `10000-59999` 中为 HTTP 和 gRPC 各选择一个当时可用的随机端口。HTTP 写入 `listen.port` 与 `loki.yml` 的 `http_listen_port`；gRPC 写入 `grpc.port`、`loki.yml` 的 `grpc_listen_port`，并绑定 `127.0.0.1`。更新会读取这两个端口文件并保持不变，同时替换二进制和 systemd unit；已经存在的 `loki.yml` 会完整保留，不再改写其中字段。要把主配置换成当前程序默认，使用「重置配置」。存在 `mtls.enabled` 时，新 unit 会继续带有 Loki HTTPS 与客户端证书验证参数。

从配置菜单直接编辑 `loki.yml` 时，会在内存中保存原内容，再使用 Loki 的 `-verify-config=true` 校验；服务已运行才重启。校验或启动失败时即时清理无效修改并恢复原配置，不保留拒绝文件。

“重置配置”会按当前程序模板重写 `loki.yml` 和 `loki.service`，并保留 `listen.port`、`grpc.port`、mTLS 文件与 `/var/lib/loki`。主配置中的手工项会回到首次安装默认，保留期恢复为 30 天。

“数据存储设置”只改写 `loki.yml` 中的 `limits_config` 与 `compactor`，不另建开关文件。首次安装和“重置配置”默认写入 30 天保留期并启用 Compactor。设置保留期后会在 `/var/lib/loki/compactor/`（或配置中的 `path_prefix/compactor`）写入压缩与删除标记；菜单“恢复默认”同样回到 30 天。已有数据目录仍保留到普通卸载或 `--purge`。

普通卸载会删除二进制和 unit，并停止、禁用服务，但保留：

```text
/etc/loki/
/var/lib/loki/
loki 用户与组
```

`--purge` 会额外删除 `/etc/loki/` 和 `/var/lib/loki/`，包括其中的全部历史日志、索引和缓存；`loki` 用户与组仍然保留。

## node_exporter 轻量指标探针

探针脚本会管理：

```text
/usr/local/bin/node_exporter
/etc/node_exporter/
├── web.yml            # 使用 mTLS 时
└── tls/               # 使用 mTLS 时的证书和私钥
/etc/systemd/system/node_exporter.service
/etc/systemd/system/multi-user.target.wants/node_exporter.service
系统用户：node_exporter
系统组：node_exporter
```

普通卸载删除二进制、unit 和启用链接，但保留 `/etc/node_exporter/`、证书以及 `node_exporter` 用户与组。

`purge` 会删除 `/etc/node_exporter/`，并删除 `node_exporter` 用户与组。node_exporter 不创建独立历史数据目录。

## Grafana Alloy 统一探针

Alloy 通过发行版包管理器安装。脚本明确写入或修改：

```text
/etc/alloy/config.alloy
/etc/alloy/monitor.name        # 指标 instance/host 与日志 host 的节点名称
/etc/alloy/tls/             # 对任一中心启用 mTLS 时
├── prometheus-ca.crt
├── prometheus-client.crt
├── prometheus-client.key
├── loki-ca.crt
├── loki-client.crt
└── loki-client.key
/var/lib/alloy/
alloy 用户的 systemd-journal、adm 附加组成员关系（对应组存在时）
```

选择共享证书模式时，上述 `prometheus-*` 与 `loki-*` 文件仍分别存在，但两组 CA、客户端证书和私钥的内容相同；卸载与清理边界不变。

Alloy 对 `/var/lib/alloy/` 内部结构拥有完整控制权，软件包版本、启用组件和队列状态都可能产生新的子目录或文件；清理边界按整个目录处理，而不是只处理当前已知文件名。

根据发行版，还会新增 Grafana 软件源：

```text
Debian/Ubuntu:
  /etc/apt/keyrings/grafana.asc
  /etc/apt/sources.list.d/grafana.list

RHEL/Fedora:
  /etc/yum.repos.d/grafana.repo
  RPM 密钥数据库中的 Grafana 签名密钥

SUSE/openSUSE:
  /etc/zypp/repos.d/grafana.repo（由 zypper 按 grafana 别名生成）
  RPM 密钥数据库中的 Grafana 签名密钥
```

Alloy 软件包自身还会安装发行版管理的二进制、systemd unit、默认环境文件和 `alloy` 系统账号；具体路径由对应版本的软件包决定。

首次安装完成后，再次运行脚本会进入维护菜单。选择“仅重新配置”不会刷新软件源或下载软件包；脚本直接编辑受管证书，使用 `alloy validate` 校验新配置，并且只在服务成功重启后提交修改。证书、配置校验失败或服务启动失败时会即时恢复操作前的 `/etc/alloy/` 内容，不生成 `.rejected-*` 残留文件。关闭 Loki mTLS 时，不再使用的三个 Loki 客户端证书文件会即时删除。

普通卸载会停止、禁用服务并调用 `apt-get remove`、`dnf remove`、`yum remove` 或 `zypper remove`，但脚本不会主动删除：

```text
/etc/alloy/
/var/lib/alloy/
Grafana 软件源与签名密钥
alloy 用户、组及其附加组成员关系
```

`purge` 会在卸载软件包后额外删除 `/etc/alloy/` 和 `/var/lib/alloy/`。它仍不会删除 Grafana 软件源、签名密钥或主动删除 `alloy` 系统账号；包管理器自身是否清理账号及其他包文件取决于发行版的软件包卸载脚本。Debian/Ubuntu 使用的是 `apt-get remove`，不是 `apt-get purge`。

卸载 Alloy 不会卸载同机存在的 node_exporter，也不会删除中心服务器上的 Prometheus/Loki 数据。

## 系统级共享状态

以下内容会因为安装或运行组件而发生变化，但不是可安全整体删除的组件专属文件：

```text
/etc/passwd              # prometheus、loki、node_exporter、alloy 账号记录
/etc/shadow              # 对应系统账号记录
/etc/group               # 对应系统组及 Alloy 附加组成员关系
/etc/gshadow             # 对应系统组安全记录
/etc/passwd-             # shadow-utils 可能生成的上一版本备份
/etc/shadow-             # shadow-utils 可能生成的上一版本备份
/etc/group-              # shadow-utils 可能生成的上一版本备份
/etc/gshadow-            # shadow-utils 可能生成的上一版本备份
/etc/.pwd.lock           # useradd/userdel/groupadd/groupdel 操作期间的临时锁

/var/log/journal/**      # 启用持久 journald 时，包含各服务日志
/run/log/journal/**      # journald 使用易失存储时，包含各服务日志

/var/lib/dpkg/**         # Debian/Ubuntu 软件包状态与 alloy 文件清单
/var/lib/apt/lists/**    # apt update 下载的软件源索引
/var/cache/apt/**        # apt 软件包缓存
/var/lib/rpm/**          # 部分 RPM 系统的软件包数据库
/usr/lib/sysimage/rpm/** # 新版 RPM 系统可能使用的软件包数据库
/var/cache/dnf/**        # DNF 缓存
/var/cache/yum/**        # YUM 缓存
/var/lib/zypp/**         # SUSE 软件源和软件包状态
/var/cache/zypp/**       # SUSE 软件包缓存
```

- Prometheus 和 Loki 的用户、组只在不存在时创建，普通卸载和 `--purge` 都保留。
- node_exporter 普通卸载保留用户、组，`purge` 调用 `userdel`/`groupdel` 删除对应记录。
- Alloy 账号、unit、环境文件和软件包数据库记录由发行版的软件包安装/卸载脚本处理。
- 安装不再执行 `systemctl enable --now`，因此默认不会创建开机启动链接，也不会拉起服务。用户在服务管理中选择“启动”（或 `monitorkit start`）时才会 `enable --now`。“停止”（或 `monitorkit stop`）执行 `disable --now`。卸载仍会 `disable --now`。systemd 自身状态和 journald 历史日志不会随项目 purge 清空。
- Alloy 安装可能同时安装或更新 `ca-certificates`、`curl`、`gpg` 等依赖；MonitorKit 不会在卸载 Alloy 时反向卸载这些共享软件包。

## 临时落盘路径

正常完成、普通错误退出或配置回滚时，脚本会清理下列临时文件。遭遇断电、内核终止或 `SIGKILL` 时可能残留：

```text
${TMPDIR:-/tmp}/monitorkit-install.XXXXXXXX/   # 管理程序在线安装下载目录
${TMPDIR:-/tmp}/monitorkit-action-*.sh        # monitorkit update/uninstall 下载的动作脚本
${TMPDIR:-/tmp}/monitorkit-prometheus-*       # Prometheus 下载和解压目录
${TMPDIR:-/tmp}/monitorkit-loki-*             # Loki 下载和解压目录

/usr/local/sbin/.monitorkit-*                 # 管理程序二进制原子替换临时文件
/usr/local/bin/.monitorkit-*                  # 中心组件二进制原子替换临时文件
/etc/prometheus/.monitorkit-*                 # 配置、端口、开关原子写入临时文件
/etc/prometheus/tls/.monitorkit-*
/etc/prometheus/probes/.monitorkit-*
/etc/prometheus/probes/<probe-id>/.monitorkit-*
/etc/loki/.monitorkit-*
/etc/loki/tls/.monitorkit-*
/etc/systemd/system/.monitorkit-*             # unit 原子写入临时文件

${TMPDIR:-/tmp}/node-exporter-install.XXXXXXXX/ # node_exporter 下载、校验和解压
${TMPDIR:-/tmp}/node-exporter-service.XXXXXXXX  # node_exporter unit 暂存文件
${TMPDIR:-/tmp}/alloy-config.XXXXXXXX/          # Alloy 配置和回滚快照
${TMPDIR:-/tmp}/tmp.*                           # RPM 模式导入 Grafana key 的 mktemp 文件
```

`vim`、`nano` 或 `vi` 可能按用户编辑器配置创建 swap、备份或撤销文件；这不是 MonitorKit 主动创建的文件，脚本无法统一预测文件名。MonitorKit 自身不会生成 `.rejected-*` 文件，并会清理旧版本遗留的同名普通文件。

## 本地构建与开发产物

这些路径只出现在源码仓库或 CI/开发环境，不会由线上安装写入服务器项目目录：

```text
<仓库>/bin/monitorkit                         # make build
<仓库>/dist/monitorkit_linux_<arch>_<version> # build_linux.sh 默认输出
<仓库>/release-notes.md                       # generate_release_notes.sh 默认输出
${TMPDIR:-/tmp}/monitorkit-installer-test.*   # 安装器测试，退出时清理
${TMPDIR:-/tmp}/tmp.*                          # 发布说明测试，退出时清理
Go 构建缓存                                   # 位置由 GOCACHE 决定
```

## 不会删除的共享系统目录

任何卸载模式都只删除明确列出的目标，不会删除这些共享父目录：

```text
/usr/local/sbin/
/usr/local/bin/
/etc/systemd/system/
/etc/
/var/lib/
/tmp/
```

上述共享父目录本身不会因为卸载单个组件而被删除；只会删除清单中明确属于目标组件的文件、链接或子目录。
