#!/usr/bin/env bash

set -Eeuo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
INSTALLER="${ROOT_DIR}/scripts/probes/alloy/install.sh"

bash -n "${INSTALLER}"

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
