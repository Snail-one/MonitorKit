#!/usr/bin/env bash
# Install or upgrade Prometheus node_exporter as a systemd service.
# Canonical probe installer: scripts/probes/node_exporter/install.sh
#
# Optional environment variables:
#   NODE_EXPORTER_VERSION=latest
#   NODE_EXPORTER_LISTEN_ADDRESS=0.0.0.0:9100
#   DOWNLOAD_BASE_URL=https://github.com/prometheus/node_exporter/releases/download

set -Eeuo pipefail

NODE_EXPORTER_VERSION="${NODE_EXPORTER_VERSION:-latest}"
NODE_EXPORTER_LISTEN_ADDRESS="${NODE_EXPORTER_LISTEN_ADDRESS:-0.0.0.0:9100}"
DOWNLOAD_BASE_URL="${DOWNLOAD_BASE_URL:-https://github.com/prometheus/node_exporter/releases/download}"
NODE_EXPORTER_MTLS_MODE_PRESET=0
[[ -n "${NODE_EXPORTER_MTLS_ENABLED+x}" ]] && NODE_EXPORTER_MTLS_MODE_PRESET=1
NODE_EXPORTER_MTLS_ENABLED="${NODE_EXPORTER_MTLS_ENABLED:-1}"

readonly EXPORTER_USER="node_exporter"
readonly EXPORTER_GROUP="node_exporter"
readonly BIN_DIR="/usr/local/bin"
readonly CONFIG_DIR="/etc/node_exporter"
readonly TLS_DIR="${CONFIG_DIR}/tls"
readonly WEB_CONFIG_FILE="${CONFIG_DIR}/web.yml"
readonly SERVICE_FILE="/etc/systemd/system/node_exporter.service"
readonly SERVICE_DROPIN_DIR="/etc/systemd/system/node_exporter.service.d"

RESET=""
BOLD=""
DIM=""
ORANGE=""
BLUE=""
GREEN=""
YELLOW=""
RED=""
GRAY=""
RL_ORANGE=""
RL_RESET=""
LATEST_VERSION=""
WEB_CONFIG_ARGUMENT=""
WEB_SCHEME="http"
MTLS_STATUS="关闭"
INTERACTIVE_DEVICE=""
TEXT_EDITOR=""
PURGE_MODE=0
SELECTED_ACTION="install"
MAINTENANCE_ACTION="install"
RETURN_TO_MAIN=0
WORK_DIR=""
CONFIG_TRANSACTION_ACTIVE=0
CONFIG_EXISTED=0

init_colors() {
  if [[ -n "${NO_COLOR+x}" ]]; then
    return
  fi
  if [[ "${FORCE_COLOR:-0}" != "1" ]]; then
    [[ "${TERM:-}" != "dumb" && -t 1 ]] || return
  fi

  local esc
  esc="$(printf '\033')"
  RESET="${esc}[0m"
  BOLD="${esc}[1m"
  DIM="${esc}[2m"
  ORANGE="${esc}[38;5;208m"
  BLUE="${esc}[34m"
  GREEN="${esc}[32m"
  YELLOW="${esc}[33m"
  RED="${esc}[31m"
  GRAY="${esc}[90m"
  RL_ORANGE=$'\001'"${esc}[1m${esc}[38;5;208m"$'\002'
  RL_RESET=$'\001'"${esc}[0m"$'\002'
}

print_banner() {
  printf '%s╭─ %sMonitorKit  ›  node_exporter%s\n' "${ORANGE}" "${BOLD}" "${RESET}"
  printf '%s│ %s%s\n' "${ORANGE}" "$1" "${RESET}"
  printf '%s╰────────────────────────────────────────────────────%s\n\n' "${ORANGE}" "${RESET}"
}

print_card() {
  local title_color="$1"
  local title="$2"
  shift 2
  printf '\n%s╭─ %sMonitorKit%s\n' "${ORANGE}" "${BOLD}" "${RESET}"
  printf '%s│ %s%s%s\n' "${ORANGE}" "${BOLD}${title_color}" "${title}" "${RESET}"
  while (( $# >= 2 )); do
    printf '%s│ %s%s：%s%s\n' "${ORANGE}" "${BLUE}" "$1" "${RESET}" "$2"
    shift 2
  done
  printf '%s╰────────────────────────────────────────────────────%s\n' "${ORANGE}" "${RESET}"
}

success_card() { print_card "${GREEN}" "$@"; }
warning_card() { print_card "${YELLOW}" "$@"; }
danger_card() { print_card "${RED}" "$@"; }

menu_section() { printf '%s%s%s\n' "${BOLD}" "$1" "${RESET}" >&2; }

menu_option() {
  local key="$1"
  local label="$2"
  local hint="${3:-}"
  printf '  %s%-4s%s %s' "${BOLD}${BLUE}" "${key}" "${RESET}" "${label}" >&2
  [[ -z "${hint}" ]] || printf '  %s-- %s%s' "${GRAY}" "${hint}" "${RESET}" >&2
  printf '\n' >&2
}

menu_exit() {
  printf '  %s%-4s%s %s%s%s\n' "${BOLD}${YELLOW}" "$1" "${RESET}" "${DIM}" "$2" "${RESET}" >&2
}

invalid_choice() { warn "无效选项，请重新输入"; }

read_editable() {
  local variable_name="$1"
  local prompt="$2"
  local initial="${3:-}"
  local value=""
  local prompt_text="${RL_ORANGE}❯${RL_RESET} ${prompt} "
  set_interactive_device
  if [[ -n "${initial}" ]]; then
    if ! IFS= read -e -r -i "${initial}" -p "${prompt_text}" value <"${INTERACTIVE_DEVICE}"; then
      exit_card "输入已结束，安全退出"
      exit 0
    fi
  elif ! IFS= read -e -r -p "${prompt_text}" value <"${INTERACTIVE_DEVICE}"; then
    exit_card "输入已结束，安全退出"
    exit 0
  fi
  printf -v "${variable_name}" '%s' "${value}"
}

exit_card() { success_card "$1" "系统修改" "无"; }

print_release_info() {
  print_card "${ORANGE}" "发布信息" \
    "平台" "Linux/$1" \
    "当前版本" "$2" \
    "目标版本" "$3" \
    "执行操作" "$4" \
    "监听地址" "${NODE_EXPORTER_LISTEN_ADDRESS}" \
    "mTLS" "${MTLS_STATUS}"
}

print_completion_card() {
  success_card "$1完成" \
    "版本" "$2" \
    "服务" "node_exporter.service（运行中、开机自启）" \
    "指标地址" "${WEB_SCHEME}://${NODE_EXPORTER_LISTEN_ADDRESS}/metrics" \
    "配置目录" "${CONFIG_DIR}"
}

print_result_card() {
  success_card "$@"
}

step() {
  printf '%s[步骤]%s %s\n' "${ORANGE}" "${RESET}" "$*"
}

info() {
  printf '%s[信息]%s %s\n' "${BLUE}" "${RESET}" "$*"
}

result() {
  printf '%s[结果]%s %s\n' "${GREEN}" "${RESET}" "$*"
}

warn() {
  printf '%s[警告]%s %s\n' "${YELLOW}" "${RESET}" "$*"
}

log() {
  info "$@"
}

die() {
  printf '%s[错误]%s %s\n' "${RED}" "${RESET}" "$*" >&2
  exit 1
}

on_error() {
  local exit_code="$1"
  local line_number="$2"
  printf '%s[错误]%s 脚本在第 %s 行执行失败（退出码：%s）\n' \
    "${RED}" "${RESET}" "${line_number}" "${exit_code}" >&2
  exit "${exit_code}"
}

on_signal() {
  printf '\n'
  warning_card "操作已中止" \
    "原因" "收到中断信号" \
    "处理" "临时下载文件已清理"
  exit 130
}

cleanup() {
  if [[ "${CONFIG_TRANSACTION_ACTIVE}" == "1" ]]; then
    restore_config_transaction
  fi
  if [[ -n "${WORK_DIR}" ]]; then
    rm -rf -- "${WORK_DIR}"
    WORK_DIR=""
  fi
}

begin_config_transaction() {
  local backup_dir="${WORK_DIR}/config-backup"
  rm -rf -- "${backup_dir}"
  install -d -m 0700 "${backup_dir}"
  CONFIG_EXISTED=0
  if [[ -d "${CONFIG_DIR}" ]]; then
    CONFIG_EXISTED=1
    cp -a -- "${CONFIG_DIR}" "${backup_dir}/config"
  fi
  CONFIG_TRANSACTION_ACTIVE=1
}

restore_config_transaction() {
  local backup_dir="${WORK_DIR}/config-backup/config"
  [[ -n "${WORK_DIR}" ]] || return 0
  rm -rf -- "${CONFIG_DIR}"
  if [[ "${CONFIG_EXISTED}" == "1" && -d "${backup_dir}" ]]; then
    cp -a -- "${backup_dir}" "${CONFIG_DIR}"
  fi
  CONFIG_TRANSACTION_ACTIVE=0
  result "已恢复操作前的 node_exporter 配置和证书"
}

commit_config_transaction() {
  CONFIG_TRANSACTION_ACTIVE=0
}

usage() {
  cat <<'EOF'
node_exporter 探针一键安装、更新与卸载脚本。

用法：
  sudo ./install.sh             # 交互选择，默认安装 mTLS
  sudo ./install.sh http        # 直接安装 HTTP 模式
  sudo ./install.sh mtls
  sudo ./install.sh uninstall   # 交互选择标准卸载或彻底清理
  sudo ./install.sh purge       # 直接彻底清理

默认通过 GitHub Releases API 安装最新正式版本，也可以指定固定版本。
未安装时直接安装目标版本；已安装时可选择检查更新或仅重新配置。
仅重新配置不会访问版本 API，也不会下载安装包。

可选安装环境变量：
  NODE_EXPORTER_VERSION=latest
  NODE_EXPORTER_LISTEN_ADDRESS=0.0.0.0:9100
  DOWNLOAD_BASE_URL=https://github.com/prometheus/node_exporter/releases/download

mTLS 环境变量：
  NODE_EXPORTER_MTLS_ENABLED=1

mTLS 模式会使用 vim、nano 或 vi 编辑并校验证书文件。
所有交互输入错误时都会提示并允许重新输入。
顶层菜单使用 0/q 安全退出，子菜单使用 0/q 取消并返回；NO_COLOR=1
可关闭颜色，FORCE_COLOR=1 可在受支持的非交互输出中强制启用颜色。
EOF
}

require_root() {
  [[ "${EUID}" -eq 0 ]] || die "该操作需要 root 权限，请使用 sudo 运行"
}

require_commands() {
  local command_name
  for command_name in "$@"; do
    command -v "${command_name}" >/dev/null 2>&1 || die "缺少必要命令：${command_name}"
  done
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) printf 'amd64\n' ;;
    aarch64|arm64) printf 'arm64\n' ;;
    armv7l|armv7) printf 'armv7\n' ;;
    armv6l|armv6) printf 'armv6\n' ;;
    ppc64le) printf 'ppc64le\n' ;;
    *) die "不支持的处理器架构：$(uname -m)" ;;
  esac
}

validate_version() {
  [[ "$1" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]] || die "无效的 NODE_EXPORTER_VERSION：$1"
}

validate_listen_address() {
  local value="$1"
  local port
  if [[ "${value}" =~ ^[A-Za-z0-9._-]*:([0-9]{1,5})$ ]] || \
     [[ "${value}" =~ ^\[[0-9A-Fa-f:.]+\]:([0-9]{1,5})$ ]]; then
    port="${BASH_REMATCH[1]}"
  else
    die "无效的 NODE_EXPORTER_LISTEN_ADDRESS：${value}"
  fi
  (( port >= 1 && port <= 65535 )) || die "监听端口必须在 1 到 65535 之间"
}

detect_interactive_device() {
  [[ -n "${INTERACTIVE_DEVICE}" ]] && return 0
  if [[ -c /dev/tty ]] && { : </dev/tty; } 2>/dev/null; then
    INTERACTIVE_DEVICE="/dev/tty"
  elif [[ -t 0 ]]; then
    INTERACTIVE_DEVICE="/dev/stdin"
  else
    return 1
  fi
}

set_interactive_device() {
  detect_interactive_device || die "当前环境没有可用的交互式终端；请使用交互终端运行"
}

ask_yes_no_default() {
  local prompt="$1"
  local default_answer="$2"
  local answer=""
  local hint="y/N"
  [[ "${default_answer}" == "yes" ]] && hint="Y/n"
  while true; do
    read_editable answer "${prompt} [${hint}]："
    case "${answer,,}" in
      y|yes) return 0 ;;
      n|no) return 1 ;;
      "")
        if [[ "${default_answer}" == "yes" ]]; then
          return 0
        fi
        return 1
        ;;
      *) warn "请输入 y 或 n" ;;
    esac
  done
}

select_text_editor() {
  local editor
  for editor in vim nano vi; do
    if command -v "${editor}" >/dev/null 2>&1; then
      TEXT_EDITOR="$(command -v "${editor}")"
      result "证书编辑器：${editor}"
      return 0
    fi
  done
  die "未找到 vim、nano 或 vi，请先安装任意一个编辑器（例如：sudo apt install vim）后重新运行"
}

open_text_editor() {
  local file_path="$1"
  if [[ "${INTERACTIVE_DEVICE}" == "/dev/tty" ]]; then
    "${TEXT_EDITOR}" "${file_path}" </dev/tty >/dev/tty 2>&1
  else
    "${TEXT_EDITOR}" "${file_path}"
  fi
}

validate_pem_file() {
  local file_path="$1"
  local content_type="$2"
  [[ -s "${file_path}" ]] || return 1
  case "${content_type}" in
    certificate) openssl x509 -in "${file_path}" -noout >/dev/null 2>&1 ;;
    private_key) openssl pkey -in "${file_path}" -passin pass: -noout >/dev/null 2>&1 ;;
    *) return 1 ;;
  esac
}

edit_pem_file() {
  local content_name="$1"
  local file_path="$2"
  local content_type="$3"
  local content_description="$4"
  local content_warning="$5"
  local answer=""
  local pem_header="-----BEGIN CERTIFICATE-----"

  [[ "${content_type}" == "certificate" ]] || pem_header="-----BEGIN PRIVATE KEY-----（也支持 RSA/EC 私钥）"

  while true; do
    warning_card "配置 ${content_name}" \
      "应填写" "${content_description}" \
      "不要填写" "${content_warning}" \
      "PEM 开头" "${pem_header}" \
      "受管文件" "${file_path}" \
      "编辑器" "${TEXT_EDITOR##*/}"
    read_editable answer "按回车打开编辑器；输入 0/q 取消："
    case "${answer}" in
      "") ;;
      0|q|Q)
        RETURN_TO_MAIN=1
        warning_card "配置已取消" \
          "已保留" "当前 node_exporter 配置和证书" \
          "系统修改" "无"
        return 0
        ;;
      *) invalid_choice; continue ;;
    esac

    if ! open_text_editor "${file_path}"; then
      warn "编辑器异常退出，请重试"
      continue
    fi
    if validate_pem_file "${file_path}" "${content_type}"; then
      result "${content_name}校验通过"
      return 0
    fi
    warn "${content_name}为空、格式无效或使用了加密私钥，请重新编辑"
  done
}

certificate_matches_private_key() {
  local certificate_file="$1"
  local private_key_file="$2"
  local certificate_public_key
  local private_public_key

  certificate_public_key="$(openssl x509 -in "${certificate_file}" -pubkey -noout 2>/dev/null | openssl pkey -pubin -outform DER 2>/dev/null | sha256sum | awk '{print $1}')" || return 1
  private_public_key="$(openssl pkey -in "${private_key_file}" -passin pass: -pubout -outform DER 2>/dev/null | sha256sum | awk '{print $1}')" || return 1
  [[ -n "${certificate_public_key}" && "${certificate_public_key}" == "${private_public_key}" ]]
}

choose_install_mode() {
  [[ "${NODE_EXPORTER_MTLS_MODE_PRESET}" == "0" ]] || return 0
  set_interactive_device
  menu_section "请选择安装方式"
  menu_option "1" "mTLS 安装" "默认；HTTPS 并验证 Prometheus 客户端证书"
  menu_option "2" "HTTP 安装" "明文指标端点；建议配合防火墙"
  menu_option "3" "普通卸载" "保留配置、证书和系统账号"
  menu_option "4" "彻底清理" "永久删除配置、证书、systemd drop-in 和系统账号"
  menu_exit "0/q" "退出"
  while true; do
    local choice=""
    read_editable choice "请选择 [1-4]（默认 1）："
    case "${choice}" in
      ""|1)
        NODE_EXPORTER_MTLS_ENABLED=1
        result "已选择 mTLS 模式"
        return
        ;;
      2)
        warning_card "HTTP 明文传输风险" \
          "指标端点" "http://${NODE_EXPORTER_LISTEN_ADDRESS}/metrics" \
          "风险" "指标可被网络中的其他设备读取或篡改" \
          "建议" "通过防火墙仅允许 Prometheus 服务器访问"
        if ! ask_yes_no_default "确认使用普通 HTTP" no; then
          info "已取消 HTTP 模式，请重新选择"
          continue
        fi
        NODE_EXPORTER_MTLS_ENABLED=0
        result "已选择普通 HTTP 模式"
        return
        ;;
      3)
        SELECTED_ACTION="uninstall"
        result "已选择标准卸载"
        return
        ;;
      4)
        SELECTED_ACTION="purge"
        return
        ;;
      0|q|Q) SELECTED_ACTION="quit"; return ;;
      *) invalid_choice ;;
    esac
  done
}

choose_uninstall_mode() {
  set_interactive_device
  menu_section "请选择卸载方式"
  menu_option "1" "普通卸载" "默认；保留配置、证书和系统账号"
  menu_option "2" "彻底清理" "永久删除配置、证书、systemd drop-in 和系统账号"
  menu_exit "0/q" "取消并返回"
  while true; do
    local choice=""
    read_editable choice "请选择 [1-2]（默认 1）："
    case "${choice}" in
      ""|1)
        PURGE_MODE=0
        result "已选择标准卸载"
        return
        ;;
      2)
        PURGE_MODE=1
        danger_card "彻底清理警告" \
          "将删除" "${CONFIG_DIR}、${BIN_DIR}/node_exporter、${SERVICE_DROPIN_DIR}" \
          "不可恢复" "mTLS 证书、服务配置、全部 systemd drop-in 和系统账号"
        return
        ;;
      0|q|Q)
        RETURN_TO_MAIN=1
        warning_card "卸载已取消" \
          "已保留" "node_exporter 程序、配置、证书和系统账号" \
          "系统修改" "无"
        return 0
        ;;
      *) invalid_choice ;;
    esac
  done
}

choose_maintenance_action() {
  set_interactive_device
  menu_section "请选择维护操作"
  menu_option "1" "检查更新" "查询最新版本并按需下载"
  menu_option "2" "仅重新配置" "默认；不访问 API、不下载安装包"
  menu_option "3" "普通卸载" "保留配置、证书和系统账号"
  menu_option "4" "彻底清理" "永久删除配置、证书、systemd drop-in 和系统账号"
  menu_exit "0/q" "退出"
  while true; do
    local choice=""
    read_editable choice "请选择 [1-4]（默认 2）："
    case "${choice}" in
      1)
        MAINTENANCE_ACTION="update"
        result "已选择检查并更新"
        return
        ;;
      ""|2)
        MAINTENANCE_ACTION="reconfigure"
        result "已选择仅重新配置"
        return
        ;;
      3)
        SELECTED_ACTION="uninstall"
        result "已选择标准卸载"
        return
        ;;
      4)
        SELECTED_ACTION="purge"
        return
        ;;
      0|q|Q) SELECTED_ACTION="quit"; return ;;
      *) invalid_choice ;;
    esac
  done
}

choose_reconfigure_mode() {
  [[ "${NODE_EXPORTER_MTLS_MODE_PRESET}" == "0" ]] || return 0
  set_interactive_device
  menu_section "请选择接入方式"
  menu_option "1" "mTLS" "默认；HTTPS 并验证 Prometheus 客户端证书"
  menu_option "2" "普通 HTTP" "明文指标端点；建议配合防火墙"
  menu_exit "0/q" "返回上一级"
  while true; do
    local choice=""
    read_editable choice "请选择 [1-2]（默认 1）："
    case "${choice}" in
      ""|1)
        NODE_EXPORTER_MTLS_ENABLED=1
        result "将重新配置为 mTLS 模式"
        return
        ;;
      2)
        warning_card "HTTP 明文传输风险" \
          "指标端点" "http://${NODE_EXPORTER_LISTEN_ADDRESS}/metrics" \
          "风险" "指标可被网络中的其他设备读取或篡改" \
          "建议" "通过防火墙仅允许 Prometheus 服务器访问"
        if ! ask_yes_no_default "确认改为普通 HTTP" no; then
          info "已取消 HTTP 模式，请重新选择"
          continue
        fi
        NODE_EXPORTER_MTLS_ENABLED=0
        result "将重新配置为普通 HTTP 模式"
        return
        ;;
      0|q|Q)
        RETURN_TO_MAIN=1
        return 0
        ;;
      *) invalid_choice ;;
    esac
  done
}

load_existing_web_mode() {
  if [[ -f "${SERVICE_FILE}" ]] && grep -q -- '--web.config.file=' "${SERVICE_FILE}"; then
    NODE_EXPORTER_MTLS_ENABLED=1
    WEB_SCHEME="https"
    MTLS_STATUS="保持现有 mTLS 配置"
    info "更新将保留现有服务模式：mTLS"
  else
    NODE_EXPORTER_MTLS_ENABLED=0
    WEB_SCHEME="http"
    MTLS_STATUS="保持现有 HTTP 配置"
    info "更新将保留现有服务模式：普通 HTTP"
  fi
}

prepare_mtls_settings() {
  case "${NODE_EXPORTER_MTLS_ENABLED,,}" in
    1|true|yes) NODE_EXPORTER_MTLS_ENABLED=1 ;;
    0|false|no|"") NODE_EXPORTER_MTLS_ENABLED=0 ;;
    *) die "NODE_EXPORTER_MTLS_ENABLED 只支持 1/0、true/false 或 yes/no" ;;
  esac

  if [[ "${NODE_EXPORTER_MTLS_ENABLED}" != "1" ]]; then
    WEB_CONFIG_ARGUMENT=""
    WEB_SCHEME="http"
    MTLS_STATUS="关闭"
    return
  fi

  require_commands chown openssl touch
  set_interactive_device
  select_text_editor

  local tls_group="root"
  if getent group "${EXPORTER_GROUP}" >/dev/null 2>&1; then
    tls_group="${EXPORTER_GROUP}"
  fi
  install -d -o root -g "${tls_group}" -m 0750 "${CONFIG_DIR}" "${TLS_DIR}"
  touch "${TLS_DIR}/server.crt" "${TLS_DIR}/server.key" "${TLS_DIR}/client-ca.crt"
  chown root:"${tls_group}" "${TLS_DIR}/server.crt" "${TLS_DIR}/server.key" "${TLS_DIR}/client-ca.crt"
  chmod 0640 "${TLS_DIR}/server.crt" "${TLS_DIR}/server.key" "${TLS_DIR}/client-ca.crt"

  edit_pem_file "Prometheus 客户端根 CA 证书（信任锚）" "${TLS_DIR}/client-ca.crt" certificate \
    "信任 Prometheus 客户端证书的根 CA 公共证书；如使用中间 CA，可同时包含对应 CA 证书链" \
    "Prometheus 客户端证书、node_exporter 服务端证书或 CA 私钥"
  [[ "${RETURN_TO_MAIN}" == "0" ]] || return 0

  while true; do
    edit_pem_file "node_exporter 完整服务端证书" "${TLS_DIR}/server.crt" certificate \
      "node_exporter 提供 HTTPS 指标服务使用的完整服务端证书或证书链，证书 SAN 应包含 Prometheus 访问时使用的域名或 IP" \
      "Prometheus 客户端证书、私钥或 CA 私钥"
    [[ "${RETURN_TO_MAIN}" == "0" ]] || return 0
    edit_pem_file "node_exporter 服务端私钥" "${TLS_DIR}/server.key" private_key \
      "与上一步 node_exporter 服务端证书匹配的未加密私钥" \
      "证书、Prometheus 客户端私钥或加密私钥"
    [[ "${RETURN_TO_MAIN}" == "0" ]] || return 0
    if certificate_matches_private_key "${TLS_DIR}/server.crt" "${TLS_DIR}/server.key"; then
      result "服务端证书和私钥匹配"
      break
    fi
    warn "服务端证书和私钥不匹配，请重新编辑"
  done

  WEB_CONFIG_ARGUMENT=" --web.config.file=${WEB_CONFIG_FILE}"
  WEB_SCHEME="https"
  MTLS_STATUS="启用（强制验证客户端证书）"
}

install_mtls_config() {
  [[ "${NODE_EXPORTER_MTLS_ENABLED}" == "1" ]] || return 0

  local temp_dir="$1"
  local web_config_tmp="${temp_dir}/mtls-web.yml"

  install -d -o root -g "${EXPORTER_GROUP}" -m 0750 "${CONFIG_DIR}" "${TLS_DIR}"
  chown root:"${EXPORTER_GROUP}" "${TLS_DIR}/server.crt" "${TLS_DIR}/server.key" "${TLS_DIR}/client-ca.crt"
  chmod 0640 "${TLS_DIR}/server.crt" "${TLS_DIR}/server.key" "${TLS_DIR}/client-ca.crt"

  cat >"${web_config_tmp}" <<EOF
tls_server_config:
  cert_file: ${TLS_DIR}/server.crt
  key_file: ${TLS_DIR}/server.key
  client_auth_type: RequireAndVerifyClientCert
  client_ca_file: ${TLS_DIR}/client-ca.crt
  min_version: TLS12
EOF
  install -o root -g "${EXPORTER_GROUP}" -m 0640 "${web_config_tmp}" "${WEB_CONFIG_FILE}"
  rm -f -- "${web_config_tmp}"
  result "mTLS Web 配置和证书已安装：${WEB_CONFIG_FILE}"
}

download() {
  local url="$1"
  local destination="$2"
  if command -v curl >/dev/null 2>&1; then
    curl --fail --location --silent --show-error --retry 3 --connect-timeout 15 --output "${destination}" "${url}"
  elif command -v wget >/dev/null 2>&1; then
    wget --quiet --tries=3 --timeout=15 --output-document="${destination}" "${url}"
  else
    die "需要 curl 或 wget 才能下载安装包"
  fi
}

download_asset() {
  local url="$1"
  local destination="$2"
  if command -v curl >/dev/null 2>&1; then
    curl --fail --location --show-error --retry 3 --connect-timeout 15 --progress-bar --output "${destination}" "${url}"
  elif command -v wget >/dev/null 2>&1; then
    wget --tries=3 --timeout=15 --output-document="${destination}" "${url}"
  else
    die "需要 curl 或 wget 才能下载安装包"
  fi
}

proxy_configured() {
  [[ -n "${HTTPS_PROXY:-}" || -n "${https_proxy:-}" ||
     -n "${HTTP_PROXY:-}" || -n "${http_proxy:-}" ||
     -n "${ALL_PROXY:-}" || -n "${all_proxy:-}" ]]
}

download_api_direct() {
  local url="$1"
  local destination="$2"
  if command -v curl >/dev/null 2>&1; then
    curl --fail --location --silent --show-error --retry 3 --connect-timeout 15 \
      --noproxy '*' --output "${destination}" "${url}"
  elif command -v wget >/dev/null 2>&1; then
    wget --no-proxy --quiet --tries=3 --timeout=15 --output-document="${destination}" "${url}"
  else
    die "需要 curl 或 wget 才能访问 GitHub API"
  fi
}

release_version_from_metadata() {
  sed -n 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' "$1" | sed -n '1p'
}

fetch_latest_version() {
  local metadata_file="$1"
  local api_url="https://api.github.com/repos/prometheus/node_exporter/releases/latest"
  local release_tag=""

  if download "${api_url}" "${metadata_file}"; then
    release_tag="$(release_version_from_metadata "${metadata_file}")"
  fi
  if [[ "${release_tag}" =~ ^v?[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]]; then
    LATEST_VERSION="${release_tag#v}"
    info "API 访问方式：当前网络配置"
    return
  fi

  if proxy_configured; then
    warn "通过代理访问 GitHub API 失败，正在尝试直连"
    rm -f -- "${metadata_file}"
    if download_api_direct "${api_url}" "${metadata_file}"; then
      release_tag="$(release_version_from_metadata "${metadata_file}")"
    fi
    if [[ "${release_tag}" =~ ^v?[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]]; then
      LATEST_VERSION="${release_tag#v}"
      info "API 访问方式：绕过代理直连"
      return
    fi
  fi

  die "无法通过 GitHub Releases API 获取 node_exporter 最新正式版本"
}

installed_version() {
  if [[ -x "${BIN_DIR}/node_exporter" ]]; then
    local version_output
    version_output="$("${BIN_DIR}/node_exporter" --version 2>/dev/null || true)"
    sed -n '1s/^node_exporter, version \([^ ]*\).*/\1/p' <<<"${version_output}"
  fi
}

create_service_account() {
  if ! getent group "${EXPORTER_GROUP}" >/dev/null; then
    groupadd --system "${EXPORTER_GROUP}"
  fi
  if ! id "${EXPORTER_USER}" >/dev/null 2>&1; then
    useradd --system --gid "${EXPORTER_GROUP}" --home-dir /nonexistent \
      --no-create-home --shell /usr/sbin/nologin "${EXPORTER_USER}"
  fi
}

write_service_file() {
  local service_tmp
  service_tmp="$(mktemp -t node-exporter-service.XXXXXXXX)"
  cat >"${service_tmp}" <<EOF
[Unit]
Description=Prometheus Node Exporter
Documentation=https://github.com/prometheus/node_exporter
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
User=${EXPORTER_USER}
Group=${EXPORTER_GROUP}
ExecStart=${BIN_DIR}/node_exporter --web.listen-address=${NODE_EXPORTER_LISTEN_ADDRESS}${WEB_CONFIG_ARGUMENT}
Restart=on-failure
RestartSec=5s
TimeoutStopSec=20s
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=true
ProtectSystem=strict
ProtectControlGroups=true
ProtectKernelModules=true
ProtectKernelTunables=true
RestrictSUIDSGID=true

[Install]
WantedBy=multi-user.target
EOF
  install -m 0644 "${service_tmp}" "${SERVICE_FILE}"
  rm -f -- "${service_tmp}"
}

uninstall_node_exporter() {
  local uninstall_mode="${1:-ask}"
  local current_version

  print_banner "一键卸载"
  step "检查安装状态"
  require_root
  require_commands getent groupdel id sed systemctl userdel

  current_version="$(installed_version)"
  info "当前版本：${current_version:-未知或未安装}"
  info "程序路径：${BIN_DIR}/node_exporter"

  printf '\n'
  step "选择卸载方式"
  case "${uninstall_mode}" in
    purge)
      PURGE_MODE=1
      warn "将彻底删除 mTLS 配置、证书、systemd drop-in 和系统账号"
      ;;
    keep)
      PURGE_MODE=0
      info "将保留 mTLS 配置、证书和系统账号"
      ;;
    *)
      choose_uninstall_mode
      [[ "${RETURN_TO_MAIN}" == "0" ]] || return 0
      ;;
  esac

  if [[ "${PURGE_MODE}" == "1" ]] && detect_interactive_device; then
    if ! ask_yes_no_default "确认永久删除 node_exporter 配置、证书、systemd drop-in 和系统账号" no; then
      RETURN_TO_MAIN=1
      warning_card "彻底清理已取消" \
        "已保留" "${CONFIG_DIR}、node_exporter 程序和系统账号" \
        "系统修改" "无"
      return 0
    fi
  fi

  printf '\n'
  step "停止 node_exporter 服务"
  info "正在停止并禁用 node_exporter.service"
  systemctl disable --now node_exporter.service >/dev/null 2>&1 || true

  printf '\n'
  step "删除服务和程序文件"
  rm -f -- "${SERVICE_FILE}"
  rm -f -- "/etc/systemd/system/multi-user.target.wants/node_exporter.service"
  rm -f -- "${BIN_DIR}/node_exporter"

  systemctl daemon-reload
  systemctl reset-failed node_exporter.service >/dev/null 2>&1 || true

  result "node_exporter 服务和程序文件已删除"
  if [[ "${PURGE_MODE}" == "1" ]]; then
    printf '\n'
    step "彻底清理 mTLS 配置、证书、systemd drop-in 和账号"
    rm -rf -- "${CONFIG_DIR}" "${SERVICE_DROPIN_DIR}"
    systemctl daemon-reload
    if id "${EXPORTER_USER}" >/dev/null 2>&1; then
      userdel "${EXPORTER_USER}"
    fi
    if getent group "${EXPORTER_GROUP}" >/dev/null; then
      groupdel "${EXPORTER_GROUP}"
    fi
    result "mTLS 配置、证书、systemd drop-in 和系统账号已删除"
    print_result_card "彻底清理完成" \
      "已删除" "程序、服务、${CONFIG_DIR}、${SERVICE_DROPIN_DIR} 和系统账号" \
      "未清理" "下载缓存以外的系统日志和 Prometheus 中已有指标"
  else
    info "mTLS 配置和证书已保留：${CONFIG_DIR}"
    info "系统账号已保留：${EXPORTER_USER}"
    print_result_card "普通卸载完成" \
      "已删除" "程序和 systemd 服务" \
      "已保留" "${CONFIG_DIR}、mTLS 证书和系统账号" \
      "未清理" "Prometheus 中已有指标"
  fi
}

main() {
  case "${1:-install}" in
    -h|--help|help)
      [[ "$#" -le 1 ]] || die "参数过多，请使用 --help 查看用法"
      usage
      exit 0
      ;;
    uninstall|--uninstall|-u)
      [[ "$#" -eq 1 ]] || die "参数过多，请使用 --help 查看用法"
      uninstall_node_exporter ask
      return 0
      ;;
    purge|--purge)
      [[ "$#" -eq 1 ]] || die "参数过多，请使用 --help 查看用法"
      uninstall_node_exporter purge
      exit 0
      ;;
    install|--install|-i)
      [[ "$#" -le 1 ]] || die "参数过多，请使用 --help 查看用法"
      ;;
    mtls|--mtls)
      [[ "$#" -eq 1 ]] || die "参数过多，请使用 --help 查看用法"
      NODE_EXPORTER_MTLS_ENABLED=1
      NODE_EXPORTER_MTLS_MODE_PRESET=1
      ;;
    http|--http)
      [[ "$#" -eq 1 ]] || die "参数过多，请使用 --help 查看用法"
      NODE_EXPORTER_MTLS_ENABLED=0
      NODE_EXPORTER_MTLS_MODE_PRESET=1
      ;;
    *)
      die "未知命令：$1，请使用 --help 查看用法"
      ;;
  esac

  print_banner "一键安装、更新与卸载"
  step "检查运行权限"
  require_root
  result "root 权限检查通过"

  local requested_version="${NODE_EXPORTER_VERSION}"
  local version=""
  local arch=""
  local current_version=""
  local action=""
  local archive=""
  local release_url=""
  local work_dir=""
  local checksum=""
  local extracted_dir=""
  local download_required=0

  require_commands mktemp rm
  work_dir="$(mktemp -d -t node-exporter-install.XXXXXXXX)"
  WORK_DIR="${work_dir}"

  while true; do
    RETURN_TO_MAIN=0
    SELECTED_ACTION="install"
    MAINTENANCE_ACTION="install"
    current_version=""
    download_required=0

    require_commands sed
    current_version="$(installed_version)"
    printf '\n'
    step "检查安装状态"
    if [[ -z "${current_version}" ]]; then
      current_version="未安装"
      result "未检测到 node_exporter，将安装最新版本"
      printf '\n'
      step "选择安装方式"
      choose_install_mode
    else
      result "检测到 node_exporter v${current_version}"
      printf '\n'
      step "选择维护操作"
      choose_maintenance_action
      if [[ "${RETURN_TO_MAIN}" == "0" && "${MAINTENANCE_ACTION}" == "reconfigure" ]]; then
        printf '\n'
        step "选择重新配置方式"
        choose_reconfigure_mode
      fi
    fi

    if [[ "${RETURN_TO_MAIN}" == "1" ]]; then
      NODE_EXPORTER_MTLS_MODE_PRESET=0
      continue
    fi

    case "${SELECTED_ACTION}" in
      quit)
        exit_card "已退出"
        return 0
        ;;
      uninstall)
        uninstall_node_exporter keep
        return 0
        ;;
      purge)
        uninstall_node_exporter purge
        return 0
        ;;
    esac

    printf '\n'
    step "检查安装依赖"
    require_commands awk cat chmod cp getent grep groupadd id install mktemp rm sed sha256sum systemctl tar uname useradd
    result "安装依赖检查通过"

    validate_listen_address "${NODE_EXPORTER_LISTEN_ADDRESS}"
    if [[ "${MAINTENANCE_ACTION}" == "update" ]]; then
      load_existing_web_mode
    else
      begin_config_transaction
      prepare_mtls_settings
    fi
    if [[ "${RETURN_TO_MAIN}" == "1" ]]; then
      [[ "${CONFIG_TRANSACTION_ACTIVE}" == "0" ]] || restore_config_transaction
      NODE_EXPORTER_MTLS_MODE_PRESET=0
      continue
    fi
    commit_config_transaction
    break
  done

  arch="$(detect_arch)"

  if [[ "${MAINTENANCE_ACTION}" == "reconfigure" ]]; then
    version="${current_version}"
    action="重新配置"
    info "跳过 GitHub Releases API 和安装包下载"
  else
    printf '\n'
    step "检查发布版本"
    if [[ "${requested_version}" == "latest" ]]; then
      info "正在通过 GitHub Releases API 获取最新正式版本"
      fetch_latest_version "${work_dir}/release.json"
      version="${LATEST_VERSION}"
      result "最新正式版本：v${version}"
    else
      version="${requested_version#v}"
      validate_version "${version}"
      info "使用指定版本：v${version}"
    fi

    if [[ "${current_version}" == "未安装" ]]; then
      action="安装"
      download_required=1
    elif [[ "${current_version}" == "${version}" ]]; then
      action="检查更新（已是目标版本）"
      result "当前已是目标版本，跳过安装包下载"
    else
      action="更新"
      download_required=1
    fi
  fi
  print_release_info "${arch}" "${current_version}" "${version}" "${action}"

  if [[ "${download_required}" == "1" ]]; then
    archive="node_exporter-${version}.linux-${arch}.tar.gz"
    release_url="${DOWNLOAD_BASE_URL%/}/v${version}"

    printf '\n'
    step "下载发布文件"
    info "正在下载：${archive}"
    download_asset "${release_url}/${archive}" "${work_dir}/${archive}"
    download "${release_url}/sha256sums.txt" "${work_dir}/sha256sums.txt"
    result "发布文件下载完成"

    printf '\n'
    step "校验发布文件"
    checksum="$(awk -v file="${archive}" '$2 == file || $2 == ("*" file) { print $1; exit }' "${work_dir}/sha256sums.txt")"
    [[ "${checksum}" =~ ^[0-9a-fA-F]{64}$ ]] || die "校验文件中没有 ${archive} 的有效校验值"
    printf '%s  %s\n' "${checksum}" "${work_dir}/${archive}" | sha256sum --check --status || die "安装包 SHA-256 校验失败"
    result "SHA-256 校验通过"

    printf '\n'
    step "安装 node_exporter"
    tar -xzf "${work_dir}/${archive}" -C "${work_dir}"
    extracted_dir="${work_dir}/node_exporter-${version}.linux-${arch}"
    [[ -x "${extracted_dir}/node_exporter" ]] || die "发布包中缺少 node_exporter 程序"
  elif [[ "${MAINTENANCE_ACTION}" == "reconfigure" ]]; then
    printf '\n'
    step "重新配置 node_exporter"
    [[ -x "${BIN_DIR}/node_exporter" ]] || die "现有安装缺少 node_exporter，无法仅重新配置，请重新运行并选择更新"
  else
    printf '\n'
    step "保留当前程序和配置"
    [[ -x "${BIN_DIR}/node_exporter" ]] || die "现有安装缺少 node_exporter，请重新运行并选择更新"
    result "无需替换程序文件"
  fi

  create_service_account
  if [[ "${download_required}" == "1" ]]; then
    install -m 0755 "${extracted_dir}/node_exporter" "${BIN_DIR}/node_exporter"
  fi
  if [[ "${MAINTENANCE_ACTION}" != "update" ]]; then
    install_mtls_config "${work_dir}"
    write_service_file
    result "程序和服务配置处理完成"
  else
    info "保留现有 systemd 服务配置：${SERVICE_FILE}"
    result "程序更新检查完成，现有配置保持不变"
  fi

  printf '\n'
  step "启动系统服务"
  systemctl daemon-reload
  systemctl enable node_exporter.service >/dev/null
  systemctl restart node_exporter.service
  if ! systemctl is-active --quiet node_exporter.service; then
    systemctl --no-pager --full status node_exporter.service || true
    die "node_exporter 服务启动失败"
  fi
  result "node_exporter 服务已启动并设置为开机自启"
  info "查看日志：journalctl -u node_exporter -f"

  rm -rf -- "${work_dir}"
  WORK_DIR=""
  print_completion_card "${action}" "${version}"
}

if [[ "${NODE_EXPORTER_INSTALLER_SOURCE_ONLY:-0}" != "1" ]]; then
  trap 'on_error "$?" "$LINENO"' ERR
  trap on_signal INT TERM
  trap cleanup EXIT
  init_colors
  main "$@"
fi
