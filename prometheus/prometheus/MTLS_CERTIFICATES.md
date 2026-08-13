# Prometheus mTLS 证书说明

本文说明 Prometheus 启用 mTLS 后，各类证书的用途、签发类型和部署位置。

## Prometheus 的两种身份

Prometheus 在两个连接方向中扮演不同角色：

```text
浏览器/API 客户端 ── mTLS ──> Prometheus
                              Prometheus 是服务端

Prometheus ── mTLS ──> node_exporter
Prometheus 是客户端
```

因此，Prometheus 通常同时需要一套服务端证书和一套客户端证书，两者用途不同，不能因为都部署在 Prometheus 主机上就混为一组。

## 证书关系

最简单的私有 PKI 可以使用同一个根 CA，但签发证书时必须选择正确类型：

```text
私有根 CA（私钥应离线保存）
├── Prometheus 服务端证书（serverAuth）
├── Prometheus Web/API 客户端证书（clientAuth）
├── Prometheus 抓取探针的客户端证书（clientAuth）
└── 各 node_exporter 服务端证书（serverAuth）
```

也可以使用独立的服务端 CA 和客户端 CA，减少不同用途之间的信任范围。

## 签发类型

| 证书 | 签发类型 | 关键要求 |
| --- | --- | --- |
| Prometheus Web/API 服务端证书 | 服务器证书 | EKU 包含 `serverAuth`，SAN 包含客户端实际访问的域名或 IP |
| 访问 Prometheus 的客户端证书 | 客户端证书 | EKU 包含 `clientAuth`，由 Prometheus 信任的客户端根 CA 签发 |
| Prometheus 抓取 node_exporter 的证书 | 客户端证书 | EKU 包含 `clientAuth`，由各 node_exporter 信任的客户端根 CA 签发 |
| 根 CA 证书 | CA 证书/信任锚 | `CA:TRUE`，业务服务器只保存 CA 公共证书，不能部署 CA 私钥 |

如果同一张客户端证书同时被所有 node_exporter 信任，一套 Prometheus 客户端证书可以抓取多个节点。多个 Prometheus 实例建议分别签发客户端证书。

## Prometheus 安装脚本需要填写的文件

安装脚本中的 mTLS 配置保护 Prometheus 自身的 Web UI 和 API：

| 路径 | 内容 |
| --- | --- |
| `/etc/prometheus/tls/server.crt` | 当前 Prometheus 实例的 Web/API 服务端证书；使用中间 CA 时可包含服务端证书链 |
| `/etc/prometheus/tls/server.key` | 与 Prometheus 服务端证书匹配的未加密私钥 |
| `/etc/prometheus/tls/client-ca.crt` | 信任 Web/API 客户端证书的根 CA 公共证书；使用中间 CA 时可包含对应 CA 链 |
| `/etc/prometheus/web.yml` | 安装脚本生成的 Prometheus mTLS Web 配置 |

`client-ca.crt` 不是客户端证书，也不能填写根 CA 私钥。它只负责验证连接 Prometheus Web UI/API 的客户端，不会自动用于抓取 node_exporter。

## 访问 Prometheus 需要的客户端文件

浏览器、运维程序或 API 客户端访问 Prometheus 时需要：

| 文件 | 用途 |
| --- | --- |
| `prometheus-web-client.crt` | 客户端证书，EKU 为 `clientAuth` |
| `prometheus-web-client.key` | 与客户端证书匹配的私钥 |
| `prometheus-server-ca.crt` | 验证 Prometheus 服务端证书的根 CA 公共证书 |

使用 curl 访问示例：

```bash
curl \
  --cacert prometheus-server-ca.crt \
  --cert prometheus-web-client.crt \
  --key prometheus-web-client.key \
  https://prometheus.example.com:9090/-/healthy
```

## Prometheus 抓取 node_exporter 需要的文件

Prometheus 作为 mTLS 客户端抓取 node_exporter 时，建议另外准备：

| 示例路径 | 内容 |
| --- | --- |
| `/etc/prometheus/client/node-server-ca.crt` | 验证 node_exporter 服务端证书的根 CA 公共证书 |
| `/etc/prometheus/client/prometheus-client.crt` | Prometheus 抓取探针使用的客户端证书，EKU 为 `clientAuth` |
| `/etc/prometheus/client/prometheus-client.key` | 与抓取客户端证书匹配的未加密私钥 |

这些文件不是 Prometheus 安装脚本中 `/etc/prometheus/tls/` 下的服务端文件，需要根据抓取配置单独部署。

使用安装脚本的“添加 node_exporter 探针”向导时，脚本会先校验这组三个文件。文件有效且证书私钥匹配时可以直接复用，并把目标追加到引用相同证书路径的现有任务；只有证书无效或选择不同 CA 时才打开编辑器。显式设置 `server_name` 的现有任务不会追加其他节点。不同 CA 使用 `/etc/prometheus/client/<探针地址>/` 独立目录和独立抓取任务。

示例配置：

```yaml
scrape_configs:
  - job_name: node
    scheme: https
    static_configs:
      - targets:
          - node01.example.com:9100
          - node02.example.com:9100
    tls_config:
      ca_file: /etc/prometheus/client/node-server-ca.crt
      cert_file: /etc/prometheus/client/prometheus-client.crt
      key_file: /etc/prometheus/client/prometheus-client.key
```

各 node_exporter 必须信任签发 `prometheus-client.crt` 的根 CA 公共证书。

## SAN 要求

Prometheus 服务端证书的 SAN 必须与客户端实际访问地址一致。

使用域名访问：

```text
访问地址: https://prometheus.example.com:9090
证书 SAN: DNS:prometheus.example.com
```

使用 IP 访问：

```text
访问地址: https://10.0.0.10:9090
证书 SAN: IP Address:10.0.0.10
```

不要只设置证书 CN；现代 TLS 客户端主要验证 SAN。服务器 IP 可能变化时，建议使用稳定域名。

## 多个 Prometheus 实例

如果部署多个 Prometheus 实例，建议每个实例使用独立的服务端证书和私钥：

```text
prometheus01.example.com:
  prometheus01-server.crt
  prometheus01-server.key

prometheus02.example.com:
  prometheus02-server.crt
  prometheus02-server.key
```

根 CA 公共证书可以共用，但不建议多个实例共用同一个服务端私钥。

## 推荐签发和部署顺序

1. 创建并安全保存私有根 CA，根 CA 私钥不要复制到 Prometheus、node_exporter 或客户端主机。
2. 为每个 Prometheus 实例签发 `serverAuth` 服务端证书，SAN 包含实际访问地址。
3. 为需要访问 Prometheus Web/API 的用户或程序签发 `clientAuth` 客户端证书。
4. 运行 Prometheus 安装脚本，填写 Prometheus 服务端证书、私钥和客户端根 CA 公共证书。
5. 为 Prometheus 签发抓取 node_exporter 使用的 `clientAuth` 客户端证书。
6. 为每个 node_exporter 节点分别签发 `serverAuth` 服务端证书并完成安装。
7. 将抓取客户端文件放到 `/etc/prometheus/client/`，配置 `scrape_configs` 后重载 Prometheus。

## 安全注意事项

- 根 CA 私钥应离线保存，不能填写到任何安装脚本或复制到业务服务器。
- 每个 Prometheus 实例使用独立服务端私钥。
- Prometheus 服务端证书、Web/API 客户端证书和抓取探针客户端证书应按用途区分。
- 私钥必须限制读取权限，避免其他系统用户读取。
- 服务端证书应包含正确 SAN，并在到期前完成轮换。
- 标准卸载会保留证书和监控数据；选择“彻底清理”才会删除 `/etc/prometheus` 和 `/var/lib/prometheus`。
