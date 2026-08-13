# Prometheus 一键安装

适用于使用 systemd 的 Linux 主机，执行命令即可完成下载、校验、安装并启动 Prometheus。

## 一键在线安装

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/Snailbash/main/prometheus/prometheus/install.sh | sudo bash
```

运行后可选择 mTLS 安装、HTTP 安装、标准卸载或彻底清理；直接回车默认安装 mTLS。只有明确选择普通 HTTP 或传入 `http` 参数时才关闭 mTLS。

## 本地安装

```bash
sudo ./install.sh
```

脚本默认通过 GitHub Releases API 获取并安装 Prometheus 最新正式版本。检测到代理环境且 API 访问失败时，会自动绕过代理进行直连重试。默认安装方式为 mTLS，监听端口为 `9090`，配置文件位于 `/etc/prometheus/prometheus.yml`。

脚本会先检测本机是否已经安装 Prometheus：

1. 未安装：获取并安装最新正式版本。
2. 已安装：显示当前版本，并选择“检查最新版本并更新”或“仅重新配置”。
3. 检查更新：保留现有证书、Prometheus 配置和 systemd 服务参数；已经是目标版本时不会重复下载安装包。
4. 仅重新配置：不访问 GitHub Releases API、不下载安装包，只重新配置 HTTP/mTLS、证书和 systemd 服务，默认选择此项。

所有交互菜单输入错误时会提示并要求重新输入，不会直接退出。输入 `q` 可以返回主菜单；在证书编辑器打开前输入 `q` 也会取消本次配置并返回主菜单。

本地安装时可指定固定版本或监听地址：

```bash
sudo PROMETHEUS_VERSION=3.13.1 \
  PROMETHEUS_LISTEN_ADDRESS=127.0.0.1:9090 \
  ./install.sh
```

查看服务状态：

```bash
systemctl status prometheus
```

## mTLS 安装

证书签发类型、Prometheus 服务端/客户端身份和完整部署关系，请先阅读：[Prometheus mTLS 证书说明](MTLS_CERTIFICATES.md)。

在线安装并启用 mTLS：

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/Snailbash/main/prometheus/prometheus/install.sh | sudo bash -s -- mtls
```

本地安装并启用 mTLS：

```bash
sudo ./install.sh mtls
```

脚本会按照 `vim → nano → vi` 的优先级自动选择已安装的编辑器，然后依次打开：

1. `/etc/prometheus/tls/server.crt`：服务端证书
2. `/etc/prometheus/tls/server.key`：服务端私钥
3. `/etc/prometheus/tls/client-ca.crt`：信任访问 Prometheus 的客户端证书的根 CA 证书（信任锚）

每一步都会显示文件路径和对应的 `sudo vim`、`sudo nano` 或 `sudo vi` 命令。按回车后脚本会直接打开编辑器，粘贴 PEM 内容并保存退出即可，不需要输入 `EOF`。脚本随后使用 OpenSSL 检查证书和私钥格式，并检查服务端证书与私钥是否匹配；检查失败会要求重新编辑。

如果三种编辑器都没有安装，脚本会停止并提示先安装任意一个，例如 Debian/Ubuntu 可以执行：

```bash
sudo apt update
sudo apt install vim
```

证书和生成的 Web 配置保存在 `/etc/prometheus`，服务使用 `RequireAndVerifyClientCert` 强制校验客户端证书。启用后访问协议变为 HTTPS；抓取受 mTLS 保护的目标时，还需要在 `prometheus.yml` 中配置对应客户端证书。

### 需要提供的 PEM 内容

| 内容 | 要求 |
| --- | --- |
| 服务端证书 | PEM 格式，访问域名或 IP 应包含在证书 SAN 中，可包含完整证书链 |
| 服务端私钥 | 与服务端证书匹配的未加密 PEM 私钥 |
| 客户端根 CA 证书（信任锚） | 用于建立和验证客户端证书的信任链；不是客户端证书，也不能填写 CA 私钥；使用中间 CA 时可包含对应 CA 证书链 |

### 安装后的文件位置

| 文件 | 路径 | 权限 |
| --- | --- | --- |
| 服务端证书 | `/etc/prometheus/tls/server.crt` | `root:prometheus 0640` |
| 服务端私钥 | `/etc/prometheus/tls/server.key` | `root:prometheus 0640` |
| 客户端根 CA 证书（信任锚） | `/etc/prometheus/tls/client-ca.crt` | `root:prometheus 0640` |
| mTLS Web 配置 | `/etc/prometheus/web.yml` | `root:prometheus 0640` |
| Prometheus 配置 | `/etc/prometheus/prometheus.yml` | `root:prometheus 0640` |
| systemd 服务 | `/etc/systemd/system/prometheus.service` | `root:root 0644` |
| 监控数据 | `/var/lib/prometheus` | `prometheus:prometheus 0750` |

客户端访问 Prometheus Web UI 或 API 时，还需要客户端证书、客户端私钥和用于验证 Prometheus 服务端证书的 CA。客户端证书必须由上述 `client-ca.crt` 所信任。

### 抓取 mTLS node_exporter

如果 node_exporter 也启用了 mTLS，需要在 Prometheus 主机准备以下客户端文件（这些文件不会由当前脚本自动创建）：

```text
/etc/prometheus/client/node-server-ca.crt
/etc/prometheus/client/prometheus-client.crt
/etc/prometheus/client/prometheus-client.key
```

然后修改 `/etc/prometheus/prometheus.yml`：

```yaml
scrape_configs:
  - job_name: node
    scheme: https
    static_configs:
      - targets: ["node.example.com:9100"]
    tls_config:
      ca_file: /etc/prometheus/client/node-server-ca.crt
      cert_file: /etc/prometheus/client/prometheus-client.crt
      key_file: /etc/prometheus/client/prometheus-client.key
      server_name: node.example.com
```

`prometheus-client.crt` 必须由 node_exporter 配置的客户端 CA 签发或信任，`server_name` 必须与 node_exporter 服务端证书的 SAN 匹配。

## 交互添加 node_exporter 探针

本机已安装 Prometheus 时重新运行脚本，选择：

```text
3. 添加 node_exporter 探针
```

向导会依次收集：

1. 探针地址，例如 `node01.example.com:9100`。
2. Prometheus 任务名称。
3. 连接方式，默认使用 mTLS/HTTPS，也可以选择普通 HTTP。
4. mTLS 模式下依次填写 node_exporter 服务端根 CA、Prometheus 客户端证书和客户端私钥。

证书输入方式与 Prometheus 主程序的 mTLS 配置相同：脚本按照 `vim → nano → vi` 自动选择编辑器，显示固定文件路径，按回车打开文件并粘贴 PEM 内容，保存退出后自动校验。无需手动输入路径。

固定文件路径：

```text
/etc/prometheus/client/node-server-ca.crt
/etc/prometheus/client/prometheus-client.crt
/etc/prometheus/client/prometheus-client.key
```

脚本会校验证书和私钥的 PEM 格式，并检查 Prometheus 客户端证书与客户端私钥是否匹配。输入 `q` 可以随时取消并返回主菜单。已有文件不会被预先清空，打开编辑器时可以直接检查或更新现有内容。

向导不会直接修改正在使用的配置。它会先生成候选配置并执行 `promtool check config`；校验通过后才写入 `/etc/prometheus/prometheus.yml` 并重载服务。配置校验失败时原配置不变，服务重载失败时会自动恢复原配置。

探针地址使用的域名或 IP 必须包含在 node_exporter 服务端证书 SAN 中。配置中已经存在相同地址时，向导不会重复添加。

## 卸载

在线卸载：

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/Snailbash/main/prometheus/prometheus/install.sh | sudo bash -s -- uninstall
```

运行后可以选择：

1. 标准卸载：删除服务和程序，保留配置、mTLS 证书、监控数据及系统账号。
2. 彻底清理：删除服务、程序、配置、mTLS 证书、监控数据及系统账号。

本地卸载：

```bash
sudo ./install.sh uninstall
```

自动化环境可以直接执行彻底清理：

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/Snailbash/main/prometheus/prometheus/install.sh | sudo bash -s -- purge
```

本地直接彻底清理：

```bash
sudo ./install.sh purge
```

彻底清理会永久删除 `/etc/prometheus` 和 `/var/lib/prometheus`，历史监控数据无法通过脚本恢复。

脚本会在终端中显示彩色步骤和结果提示。设置 `NO_COLOR=1` 可关闭颜色，设置 `FORCE_COLOR=1` 可强制开启颜色。
