# 文件写入与卸载清理边界

本文记录 MonitorKit 管理程序、中心组件和探针在默认配置下写入的系统路径，以及不同卸载命令会删除或保留的内容。通过环境变量修改路径时，以实际配置为准。

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
/usr/local/bin/monitorkit
```

安装期间还会使用以下临时文件，正常结束或失败退出时自动删除：

```text
/tmp/monitorkit-install.*/
/usr/local/bin/.monitorkit.new.<PID>
```

`MONITORKIT_INSTALL_DIR` 和 `MONITORKIT_BINARY_NAME` 可以改变实际安装位置及文件名。

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
├── web.yml                 # 启用 mTLS 时的受管 Web 配置
├── probes/
│   ├── inventory.json         # node_exporter 接入清单、启停状态和目标地址
│   └── <probe-id>/            # 对应探针使用 mTLS 时
│       ├── ca.crt              # node_exporter 服务端 CA
│       ├── client.crt          # Prometheus 客户端证书
│       └── client.key          # Prometheus 客户端私钥
└── tls/                    # 启用或曾启用 mTLS 时
    ├── server.crt
    ├── server.key
    ├── client-ca.crt
    └── mtls.enabled        # 仅在 mTLS 启用时存在
/var/lib/prometheus/
/etc/systemd/system/prometheus.service
/etc/systemd/system/multi-user.target.wants/prometheus.service
系统用户：prometheus
系统组：prometheus
```

首次安装从 `10000-59999` 中选择一个当时可用的随机端口。更新会读取 `listen.port` 并保持端口不变，同时替换二进制和 systemd unit；已经存在的 `prometheus.yml` 会保留，不会被默认配置覆盖。存在 `mtls.enabled` 时，新 unit 会继续引用 `web.yml`，不会在更新后退回 HTTP。远程写入默认关闭，只有 mTLS 已启用且存在 `remote-write.enabled` 时，unit 才会开放 `/api/v1/write`；关闭 mTLS 会同步删除该开关标记。

添加、修改、启停或删除 node_exporter 接入时，MonitorKit 会更新 `inventory.json` 和 `prometheus.yml` 中带有 `BEGIN/END MONITORKIT MANAGED PROBES` 标记的受管 scrape job。新配置只有通过 `promtool` 校验并成功 reload/restart 后才生效，失败会恢复清单和主配置。删除接入配置会删除该探针在中心端保存的 mTLS 文件，但不会连接远程服务器或卸载远程 node_exporter。

从配置菜单直接编辑 `prometheus.yml` 时，原配置内容只在操作期间保存。新配置通过 `promtool` 校验后执行 reload；失败时即时清理无效修改并恢复原配置，不生成残留文件。配置操作及安装/更新会删除旧版本遗留的同名 `.rejected-*` 普通文件。

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
├── listen.port             # 首次安装生成，记录当前随机或自定义监听端口
└── tls/                    # 启用或曾启用 mTLS 时
    ├── server.crt
    ├── server.key
    ├── client-ca.crt
    └── mtls.enabled        # 仅在 mTLS 启用时存在
/var/lib/loki/
├── chunks/
├── rules/
└── 运行时生成的索引及缓存
/etc/systemd/system/loki.service
/etc/systemd/system/multi-user.target.wants/loki.service
系统用户：loki
系统组：loki
```

首次安装从 `10000-59999` 中选择一个当时可用的随机端口。更新会读取 `listen.port` 并保持端口不变，同时替换二进制和 systemd unit；已经存在的 `loki.yml` 会保留。存在 `mtls.enabled` 时，新 unit 会继续带有 Loki HTTPS 与客户端证书验证参数。

从配置菜单直接编辑 `loki.yml` 时，会在内存中保存原内容，再使用 Loki 的 `-verify-config=true` 校验并重启。校验或启动失败时即时清理无效修改并恢复原配置，不保留拒绝文件。

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

根据发行版，还会新增 Grafana 软件源：

```text
Debian/Ubuntu:
  /etc/apt/keyrings/grafana.asc
  /etc/apt/sources.list.d/grafana.list

RHEL/Fedora:
  /etc/yum.repos.d/grafana.repo
  RPM 密钥数据库中的 Grafana 签名密钥

SUSE/openSUSE:
  zypper 中名为 grafana 的软件源配置
```

Alloy 软件包自身还会安装发行版管理的二进制、systemd unit、默认环境文件和 `alloy` 系统账号；具体路径由对应版本的软件包决定。

普通卸载会停止、禁用服务并调用 `apt-get remove`、`dnf remove`、`yum remove` 或 `zypper remove`，但脚本不会主动删除：

```text
/etc/alloy/
/var/lib/alloy/
Grafana 软件源与签名密钥
alloy 用户、组及其附加组成员关系
```

`purge` 会在卸载软件包后额外删除 `/etc/alloy/` 和 `/var/lib/alloy/`。它仍不会删除 Grafana 软件源、签名密钥或主动删除 `alloy` 系统账号；包管理器自身是否清理账号及其他包文件取决于发行版的软件包卸载脚本。Debian/Ubuntu 使用的是 `apt-get remove`，不是 `apt-get purge`。

卸载 Alloy 不会卸载同机存在的 node_exporter，也不会删除中心服务器上的 Prometheus/Loki 数据。

## 不会删除的共享系统目录

任何卸载模式都只删除明确列出的目标，不会删除这些共享父目录：

```text
/usr/local/bin/
/etc/systemd/system/
/etc/
/var/lib/
/tmp/
```

下载和解压使用的 MonitorKit 临时目录会在进程正常结束或返回错误时清理；系统强制断电或 `SIGKILL` 时可能遗留带有 `monitorkit-install.*`、`monitorkit-*` 等前缀的临时目录。
