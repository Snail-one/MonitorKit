# node_exporter 探针一键安装

适用于使用 systemd 的 Linux 主机，执行命令即可完成下载、校验、安装并启动 node_exporter。

## 一键在线安装

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/Snailbash/main/prometheus/node_exporter/install.sh | sudo bash
```

## 本地安装

```bash
sudo ./install.sh
```

默认监听端口为 `9100`，指标地址为 `http://服务器IP:9100/metrics`。

本地安装时可自定义版本或监听地址：

```bash
sudo NODE_EXPORTER_VERSION=1.12.1 \
  NODE_EXPORTER_LISTEN_ADDRESS=127.0.0.1:9100 \
  ./install.sh
```

查看服务状态：

```bash
systemctl status node_exporter
```
