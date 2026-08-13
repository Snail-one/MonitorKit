# node_exporter 探针一键安装

适用于使用 systemd 的 Linux 主机，执行命令即可完成下载、校验、安装并启动 node_exporter。

## 一键在线安装

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/Snailbash/main/prometheus/node_exporter/install.sh | sudo bash
```

运行后可选择普通 HTTP 或 mTLS 安装方式。

## 本地安装

```bash
sudo ./install.sh
```

脚本默认通过 GitHub Releases API 获取并安装 node_exporter 最新正式版本。检测到代理环境且 API 访问失败时，会自动绕过代理进行直连重试。默认监听端口为 `9100`，指标地址为 `http://服务器IP:9100/metrics`。

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

在线安装并启用 mTLS：

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/Snailbash/main/prometheus/node_exporter/install.sh | sudo bash -s -- mtls
```

本地安装并启用 mTLS：

```bash
sudo ./install.sh mtls
```

脚本会依次要求粘贴：

1. 服务端证书 PEM 内容
2. 服务端私钥 PEM 内容
3. 用于验证客户端证书的 CA PEM 内容

每段内容粘贴完成后，需要单独输入一行 `EOF`。私钥输入不会在终端回显。证书和生成的 Web 配置保存在 `/etc/node_exporter`，服务使用 `RequireAndVerifyClientCert` 强制校验客户端证书，指标地址变为 HTTPS。

## 卸载

在线卸载：

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/Snailbash/main/prometheus/node_exporter/install.sh | sudo bash -s -- uninstall
```

本地卸载：

```bash
sudo ./install.sh uninstall
```

卸载命令会停止服务，并删除 node_exporter 服务文件、程序文件、mTLS 证书副本、Web 配置及专用系统用户组，不会影响 Prometheus 服务端。

脚本会在终端中显示彩色步骤和结果提示。设置 `NO_COLOR=1` 可关闭颜色，设置 `FORCE_COLOR=1` 可强制开启颜色。
