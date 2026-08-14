#!/usr/bin/env bash

set -Eeuo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
INSTALLER="${ROOT_DIR}/scripts/probes/alloy/install.sh"
NODE_EXPORTER_INSTALLER="${ROOT_DIR}/scripts/probes/node_exporter/install.sh"

bash -n "${INSTALLER}" "${NODE_EXPORTER_INSTALLER}"

unset PROMETHEUS_MTLS_ENABLED LOKI_MTLS_ENABLED MONITOR_NAME
ALLOY_INSTALLER_SOURCE_ONLY=1 source "${INSTALLER}"
if [[ "${PROMETHEUS_MTLS_ENABLED}" != "1" || "${LOKI_MTLS_ENABLED}" != "1" ]]; then
  printf 'Alloy 首次配置没有默认启用 Prometheus/Loki mTLS\n' >&2
  exit 1
fi

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

valid_monitor_name '上海-Web-01'
if valid_monitor_name 'bad$name'; then
  printf 'Alloy 节点名称校验错误地接受了美元符号\n' >&2
  exit 1
fi

actual="$(normalize_backend_url_input '192.168.213.139:3100')"
if [[ "${actual}" != "https://192.168.213.139:3100" ]]; then
  printf 'Alloy 裸地址默认协议不是 HTTPS：%s\n' "${actual}" >&2
  exit 1
fi

valid_backend_url 'https://192.168.213.139:19367'
if valid_backend_url 'https://192.168.213.139:70000'; then
  printf 'Alloy 地址校验错误地接受了无效端口\n' >&2
  exit 1
fi

TEST_INPUT="$(mktemp)"
trap 'rm -f -- "${TEST_INPUT}"' EXIT
printf '192.168.213.139:3100\n' >"${TEST_INPUT}"
INTERACTIVE_DEVICE="${TEST_INPUT}"
LOKI_URL=""
prompt_backend_url LOKI_URL Loki https 1
if [[ "${LOKI_URL}" != "https://192.168.213.139:3100" ]]; then
  printf 'Loki 默认 mTLS 地址没有规范化为 HTTPS：%s\n' "${LOKI_URL}" >&2
  exit 1
fi

LOKI_MTLS_ENABLED=no
normalize_mtls_mode LOKI_MTLS_ENABLED
if [[ "${LOKI_MTLS_ENABLED}" != "0" ]]; then
  printf 'Loki 显式关闭 mTLS 的配置没有规范化为 HTTP 模式\n' >&2
  exit 1
fi

printf '1\n' >"${TEST_INPUT}"
INTERACTIVE_DEVICE="${TEST_INPUT}"
PROMETHEUS_MTLS_ENABLED=1
LOKI_MTLS_ENABLED=1
ALLOY_MTLS_CERT_MODE_PRESET=0
ALLOY_MTLS_CERT_MODE="separate"
RETURN_TO_MAIN=0
choose_mtls_certificate_mode >/dev/null 2>&1
if [[ "${ALLOY_MTLS_CERT_MODE}" != "shared" ]]; then
  printf 'Alloy 没有接受一套共享证书模式\n' >&2
  exit 1
fi

printf 'q\n' >"${TEST_INPUT}"
INTERACTIVE_DEVICE="${TEST_INPUT}"
SELECTED_ACTION="install"
choose_install_action >/dev/null 2>&1
if [[ "${SELECTED_ACTION}" != "quit" ]]; then
  printf 'Alloy 顶层 q 没有退出脚本\n' >&2
  exit 1
fi

printf '0\n' >"${TEST_INPUT}"
INTERACTIVE_DEVICE="${TEST_INPUT}"
RETURN_TO_MAIN=0
choose_uninstall_mode >/dev/null 2>&1
if [[ "${RETURN_TO_MAIN}" != "1" ]]; then
  printf 'Alloy 卸载菜单 0 没有取消操作\n' >&2
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
  'MONITOR_NAME=debian-web-01' \
  'Prometheus 和 Loki 均默认推荐 mTLS' \
  '普通卸载保留 /etc/alloy 和 /var/lib/alloy'; do
  grep -Fq -- "${expected}" <<<"${HELP_OUTPUT}" || {
    printf 'Alloy 帮助缺少：%s\n' "${expected}" >&2
    exit 1
  }
done

for expected in \
  'choose_maintenance_action()' \
  'configure_backend_certificates()' \
  'configure_shared_certificates()' \
  'choose_mtls_certificate_mode()' \
  'ALLOY_MTLS_CERT_MODE=separate' \
  '共享证书已分别写入 Prometheus 与 Loki 的受管文件' \
  'begin_config_transaction()' \
  'restore_config_transaction()' \
  'PACKAGE_WAS_INSTALLED' \
  'choose_backend_mtls_mode()' \
  '确认接受风险并让 ${label} 使用 HTTP' \
  'read_editable()' \
  'menu_option()' \
  'menu_exit()' \
  'warning_card()' \
  'trap on_signal INT TERM' \
  'prometheus.relabel "host_identity"' \
  'target_label = "job"' \
  'replacement  = "alloy-one"' \
  'target_label = "instance"' \
  'target_label = "host"' \
  'host = "${MONITOR_NAME}"' \
  'MONITOR_NAME_FILE' \
  'prometheus_tls_config=""' \
  'alloy validate "${temp_file}"' \
  'rm -rf -- "${CONFIG_DIR}" "${DATA_DIR}"' \
  '"未清理"'; do
  grep -Fq -- "${expected}" "${INSTALLER}" || {
    printf 'Alloy 维护框架缺少：%s\n' "${expected}" >&2
    exit 1
  }
done

if grep -Fq 'target_label = "nodename"' "${INSTALLER}"; then
  printf 'Alloy 不应覆盖系统指标原生的 nodename 标签\n' >&2
  exit 1
fi


for expected in \
  'MonitorKit  ›  node_exporter' \
  'read_editable()' \
  'menu_option()' \
  'menu_exit "0/q" "退出"' \
  'SELECTED_ACTION="quit"' \
  'trap on_signal INT TERM' \
  'NODE_EXPORTER_INSTALLER_SOURCE_ONLY'; do
  grep -Fq -- "${expected}" "${NODE_EXPORTER_INSTALLER}" || {
    printf 'node_exporter 交互框架缺少：%s\n' "${expected}" >&2
    exit 1
  }
done

printf 'q\n' >"${TEST_INPUT}"
if ! NO_COLOR=1 NODE_EXPORTER_INSTALLER_SOURCE_ONLY=1 NODE_TEST_INPUT="${TEST_INPUT}" \
  bash -c '
    source "$1"
    INTERACTIVE_DEVICE="${NODE_TEST_INPUT}"
    choose_install_mode >/dev/null 2>&1
    [[ "${SELECTED_ACTION}" == "quit" ]]
  ' _ "${NODE_EXPORTER_INSTALLER}"; then
  printf 'node_exporter 顶层 q 没有退出脚本\n' >&2
  exit 1
fi

if ! NO_COLOR=1 NODE_EXPORTER_INSTALLER_SOURCE_ONLY=1 \
  bash -c '
    source "$1"
    uninstall_node_exporter() { RETURN_TO_MAIN=1; }
    require_root() { return 9; }
    main uninstall
  ' _ "${NODE_EXPORTER_INSTALLER}"; then
  printf 'node_exporter 直接卸载取消后错误地继续了安装流程\n' >&2
  exit 1
fi

ca_line="$(grep -nF 'edit_pem_file "${label} 服务端 CA 证书"' "${INSTALLER}" | cut -d: -f1)"
cert_line="$(grep -nF 'edit_pem_file "${label} Alloy 完整客户端证书"' "${INSTALLER}" | cut -d: -f1)"
key_line="$(grep -nF 'edit_pem_file "${label} Alloy 客户端私钥"' "${INSTALLER}" | cut -d: -f1)"
if [[ -z "${ca_line}" || -z "${cert_line}" || -z "${key_line}" ]] || \
   (( ca_line >= cert_line || cert_line >= key_line )); then
  printf 'Alloy 证书录入顺序不是 CA → 完整证书 → 私钥\n' >&2
  exit 1
fi

ca_line="$(grep -nF 'edit_pem_file "Prometheus 客户端根 CA 证书（信任锚）"' "${NODE_EXPORTER_INSTALLER}" | cut -d: -f1)"
cert_line="$(grep -nF 'edit_pem_file "node_exporter 完整服务端证书"' "${NODE_EXPORTER_INSTALLER}" | cut -d: -f1)"
key_line="$(grep -nF 'edit_pem_file "node_exporter 服务端私钥"' "${NODE_EXPORTER_INSTALLER}" | cut -d: -f1)"
if [[ -z "${ca_line}" || -z "${cert_line}" || -z "${key_line}" ]] || \
   (( ca_line >= cert_line || cert_line >= key_line )); then
  printf 'node_exporter 证书录入顺序不是 CA → 完整证书 → 私钥\n' >&2
  exit 1
fi

printf 'Alloy 安装器维护框架回归测试通过\n'
