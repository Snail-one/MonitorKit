# Prometheus 一键安装

适用于使用 systemd 的 Linux 主机，执行命令即可完成下载、校验、安装并启动 Prometheus。

## 一键在线安装

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/Snailbash/main/prometheus/prometheus/install.sh | sudo bash
```

运行后可选择 HTTP 安装、mTLS 安装、标准卸载或彻底清理。

## 本地安装

```bash
sudo ./install.sh
```

脚本默认通过 GitHub Releases API 获取并安装 Prometheus 最新正式版本。检测到代理环境且 API 访问失败时，会自动绕过代理进行直连重试。默认监听端口为 `9090`，配置文件位于 `/etc/prometheus/prometheus.yml`。

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

在线安装并启用 mTLS：

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/Snailbash/main/prometheus/prometheus/install.sh | sudo bash -s -- mtls
```

本地安装并启用 mTLS：

```bash
sudo ./install.sh mtls
```

脚本会依次要求粘贴：

1. 服务端证书 PEM 内容
2. 服务端私钥 PEM 内容
3. 用于验证客户端证书的 CA PEM 内容

每段内容粘贴完成后，需要单独输入一行 `EOF`。输入的证书和私钥内容会正常显示在终端中，便于粘贴时检查。证书和生成的 Web 配置保存在 `/etc/prometheus`，服务使用 `RequireAndVerifyClientCert` 强制校验客户端证书。启用后访问协议变为 HTTPS；抓取受 mTLS 保护的目标时，还需要在 `prometheus.yml` 中配置对应客户端证书。

### 需要提供的 PEM 内容

| 内容 | 要求 |
| --- | --- |
| 服务端证书 | PEM 格式，访问域名或 IP 应包含在证书 SAN 中，可包含完整证书链 |
| 服务端私钥 | 与服务端证书匹配的未加密 PEM 私钥 |
| 客户端 CA | 用于签发和验证客户端证书的 CA，可包含 CA 证书链 |

### 安装后的文件位置

| 文件 | 路径 | 权限 |
| --- | --- | --- |
| 服务端证书 | `/etc/prometheus/tls/server.crt` | `root:prometheus 0640` |
| 服务端私钥 | `/etc/prometheus/tls/server.key` | `root:prometheus 0640` |
| 客户端 CA | `/etc/prometheus/tls/client-ca.crt` | `root:prometheus 0640` |
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
