# Node Exporter 安装落盘说明

本文档对应当前目录下的 `install.sh`，说明脚本安装、更新或重新配置 Node Exporter 时创建的目录、文件、系统用户和系统组，以及各项内容的用途和卸载行为。

## 安装结果概览

普通 HTTP 模式的主要落盘内容如下：

```text
/usr/local/bin/node_exporter
/etc/systemd/system/node_exporter.service
/etc/systemd/system/multi-user.target.wants/node_exporter.service -> ../node_exporter.service
```

mTLS 模式还会创建：

```text
/etc/node_exporter/
├── web.yml
└── tls/
    ├── server.crt
    ├── server.key
    └── client-ca.crt
```

Node Exporter 本身是无状态指标采集程序。该脚本不会为它创建类似 Prometheus TSDB 的持久化数据目录，也不会把采集结果写入本地数据文件。

## 系统用户和系统组

| 类型 | 名称 | 创建方式和属性 | 用途 |
| --- | --- | --- | --- |
| 系统组 | `node_exporter` | `groupadd --system node_exporter` | 限制 Node Exporter 进程及证书文件的访问范围 |
| 系统用户 | `node_exporter` | 系统用户；主组为 `node_exporter`；主目录声明为 `/nonexistent`；不创建主目录；登录 Shell 为 `/usr/sbin/nologin` | 以非 root 身份运行 `node_exporter.service` |

只有对应用户或组不存在时脚本才会创建；已经存在的同名用户或组不会被重建。`/nonexistent` 只是账号记录中的主目录字段，脚本不会创建该目录。

创建账号时，`useradd` 和 `groupadd` 会更新操作系统的账号数据库，通常包括 `/etc/passwd`、`/etc/group`、`/etc/shadow` 和 `/etc/gshadow`；具体文件由所在发行版的账号管理机制决定。

## 长期保留的目录和文件

### `/usr/local/bin/node_exporter`

| 属性 | 说明 |
| --- | --- |
| 类型 | ELF 可执行文件 |
| 权限 | `0755` |
| 所有者 | 脚本以 root 运行时通常为 `root:root` |
| 创建条件 | 首次安装，或者目标版本与当前版本不同并执行更新 |
| 用途 | Node Exporter 主程序；读取主机内核和系统信息，并通过 `/metrics` 暴露指标 |

如果检查更新时当前版本已经是目标版本，或者选择“仅重新配置”，脚本不会替换这个文件。

### `/etc/systemd/system/node_exporter.service`

| 属性 | 说明 |
| --- | --- |
| 类型 | systemd 服务单元 |
| 权限 | `0644` |
| 所有者 | 脚本以 root 运行时通常为 `root:root` |
| 创建条件 | 首次安装或“仅重新配置”；单纯执行版本更新时保留现有文件 |
| 用途 | 指定运行用户、监听地址、安全限制、自动重启和开机启动目标 |

核心启动方式如下：

```text
/usr/local/bin/node_exporter --web.listen-address=<监听地址>
```

mTLS 模式会额外带上：

```text
--web.config.file=/etc/node_exporter/web.yml
```

服务使用 `User=node_exporter`、`Group=node_exporter`，并启用了 `NoNewPrivileges`、`PrivateTmp`、`ProtectHome`、`ProtectSystem=strict` 等 systemd 安全限制。脚本没有为该服务配置 `ExecReload`，因此修改 Web 配置或证书后应执行：

```bash
sudo systemctl restart node_exporter
```

### `/etc/systemd/system/multi-user.target.wants/node_exporter.service`

这是 `systemctl enable node_exporter.service` 创建的开机启动软链接，目标是 `/etc/systemd/system/node_exporter.service`。它不是脚本通过普通文件写入命令直接生成的，但属于安装产生的系统状态。

### `/etc/node_exporter/`

| 属性 | 说明 |
| --- | --- |
| 创建条件 | 启用 mTLS 时创建；纯 HTTP 的全新安装不会创建 |
| 最终所有者 | `root:node_exporter` |
| 最终权限 | `0750` |
| 用途 | 保存 Node Exporter Web TLS 配置和证书 |

目录只允许 root 和 `node_exporter` 组访问。由 mTLS 切换为 HTTP 时，服务不再引用这里的配置，但脚本不会自动删除已有目录和证书。

### `/etc/node_exporter/web.yml`

| 属性 | 说明 |
| --- | --- |
| 所有者 | `root:node_exporter` |
| 权限 | `0640` |
| 创建条件 | mTLS 模式 |
| 用途 | 开启 HTTPS，并强制校验访问 `/metrics` 的客户端证书 |

脚本生成的配置等价于：

```yaml
tls_server_config:
  cert_file: /etc/node_exporter/tls/server.crt
  key_file: /etc/node_exporter/tls/server.key
  client_auth_type: RequireAndVerifyClientCert
  client_ca_file: /etc/node_exporter/tls/client-ca.crt
  min_version: TLS12
```

### `/etc/node_exporter/tls/`

| 属性 | 说明 |
| --- | --- |
| 所有者 | `root:node_exporter` |
| 权限 | `0750` |
| 创建条件 | mTLS 模式 |
| 用途 | 保存 Node Exporter 服务端身份和客户端信任材料 |

目录中的文件均为 `root:node_exporter`、权限 `0640`：

| 文件 | 作用 | 是否敏感 |
| --- | --- | --- |
| `server.crt` | Node Exporter 向 Prometheus 出示的 HTTPS 服务端证书；SAN 应包含 Prometheus 实际访问的域名或 IP | 可公开，但不应随意篡改 |
| `server.key` | 与 `server.crt` 配对的未加密服务端私钥 | 高度敏感，不能泄露 |
| `client-ca.crt` | 用于验证 Prometheus 客户端证书的根 CA 公共证书或 CA 证书链 | 公共信任材料，不能随意替换 |

Prometheus 自己出示的客户端证书和私钥不存放在这里，而通常存放在 Prometheus 主机的 `/etc/prometheus/client/` 中。

## 临时落盘内容

脚本使用 `mktemp` 创建临时文件。默认位置通常是 `/tmp`；设置了 `TMPDIR` 时，以系统 `mktemp` 的实际行为为准。

| 临时路径 | 内容 | 清理行为 |
| --- | --- | --- |
| `node-exporter-install.XXXXXXXX/` | GitHub Release 元数据、压缩包、校验文件、解压目录和临时 `mtls-web.yml` | 正常完成及脚本捕获退出时删除 |
| `node-exporter-service.XXXXXXXX` | 写入正式 systemd 单元前的临时文件 | 安装到正式路径后删除 |

进程被 `SIGKILL`、机器掉电或系统异常中断时，临时内容可能残留，需要人工检查 `/tmp` 或 `TMPDIR`。

## 日志和运行时文件

脚本没有创建 `/var/log/node_exporter/`，也没有为 Node Exporter 配置独立日志文件。标准输出和标准错误由 systemd journal 接收：

```bash
journalctl -u node_exporter
```

journal 可能持久化到 `/var/log/journal/`，也可能只保存在 `/run/log/journal/`；这由操作系统的 journald 配置决定，不是本脚本决定的。

服务设置了 `PrivateTmp=true`，systemd 可能为服务建立私有的临时目录命名空间；这是运行时系统行为，不是脚本保存业务数据的位置。

## 不会创建的内容

- 不创建 Node Exporter 数据库或指标历史数据目录。
- 不创建 `/var/log/node_exporter/`。
- 不创建 `/nonexistent` 主目录。
- 不修改防火墙规则。
- 不创建 cron 任务。

## 卸载行为

### 标准卸载

会停止并禁用服务，然后删除：

```text
/usr/local/bin/node_exporter
/etc/systemd/system/node_exporter.service
/etc/systemd/system/multi-user.target.wants/node_exporter.service
```

会保留：

```text
/etc/node_exporter/
node_exporter 系统用户
node_exporter 系统组
```

### 彻底清理（purge）

在标准卸载基础上继续删除：

```text
/etc/node_exporter/
node_exporter 系统用户
node_exporter 系统组
```

证书和私钥会随 `/etc/node_exporter/` 永久删除，执行前应确认是否需要备份。

