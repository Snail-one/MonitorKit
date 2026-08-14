#!/usr/bin/env bash
# Install, update, configure, uninstall, or purge Grafana Alloy.
# Canonical probe installer: scripts/probes/alloy/install.sh

set -Eeuo pipefail

readonly CONFIG_DIR="/etc/alloy"
readonly CONFIG_FILE="${CONFIG_DIR}/config.alloy"
readonly DATA_DIR="/var/lib/alloy"
readonly TLS_DIR="${CONFIG_DIR}/tls"

PROMETHEUS_URL_PRESET=0
LOKI_URL_PRESET=0
PROMETHEUS_MTLS_MODE_PRESET=0
LOKI_MTLS_MODE_PRESET=0
[[ -n "${PROMETHEUS_URL+x}" ]] && PROMETHEUS_URL_PRESET=1
[[ -n "${LOKI_URL+x}" ]] && LOKI_URL_PRESET=1
[[ -n "${PROMETHEUS_MTLS_ENABLED+x}" ]] && PROMETHEUS_MTLS_MODE_PRESET=1
[[ -n "${LOKI_MTLS_ENABLED+x}" ]] && LOKI_MTLS_MODE_PRESET=1

PROMETHEUS_URL="${PROMETHEUS_URL:-}"
LOKI_URL="${LOKI_URL:-}"
PROMETHEUS_MTLS_CA_FILE="${PROMETHEUS_MTLS_CA_FILE:-}"
PROMETHEUS_MTLS_CERT_FILE="${PROMETHEUS_MTLS_CERT_FILE:-}"
PROMETHEUS_MTLS_KEY_FILE="${PROMETHEUS_MTLS_KEY_FILE:-}"
PROMETHEUS_TLS_SERVER_NAME="${PROMETHEUS_TLS_SERVER_NAME:-}"
LOKI_MTLS_CA_FILE="${LOKI_MTLS_CA_FILE:-}"
LOKI_MTLS_CERT_FILE="${LOKI_MTLS_CERT_FILE:-}"
LOKI_MTLS_KEY_FILE="${LOKI_MTLS_KEY_FILE:-}"
LOKI_TLS_SERVER_NAME="${LOKI_TLS_SERVER_NAME:-}"

RESET=""
BOLD=""
ORANGE=""
BLUE=""
GREEN=""
YELLOW=""
RED=""
INTERACTIVE_DEVICE=""
TEXT_EDITOR=""
SELECTED_ACTION="install"
RETURN_TO_MAIN=0
PURGE_MODE=0
PROMETHEUS_MTLS_ENABLED="${PROMETHEUS_MTLS_ENABLED:-1}"
LOKI_MTLS_ENABLED="${LOKI_MTLS_ENABLED:-1}"
WORK_DIR=""
TRANSACTION_ACTIVE=0
CONFIG_EXISTED=0
SERVICE_WAS_ACTIVE=0
SERVICE_WAS_ENABLED=0
PACKAGE_WAS_INSTALLED=0
DATA_EXISTED=0

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
  ORANGE="${esc}[38;5;208m"
  BLUE="${esc}[34m"
  GREEN="${esc}[32m"
  YELLOW="${esc}[33m"
  RED="${esc}[31m"
}

print_banner() {
  printf '%s%s%s\n' "${BOLD}${ORANGE}" "╭─ Grafana Alloy" "${RESET}"
  printf '%s│ %s%s\n' "${ORANGE}" "$1" "${RESET}"
  printf '%s%s%s\n\n' "${ORANGE}" "╰────────────────────────────────────────────────────" "${RESET}"
}

step() { printf '%s[步骤]%s %s\n' "${ORANGE}" "${RESET}" "$*"; }
info() { printf '%s[信息]%s %s\n' "${BLUE}" "${RESET}" "$*"; }
result() { printf '%s[结果]%s %s\n' "${GREEN}" "${RESET}" "$*"; }
warn() { printf '%s[警告]%s %s\n' "${YELLOW}" "${RESET}" "$*"; }
die() { printf '%s[错误]%s %s\n' "${RED}" "${RESET}" "$*" >&2; exit 1; }

print_completion_card() {
  local title="$1"
  printf '\n%s%s%s\n' "${BOLD}${ORANGE}" "╭─ Grafana Alloy" "${RESET}"
  printf '%s│ %s%s%s%s\n' "${ORANGE}" "${BOLD}${GREEN}" "${title}" "${RESET}" ""
  printf '%s│ %s服务：%salloy.service（运行中、开机自启）\n' "${ORANGE}" "${BLUE}" "${RESET}"
  printf '%s│ %s指标中心：%s%s\n' "${ORANGE}" "${BLUE}" "${RESET}" "${PROMETHEUS_URL}"
  printf '%s│ %s日志中心：%s%s\n' "${ORANGE}" "${BLUE}" "${RESET}" "${LOKI_URL}"
  printf '%s│ %sPrometheus mTLS：%s%s\n' "${ORANGE}" "${BLUE}" "${RESET}" "$([[ "${PROMETHEUS_MTLS_ENABLED}" == "1" ]] && printf '已启用' || printf '未启用（HTTP，未加密）')"
  printf '%s│ %sLoki mTLS：%s%s\n' "${ORANGE}" "${BLUE}" "${RESET}" "$([[ "${LOKI_MTLS_ENABLED}" == "1" ]] && printf '已启用' || printf '未启用（HTTP，未加密）')"
  printf '%s│ %s配置文件：%s%s\n' "${ORANGE}" "${BLUE}" "${RESET}" "${CONFIG_FILE}"
  printf '%s%s%s\n' "${ORANGE}" "╰────────────────────────────────────────────────────" "${RESET}"
}

print_uninstall_card() {
  local title="$1"
  local retained="$2"
  printf '\n%s%s%s\n' "${BOLD}${ORANGE}" "╭─ Grafana Alloy" "${RESET}"
  printf '%s│ %s%s%s\n' "${ORANGE}" "${BOLD}${GREEN}" "${title}" "${RESET}"
  printf '%s│ %s已删除：%sAlloy 软件包及其服务\n' "${ORANGE}" "${BLUE}" "${RESET}"
  printf '%s│ %s%s\n' "${ORANGE}" "${retained}" "${RESET}"
  printf '%s│ %s未清理：%sGrafana 软件源/签名密钥、软件包缓存、journald 历史日志\n' "${ORANGE}" "${BLUE}" "${RESET}"
  printf '%s│ %s账号说明：%salloy 账号由系统软件包管理器决定是否保留\n' "${ORANGE}" "${BLUE}" "${RESET}"
  printf '%s%s%s\n' "${ORANGE}" "╰────────────────────────────────────────────────────" "${RESET}"
}

usage() {
  cat <<'EOF'
Grafana Alloy 指标与日志统一探针维护脚本。

用法：
  curl -fsSL https://raw.githubusercontent.com/Snail-one/MonitorKit/main/scripts/probes/alloy/install.sh | sudo bash
  sudo ./install.sh                 # 安装或进入维护菜单
  sudo ./install.sh update          # 更新软件包，保留现有配置
  sudo ./install.sh reconfigure     # 仅重新配置，不更新软件包
  sudo ./install.sh status          # 查看运行状态和受管文件
  sudo ./install.sh uninstall       # 交互选择普通卸载或彻底清理
  sudo ./install.sh purge           # 直接彻底清理

交互模式会填写 Prometheus/Loki 中心地址，并使用 vim、nano 或 vi 直接
编辑 /etc/alloy/tls/ 中的 mTLS 证书。Prometheus 和 Loki 均默认推荐 mTLS；
选择不启用时会显示明文传输风险并要求再次确认，然后使用普通 HTTP。

无人值守配置变量：
  PROMETHEUS_URL=https://monitor.example.com:24567
  PROMETHEUS_MTLS_ENABLED=1
  PROMETHEUS_MTLS_CA_FILE=/root/prometheus-ca.crt
  PROMETHEUS_MTLS_CERT_FILE=/root/prometheus-alloy.crt
  PROMETHEUS_MTLS_KEY_FILE=/root/prometheus-alloy.key
  PROMETHEUS_TLS_SERVER_NAME=monitor.example.com
  LOKI_URL=https://logs.example.com:34567
  LOKI_MTLS_ENABLED=1
  LOKI_MTLS_CA_FILE=/root/loki-ca.crt
  LOKI_MTLS_CERT_FILE=/root/loki-alloy.crt
  LOKI_MTLS_KEY_FILE=/root/loki-alloy.key
  LOKI_TLS_SERVER_NAME=logs.example.com

普通卸载保留 /etc/alloy 和 /var/lib/alloy；purge 会删除这两个目录。
两种卸载方式均不清理 Grafana 软件源、签名密钥、包缓存和历史日志。
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
  detect_interactive_device || die "当前环境没有可用的交互式终端；请提供完整环境变量"
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
  die "未找到 vim、nano 或 vi，请先安装任意一个文本编辑器"
}

open_text_editor() {
  local file_path="$1"
  if [[ "${INTERACTIVE_DEVICE}" == "/dev/tty" ]]; then
    "${TEXT_EDITOR}" "${file_path}" </dev/tty >/dev/tty 2>&1
  else
    "${TEXT_EDITOR}" "${file_path}"
  fi
}

trim_value() {
  local value="$1"
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  printf '%s' "${value}"
}

ask_yes_no_default() {
  local prompt="$1"
  local default_answer="$2"
  local answer=""
  local hint="y/N"
  [[ "${default_answer}" == "yes" ]] && hint="Y/n"
  set_interactive_device
  while true; do
    printf '❯ %s [%s]： ' "${prompt}" "${hint}" >&2
    IFS= read -r answer <"${INTERACTIVE_DEVICE}" || die "读取输入失败"
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

valid_backend_url() {
  local value="$1"
  local port=""
  if [[ "${value}" =~ ^https?://[A-Za-z0-9._-]+(:([0-9]{1,5}))?$ ]]; then
    port="${BASH_REMATCH[2]:-}"
    if [[ -z "${port}" ]]; then
      return 0
    fi
    (( 10#${port} >= 1 && 10#${port} <= 65535 ))
    return
  fi
  return 1
}

normalize_backend_url_input() {
  local value="$1"
  local default_scheme="${2:-https}"
  value="$(trim_value "${value}")"
  value="${value%/}"
  if [[ -n "${value}" && "${value}" != *://* ]]; then
    value="${default_scheme}://${value}"
  fi
  printf '%s' "${value}"
}

prompt_backend_url() {
  local variable_name="$1"
  local label="$2"
  local required_scheme="${3:-}"
  local force_prompt="${4:-0}"
  local value="${!variable_name:-}"
  local entered=""
  local candidate=""
  local default_scheme="${required_scheme:-https}"
  local prompt_text=""

  if [[ -n "${value}" && "${force_prompt}" != "1" ]]; then
    value="$(normalize_backend_url_input "${value}" "${default_scheme}")"
    printf -v "${variable_name}" '%s' "${value}"
    return 0
  fi
  value="$(normalize_backend_url_input "${value}" "${default_scheme}")"
  if ! valid_backend_url "${value}" || { [[ -n "${required_scheme}" ]] && [[ "${value}" != "${required_scheme}://"* ]]; }; then
    value=""
  fi
  detect_interactive_device || die "无人值守配置缺少 ${variable_name}"
  while true; do
    if [[ -n "${value}" ]]; then
      printf -v prompt_text '❯ %s 根地址 [%s]： ' "${label}" "${value}"
    elif [[ "${required_scheme}" == "https" ]]; then
      printf -v prompt_text '❯ %s 地址（可直接输入 IP:端口，例如 10.0.0.10:24567，自动使用 HTTPS）： ' "${label}"
    elif [[ "${required_scheme}" == "http" ]]; then
      printf -v prompt_text '❯ %s 地址（已确认不启用 mTLS；输入 IP:端口将使用 HTTP）： ' "${label}"
    else
      printf -v prompt_text '❯ %s 根地址（请输入 http:// 或 https://）： ' "${label}"
    fi
    IFS= read -e -r -p "${prompt_text}" entered <"${INTERACTIVE_DEVICE}" || die "读取输入失败"
    entered="$(trim_value "${entered}")"
    candidate="${entered:-${value}}"
    candidate="$(normalize_backend_url_input "${candidate}" "${default_scheme}")"
    if valid_backend_url "${candidate}" && { [[ -z "${required_scheme}" ]] || [[ "${candidate}" == "${required_scheme}://"* ]]; }; then
      printf -v "${variable_name}" '%s' "${candidate}"
      return 0
    fi
    if [[ "${required_scheme}" == "https" ]]; then
      warn "已选择 mTLS；请输入 主机:端口（自动使用 HTTPS）或 https://主机:端口"
    elif [[ "${required_scheme}" == "http" ]]; then
      warn "已选择不启用 mTLS；请输入 主机:端口（自动使用 HTTP）或 http://主机:端口"
    else
      warn "地址无效，请输入 主机:端口、http://主机:端口 或 https://主机:端口"
    fi
  done
}

host_from_url() {
  local host="${1#*://}"
  host="${host%%:*}"
  printf '%s' "${host}"
}

prompt_server_name() {
  local variable_name="$1"
  local label="$2"
  local url="$3"
  local force_prompt="${4:-0}"
  local value="${!variable_name:-}"
  local entered=""
  local default_value
  local prompt_text=""
  default_value="${value:-$(host_from_url "${url}")}"

  if [[ -n "${value}" && "${force_prompt}" != "1" ]]; then
    return 0
  fi
  if ! detect_interactive_device; then
    printf -v "${variable_name}" '%s' "${default_value}"
    return 0
  fi
  set_interactive_device
  while true; do
    printf -v prompt_text '❯ %s TLS server_name [%s]： ' "${label}" "${default_value}"
    IFS= read -e -r -p "${prompt_text}" entered <"${INTERACTIVE_DEVICE}" || die "读取输入失败"
    entered="$(trim_value "${entered:-${default_value}}")"
    if [[ "${entered}" =~ ^[A-Za-z0-9._-]+$ ]]; then
      printf -v "${variable_name}" '%s' "${entered}"
      return 0
    fi
    warn "server_name 只能包含字母、数字、点、下划线和连字符"
  done
}

installed_version() {
  if command -v alloy >/dev/null 2>&1; then
    alloy --version 2>/dev/null | head -n 1 || true
  elif command -v dpkg-query >/dev/null 2>&1 && dpkg-query -W -f='${Version}' alloy >/dev/null 2>&1; then
    dpkg-query -W -f='${Version}\n' alloy
  elif command -v rpm >/dev/null 2>&1 && rpm -q alloy >/dev/null 2>&1; then
    rpm -q --qf '%{VERSION}-%{RELEASE}\n' alloy
  fi
}

is_installed() {
  [[ -n "$(installed_version)" ]]
}

load_existing_settings() {
  [[ -r "${CONFIG_FILE}" ]] || return 0
  if [[ -z "${PROMETHEUS_URL}" ]]; then
    PROMETHEUS_URL="$(awk -F'"' '/url[[:space:]]*=[[:space:]]*".*\/api\/v1\/write"/ { sub(/\/api\/v1\/write$/, "", $2); print $2; exit }' "${CONFIG_FILE}")"
  fi
  if [[ -z "${LOKI_URL}" ]]; then
    LOKI_URL="$(awk -F'"' '/url[[:space:]]*=[[:space:]]*".*\/loki\/api\/v1\/push"/ { sub(/\/loki\/api\/v1\/push$/, "", $2); print $2; exit }' "${CONFIG_FILE}")"
  fi
  if [[ -z "${PROMETHEUS_TLS_SERVER_NAME}" ]]; then
    PROMETHEUS_TLS_SERVER_NAME="$(awk -F'"' '/prometheus\.remote_write "center"/ { active=1 } /loki\.source\.journal/ { active=0 } active && /server_name[[:space:]]*=/ { print $2; exit }' "${CONFIG_FILE}")"
  fi
  if [[ -z "${LOKI_TLS_SERVER_NAME}" ]]; then
    LOKI_TLS_SERVER_NAME="$(awk -F'"' '/loki\.write "center"/ { active=1 } active && /server_name[[:space:]]*=/ { print $2; exit }' "${CONFIG_FILE}")"
  fi
  if [[ "${PROMETHEUS_MTLS_MODE_PRESET}" == "0" ]]; then
    PROMETHEUS_MTLS_ENABLED=0
    if grep -qF "${TLS_DIR}/prometheus-ca.crt" "${CONFIG_FILE}"; then
      PROMETHEUS_MTLS_ENABLED=1
    fi
  fi
  if [[ "${LOKI_MTLS_MODE_PRESET}" == "0" ]]; then
    LOKI_MTLS_ENABLED=0
    if grep -qF "${TLS_DIR}/loki-ca.crt" "${CONFIG_FILE}"; then
      LOKI_MTLS_ENABLED=1
    fi
  fi
}

choose_install_action() {
  set_interactive_device
  printf '%s  1.%s 安装 Grafana Alloy（默认）\n' "${ORANGE}" "${RESET}" >&2
  printf '%s  2.%s 查看安装状态\n' "${GREEN}" "${RESET}" >&2
  printf '%s  3.%s 彻底清理残留配置和数据\n' "${RED}" "${RESET}" >&2
  printf '%s  q.%s 退出\n' "${BLUE}" "${RESET}" >&2
  while true; do
    local choice=""
    printf '%s请选择操作 [1-3]（默认 1）：%s' "${BLUE}" "${RESET}" >&2
    IFS= read -r choice <"${INTERACTIVE_DEVICE}" || die "读取操作失败"
    case "${choice}" in
      ""|1) SELECTED_ACTION="install"; return ;;
      2) SELECTED_ACTION="status"; return ;;
      3) SELECTED_ACTION="purge"; return ;;
      q|Q) SELECTED_ACTION="quit"; return ;;
      *) warn "输入无效：请输入 1、2、3 或 q" ;;
    esac
  done
}

choose_maintenance_action() {
  set_interactive_device
  printf '%s  1.%s 更新 Alloy 软件包（保留现有配置）\n' "${ORANGE}" "${RESET}" >&2
  printf '%s  2.%s 仅重新配置（默认）\n' "${GREEN}" "${RESET}" >&2
  printf '%s  3.%s 查看状态和受管文件\n' "${BLUE}" "${RESET}" >&2
  printf '%s  4.%s 普通卸载（保留配置、证书和数据）\n' "${YELLOW}" "${RESET}" >&2
  printf '%s  5.%s 彻底清理（删除配置、证书和数据）\n' "${RED}" "${RESET}" >&2
  printf '%s  q.%s 退出\n' "${BLUE}" "${RESET}" >&2
  while true; do
    local choice=""
    printf '%s请选择维护操作 [1-5]（默认 2）：%s' "${BLUE}" "${RESET}" >&2
    IFS= read -r choice <"${INTERACTIVE_DEVICE}" || die "读取维护操作失败"
    case "${choice}" in
      1) SELECTED_ACTION="update"; return ;;
      ""|2) SELECTED_ACTION="reconfigure"; return ;;
      3) SELECTED_ACTION="status"; return ;;
      4) SELECTED_ACTION="uninstall"; return ;;
      5) SELECTED_ACTION="purge"; return ;;
      q|Q) SELECTED_ACTION="quit"; return ;;
      *) warn "输入无效：请输入 1、2、3、4、5 或 q" ;;
    esac
  done
}

choose_uninstall_mode() {
  set_interactive_device
  printf '%s  1.%s 普通卸载（保留 /etc/alloy 和 /var/lib/alloy，默认）\n' "${GREEN}" "${RESET}" >&2
  printf '%s  2.%s 彻底清理（删除配置、证书和数据）\n' "${RED}" "${RESET}" >&2
  printf '%s  q.%s 取消\n' "${BLUE}" "${RESET}" >&2
  while true; do
    local choice=""
    printf '%s请选择卸载方式 [1-2]（默认 1）：%s' "${BLUE}" "${RESET}" >&2
    IFS= read -r choice <"${INTERACTIVE_DEVICE}" || die "读取卸载方式失败"
    case "${choice}" in
      ""|1) PURGE_MODE=0; return ;;
      2) PURGE_MODE=1; return ;;
      q|Q) RETURN_TO_MAIN=1; return ;;
      *) warn "输入无效：请输入 1、2 或 q" ;;
    esac
  done
}

download_file() {
  local url="$1"
  local destination="$2"
  local args=(-fL --proto '=https')
  if [[ -t 2 ]]; then
    args+=(--progress-bar)
  else
    args+=(-sS)
  fi
  curl "${args[@]}" "${url}" -o "${destination}"
}

install_debian() {
  export DEBIAN_FRONTEND=noninteractive
  apt-get update
  apt-get install -y ca-certificates curl gpg
  install -d -m 0755 /etc/apt/keyrings
  download_file https://apt.grafana.com/gpg-full.key /etc/apt/keyrings/grafana.asc
  chmod 0644 /etc/apt/keyrings/grafana.asc
  printf '%s\n' 'deb [signed-by=/etc/apt/keyrings/grafana.asc] https://apt.grafana.com stable main' > /etc/apt/sources.list.d/grafana.list
  apt-get update
  apt-get install -y alloy
}

install_rpm() {
  local package_manager="$1"
  local key_file
  command -v curl >/dev/null 2>&1 || "${package_manager}" install -y curl
  key_file="$(mktemp)"
  download_file https://rpm.grafana.com/gpg.key "${key_file}"
  rpm --import "${key_file}"
  rm -f -- "${key_file}"
  install -d -m 0755 /etc/yum.repos.d
  tee /etc/yum.repos.d/grafana.repo >/dev/null <<'EOF'
[grafana]
name=grafana
baseurl=https://rpm.grafana.com
repo_gpgcheck=1
enabled=1
gpgcheck=1
gpgkey=https://rpm.grafana.com/gpg.key
sslverify=1
sslcacert=/etc/pki/tls/certs/ca-bundle.crt
EOF
  "${package_manager}" install -y alloy
}

install_suse() {
  zypper --non-interactive addrepo --check --refresh https://rpm.grafana.com grafana || true
  zypper --non-interactive --gpg-auto-import-keys refresh
  zypper --non-interactive install alloy
}

install_package() {
  if command -v apt-get >/dev/null 2>&1; then
    install_debian
  elif command -v dnf >/dev/null 2>&1; then
    install_rpm dnf
  elif command -v yum >/dev/null 2>&1; then
    install_rpm yum
  elif command -v zypper >/dev/null 2>&1; then
    install_suse
  else
    die "不支持当前发行版；需要 apt-get、dnf、yum 或 zypper"
  fi
}

remove_package() {
  if command -v apt-get >/dev/null 2>&1; then
    apt-get remove -y alloy
  elif command -v dnf >/dev/null 2>&1; then
    dnf remove -y alloy
  elif command -v yum >/dev/null 2>&1; then
    yum remove -y alloy
  elif command -v zypper >/dev/null 2>&1; then
    zypper --non-interactive remove alloy
  else
    warn "未找到受支持的软件包管理器，请手动删除 Alloy 软件包"
  fi
}

begin_config_transaction() {
  WORK_DIR="$(mktemp -d -t alloy-config.XXXXXXXX)"
  CONFIG_EXISTED=0
  DATA_EXISTED=0
  SERVICE_WAS_ACTIVE=0
  SERVICE_WAS_ENABLED=0
  PACKAGE_WAS_INSTALLED=0
  is_installed && PACKAGE_WAS_INSTALLED=1
  if [[ -d "${CONFIG_DIR}" ]]; then
    cp -a "${CONFIG_DIR}" "${WORK_DIR}/config-backup"
    CONFIG_EXISTED=1
  fi
  [[ ! -d "${DATA_DIR}" ]] || DATA_EXISTED=1
  systemctl is-active --quiet alloy.service 2>/dev/null && SERVICE_WAS_ACTIVE=1
  systemctl is-enabled --quiet alloy.service 2>/dev/null && SERVICE_WAS_ENABLED=1
  TRANSACTION_ACTIVE=1
}

restore_config_transaction() {
  [[ "${TRANSACTION_ACTIVE}" == "1" ]] || return 0
  warn "配置操作未完成，正在恢复操作前状态"
  systemctl stop alloy.service >/dev/null 2>&1 || true
  if [[ "${PACKAGE_WAS_INSTALLED}" == "0" ]] && is_installed; then
    remove_package >/dev/null 2>&1 || warn "自动移除本次新安装的 Alloy 软件包失败，请手动检查"
    info "本次新安装的 Alloy 软件包已回滚；Grafana 软件源和签名密钥仍保留"
  fi
  rm -rf -- "${CONFIG_DIR}"
  if [[ "${CONFIG_EXISTED}" == "1" ]]; then
    cp -a "${WORK_DIR}/config-backup" "${CONFIG_DIR}"
  fi
  if [[ "${DATA_EXISTED}" == "0" ]]; then
    rm -rf -- "${DATA_DIR}"
  fi
  if [[ "${SERVICE_WAS_ACTIVE}" == "1" ]]; then
    systemctl restart alloy.service >/dev/null 2>&1 || warn "旧配置已恢复，但 Alloy 服务未能重新启动"
  else
    systemctl stop alloy.service >/dev/null 2>&1 || true
  fi
  if [[ "${SERVICE_WAS_ENABLED}" == "1" ]]; then
    systemctl enable alloy.service >/dev/null 2>&1 || true
  else
    systemctl disable alloy.service >/dev/null 2>&1 || true
  fi
  TRANSACTION_ACTIVE=0
  result "配置文件和证书已恢复"
}

commit_config_transaction() {
  TRANSACTION_ACTIVE=0
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

  install -d -o root -g alloy -m 0750 "${TLS_DIR}"
  [[ -e "${file_path}" ]] || install -o root -g alloy -m 0640 /dev/null "${file_path}"
  while true; do
    printf '\n'
    step "配置${content_name}"
    info "填写内容：${content_description}"
    warn "不要填写：${content_warning}"
    if [[ "${content_type}" == "certificate" ]]; then
      info "PEM 开头：-----BEGIN CERTIFICATE-----"
    else
      info "PEM 开头：-----BEGIN PRIVATE KEY-----（也支持 RSA/EC 私钥）"
    fi
    info "受管文件：${file_path}"
    info "以后可手动编辑：sudo ${TEXT_EDITOR##*/} ${file_path}"
    printf '%s按回车打开 %s，输入 q 取消本次配置：%s' "${BLUE}" "${TEXT_EDITOR##*/}" "${RESET}" >&2
    IFS= read -r answer <"${INTERACTIVE_DEVICE}" || die "读取输入失败"
    case "${answer}" in
      "") ;;
      q|Q) RETURN_TO_MAIN=1; return 0 ;;
      *) warn "请直接按回车打开编辑器，或输入 q 返回"; continue ;;
    esac
    if ! open_text_editor "${file_path}"; then
      warn "编辑器异常退出，请重试"
      continue
    fi
    if validate_pem_file "${file_path}" "${content_type}"; then
      chown root:alloy "${file_path}"
      chmod 0640 "${file_path}"
      result "${content_name}校验通过"
      return 0
    fi
    warn "${content_name}为空、格式无效或私钥已加密，请重新编辑"
  done
}

certificate_matches_private_key() {
  local certificate_file="$1"
  local private_key_file="$2"
  local certificate_public_key private_public_key
  certificate_public_key="$(openssl x509 -in "${certificate_file}" -pubkey -noout 2>/dev/null | openssl pkey -pubin -outform DER 2>/dev/null | sha256sum | awk '{print $1}')" || return 1
  private_public_key="$(openssl pkey -in "${private_key_file}" -passin pass: -pubout -outform DER 2>/dev/null | sha256sum | awk '{print $1}')" || return 1
  [[ -n "${certificate_public_key}" && "${certificate_public_key}" == "${private_public_key}" ]]
}

validate_certificate_bundle() {
  local label="$1"
  local ca_file="$2"
  local cert_file="$3"
  local key_file="$4"
  validate_pem_file "${ca_file}" certificate || die "${label} CA 证书无效：${ca_file}"
  validate_pem_file "${cert_file}" certificate || die "${label} 客户端证书无效：${cert_file}"
  validate_pem_file "${key_file}" private_key || die "${label} 客户端私钥无效或已加密：${key_file}"
  certificate_matches_private_key "${cert_file}" "${key_file}" || die "${label} 客户端证书与私钥不匹配"
}

install_tls_file() {
  local source="$1"
  local destination="$2"
  [[ -r "${source}" ]] || die "无法读取证书文件：${source}"
  if [[ "$(readlink -f "${source}")" != "$(readlink -m "${destination}")" ]]; then
    install -o root -g alloy -m 0640 "${source}" "${destination}"
  else
    chown root:alloy "${destination}"
    chmod 0640 "${destination}"
  fi
}

configure_backend_certificates() {
  local backend="$1"
  local label="$2"
  local ca_variable="${backend}_MTLS_CA_FILE"
  local cert_variable="${backend}_MTLS_CERT_FILE"
  local key_variable="${backend}_MTLS_KEY_FILE"
  local ca_source="${!ca_variable:-}"
  local cert_source="${!cert_variable:-}"
  local key_source="${!key_variable:-}"
  local prefix="${backend,,}"
  local ca_destination="${TLS_DIR}/${prefix}-ca.crt"
  local cert_destination="${TLS_DIR}/${prefix}-client.crt"
  local key_destination="${TLS_DIR}/${prefix}-client.key"
  local existing_valid=0
  local retain_existing=0

  install -d -o root -g alloy -m 0750 "${TLS_DIR}"
  if [[ -n "${ca_source}${cert_source}${key_source}" ]]; then
    [[ -n "${ca_source}" && -n "${cert_source}" && -n "${key_source}" ]] || die "${label} mTLS 必须同时提供 CA、客户端证书和客户端私钥"
    validate_certificate_bundle "${label}" "${ca_source}" "${cert_source}" "${key_source}"
    install_tls_file "${ca_source}" "${ca_destination}"
    install_tls_file "${cert_source}" "${cert_destination}"
    install_tls_file "${key_source}" "${key_destination}"
    result "${label} mTLS 证书已导入受管目录"
  else
    if validate_pem_file "${ca_destination}" certificate && \
       validate_pem_file "${cert_destination}" certificate && \
       validate_pem_file "${key_destination}" private_key && \
       certificate_matches_private_key "${cert_destination}" "${key_destination}"; then
      existing_valid=1
    fi
    if [[ "${existing_valid}" == "1" ]]; then
      if ! detect_interactive_device || ask_yes_no_default "${label} 是否继续使用现有 mTLS 证书" yes; then
        retain_existing=1
      fi
    fi
    if [[ "${retain_existing}" == "1" ]]; then
      result "保留现有 ${label} mTLS 证书"
    else
      detect_interactive_device || die "无人值守配置缺少 ${label} CA、客户端证书和客户端私钥变量"
      select_text_editor
      edit_pem_file "${label} 服务端 CA 证书" "${ca_destination}" certificate \
        "签发中心端 ${label} 服务器证书的 CA 证书" "服务器证书、客户端证书或私钥"
      [[ "${RETURN_TO_MAIN}" == "0" ]] || return 0
      edit_pem_file "${label} Alloy 客户端证书" "${cert_destination}" certificate \
        "由 ${label} 信任的 CA 为本机 Alloy 签发的客户端证书" "中心端服务器证书、CA 私钥或其他主机证书"
      [[ "${RETURN_TO_MAIN}" == "0" ]] || return 0
      edit_pem_file "${label} Alloy 客户端私钥" "${key_destination}" private_key \
        "与上一项 Alloy 客户端证书匹配的未加密私钥" "CA 私钥、服务器私钥或带密码私钥"
      [[ "${RETURN_TO_MAIN}" == "0" ]] || return 0
      validate_certificate_bundle "${label}" "${ca_destination}" "${cert_destination}" "${key_destination}"
      result "${label} mTLS 证书链和密钥匹配校验通过"
    fi
  fi

  printf -v "${ca_variable}" '%s' "${ca_destination}"
  printf -v "${cert_variable}" '%s' "${cert_destination}"
  printf -v "${key_variable}" '%s' "${key_destination}"
}

normalize_mtls_mode() {
  local variable_name="$1"
  local value="${!variable_name:-}"
  case "${value,,}" in
    1|true|yes|y) printf -v "${variable_name}" '%s' 1 ;;
    0|false|no|n) printf -v "${variable_name}" '%s' 0 ;;
    *) die "${variable_name} 只支持 1/0、true/false、yes/no 或 y/n" ;;
  esac
}

choose_backend_mtls_mode() {
  local backend="$1"
  local label="$2"
  local enabled_variable="${backend}_MTLS_ENABLED"
  local preset_variable="${backend}_MTLS_MODE_PRESET"
  local current="${!enabled_variable:-1}"

  normalize_mtls_mode "${enabled_variable}"
  [[ "${!preset_variable}" == "0" ]] || return 0
  detect_interactive_device || return 0

  while true; do
    current="${!enabled_variable}"
    if ask_yes_no_default "${label} 是否启用 mTLS（推荐）" "$([[ "${current}" == "1" ]] && printf yes || printf no)"; then
      printf -v "${enabled_variable}" '%s' 1
      result "${label} 将使用 HTTPS + mTLS"
      return 0
    fi
    printf '\n'
    warn "${label} 将使用 HTTP 明文传输，数据和请求内容可能被网络中的其他设备读取或篡改"
    warn "请通过防火墙限制中心端口只允许可信探针 IP 访问"
    if ask_yes_no_default "确认接受风险并让 ${label} 使用 HTTP" no; then
      printf -v "${enabled_variable}" '%s' 0
      result "已确认：${label} 将使用 HTTP（未加密）"
      return 0
    fi
    info "已取消 HTTP 模式，请重新选择"
    printf -v "${enabled_variable}" '%s' 1
  done
}

collect_connection_settings() {
  local force_prompt="$1"
  local prometheus_env_configured="${PROMETHEUS_MTLS_CA_FILE}${PROMETHEUS_MTLS_CERT_FILE}${PROMETHEUS_MTLS_KEY_FILE}${PROMETHEUS_TLS_SERVER_NAME}"
  local loki_env_configured="${LOKI_MTLS_CA_FILE}${LOKI_MTLS_CERT_FILE}${LOKI_MTLS_KEY_FILE}${LOKI_TLS_SERVER_NAME}"
  local prometheus_scheme="https"
  local loki_scheme="https"

  printf '\n'
  step "配置数据接收中心"
  info "Prometheus 接收端必须已单独开启远程写入接收；mTLS 为推荐项但不再强制"
  if [[ "${PROMETHEUS_MTLS_MODE_PRESET}" == "0" && -n "${prometheus_env_configured}" ]]; then
    PROMETHEUS_MTLS_ENABLED=1
  fi
  if [[ "${LOKI_MTLS_MODE_PRESET}" == "0" && -n "${loki_env_configured}" ]]; then
    LOKI_MTLS_ENABLED=1
  fi
  choose_backend_mtls_mode PROMETHEUS Prometheus
  choose_backend_mtls_mode LOKI Loki

  [[ "${PROMETHEUS_MTLS_ENABLED}" == "1" ]] || prometheus_scheme="http"
  [[ "${LOKI_MTLS_ENABLED}" == "1" ]] || loki_scheme="http"
  prompt_backend_url PROMETHEUS_URL Prometheus "${prometheus_scheme}" "$([[ "${PROMETHEUS_URL_PRESET}" == "1" ]] && printf 0 || printf '%s' "${force_prompt}")"
  prompt_backend_url LOKI_URL Loki "${loki_scheme}" "$([[ "${LOKI_URL_PRESET}" == "1" ]] && printf 0 || printf '%s' "${force_prompt}")"

  valid_backend_url "${PROMETHEUS_URL}" || die "PROMETHEUS_URL 格式无效"
  valid_backend_url "${LOKI_URL}" || die "LOKI_URL 格式无效"
  [[ "${PROMETHEUS_URL}" == "${prometheus_scheme}://"* ]] || die "Prometheus 地址协议与 mTLS 选择不一致"
  [[ "${LOKI_URL}" == "${loki_scheme}://"* ]] || die "Loki 地址协议与 mTLS 选择不一致"
}

prepare_mtls_configuration() {
  local prometheus_force_prompt=0
  local loki_force_prompt=0
  if detect_interactive_device; then
    [[ "${PROMETHEUS_URL_PRESET}" == "1" ]] || prometheus_force_prompt=1
    [[ "${LOKI_URL_PRESET}" == "1" ]] || loki_force_prompt=1
  fi

  if [[ "${PROMETHEUS_MTLS_ENABLED}" == "1" ]]; then
    printf '\n'
    step "配置 Prometheus mTLS"
    configure_backend_certificates PROMETHEUS Prometheus
    [[ "${RETURN_TO_MAIN}" == "0" ]] || return 0
    prompt_server_name PROMETHEUS_TLS_SERVER_NAME Prometheus "${PROMETHEUS_URL}" "${prometheus_force_prompt}"
    [[ "${PROMETHEUS_TLS_SERVER_NAME}" =~ ^[A-Za-z0-9._-]+$ ]] || die "Prometheus TLS server_name 格式无效"
  else
    rm -f -- "${TLS_DIR}/prometheus-ca.crt" "${TLS_DIR}/prometheus-client.crt" "${TLS_DIR}/prometheus-client.key"
    PROMETHEUS_TLS_SERVER_NAME=""
    info "Prometheus mTLS 未启用，旧的 Prometheus 客户端证书（如有）已即时删除"
  fi

  if [[ "${LOKI_MTLS_ENABLED}" == "1" ]]; then
    printf '\n'
    step "配置 Loki mTLS"
    configure_backend_certificates LOKI Loki
    [[ "${RETURN_TO_MAIN}" == "0" ]] || return 0
    prompt_server_name LOKI_TLS_SERVER_NAME Loki "${LOKI_URL}" "${loki_force_prompt}"
    [[ "${LOKI_TLS_SERVER_NAME}" =~ ^[A-Za-z0-9._-]+$ ]] || die "Loki TLS server_name 格式无效"
  else
    rm -f -- "${TLS_DIR}/loki-ca.crt" "${TLS_DIR}/loki-client.crt" "${TLS_DIR}/loki-client.key"
    LOKI_TLS_SERVER_NAME=""
    info "Loki mTLS 未启用，旧的 Loki 客户端证书（如有）已即时删除"
  fi
}

write_config() {
  local temp_file prometheus_tls_config="" loki_tls_config=""
  temp_file="${WORK_DIR}/config.alloy.new"
  if [[ "${PROMETHEUS_MTLS_ENABLED}" == "1" ]]; then
    prometheus_tls_config="$(cat <<EOF
    tls_config {
      ca_file     = "${TLS_DIR}/prometheus-ca.crt"
      cert_file   = "${TLS_DIR}/prometheus-client.crt"
      key_file    = "${TLS_DIR}/prometheus-client.key"
      server_name = "${PROMETHEUS_TLS_SERVER_NAME}"
    }
EOF
)"
  fi
  if [[ "${LOKI_MTLS_ENABLED}" == "1" ]]; then
    loki_tls_config="$(cat <<EOF
    tls_config {
      ca_file     = "${TLS_DIR}/loki-ca.crt"
      cert_file   = "${TLS_DIR}/loki-client.crt"
      key_file    = "${TLS_DIR}/loki-client.key"
      server_name = "${LOKI_TLS_SERVER_NAME}"
    }
EOF
)"
  fi

  cat >"${temp_file}" <<EOF
logging {
  level = "info"
}

prometheus.exporter.unix "host" {
}

prometheus.scrape "host" {
  targets         = prometheus.exporter.unix.host.targets
  forward_to      = [prometheus.remote_write.center.receiver]
  scrape_interval = "15s"
}

prometheus.remote_write "center" {
  endpoint {
    url = "${PROMETHEUS_URL}/api/v1/write"
${prometheus_tls_config}
  }
}

loki.source.journal "system" {
  forward_to = [loki.write.center.receiver]
  labels = {
    source = "systemd-journal",
  }
}

loki.write "center" {
  endpoint {
    url = "${LOKI_URL}/loki/api/v1/push"
${loki_tls_config}
  }
  external_labels = {
    host = constants.hostname,
  }
}
EOF

  printf '\n'
  step "校验并写入 Alloy 配置"
  alloy validate "${temp_file}" || die "Alloy 配置校验失败，现有配置不会被替换"
  install -d -o root -g alloy -m 0750 "${CONFIG_DIR}" "${DATA_DIR}"
  install -o root -g alloy -m 0640 "${temp_file}" "${CONFIG_FILE}"
  chown alloy:alloy "${DATA_DIR}"
  result "配置校验通过并写入 ${CONFIG_FILE}"
}

start_service() {
  printf '\n'
  step "启动 Alloy 服务"
  getent group systemd-journal >/dev/null 2>&1 && usermod -aG systemd-journal alloy
  getent group adm >/dev/null 2>&1 && usermod -aG adm alloy
  systemctl daemon-reload
  systemctl enable alloy.service >/dev/null
  systemctl restart alloy.service
  if ! systemctl is-active --quiet alloy.service; then
    systemctl --no-pager --full status alloy.service || true
    die "alloy.service 启动失败"
  fi
  result "Alloy 已启动并设置为开机自启"
  info "查看实时日志：journalctl -u alloy -f"
}

configure_probe() {
  local mode="$1"
  local force_prompt=0
  if [[ "${mode}" == "reconfigure" ]] && detect_interactive_device; then
    force_prompt=1
  fi
  require_commands awk cat chmod chown cp getent install mktemp openssl readlink rm sha256sum systemctl usermod
  [[ "${mode}" != "install" ]] || require_commands curl
  load_existing_settings
  begin_config_transaction
  collect_connection_settings "${force_prompt}"

  if [[ "${mode}" == "install" ]]; then
    printf '\n'
    step "安装 Grafana Alloy 软件包"
    info "软件包管理器将显示仓库刷新和下载进度"
    install_package
    result "Grafana Alloy 软件包安装完成"
  fi

  require_commands alloy
  getent group alloy >/dev/null 2>&1 || die "Alloy 软件包未创建 alloy 系统组"
  prepare_mtls_configuration
  if [[ "${RETURN_TO_MAIN}" == "1" ]]; then
    restore_config_transaction
    return 0
  fi
  write_config
  start_service
  commit_config_transaction
  print_completion_card "$([[ "${mode}" == "install" ]] && printf '安装完成' || printf '重新配置完成')"
}

update_probe() {
  require_commands alloy curl
  printf '\n'
  step "校验现有配置"
  [[ -r "${CONFIG_FILE}" ]] || die "缺少 ${CONFIG_FILE}，请先选择仅重新配置"
  alloy validate "${CONFIG_FILE}" || die "现有配置校验失败，请先修复或重新配置"
  result "现有配置校验通过"

  printf '\n'
  step "更新 Grafana Alloy 软件包"
  info "现有配置、mTLS 证书和数据不会被修改"
  install_package
  systemctl restart alloy.service
  systemctl enable alloy.service >/dev/null
  systemctl is-active --quiet alloy.service || die "更新后 alloy.service 启动失败，请查看 journalctl -u alloy"
  result "Alloy 软件包更新检查完成，服务运行正常"
  load_existing_settings
  print_completion_card "更新完成"
}

show_status() {
  local version active enabled prometheus_status loki_status
  version="$(installed_version)"
  active="$(systemctl is-active alloy.service 2>/dev/null || true)"
  enabled="$(systemctl is-enabled alloy.service 2>/dev/null || true)"
  load_existing_settings
  prometheus_status="未启用（HTTP，未加密）"
  loki_status="未启用（HTTP，未加密）"
  [[ "${PROMETHEUS_MTLS_ENABLED}" == "1" ]] && prometheus_status="已启用"
  [[ "${LOKI_MTLS_ENABLED}" == "1" ]] && loki_status="已启用"

  printf '\n%s%s%s\n' "${BOLD}${ORANGE}" "╭─ Grafana Alloy" "${RESET}"
  printf '%s│ %s安装版本：%s%s\n' "${ORANGE}" "${BLUE}" "${RESET}" "${version:-未安装}"
  printf '%s│ %s服务状态：%s%s；开机自启：%s\n' "${ORANGE}" "${BLUE}" "${RESET}" "${active:-未知}" "${enabled:-未知}"
  printf '%s│ %sPrometheus：%s%s\n' "${ORANGE}" "${BLUE}" "${RESET}" "${PROMETHEUS_URL:-未配置}"
  printf '%s│ %sPrometheus mTLS：%s%s\n' "${ORANGE}" "${BLUE}" "${RESET}" "${prometheus_status}"
  printf '%s│ %sLoki：%s%s\n' "${ORANGE}" "${BLUE}" "${RESET}" "${LOKI_URL:-未配置}"
  printf '%s│ %sLoki mTLS：%s%s\n' "${ORANGE}" "${BLUE}" "${RESET}" "${loki_status}"
  printf '%s│ %s配置文件：%s%s\n' "${ORANGE}" "${BLUE}" "${RESET}" "${CONFIG_FILE}"
  printf '%s│ %s证书目录：%s%s\n' "${ORANGE}" "${BLUE}" "${RESET}" "${TLS_DIR}"
  printf '%s│ %s数据目录：%s%s\n' "${ORANGE}" "${BLUE}" "${RESET}" "${DATA_DIR}"
  printf '%s%s%s\n' "${ORANGE}" "╰────────────────────────────────────────────────────" "${RESET}"
}

uninstall_probe() {
  local mode="${1:-ask}"
  local current_version=""
  current_version="$(installed_version)"
  print_banner "卸载与清理"
  step "检查安装状态"
  info "当前版本：${current_version:-未安装}"
  info "配置目录：${CONFIG_DIR}"
  info "数据目录：${DATA_DIR}"

  case "${mode}" in
    purge) PURGE_MODE=1 ;;
    keep) PURGE_MODE=0 ;;
    ask)
      printf '\n'
      step "选择卸载方式"
      choose_uninstall_mode
      [[ "${RETURN_TO_MAIN}" == "0" ]] || return 0
      ;;
  esac

  printf '\n'
  step "停止服务并卸载软件包"
  systemctl disable --now alloy.service >/dev/null 2>&1 || true
  if is_installed; then
    remove_package
  else
    info "未检测到 Alloy 软件包，跳过软件包删除"
  fi
  systemctl daemon-reload >/dev/null 2>&1 || true
  systemctl reset-failed alloy.service >/dev/null 2>&1 || true
  result "Alloy 软件包卸载步骤完成"

  if [[ "${PURGE_MODE}" == "1" ]]; then
    printf '\n'
    step "删除受管配置、mTLS 证书和运行数据"
    rm -rf -- "${CONFIG_DIR}" "${DATA_DIR}"
    result "已删除 ${CONFIG_DIR} 和 ${DATA_DIR}"
    print_uninstall_card "彻底清理完成" "${BLUE}已删除：${RESET}${CONFIG_DIR}、${DATA_DIR}"
  else
    info "配置和 mTLS 证书已保留：${CONFIG_DIR}"
    info "Alloy 运行数据已保留：${DATA_DIR}"
    print_uninstall_card "普通卸载完成" "${BLUE}已保留：${RESET}${CONFIG_DIR}、${DATA_DIR}"
  fi
}

cleanup() {
  if [[ "${TRANSACTION_ACTIVE}" == "1" ]]; then
    restore_config_transaction
  fi
  if [[ -n "${WORK_DIR}" && -d "${WORK_DIR}" ]]; then
    rm -rf -- "${WORK_DIR}"
    WORK_DIR=""
  fi
}

on_error() {
  local exit_code="$1"
  local line_number="$2"
  printf '%s[错误]%s 脚本在第 %s 行执行失败（退出码：%s）\n' "${RED}" "${RESET}" "${line_number}" "${exit_code}" >&2
  exit "${exit_code}"
}

main() {
  local requested_action="${1:-install}"
  case "${requested_action}" in
    help|-h|--help)
      [[ "$#" -eq 1 ]] || die "参数过多，请使用 --help 查看用法"
      usage
      return 0
      ;;
    install|--install|-i) SELECTED_ACTION="install" ;;
    update|--update) SELECTED_ACTION="update" ;;
    reconfigure|--reconfigure) SELECTED_ACTION="reconfigure" ;;
    status|--status) SELECTED_ACTION="status" ;;
    uninstall|--uninstall|-u) SELECTED_ACTION="uninstall" ;;
    purge|--purge) SELECTED_ACTION="purge" ;;
    *) die "未知操作：${requested_action}，请使用 --help 查看用法" ;;
  esac
  [[ "$#" -eq 1 || "$#" -eq 0 ]] || die "参数过多，请使用 --help 查看用法"

  print_banner "安装、配置、更新、卸载与清理"
  step "检查运行权限和基础依赖"
  require_root
  require_commands awk head rm sed systemctl
  result "root 权限和基础依赖检查通过"

  if [[ "${requested_action}" == "install" || "${requested_action}" == "--install" || "${requested_action}" == "-i" ]]; then
    if detect_interactive_device; then
      printf '\n'
      step "检查安装状态"
      if is_installed; then
        result "检测到 $(installed_version)"
        printf '\n'
        step "选择维护操作"
        choose_maintenance_action
      else
        result "未检测到 Grafana Alloy"
        printf '\n'
        step "选择操作"
        choose_install_action
      fi
    elif is_installed; then
      SELECTED_ACTION="update"
    fi
  fi

  case "${SELECTED_ACTION}" in
    install) configure_probe install ;;
    update)
      is_installed || die "Alloy 尚未安装，请先执行安装"
      update_probe
      ;;
    reconfigure)
      is_installed || die "Alloy 尚未安装，无法仅重新配置"
      configure_probe reconfigure
      ;;
    status) show_status ;;
    uninstall)
      if detect_interactive_device; then
        uninstall_probe ask
      else
        uninstall_probe keep
      fi
      ;;
    purge) uninstall_probe purge ;;
    quit) result "已退出，未修改系统" ;;
  esac
}

if [[ "${ALLOY_INSTALLER_SOURCE_ONLY:-0}" != "1" ]]; then
  trap 'on_error "$?" "$LINENO"' ERR
  trap cleanup EXIT
  init_colors
  main "$@"
fi
