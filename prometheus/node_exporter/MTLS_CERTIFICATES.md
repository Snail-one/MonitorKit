# node_exporter mTLS 证书说明

本文说明 Prometheus 通过 mTLS 抓取 node_exporter 时，各类证书的用途、签发类型和部署位置。

## 证书关系

```text
私有根 CA（私钥应离线保存）
├── node01 服务端证书（serverAuth）
├── node02 服务端证书（serverAuth）
├── node03 服务端证书（serverAuth）
└── Prometheus 客户端证书（clientAuth）

Prometheus（客户端）── mTLS ──> node_exporter（服务端）
```

连接时会进行双向验证：

1. Prometheus 验证 node_exporter 的服务端证书和访问地址。
2. node_exporter 使用客户端根 CA 证书（信任锚）验证 Prometheus 的客户端证书。

## 每个节点是否需要单独证书

是。建议每台安装 node_exporter 的服务器使用独立的服务端证书和私钥：

```text
node01.example.com:
  node01-server.crt
  node01-server.key

node02.example.com:
  node02-server.crt
  node02-server.key
```

各节点可以由同一个根 CA 签发，但不要共用服务端私钥。独立私钥可以避免一个节点泄露后影响所有节点。

Prometheus 客户端证书通常不需要为每个 node_exporter 单独生成。只要所有节点的 `client-ca.crt` 信任同一个根 CA，一套 Prometheus 客户端证书便可抓取所有节点。多个 Prometheus 实例建议分别签发客户端证书。

## 签发类型

| 证书 | 签发类型 | 关键要求 |
| --- | --- | --- |
| node_exporter 服务端证书 | 服务器证书 | EKU 包含 `serverAuth`，SAN 包含 Prometheus 实际访问的域名或 IP |
| Prometheus 客户端证书 | 客户端证书 | EKU 包含 `clientAuth` |
| 根 CA 证书 | CA 证书/信任锚 | `CA:TRUE`，只向服务器部署公共证书，不部署 CA 私钥 |

Prometheus 自身 Web UI 使用的 `server.crt` 也是服务器证书，但它与 Prometheus 抓取 node_exporter 使用的客户端证书不是同一用途。

## node_exporter 需要填写的文件

每个节点运行安装脚本时，需要填写：

| 路径 | 内容 |
| --- | --- |
| `/etc/node_exporter/tls/server.crt` | 当前节点的 node_exporter 服务端证书；使用中间 CA 时可包含服务端证书链 |
| `/etc/node_exporter/tls/server.key` | 与当前节点服务端证书匹配的未加密私钥 |
| `/etc/node_exporter/tls/client-ca.crt` | 信任 Prometheus 客户端证书的根 CA 公共证书；使用中间 CA 时可包含对应 CA 链 |

`client-ca.crt` 不是 Prometheus 客户端证书，也不能填写根 CA 私钥。

## Prometheus 需要准备的文件

Prometheus 主机需要：

| 示例路径 | 内容 |
| --- | --- |
| `/etc/prometheus/client/node-server-ca.crt` | 用于验证各 node_exporter 服务端证书的根 CA 公共证书 |
| `/etc/prometheus/client/prometheus-client.crt` | Prometheus 客户端证书，类型为 `clientAuth` |
| `/etc/prometheus/client/prometheus-client.key` | 与客户端证书匹配的未加密私钥 |

如果 node_exporter 服务端证书和 Prometheus 客户端证书由同一个根 CA 签发，`node-server-ca.crt` 和各节点的 `client-ca.crt` 可以使用同一份根 CA 公共证书，但部署路径和用途不同。

## SAN 要求

服务端证书的 SAN 必须与 Prometheus 配置中的目标地址一致。

使用域名抓取：

```text
Prometheus target: node01.example.com:9100
证书 SAN:         DNS:node01.example.com
```

使用 IP 抓取：

```text
Prometheus target: 10.0.0.11:9100
证书 SAN:         IP Address:10.0.0.11
```

不要只设置证书的 CN；现代 TLS 客户端主要验证 SAN。服务器 IP 可能变化时，建议使用稳定域名。

## Prometheus 抓取配置

多个节点使用各自域名和服务端证书时，可以共用一套 Prometheus 客户端证书：

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

Prometheus 会根据目标域名验证每个节点的 SAN。如果通过 IP 连接、证书只包含域名，则需要使用 `server_name`，并将不同域名的节点拆分到对应的抓取任务中。

## 推荐签发和部署顺序

1. 创建并安全保存私有根 CA，根 CA 私钥不要复制到 Prometheus 或 node_exporter。
2. 为每个 node_exporter 节点分别签发 `serverAuth` 服务端证书。
3. 为每个 Prometheus 实例签发 `clientAuth` 客户端证书。
4. 在每个节点运行安装脚本，填写该节点的服务端证书、私钥和客户端根 CA 公共证书。
5. 将 Prometheus 客户端证书、私钥和 node_exporter 服务端根 CA 公共证书部署到 Prometheus。
6. 配置 `scrape_configs`，重载 Prometheus 后检查目标状态。

## 安全注意事项

- 根 CA 私钥应离线保存，不能填写到任何安装脚本或复制到业务服务器。
- 每个 node_exporter 使用独立私钥，不要在所有节点共用同一个 `server.key`。
- 私钥必须限制读取权限；安装脚本会将 node_exporter 私钥设置为 `root:node_exporter 0640`。
- 服务端证书应包含正确 SAN，并在到期前完成轮换。
- 标准卸载会保留证书；选择“彻底清理”才会删除 `/etc/node_exporter` 下的证书和配置。
