# Prometheus 安装落盘说明

本文档对应当前目录下的 `install.sh`，说明脚本安装、更新、重新配置或添加 node_exporter 探针时创建的目录、文件、系统用户和系统组，以及各项内容的用途和卸载行为。

## 安装结果概览

普通 HTTP 模式的主要落盘内容如下：

```text
/usr/local/bin/
├── prometheus
└── promtool

/etc/systemd/system/prometheus.service
/etc/systemd/system/multi-user.target.wants/prometheus.service -> ../prometheus.service

/etc/prometheus/
├── prometheus.yml
├── consoles/                   # 发布包包含时
└── console_libraries/          # 发布包包含时

/var/lib/prometheus/            # Prometheus TSDB 数据
```

Prometheus Web 端启用 mTLS 时还会创建：

```text
/etc/prometheus/
├── web.yml
└── tls/
    ├── server.crt
    ├── server.key
    └── client-ca.crt
```

通过脚本向导添加 mTLS node_exporter 探针时，还可能创建：

```text
/etc/prometheus/client/
├── node-server-ca.crt
├── prometheus-client.crt
├── prometheus-client.key
└── <探针主机名>/              # 选择独立证书组时
    ├── node-server-ca.crt
    ├── prometheus-client.crt
    └── prometheus-client.key
```

## 系统用户和系统组

| 类型 | 名称 | 创建方式和属性 | 用途 |
| --- | --- | --- | --- |
| 系统组 | `prometheus` | `groupadd --system prometheus` | 限制 Prometheus 进程、配置和证书的访问范围 |
| 系统用户 | `prometheus` | 系统用户；主组为 `prometheus`；主目录声明为 `/var/lib/prometheus`；不由 `useradd` 创建主目录；登录 Shell 为 `/usr/sbin/nologin` | 以非 root 身份运行 Prometheus，并拥有 TSDB 数据目录 |

只有对应用户或组不存在时脚本才会创建；已经存在的同名用户或组不会被重建。虽然 `useradd` 使用 `--no-create-home`，脚本随后会明确创建 `/var/lib/prometheus/` 作为数据目录。

创建账号时，`useradd` 和 `groupadd` 会更新操作系统的账号数据库，通常包括 `/etc/passwd`、`/etc/group`、`/etc/shadow` 和 `/etc/gshadow`；具体文件由所在发行版的账号管理机制决定。

## 二进制文件

### `/usr/local/bin/prometheus`

| 属性 | 说明 |
| --- | --- |
| 类型 | ELF 可执行文件 |
| 权限 | `0755` |
| 所有者 | 脚本以 root 运行时通常为 `root:root` |
| 创建条件 | 首次安装，或者目标版本与当前版本不同并执行更新 |
| 用途 | Prometheus 服务主程序，负责抓取、存储、查询和规则计算 |

### `/usr/local/bin/promtool`

| 属性 | 说明 |
| --- | --- |
| 类型 | ELF 可执行文件 |
| 权限 | `0755` |
| 所有者 | 脚本以 root 运行时通常为 `root:root` |
| 创建条件 | 与 `prometheus` 二进制同时安装或更新 |
| 用途 | Prometheus 配套管理工具；脚本使用它校验 `/etc/prometheus/prometheus.yml`，也可用于检查规则和运行测试 |

常用检查命令：

```bash
/usr/local/bin/promtool check config /etc/prometheus/prometheus.yml
```

如果检查更新时当前版本已经是目标版本，或者选择“仅重新配置”，脚本不会替换上述两个二进制文件。

## systemd 文件

### `/etc/systemd/system/prometheus.service`

| 属性 | 说明 |
| --- | --- |
| 类型 | systemd 服务单元 |
| 权限 | `0644` |
| 所有者 | 脚本以 root 运行时通常为 `root:root` |
| 创建条件 | 首次安装或“仅重新配置”；单纯执行版本更新时保留现有文件 |
| 用途 | 指定运行用户、配置文件、TSDB 路径、监听地址、安全限制、热加载和开机启动目标 |

核心启动方式如下：

```text
/usr/local/bin/prometheus \
  --config.file=/etc/prometheus/prometheus.yml \
  --storage.tsdb.path=/var/lib/prometheus \
  --web.listen-address=<监听地址>
```

Prometheus Web 端启用 mTLS 时会额外带上：

```text
--web.config.file=/etc/prometheus/web.yml
```

服务使用 `User=prometheus`、`Group=prometheus`，通过 `ReadWritePaths=/var/lib/prometheus` 只允许服务在数据目录写入，并配置：

```ini
ExecReload=/bin/kill -HUP $MAINPID
```

因此修改并校验 `prometheus.yml` 后可以执行：

```bash
sudo systemctl reload prometheus
```

修改启动参数、systemd 单元、Web TLS 配置或证书时，建议执行：

```bash
sudo systemctl restart prometheus
```

### `/etc/systemd/system/multi-user.target.wants/prometheus.service`

这是 `systemctl enable prometheus.service` 创建的开机启动软链接，目标是 `/etc/systemd/system/prometheus.service`。它不是脚本通过普通文件写入命令直接生成的，但属于安装产生的系统状态。

## 配置目录 `/etc/prometheus/`

脚本最终将目录设置为 `root:prometheus`、权限 `0750`。这使 root 可以修改配置，而 Prometheus 进程能够通过组权限读取配置。

### `/etc/prometheus/prometheus.yml`

| 属性 | 说明 |
| --- | --- |
| 最终所有者 | `root:prometheus` |
| 权限 | `0640` |
| 创建条件 | 文件不存在时创建；已存在时保留 |
| 用途 | Prometheus 主配置，定义全局参数、抓取任务、规则文件、告警管理器等 |

新建的默认配置包含 Prometheus 自身和本机 Node Exporter 两个抓取目标：

```yaml
global:
  scrape_interval: 15s
  evaluation_interval: 15s

scrape_configs:
  - job_name: prometheus
    static_configs:
      - targets: ["localhost:9090"]

  - job_name: node
    static_configs:
      - targets: ["localhost:9100"]
```

其中 Prometheus 端口会根据 `PROMETHEUS_LISTEN_ADDRESS` 的端口部分生成。脚本每次安装流程都会使用 `promtool check config` 检查该文件。

通过“添加 node_exporter 探针”向导时，脚本会校验候选配置，成功后以 `root:prometheus`、`0640` 覆盖该文件，再通过 `systemctl reload prometheus.service` 应用；重载失败则恢复备份。

### `/etc/prometheus/web.yml`

| 属性 | 说明 |
| --- | --- |
| 所有者 | `root:prometheus` |
| 权限 | `0640` |
| 创建条件 | Prometheus Web 端启用 mTLS |
| 用途 | 开启 Prometheus Web/API 的 HTTPS，并强制校验访问者的客户端证书 |

脚本生成的配置等价于：

```yaml
tls_server_config:
  cert_file: /etc/prometheus/tls/server.crt
  key_file: /etc/prometheus/tls/server.key
  client_auth_type: RequireAndVerifyClientCert
  client_ca_file: /etc/prometheus/tls/client-ca.crt
  min_version: TLS12
```

### `/etc/prometheus/tls/`

| 属性 | 说明 |
| --- | --- |
| 所有者 | `root:prometheus` |
| 权限 | `0750` |
| 创建条件 | Prometheus Web 端启用 mTLS |
| 用途 | 保存 Prometheus Web 服务端身份和访问者信任材料 |

目录中的文件均为 `root:prometheus`、权限 `0640`：

| 文件 | 作用 | 是否敏感 |
| --- | --- | --- |
| `server.crt` | Prometheus Web/API 向访问者出示的 HTTPS 服务端证书 | 可公开，但不应随意篡改 |
| `server.key` | 与 `server.crt` 配对的未加密服务端私钥 | 高度敏感，不能泄露 |
| `client-ca.crt` | 用于验证访问 Prometheus 的客户端证书的根 CA 公共证书或 CA 证书链 | 公共信任材料，不能随意替换 |

Prometheus Web 端 mTLS 与 Prometheus 抓取 node_exporter 使用的 mTLS 是两个方向，不能混淆：前者使用本目录，后者使用 `/etc/prometheus/client/`。

### `/etc/prometheus/client/`

| 属性 | 说明 |
| --- | --- |
| 所有者 | `root:prometheus` |
| 权限 | `0750` |
| 创建条件 | 使用脚本向导添加 HTTPS/mTLS node_exporter 探针且需要录入证书 |
| 用途 | 保存 Prometheus 抓取 node_exporter 时出示和验证所需的证书 |

默认共享证书组包含以下文件，均为 `root:prometheus`、权限 `0640`：

| 文件 | 作用 | 是否敏感 |
| --- | --- | --- |
| `node-server-ca.crt` | 验证 node_exporter HTTPS 服务端证书的根 CA 公共证书 | 公共信任材料 |
| `prometheus-client.crt` | Prometheus 连接 node_exporter 时出示的客户端证书 | 可公开，但不应随意篡改 |
| `prometheus-client.key` | 与客户端证书配对的未加密私钥 | 高度敏感，不能泄露 |

如果添加探针时选择“使用另一套 CA 和客户端证书”，脚本会创建：

```text
/etc/prometheus/client/<规范化后的探针主机名>/
```

主机名中的字母、数字、点、下划线和连字符会保留，其他字符会替换为下划线。每个独立目录包含同名的三份证书文件。选择普通 HTTP 探针时不会创建这些证书目录。

### `/etc/prometheus/consoles/`

| 属性 | 说明 |
| --- | --- |
| 目录权限 | 创建时为 `0755` |
| 最终所有者 | `root:prometheus` |
| 创建条件 | 下载的 Prometheus 发布包中存在 `consoles` 目录 |
| 用途 | Prometheus 内置 Console 模板页面 |

### `/etc/prometheus/console_libraries/`

| 属性 | 说明 |
| --- | --- |
| 目录权限 | 创建时为 `0755` |
| 最终所有者 | `root:prometheus` |
| 创建条件 | 下载的 Prometheus 发布包中存在 `console_libraries` 目录 |
| 用途 | Console 模板使用的公共资源和库 |

发布包内容使用 `cp -a` 复制，内部文件权限会沿用发布包中的权限。较新的发布包如果不再包含这些目录，脚本不会创建它们。

由 mTLS 切换为 HTTP 时，服务不再引用 `/etc/prometheus/web.yml`，但脚本不会自动删除已有的 `web.yml`、`tls/` 或 `client/`。

## 数据目录 `/var/lib/prometheus/`

| 属性 | 说明 |
| --- | --- |
| 所有者 | `prometheus:prometheus` |
| 权限 | `0750` |
| 创建条件 | 每次正常安装、更新或重新配置流程都会确保存在 |
| 用途 | Prometheus TSDB 持久化数据目录，也是 `prometheus` 账号声明的主目录 |

Prometheus 服务运行后会在这里持续写入 WAL、内存块持久化数据、压缩后的时序数据块、锁文件及查询相关状态。内部结构由所安装的 Prometheus 版本管理，不应手工修改。

这个目录是历史监控数据的核心备份对象。删除它会丢失本机保存的全部历史指标；仅备份 `/etc/prometheus/` 并不能备份监控历史数据。

## 临时落盘内容

脚本使用 `mktemp` 创建临时文件。默认位置通常是 `/tmp`；设置了 `TMPDIR` 时，以系统 `mktemp` 的实际行为为准。

| 临时路径 | 内容 | 清理行为 |
| --- | --- | --- |
| `prometheus-install.XXXXXXXX/` | GitHub Release 元数据、压缩包、校验文件、解压目录和临时 `mtls-web.yml` | 正常完成及脚本捕获退出时删除 |
| `prometheus-add-target.XXXXXXXX/` | 新增探针的 YAML 片段、候选 `prometheus.yml` 和原配置备份 | 成功、校验失败或重载失败后删除 |
| `prometheus-config.XXXXXXXX` | 创建默认 `prometheus.yml` 前的临时文件 | 安装到正式路径后删除 |
| `prometheus-service.XXXXXXXX` | 写入正式 systemd 单元前的临时文件 | 安装到正式路径后删除 |

进程被 `SIGKILL`、机器掉电或系统异常中断时，临时内容可能残留，需要人工检查 `/tmp` 或 `TMPDIR`。

## 日志和运行时文件

脚本没有创建 `/var/log/prometheus/`，也没有为 Prometheus 配置独立日志文件。标准输出和标准错误由 systemd journal 接收：

```bash
journalctl -u prometheus
```

journal 可能持久化到 `/var/log/journal/`，也可能只保存在 `/run/log/journal/`；这由操作系统的 journald 配置决定，不是本脚本决定的。

服务设置了 `PrivateTmp=true`，systemd 可能为服务建立私有的临时目录命名空间；这是运行时系统行为，不是 Prometheus TSDB 数据的位置。

## 不会创建的内容

- 不创建 `/var/log/prometheus/`。
- 不修改防火墙规则。
- 不创建 cron 任务。
- 不单独创建告警规则目录；如果需要 `/etc/prometheus/rules/`，应由管理员自行创建并在 `prometheus.yml` 中引用。
- 不安装 Alertmanager。

## 卸载行为

### 标准卸载

会停止并禁用服务，然后删除：

```text
/usr/local/bin/prometheus
/usr/local/bin/promtool
/etc/systemd/system/prometheus.service
/etc/systemd/system/multi-user.target.wants/prometheus.service
```

会保留：

```text
/etc/prometheus/
/var/lib/prometheus/
prometheus 系统用户
prometheus 系统组
```

标准卸载后配置、证书和历史监控数据仍在，可以用于后续重新安装。

### 彻底清理（purge）

在标准卸载基础上继续删除：

```text
/etc/prometheus/
/var/lib/prometheus/
prometheus 系统用户
prometheus 系统组
```

`/var/lib/prometheus/` 中的全部历史监控数据以及 `/etc/prometheus/` 中的配置、证书和私钥都会永久删除，执行前应确认是否需要备份。

