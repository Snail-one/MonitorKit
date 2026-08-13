# Prometheus 一键安装

适用于使用 systemd 的 Linux 主机，执行命令即可完成下载、校验、安装并启动 Prometheus。

## 一键在线安装

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/Snailbash/main/prometheus/prometheus/install.sh | sudo bash
```

## 本地安装

```bash
sudo ./install.sh
```

默认监听端口为 `9090`，配置文件位于 `/etc/prometheus/prometheus.yml`。

本地安装时可自定义版本或监听地址：

```bash
sudo PROMETHEUS_VERSION=3.13.1 \
  PROMETHEUS_LISTEN_ADDRESS=127.0.0.1:9090 \
  ./install.sh
```

查看服务状态：

```bash
systemctl status prometheus
```

## 卸载

在线卸载：

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/Snailbash/main/prometheus/prometheus/install.sh | sudo bash -s -- uninstall
```

本地卸载：

```bash
sudo ./install.sh uninstall
```

卸载命令会停止服务，并删除 Prometheus 服务文件、`prometheus` 和 `promtool` 程序。配置目录 `/etc/prometheus`、数据目录 `/var/lib/prometheus` 及服务账号会被保留，方便以后恢复或重新安装。
