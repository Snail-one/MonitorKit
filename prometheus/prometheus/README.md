# Prometheus 一键安装

适用于使用 systemd 的 Linux 主机，执行命令即可完成下载、校验、安装并启动 Prometheus。

## 一键在线安装

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/Snailbash/main/prometheus/prometheus/install.sh | sudo bash
```

运行后可选择普通 HTTP 或 mTLS 安装方式。

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

每段内容粘贴完成后，需要单独输入一行 `EOF`。私钥输入不会在终端回显。证书和生成的 Web 配置保存在 `/etc/prometheus`，服务使用 `RequireAndVerifyClientCert` 强制校验客户端证书。启用后访问协议变为 HTTPS；抓取受 mTLS 保护的目标时，还需要在 `prometheus.yml` 中配置对应客户端证书。

## 卸载

在线卸载：

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/Snailbash/main/prometheus/prometheus/install.sh | sudo bash -s -- uninstall
```

本地卸载：

```bash
sudo ./install.sh uninstall
```

卸载命令会停止服务，并删除 Prometheus 服务文件、`prometheus` 和 `promtool` 程序。配置目录 `/etc/prometheus`、mTLS 证书副本、数据目录 `/var/lib/prometheus` 及服务账号会被保留，方便以后恢复或重新安装。

脚本会在终端中显示彩色步骤和结果提示。设置 `NO_COLOR=1` 可关闭颜色，设置 `FORCE_COLOR=1` 可强制开启颜色。
