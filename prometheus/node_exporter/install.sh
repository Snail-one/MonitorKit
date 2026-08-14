#!/usr/bin/env bash
# Backward-compatible entry point. New deployments should use
# scripts/probes/node_exporter/install.sh from the repository.

set -Eeuo pipefail

readonly SCRIPT_URL="${SNAILMON_NODE_EXPORTER_SCRIPT_URL:-https://raw.githubusercontent.com/Snail-one/Snailbash/main/scripts/probes/node_exporter/install.sh}"

if [[ "${BASH_SOURCE[0]}" != /dev/fd/* && "${BASH_SOURCE[0]}" != /proc/self/fd/* ]]; then
  script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
  local_script="${script_dir}/../../scripts/probes/node_exporter/install.sh"
  if [[ -f "${local_script}" ]]; then
    exec bash "${local_script}" "$@"
  fi
fi

command -v curl >/dev/null 2>&1 || {
  printf '[错误] 缺少必要命令：curl\n' >&2
  exit 1
}

temp_script="$(mktemp)"
trap 'rm -f -- "${temp_script}"' EXIT
curl -fsSL --proto '=https' "${SCRIPT_URL}" -o "${temp_script}"
exec bash "${temp_script}" "$@"
