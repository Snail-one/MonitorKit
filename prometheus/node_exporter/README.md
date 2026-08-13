# node_exporter 探针一键安装

适用于使用 systemd 的 Linux 主机，执行命令即可完成下载、校验、安装并启动 node_exporter。

## 一键在线安装

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/Snailbash/main/prometheus/node_exporter/install.sh | sudo bash
```

运行后可选择 mTLS 安装、HTTP 安装、标准卸载或彻底清理；直接回车默认安装 mTLS。只有明确选择普通 HTTP 或传入 `http` 参数时才关闭 mTLS。

## 本地安装

```bash
sudo ./install.sh
```

脚本默认通过 GitHub Releases API 获取并安装 node_exporter 最新正式版本。检测到代理环境且 API 访问失败时，会自动绕过代理进行直连重试。默认安装方式为 mTLS，监听端口为 `9100`，指标地址为 `https://服务器IP:9100/metrics`。

脚本会先检测本机是否已经安装 node_exporter：

1. 未安装：获取并安装最新正式版本。
2. 已安装：显示当前版本，并选择“检查最新版本并更新”或“仅重新配置”。
3. 检查更新：保留现有证书和 systemd 服务参数；已经是目标版本时不会重复下载安装包。
4. 仅重新配置：不访问 GitHub Releases API、不下载安装包，只重新配置 HTTP/mTLS、证书和 systemd 服务，默认选择此项。

本地安装时可指定固定版本或监听地址：

```bash
sudo NODE_EXPORTER_VERSION=1.12.1 \
  NODE_EXPORTER_LISTEN_ADDRESS=127.0.0.1:9100 \
  ./install.sh
```

查看服务状态：

```bash
systemctl status node_exporter
```

## mTLS 安装

证书签发类型、每个节点的证书规划和完整部署关系，请先阅读：[node_exporter mTLS 证书说明](MTLS_CERTIFICATES.md)。

在线安装并启用 mTLS：

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/Snailbash/main/prometheus/node_exporter/install.sh | sudo bash -s -- mtls
```

本地安装并启用 mTLS：

```bash
sudo ./install.sh mtls
```

脚本会按照 `vim → nano → vi` 的优先级自动选择已安装的编辑器，然后依次打开：

1. `/etc/node_exporter/tls/server.crt`：服务端证书
2. `/etc/node_exporter/tls/server.key`：服务端私钥
3. `/etc/node_exporter/tls/client-ca.crt`：信任 Prometheus 客户端证书的根 CA 证书（信任锚）

每一步都会显示文件路径和对应的 `sudo vim`、`sudo nano` 或 `sudo vi` 命令。按回车后脚本会直接打开编辑器，粘贴 PEM 内容并保存退出即可，不需要输入 `EOF`。脚本随后使用 OpenSSL 检查证书和私钥格式，并检查服务端证书与私钥是否匹配；检查失败会要求重新编辑。

如果三种编辑器都没有安装，脚本会停止并提示先安装任意一个，例如 Debian/Ubuntu 可以执行：

```bash
sudo apt update
sudo apt install vim
```

证书和生成的 Web 配置保存在 `/etc/node_exporter`，服务使用 `RequireAndVerifyClientCert` 强制校验客户端证书，指标地址变为 HTTPS。

### 需要提供的 PEM 内容

| 内容 | 要求 |
| --- | --- |
| 服务端证书 | PEM 格式，访问域名或 IP 应包含在证书 SAN 中，可包含完整证书链 |
| 服务端私钥 | 与服务端证书匹配的未加密 PEM 私钥 |
| Prometheus 客户端根 CA 证书（信任锚） | 用于建立和验证 Prometheus 客户端证书的信任链；不是 Prometheus 客户端证书，也不能填写 CA 私钥；使用中间 CA 时可包含对应 CA 证书链 |

### 安装后的文件位置

| 文件 | 路径 | 权限 |
| --- | --- | --- |
| 服务端证书 | `/etc/node_exporter/tls/server.crt` | `root:node_exporter 0640` |
| 服务端私钥 | `/etc/node_exporter/tls/server.key` | `root:node_exporter 0640` |
| Prometheus 客户端根 CA 证书（信任锚） | `/etc/node_exporter/tls/client-ca.crt` | `root:node_exporter 0640` |
| mTLS Web 配置 | `/etc/node_exporter/web.yml` | `root:node_exporter 0640` |
| systemd 服务 | `/etc/systemd/system/node_exporter.service` | `root:root 0644` |
| node_exporter 程序 | `/usr/local/bin/node_exporter` | `root:root 0755` |

访问 node_exporter 指标接口的客户端还需要：

```text
client.crt       客户端证书
client.key       客户端私钥
server-ca.crt    用于验证 node_exporter 服务端证书的 CA
```

客户端证书必须由 node_exporter 配置的 `client-ca.crt` 所信任。Prometheus 抓取该节点时，需要在对应的 `scrape_config` 中设置 `scheme: https` 和 `tls_config`：

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

`server_name` 必须与 node_exporter 服务端证书的 SAN 匹配。

## 卸载

在线卸载：

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/Snailbash/main/prometheus/node_exporter/install.sh | sudo bash -s -- uninstall
```

运行后可以选择：

1. 标准卸载：删除服务和程序，保留 mTLS 配置、证书及系统账号。
2. 彻底清理：删除服务、程序、mTLS 配置、证书及系统账号。

本地卸载：

```bash
sudo ./install.sh uninstall
```

自动化环境可以直接执行彻底清理：

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/Snailbash/main/prometheus/node_exporter/install.sh | sudo bash -s -- purge
```

本地直接彻底清理：

```bash
sudo ./install.sh purge
```

以上操作均不会影响 Prometheus 服务端。

脚本会在终端中显示彩色步骤和结果提示。设置 `NO_COLOR=1` 可关闭颜色，设置 `FORCE_COLOR=1` 可强制开启颜色。
