# Snailbash

Snailbash 是一个常用 Linux 服务与运维工具的一键安装脚本集合。

仓库会持续收录 Prometheus、系统监控探针以及其他服务的安装和管理脚本。每个工具使用独立目录存放，目录内包含安装脚本和简单的使用说明。

## 使用说明

运行在线安装命令前，请确认服务器能够访问 GitHub。安装脚本通常需要创建系统用户、写入 systemd 服务以及安装程序到系统目录，因此需要 root 权限。

## 现有脚本

### Prometheus

一键安装 Prometheus 服务端：

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/Snailbash/main/prometheus/prometheus/install.sh | sudo bash
```

运行后可选择普通 HTTP 或 mTLS 安装方式。

详细说明：[prometheus/prometheus/README.md](prometheus/prometheus/README.md)

mTLS 交互安装：

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/Snailbash/main/prometheus/prometheus/install.sh | sudo bash -s -- mtls
```

卸载命令：

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/Snailbash/main/prometheus/prometheus/install.sh | sudo bash -s -- uninstall
```

### node_exporter

一键安装 Prometheus 主机监控探针：

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/Snailbash/main/prometheus/node_exporter/install.sh | sudo bash
```

运行后可选择普通 HTTP 或 mTLS 安装方式。

详细说明：[prometheus/node_exporter/README.md](prometheus/node_exporter/README.md)

mTLS 交互安装：

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/Snailbash/main/prometheus/node_exporter/install.sh | sudo bash -s -- mtls
```

卸载命令：

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/Snailbash/main/prometheus/node_exporter/install.sh | sudo bash -s -- uninstall
```

## 目录结构

```text
Snailbash/
├── README.md
└── prometheus/
    ├── prometheus/
    │   ├── install.sh
    │   └── README.md
    └── node_exporter/
        ├── install.sh
        └── README.md
```

后续新增工具时，将继续按照“工具目录、安装脚本、README 说明”的方式组织。

## 注意事项

- 建议先阅读对应工具目录中的 README。
- 默认配置适合快速部署，生产环境请根据安全要求调整监听地址、防火墙和访问控制。
- 重复执行脚本前，请确认已有配置是否需要备份。
