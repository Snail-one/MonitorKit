#!/usr/bin/env bash

set -Eeuo pipefail

PROJECT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
OUTPUT="${OUTPUT:-bin/snailmon}"
VERSION="${VERSION:-$(git -C "${PROJECT_DIR}" describe --tags --always --dirty 2>/dev/null || printf dev)}"
COMMIT="${COMMIT:-$(git -C "${PROJECT_DIR}" rev-parse --short HEAD 2>/dev/null || printf unknown)}"
BUILD_DATE="${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"

if [[ "${OUTPUT}" != /* ]]; then
  OUTPUT="${PROJECT_DIR}/${OUTPUT}"
fi

if ! command -v go >/dev/null 2>&1; then
  echo "错误：未找到 Go，请先安装 Go 1.22 或更高版本。" >&2
  exit 1
fi

mkdir -p "$(dirname -- "${OUTPUT}")"

case "${VERSION}" in
  *[!A-Za-z0-9._-]*) echo "错误：版本包含文件名不支持的字符：${VERSION}" >&2; exit 1 ;;
esac

LDFLAGS="-s -w -X github.com/Snail-one/MonitorKit/internal/version.Version=${VERSION} -X github.com/Snail-one/MonitorKit/internal/version.Commit=${COMMIT} -X github.com/Snail-one/MonitorKit/internal/version.BuildDate=${BUILD_DATE}"

echo "正在编译 SnailMon ${VERSION}（$(go version)）..."
(
  cd "${PROJECT_DIR}"
  CGO_ENABLED="${CGO_ENABLED:-0}" go build -trimpath -ldflags="${LDFLAGS}" -o "${OUTPUT}" ./cmd/snailmon
)

echo "编译完成：${OUTPUT}"
