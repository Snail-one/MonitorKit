#!/usr/bin/env bash

set -Eeuo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || printf dev)}"
case "${VERSION}" in
  *[!A-Za-z0-9._-]*) printf '版本包含文件名不支持的字符：%s\n' "${VERSION}" >&2; exit 1 ;;
esac
ARCH="${GOARCH:-$(go env GOARCH)}"
case "${ARCH}" in
  amd64|arm64) ;;
  *) printf '不支持的发布架构：%s\n' "${ARCH}" >&2; exit 1 ;;
esac
OUTPUT="${OUTPUT:-dist/snailmon_linux_${ARCH}_${VERSION}}"
if [[ "${OUTPUT}" != /* ]]; then
  OUTPUT="${ROOT_DIR}/${OUTPUT}"
fi
mkdir -p "$(dirname -- "${OUTPUT}")"
COMMIT="${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || printf unknown)}"
BUILD_DATE="${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
LDFLAGS="-s -w -X github.com/Snail-one/MonitorKit/internal/version.Version=${VERSION} -X github.com/Snail-one/MonitorKit/internal/version.Commit=${COMMIT} -X github.com/Snail-one/MonitorKit/internal/version.BuildDate=${BUILD_DATE}"

CGO_ENABLED=0 GOOS=linux GOARCH="${ARCH}" go build -trimpath -ldflags="${LDFLAGS}" -o "${OUTPUT}" ./cmd/snailmon
printf '构建完成：%s\n' "${OUTPUT}"
