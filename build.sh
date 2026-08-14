#!/usr/bin/env bash

set -Eeuo pipefail

PROJECT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
OUTPUT="${OUTPUT:-bin/snailmon}"

if [[ "${OUTPUT}" != /* ]]; then
  OUTPUT="${PROJECT_DIR}/${OUTPUT}"
fi

if ! command -v go >/dev/null 2>&1; then
  echo "错误：未找到 Go，请先安装 Go 1.22 或更高版本。" >&2
  exit 1
fi

mkdir -p "$(dirname -- "${OUTPUT}")"

echo "正在编译 SnailMon（$(go version)）..."
(
  cd "${PROJECT_DIR}"
  go build -trimpath -o "${OUTPUT}" ./cmd/snailmon
)

echo "编译完成：${OUTPUT}"
