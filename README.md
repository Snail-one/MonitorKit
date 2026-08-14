# SnailMon

SnailMon 是一套中心化服务器监控配置工具：中心服务器运行 Go 管理程序，负责 Prometheus 和 Loki 的安装与生命周期管理；被监控服务器上的指标、日志探针全部使用独立 `.sh` 脚本安装。

## 项目结构

```text
MonitorKit/
├── cmd/snailmon/                  # Go 中心端入口
├── internal/
│   ├── app/                       # 交互菜单与业务流程编排
│   ├── manager/                   # 组件注册、下载、校验、安装与 systemd 管理
│   ├── server/                    # HTTP API、鉴权和响应模型
│   └── ui/                        # 终端视觉组件与操作反馈
├── configs/                       # 配置示例
├── deploy/systemd/                # 中心端 systemd unit
├── scripts/probes/
│   ├── node_exporter/install.sh   # 主机指标探针
│   └── alloy/install.sh           # 指标与日志统一探针
└── docs/architecture.md           # 扩展规范
```

详细的分层和扩展方式见 [架构文档](docs/architecture.md)。

## 构建中心端

需要 Go 1.22 或更高版本：

```bash
./build.sh
sudo install -m 0755 bin/snailmon /usr/local/bin/snailmon
```

也可以运行 `make build`。编译脚本默认生成 `bin/snailmon`，并支持通过环境变量自定义输出路径或进行交叉编译：

```bash
OUTPUT=dist/snailmon-linux-amd64 GOOS=linux GOARCH=amd64 ./build.sh
```

启动交互式管理界面：

```bash
sudo snailmon
```

交互界面提供中心组件状态总览、Prometheus/Loki 独立管理、监控栈一键部署、探针接入命令和 HTTP API 信息。终端支持颜色时会显示状态徽标和动态操作反馈；设置 `NO_COLOR=1` 可关闭颜色。

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
curl -fsSL https://raw.githubusercontent.com/Snail-one/MonitorKit/main/scripts/probes/node_exporter/install.sh | sudo bash
```

Grafana Alloy 指标与日志统一探针：

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/MonitorKit/main/scripts/probes/alloy/install.sh | \
  sudo PROMETHEUS_URL=http://10.0.0.10:9090 LOKI_URL=http://10.0.0.10:3100 bash
```

两种探针按项目需求二选一：只需要主机指标时安装 node_exporter；同时需要指标和日志时安装 Alloy。Alloy 已内置 Unix 主机指标采集，同一服务器不应再重复安装 node_exporter。

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
