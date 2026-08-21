#!/usr/bin/env bash

set -Eeuo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
INSTALLER="${ROOT_DIR}/scripts/probes/alloy/install.sh"
NODE_EXPORTER_INSTALLER="${ROOT_DIR}/scripts/probes/node_exporter/install.sh"

bash -n "${INSTALLER}" "${NODE_EXPORTER_INSTALLER}"

unset PROMETHEUS_MTLS_ENABLED LOKI_MTLS_ENABLED MONITOR_NAME ALLOY_INSTALL_METHOD
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

printf '\n' >"${TEST_INPUT}"
INTERACTIVE_DEVICE="${TEST_INPUT}"
SELECTED_ACTION="status"
choose_install_action >/dev/null 2>&1
if [[ "${SELECTED_ACTION}" != "install" || "${ALLOY_INSTALL_METHOD}" != "release" ]]; then
  printf 'Alloy 安装菜单默认项不是 Release 软件包安装\n' >&2
  exit 1
fi

printf '2\n' >"${TEST_INPUT}"
INTERACTIVE_DEVICE="${TEST_INPUT}"
SELECTED_ACTION="status"
choose_install_action >/dev/null 2>&1
if [[ "${SELECTED_ACTION}" != "install" || "${ALLOY_INSTALL_METHOD}" != "repository" ]]; then
  printf 'Alloy 安装菜单没有选择官方软件源安装\n' >&2
  exit 1
fi

printf '3\n' >"${TEST_INPUT}"
INTERACTIVE_DEVICE="${TEST_INPUT}"
SELECTED_ACTION="install"
choose_install_action >/dev/null 2>&1
if [[ "${SELECTED_ACTION}" != "status" ]]; then
  printf 'Alloy 安装菜单没有选择查看状态\n' >&2
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

printf '\n' >"${TEST_INPUT}"
INTERACTIVE_DEVICE="${TEST_INPUT}"
if ! ask_yes_no_default "确认删除 Grafana 官方软件源" yes; then
  printf 'Alloy 软件源删除确认的默认项不是确认删除\n' >&2
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
  'ALLOY_VERSION=latest' \
  'ALLOY_INSTALL_METHOD=repository' \
  'Grafana 官方 apt/rpm 软件源' \
  'GitHub Release DEB/RPM 直装' \
  '不安装独立二进制' \
  '普通卸载保留 /etc/alloy、/var/lib/alloy、Grafana Cloud systemd 凭据和软件源'; do
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
  'INSTALL_METHOD_FILE' \
  'persist_install_method()' \
  'install_repository_package()' \
  'remove_grafana_repository()' \
  'remove_grafana_cloud_dropin()' \
  'rm -rf -- "${GRAFANA_CLOUD_DROPIN_DIR}"' \
  'rm -f -- /etc/apt/sources.list.d/grafana.list' \
  'rm -f -- /etc/yum.repos.d/grafana.repo' \
  'zypper --non-interactive removerepo grafana' \
  'apt-get update' \
  'apt-get -o Dpkg::Options::=--force-confold install -y alloy' \
  'https://apt.grafana.com stable main' \
  'https://apt.grafana.com/gpg-full.key' \
  'https://rpm.grafana.com/gpg.key' \
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
  'ask_yes_no_default "确认删除 Grafana 官方软件源" yes' \
  'REPOSITORY_REMOVED=1' \
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

if ! NO_COLOR=1 ALLOY_INSTALLER_SOURCE_ONLY=1 bash -c '
  source "$1"
  metadata="$(mktemp)"
  trap '\''rm -f -- "${metadata}"'\'' EXIT
  printf '\''%s\n'\'' \
    '\''{"tag_name": "v1.18.0", "assets": ['\'' \
    '\''  {'\'' \
    '\''    "name": "alloy-1.18.0-1.amd64.deb",'\'' \
    '\''    "digest": "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"'\'' \
    '\''  }'\'' \
    '\'']}'\'' >"${metadata}"
  [[ "$(release_version "${metadata}")" == "1.18.0" ]]
  [[ "$(release_digest "${metadata}" alloy-1.18.0-1.amd64.deb)" == \
     "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" ]]
  validate_alloy_version v1.18.0
' _ "${INSTALLER}"; then
  printf 'Alloy 安装器无法解析 Release 版本或 SHA-256 digest\n' >&2
  exit 1
fi

if ! NO_COLOR=1 ALLOY_INSTALLER_SOURCE_ONLY=1 bash -c '
  source "$1"
  [[ "$(normalize_install_method package)" == "repository" ]]
  [[ "$(normalize_install_method direct)" == "release" ]]
  ALLOY_INSTALL_METHOD_PRESET=0
  persisted_install_method() { return 1; }
  package_is_installed() { return 1; }
  grafana_repository_configured() { return 1; }
  initialize_install_method
  [[ "${ALLOY_INSTALL_METHOD}" == "release" ]]
  persisted_install_method() { printf "repository\n"; }
  initialize_install_method
  [[ "${ALLOY_INSTALL_METHOD}" == "repository" ]]
  install_repository_package() { [[ "$1" == "install" ]]; }
  install_official_package() { return 1; }
  ALLOY_INSTALL_METHOD=repository
  install_selected_package install
  install_repository_package() { return 1; }
  install_official_package() { return 0; }
  ALLOY_INSTALL_METHOD=release
  install_selected_package update
' _ "${INSTALLER}"; then
  printf 'Alloy 安装来源规范化或分派失败\n' >&2
  exit 1
fi

for expected in \
  'alloy-${version}-1.${arch}.${package_system}' \
  'sha256sum --check --status' \
  'install_official_package()' \
  'dpkg --force-confold --install "${package_file}"' \
  'rpm -Uvh --replacepkgs "${package_file}"' \
  'detect_package_arch()' \
  'remove_official_package()' \
  'has_install_artifacts()' \
  '官方 Release DEB/RPM 软件包' \
  'install_selected_package()' \
  'install_debian_repository_package()' \
  'install_rpm_repository_package()' \
  'install_suse_repository_package()'; do
  grep -Fq -- "${expected}" "${INSTALLER}" || {
    printf 'Alloy 官方软件包安装器缺少：%s\n' "${expected}" >&2
    exit 1
  }
done

for forbidden in \
  'install_binary_backend()' \
  'write_binary_service()' \
  'create_binary_service_account()' \
  'alloy-linux-${arch}.zip'; do
  if grep -Fq -- "${forbidden}" "${INSTALLER}"; then
    printf 'Alloy 安装器仍包含已移除的安装逻辑：%s\n' "${forbidden}" >&2
    exit 1
  fi
done


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

for expected in \
  'SERVICE_DROPIN_DIR="/etc/systemd/system/node_exporter.service.d"' \
  'rm -rf -- "${CONFIG_DIR}" "${SERVICE_DROPIN_DIR}"' \
  'systemd drop-in'; do
  grep -Fq -- "${expected}" "${NODE_EXPORTER_INSTALLER}" || {
    printf 'node_exporter 彻底清理缺少：%s\n' "${expected}" >&2
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

if ! grep -qF 'source_labels = ["__journal__systemd_unit"]' "${INSTALLER}" || \
   ! grep -qF 'target_label  = "unit"' "${INSTALLER}" || \
   ! grep -qF 'source_labels = ["__journal_syslog_identifier"]' "${INSTALLER}" || \
   ! grep -qF 'max_age       = "7h"' "${INSTALLER}"; then
  printf 'Alloy 默认 journal 配置没有按官方字段名保留服务标签或启动回读窗口\n' >&2
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
