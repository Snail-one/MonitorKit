#!/usr/bin/env bash

set -Eeuo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
INSTALLER="${ROOT_DIR}/scripts/probes/alloy/install.sh"

bash -n "${INSTALLER}"

ALLOY_INSTALLER_SOURCE_ONLY=1 source "${INSTALLER}"

for test_case in \
  '192.168.213.139:19367|https|https://192.168.213.139:19367' \
  'monitor.example.com:19367|https|https://monitor.example.com:19367' \
  'https://192.168.213.139:19367/|https|https://192.168.213.139:19367' \
  '192.168.213.139:3100|http|http://192.168.213.139:3100'; do
  IFS='|' read -r input scheme expected <<<"${test_case}"
  actual="$(normalize_backend_url_input "${input}" "${scheme}")"
  if [[ "${actual}" != "${expected}" ]]; then
    printf 'Alloy 地址规范化失败：%s => %s，预期 %s\n' "${input}" "${actual}" "${expected}" >&2
    exit 1
  fi
done

valid_backend_url 'https://192.168.213.139:19367'
if valid_backend_url 'https://192.168.213.139:70000'; then
  printf 'Alloy 地址校验错误地接受了无效端口\n' >&2
  exit 1
fi

HELP_OUTPUT="$(NO_COLOR=1 bash "${INSTALLER}" --help)"
for expected in \
  'install.sh reconfigure' \
  'install.sh update' \
  'install.sh status' \
  'install.sh uninstall' \
  'install.sh purge' \
  '/etc/alloy/tls/' \
  '普通卸载保留 /etc/alloy 和 /var/lib/alloy'; do
  grep -Fq -- "${expected}" <<<"${HELP_OUTPUT}" || {
    printf 'Alloy 帮助缺少：%s\n' "${expected}" >&2
    exit 1
  }
done

for expected in \
  'choose_maintenance_action()' \
  'configure_backend_certificates()' \
  'begin_config_transaction()' \
  'restore_config_transaction()' \
  'PACKAGE_WAS_INSTALLED' \
  'alloy validate "${temp_file}"' \
  'rm -rf -- "${CONFIG_DIR}" "${DATA_DIR}"' \
  '未清理：'; do
  grep -Fq -- "${expected}" "${INSTALLER}" || {
    printf 'Alloy 维护框架缺少：%s\n' "${expected}" >&2
    exit 1
  }
done

printf 'Alloy 安装器维护框架回归测试通过\n'
