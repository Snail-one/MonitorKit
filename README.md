# SnailMon

SnailMon 是一套中心化服务器监控配置工具：中心服务器运行 Go 管理程序，负责 Prometheus 和 Loki 的安装与生命周期管理；被监控服务器上的指标、日志探针全部使用独立 `.sh` 脚本安装。

## 项目结构

```text
Snailbash/
├── cmd/snailmon/                  # Go 中心端入口
├── internal/
│   ├── manager/                   # 组件注册、下载、校验、安装与 systemd 管理
│   └── server/                    # HTTP API、鉴权和响应模型
├── configs/                       # 配置示例
├── deploy/systemd/                # 中心端 systemd unit
├── scripts/probes/
│   ├── node_exporter/install.sh   # 主机指标探针
│   └── alloy/install.sh           # Loki 日志探针
└── docs/architecture.md           # 扩展规范
```

详细的分层和扩展方式见 [架构文档](docs/architecture.md)。

## 构建中心端

需要 Go 1.22 或更高版本：

```bash
make build
sudo install -m 0755 bin/snailmon /usr/local/bin/snailmon
```

直接使用 CLI 安装中心组件：

```bash
sudo snailmon install prometheus
sudo snailmon install loki
snailmon status
```

固定版本安装：

```bash
sudo snailmon install prometheus --version 3.13.1
sudo snailmon install loki --version 3.7.4
```

普通卸载会保留配置和数据；彻底清理由显式参数控制：

```bash
sudo snailmon uninstall prometheus
sudo snailmon uninstall loki --purge
```

中心组件默认路径如下：

- 二进制：`/usr/local/bin/{prometheus,promtool,loki}`
- 配置：`/etc/prometheus/prometheus.yml`、`/etc/loki/loki.yml`
- 数据：`/var/lib/prometheus`、`/var/lib/loki`
- 服务：`prometheus.service`、`loki.service`

已有配置在更新时会保留，发布包必须通过 SHA-256 校验才会安装。
频繁查询 GitHub Release 时可设置可选的 `GITHUB_TOKEN` 以提高 API 请求限额。

## 运行管理 API

API 默认仅监听本机：

```bash
sudo snailmon serve
```

需要监听其他网卡时必须配置 Token：

```bash
sudo SNAILMON_LISTEN=0.0.0.0:8088 \
  SNAILMON_TOKEN="$(openssl rand -hex 32)" \
  snailmon serve
```

可将 [systemd unit](deploy/systemd/snailmon.service) 安装到 `/etc/systemd/system/snailmon.service`，并参考 [环境变量示例](configs/snailmon.env.example) 创建 `/etc/snailmon/snailmon.env`。

主要接口：

```text
GET    /healthz
GET    /api/v1/components
GET    /api/v1/components/{name}
POST   /api/v1/components/{name}/install
DELETE /api/v1/components/{name}?purge=true
```

安装请求示例：

```bash
curl -X POST http://127.0.0.1:8088/api/v1/components/prometheus/install \
  -H 'Content-Type: application/json' \
  -d '{"version":"latest"}'
```

配置了 Token 时增加 `Authorization: Bearer <token>` 请求头。API 只接受注册组件和预定义动作，不提供任意命令执行能力。

## 安装探针

node_exporter 指标探针：

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/Snailbash/main/scripts/probes/node_exporter/install.sh | sudo bash
```

Grafana Alloy 日志探针（将地址替换为中心服务器 Loki 地址）：

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/Snailbash/main/scripts/probes/alloy/install.sh | \
  sudo LOKI_URL=http://10.0.0.10:3100 bash
```

探针卸载：

```bash
sudo bash scripts/probes/node_exporter/install.sh uninstall
sudo bash scripts/probes/alloy/install.sh uninstall
```

所有探针遵循 `scripts/probes/<name>/install.sh` 目录规范，不依赖中心端 Go 程序。

## 开发检查

```bash
make check
```

该命令运行 Go 单元测试、`go vet` 和所有探针脚本的 Bash 语法检查。
