# 发布、安装与卸载

MonitorKit 使用 GitHub Actions 构建 Linux amd64/arm64 单文件程序，并通过 GitHub Releases 分发。正式发布资产遵循固定命名：

```text
monitorkit_linux_amd64_v1.2.0
monitorkit_linux_arm64_v1.2.0
checksums.txt
```

## 发布新版本

先完成本地检查：

```bash
go test ./...
go vet ./...
bash -n scripts/*.sh scripts/probes/*/install.sh
```

创建并推送语义化版本标签：

```bash
git tag v1.0.0
git push origin v1.0.0
```

`.github/workflows/release.yml` 会依次执行测试、双架构构建、版本信息注入、SHA-256 生成、发布说明生成和 GitHub Release 创建。任一测试或架构构建失败都不会发布不完整版本。

也可以在 GitHub Actions 的 `release` 工作流中手动运行并填写 `v*` 标签。发布任务是唯一具有 `contents: write` 权限的任务。

## 在线安装和更新

最新正式版：

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/MonitorKit/main/scripts/install.sh | sudo sh
```

系统没有 curl 时：

```bash
wget -qO- https://raw.githubusercontent.com/Snail-one/MonitorKit/main/scripts/install.sh | sudo sh
```

指定版本：

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/MonitorKit/main/scripts/install.sh | sudo sh -s -- v1.0.0
```

已安装后可由程序自身调用相同脚本更新：

```bash
sudo monitorkit update
```

安装器会先取得 `checksums.txt`，只有版本和本地 SHA-256 都一致时才跳过更新。新文件下载后会再次校验 SHA-256 和 `--version`，最后在 `/usr/local/bin` 内原子替换旧程序。

## 在线卸载

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/MonitorKit/main/scripts/install.sh | sudo sh -s -- --uninstall
```

或使用已安装的命令：

```bash
sudo monitorkit uninstall
```

自身卸载只删除 `/usr/local/bin/monitorkit`，不会删除 Prometheus、Loki、配置和监控数据。中心组件仍分别使用下面的命令管理：

```bash
sudo monitorkit uninstall prometheus
sudo monitorkit uninstall loki --purge
```
