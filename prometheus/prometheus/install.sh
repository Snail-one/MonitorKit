#!/usr/bin/env bash
# Install or upgrade Prometheus as a systemd service.
# Legacy compatibility script. New central-server deployments are managed by
# the Go program under cmd/snailmon.
#
# Optional environment variables:
#   PROMETHEUS_VERSION=latest
#   PROMETHEUS_LISTEN_ADDRESS=0.0.0.0:9090
#   DOWNLOAD_BASE_URL=https://github.com/prometheus/prometheus/releases/download

set -Eeuo pipefail

PROMETHEUS_VERSION="${PROMETHEUS_VERSION:-latest}"
PROMETHEUS_LISTEN_ADDRESS="${PROMETHEUS_LISTEN_ADDRESS:-0.0.0.0:9090}"
DOWNLOAD_BASE_URL="${DOWNLOAD_BASE_URL:-https://github.com/prometheus/prometheus/releases/download}"
PROMETHEUS_MTLS_MODE_PRESET=0
[[ -n "${PROMETHEUS_MTLS_ENABLED+x}" ]] && PROMETHEUS_MTLS_MODE_PRESET=1
PROMETHEUS_MTLS_ENABLED="${PROMETHEUS_MTLS_ENABLED:-1}"

readonly PROMETHEUS_USER="prometheus"
readonly PROMETHEUS_GROUP="prometheus"
readonly CONFIG_DIR="/etc/prometheus"
readonly DATA_DIR="/var/lib/prometheus"
readonly BIN_DIR="/usr/local/bin"
readonly TLS_DIR="${CONFIG_DIR}/tls"
readonly WEB_CONFIG_FILE="${CONFIG_DIR}/web.yml"
readonly SERVICE_FILE="/etc/systemd/system/prometheus.service"

RESET=""
BOLD=""
ORANGE=""
BLUE=""
GREEN=""
YELLOW=""
RED=""
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
  printf '%s%s%s\n' "${BOLD}${ORANGE}" "╭─ Prometheus" "${RESET}"
  printf '%s│ %s%s\n' "${ORANGE}" "$1" "${RESET}"
  printf '%s%s%s\n\n' "${ORANGE}" "╰──────────────────────────────────────────────" "${RESET}"
}

print_release_info() {
  printf '\n%s%s%s\n' "${BOLD}${ORANGE}" "╭─ Prometheus" "${RESET}"
  printf '%s│ %s%s%s\n' "${ORANGE}" "${BOLD}${ORANGE}" "发布信息" "${RESET}"
  printf '%s│ %s平台：%sLinux/%s\n' "${ORANGE}" "${BLUE}" "${RESET}" "$1"
  printf '%s│ %s当前版本：%s%s\n' "${ORANGE}" "${BLUE}" "${RESET}" "$2"
  printf '%s│ %s目标版本：%s%s%s%s\n' "${ORANGE}" "${BLUE}" "${RESET}" "${BOLD}" "$3" "${RESET}"
  printf '%s│ %s执行操作：%s%s%s%s\n' "${ORANGE}" "${BLUE}" "${RESET}" "${BOLD}" "$4" "${RESET}"
  printf '%s│ %s监听地址：%s%s\n' "${ORANGE}" "${BLUE}" "${RESET}" "${PROMETHEUS_LISTEN_ADDRESS}"
  printf '%s│ %smTLS：%s%s\n' "${ORANGE}" "${BLUE}" "${RESET}" "${MTLS_STATUS}"
  printf '%s%s%s\n' "${ORANGE}" "╰──────────────────────────────────────────────" "${RESET}"
}

print_completion_card() {
  printf '\n%s%s%s\n' "${BOLD}${ORANGE}" "╭─ Prometheus" "${RESET}"
  printf '%s│ %s%s完成%s\n' "${ORANGE}" "${BOLD}${GREEN}" "$1" "${RESET}"
  printf '%s│ %s版本：%s%s%s%s\n' "${ORANGE}" "${BLUE}" "${RESET}" "${BOLD}" "$2" "${RESET}"
  printf '%s│ %s服务：%s%s\n' "${ORANGE}" "${BLUE}" "${RESET}" "prometheus.service"
  printf '%s│ %s访问：%s%s\n' "${ORANGE}" "${BLUE}" "${RESET}" "${WEB_SCHEME}://${PROMETHEUS_LISTEN_ADDRESS}/"
  printf '%s%s%s\n' "${ORANGE}" "╰──────────────────────────────────────────────" "${RESET}"
}

print_result_card() {
  printf '\n%s%s%s\n' "${BOLD}${ORANGE}" "╭─ Prometheus" "${RESET}"
  printf '%s│ %s%s%s\n' "${ORANGE}" "${BOLD}${GREEN}" "$1" "${RESET}"
  printf '%s%s%s\n' "${ORANGE}" "╰──────────────────────────────────────────────" "${RESET}"
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

cleanup() {
  if [[ -n "${WORK_DIR}" ]]; then
    rm -rf -- "${WORK_DIR}"
    WORK_DIR=""
  fi
}

usage() {
  cat <<'EOF'
Prometheus 一键安装、更新与卸载脚本。

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
  PROMETHEUS_VERSION=latest
  PROMETHEUS_LISTEN_ADDRESS=0.0.0.0:9090
  DOWNLOAD_BASE_URL=https://github.com/prometheus/prometheus/releases/download

mTLS 环境变量：
  PROMETHEUS_MTLS_ENABLED=1

mTLS 模式会使用 vim、nano 或 vi 编辑并校验证书文件。
所有交互输入错误时会提示重新输入；输入 q 可返回主菜单。
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
    ppc64le) printf 'ppc64le\n' ;;
    s390x) printf 's390x\n' ;;
    *) die "不支持的处理器架构：$(uname -m)" ;;
  esac
}

validate_version() {
  [[ "$1" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]] || die "无效的 PROMETHEUS_VERSION：$1"
}

validate_listen_address() {
  local value="$1"
  local port
  if [[ "${value}" =~ ^[A-Za-z0-9._-]*:([0-9]{1,5})$ ]] || \
     [[ "${value}" =~ ^\[[0-9A-Fa-f:.]+\]:([0-9]{1,5})$ ]]; then
    port="${BASH_REMATCH[1]}"
  else
    die "无效的 PROMETHEUS_LISTEN_ADDRESS：${value}"
  fi
  (( port >= 1 && port <= 65535 )) || die "监听端口必须在 1 到 65535 之间"
}

set_interactive_device() {
  if [[ -c /dev/tty ]] && { : </dev/tty; } 2>/dev/null; then
    INTERACTIVE_DEVICE="/dev/tty"
  elif [[ -t 0 ]]; then
    INTERACTIVE_DEVICE="/dev/stdin"
  else
    die "当前环境没有可用的交互式终端"
  fi
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
    info "文件路径：${file_path}"
    info "手动编辑命令：sudo ${TEXT_EDITOR##*/} ${file_path}"
    printf '%s按回车打开 %s，输入 q 返回主菜单：%s' "${BLUE}" "${TEXT_EDITOR##*/}" "${RESET}" >&2
    IFS= read -e -r answer <"${INTERACTIVE_DEVICE}" || die "读取用户输入失败"
    case "${answer}" in
      "") ;;
      q|Q)
        RETURN_TO_MAIN=1
        result "正在返回主菜单"
        return 0
        ;;
      *) warn "输入无效：请直接按回车打开编辑器，或输入 q 返回主菜单"; continue ;;
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
  [[ "${PROMETHEUS_MTLS_MODE_PRESET}" == "0" ]] || return 0
  set_interactive_device

  printf '%s  1.%s 安装/更新：mTLS（HTTPS，强制验证客户端证书，默认）\n' "${ORANGE}" "${RESET}" >&2
  printf '%s  2.%s 安装/更新：普通 HTTP\n' "${GREEN}" "${RESET}" >&2
  printf '%s  3.%s 标准卸载（保留配置、证书、数据和账号）\n' "${YELLOW}" "${RESET}" >&2
  printf '%s  4.%s 彻底清理（删除配置、证书、数据和账号）\n' "${RED}" "${RESET}" >&2
  printf '%s  q.%s 返回主菜单\n' "${BLUE}" "${RESET}" >&2
  while true; do
    printf '%s请选择操作 [1-4]（默认 1：mTLS）：%s' "${BLUE}" "${RESET}" >&2
    local choice=""
    IFS= read -e -r choice <"${INTERACTIVE_DEVICE}" || die "读取安装方式失败"
    case "${choice}" in
      ""|1)
        PROMETHEUS_MTLS_ENABLED=1
        result "已选择 mTLS 模式"
        return
        ;;
      2)
        PROMETHEUS_MTLS_ENABLED=0
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
        warn "已选择彻底清理，历史监控数据将被永久删除"
        return
        ;;
      q|Q)
        RETURN_TO_MAIN=1
        result "正在返回主菜单"
        return 0
        ;;
      *) warn "输入无效：请输入 1、2、3、4 或 q" ;;
    esac
  done
}

choose_uninstall_mode() {
  set_interactive_device
  printf '%s  1.%s 标准卸载（保留配置、证书、数据和账号）\n' "${GREEN}" "${RESET}" >&2
  printf '%s  2.%s 彻底清理（删除全部配置、证书、数据和账号）\n' "${RED}" "${RESET}" >&2
  printf '%s  q.%s 返回主菜单\n' "${BLUE}" "${RESET}" >&2
  while true; do
    printf '%s请选择卸载方式 [1-2]（默认 1）：%s' "${BLUE}" "${RESET}" >&2
    local choice=""
    IFS= read -e -r choice <"${INTERACTIVE_DEVICE}" || die "读取卸载方式失败"
    case "${choice}" in
      ""|1)
        PURGE_MODE=0
        result "已选择标准卸载"
        return
        ;;
      2)
        PURGE_MODE=1
        warn "已选择彻底清理，历史监控数据将被永久删除"
        return
        ;;
      q|Q)
        RETURN_TO_MAIN=1
        result "正在返回主菜单"
        return 0
        ;;
      *) warn "输入无效：请输入 1、2 或 q" ;;
    esac
  done
}

choose_maintenance_action() {
  set_interactive_device
  printf '%s  1.%s 检查最新版本并更新\n' "${ORANGE}" "${RESET}" >&2
  printf '%s  2.%s 仅重新配置（不访问 API、不下载安装包，默认）\n' "${GREEN}" "${RESET}" >&2
  printf '%s  3.%s 添加 node_exporter 探针\n' "${BLUE}" "${RESET}" >&2
  printf '%s  4.%s 标准卸载（保留配置、证书、数据和账号）\n' "${YELLOW}" "${RESET}" >&2
  printf '%s  5.%s 彻底清理（删除配置、证书、数据和账号）\n' "${RED}" "${RESET}" >&2
  printf '%s  q.%s 返回主菜单\n' "${BLUE}" "${RESET}" >&2
  while true; do
    printf '%s请选择维护操作 [1-5]（默认 2）：%s' "${BLUE}" "${RESET}" >&2
    local choice=""
    IFS= read -e -r choice <"${INTERACTIVE_DEVICE}" || die "读取维护操作失败"
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
        SELECTED_ACTION="add_probe"
        result "已选择添加 node_exporter 探针"
        return
        ;;
      4)
        SELECTED_ACTION="uninstall"
        result "已选择标准卸载"
        return
        ;;
      5)
        SELECTED_ACTION="purge"
        warn "已选择彻底清理，历史监控数据将被永久删除"
        return
        ;;
      q|Q)
        RETURN_TO_MAIN=1
        result "正在返回主菜单"
        return 0
        ;;
      *) warn "输入无效：请输入 1、2、3、4、5 或 q" ;;
    esac
  done
}

choose_reconfigure_mode() {
  [[ "${PROMETHEUS_MTLS_MODE_PRESET}" == "0" ]] || return 0
  set_interactive_device
  printf '%s  1.%s mTLS（HTTPS，强制验证客户端证书，默认）\n' "${ORANGE}" "${RESET}" >&2
  printf '%s  2.%s 普通 HTTP\n' "${GREEN}" "${RESET}" >&2
  printf '%s  q.%s 返回主菜单\n' "${BLUE}" "${RESET}" >&2
  while true; do
    printf '%s请选择重新配置方式 [1-2]（默认 1：mTLS）：%s' "${BLUE}" "${RESET}" >&2
    local choice=""
    IFS= read -e -r choice <"${INTERACTIVE_DEVICE}" || die "读取重新配置方式失败"
    case "${choice}" in
      ""|1)
        PROMETHEUS_MTLS_ENABLED=1
        result "将重新配置为 mTLS 模式"
        return
        ;;
      2)
        PROMETHEUS_MTLS_ENABLED=0
        result "将重新配置为普通 HTTP 模式"
        return
        ;;
      q|Q)
        RETURN_TO_MAIN=1
        result "正在返回主菜单"
        return 0
        ;;
      *) warn "输入无效：请输入 1、2 或 q" ;;
    esac
  done
}

load_existing_web_mode() {
  if [[ -f "${SERVICE_FILE}" ]] && grep -q -- '--web.config.file=' "${SERVICE_FILE}"; then
    PROMETHEUS_MTLS_ENABLED=1
    WEB_SCHEME="https"
    MTLS_STATUS="保持现有 mTLS 配置"
    info "更新将保留现有服务模式：mTLS"
  else
    PROMETHEUS_MTLS_ENABLED=0
    WEB_SCHEME="http"
    MTLS_STATUS="保持现有 HTTP 配置"
    info "更新将保留现有服务模式：普通 HTTP"
  fi
}

is_valid_target_address() {
  local value="$1"
  local port=""
  if [[ "${value}" =~ ^[A-Za-z0-9._-]+:([0-9]{1,5})$ ]] || \
     [[ "${value}" =~ ^\[[0-9A-Fa-f:.]+\]:([0-9]{1,5})$ ]]; then
    port="${BASH_REMATCH[1]}"
    (( port >= 1 && port <= 65535 ))
    return
  fi
  return 1
}

mtls_client_files_valid() {
  local ca_file="$1"
  local cert_file="$2"
  local key_file="$3"
  validate_pem_file "${ca_file}" certificate &&
    validate_pem_file "${cert_file}" certificate &&
    validate_pem_file "${key_file}" private_key &&
    certificate_matches_private_key "${cert_file}" "${key_file}"
}

append_target_to_reusable_job() {
  local source_file="$1"
  local destination_file="$2"
  local target="$3"
  local ca_file="$4"
  local cert_file="$5"
  local key_file="$6"

  awk -v target="${target}" -v ca_file="${ca_file}" -v cert_file="${cert_file}" -v key_file="${key_file}" '
    function flush_job(    count, lines, index_number, line, inserted) {
      if (job_buffer == "") return
      compatible = index(job_buffer, "scheme: https") &&
                   index(job_buffer, "static_configs:") &&
                   index(job_buffer, "ca_file:") && index(job_buffer, ca_file) &&
                   index(job_buffer, "cert_file:") && index(job_buffer, cert_file) &&
                   index(job_buffer, "key_file:") && index(job_buffer, key_file) &&
                   !index(job_buffer, "server_name:")
      if (compatible && !reused) {
        count = split(job_buffer, lines, "\n")
        inserted = 0
        for (index_number = 1; index_number <= count; index_number++) {
          line = lines[index_number]
          if (!inserted && line ~ /^    tls_config:[[:space:]]*$/) {
            print "      - targets: [\"" target "\"]"
            inserted = 1
            reused = 1
          }
          if (line != "") print line
        }
      } else {
        printf "%s", job_buffer
      }
      job_buffer = ""
    }
    BEGIN { in_job = 0; reused = 0 }
    /^  - job_name:/ {
      flush_job()
      in_job = 1
    }
    {
      if (in_job) job_buffer = job_buffer $0 ORS
      else print
    }
    END {
      flush_job()
      if (!reused) exit 42
    }
  ' "${source_file}" >"${destination_file}"
}

add_node_exporter_target() {
  local config_file="${CONFIG_DIR}/prometheus.yml"
  local target=""
  local target_input=""
  local job_name=""
  local default_job_name=""
  local scrape_scheme="https"
  local scrape_choice=""
  local client_dir="/etc/prometheus/client"
  local ca_file="/etc/prometheus/client/node-server-ca.crt"
  local cert_file="/etc/prometheus/client/prometheus-client.crt"
  local key_file="/etc/prometheus/client/prometheus-client.key"
  local certificate_choice=""
  local reuse_certificates=0
  local reused_job=0
  local temp_dir=""
  local fragment_file=""
  local candidate_file=""
  local backup_file=""

  print_banner "添加 node_exporter 探针"
  require_commands awk cat chmod chown cp grep install mktemp openssl sed sha256sum systemctl touch
  set_interactive_device
  [[ -x "${BIN_DIR}/promtool" ]] || die "缺少 promtool，无法安全修改 Prometheus 配置"
  [[ -f "${config_file}" ]] || die "Prometheus 配置不存在：${config_file}"

  while true; do
    printf '%s探针地址（例如 node01.example.com:9100，q 返回主菜单）：%s' "${BLUE}" "${RESET}" >&2
    IFS= read -e -r target_input <"${INTERACTIVE_DEVICE}" || die "读取探针地址失败"
    case "${target_input}" in
      q|Q)
        RETURN_TO_MAIN=1
        result "正在返回主菜单"
        return 0
        ;;
    esac
    if is_valid_target_address "${target_input}"; then
      target="${target_input}"
      break
    fi
    warn "输入无效：请使用 域名:端口、IP:端口 或 [IPv6]:端口，也可以输入 q 返回主菜单"
  done

  printf '%s  1.%s mTLS/HTTPS（默认）\n' "${ORANGE}" "${RESET}" >&2
  printf '%s  2.%s 普通 HTTP\n' "${GREEN}" "${RESET}" >&2
  printf '%s  q.%s 返回主菜单\n' "${BLUE}" "${RESET}" >&2
  while true; do
    printf '%s请选择探针连接方式 [1-2]（默认 1）：%s' "${BLUE}" "${RESET}" >&2
    IFS= read -e -r scrape_choice <"${INTERACTIVE_DEVICE}" || die "读取探针连接方式失败"
    case "${scrape_choice}" in
      ""|1) scrape_scheme="https"; break ;;
      2) scrape_scheme="http"; break ;;
      q|Q)
        RETURN_TO_MAIN=1
        result "正在返回主菜单"
        return 0
        ;;
      *) warn "输入无效：请输入 1、2 或 q" ;;
    esac
  done

  if grep -Fq -- "${target}" "${config_file}"; then
    warn "配置中已经存在该探针地址：${target}"
    RETURN_TO_MAIN=1
    result "未修改配置，正在返回主菜单"
    return 0
  fi

  if grep -Eq '^scrape_configs:' "${config_file}" && \
     ! grep -Eq '^scrape_configs:[[:space:]]*$' "${config_file}"; then
    warn "现有 scrape_configs 使用了行内写法，向导无法安全插入探针"
    info "请先将其改为标准多行 YAML 后重试"
    RETURN_TO_MAIN=1
    result "原配置未修改，正在返回主菜单"
    return 0
  fi

  if [[ "${scrape_scheme}" == "https" ]]; then
    printf '\n'
    step "配置 Prometheus 抓取探针使用的 mTLS 证书"
    if mtls_client_files_valid "${ca_file}" "${cert_file}" "${key_file}"; then
      result "检测到有效的现有 mTLS 客户端证书组"
      info "根 CA：${ca_file}"
      info "客户端证书：${cert_file}"
      info "客户端私钥：${key_file}"
      printf '%s  1.%s 复用现有证书和兼容的 node_exporter 任务（默认）\n' "${GREEN}" "${RESET}" >&2
      printf '%s  2.%s 使用另一套 CA 和客户端证书（创建独立任务）\n' "${ORANGE}" "${RESET}" >&2
      printf '%s  q.%s 返回主菜单\n' "${BLUE}" "${RESET}" >&2
      while true; do
        printf '%s请选择证书使用方式 [1-2]（默认 1）：%s' "${BLUE}" "${RESET}" >&2
        IFS= read -e -r certificate_choice <"${INTERACTIVE_DEVICE}" || die "读取证书使用方式失败"
        case "${certificate_choice}" in
          ""|1)
            reuse_certificates=1
            result "将复用现有 mTLS 客户端证书组"
            break
            ;;
          2)
            client_dir="/etc/prometheus/client/$(sed 's/[^A-Za-z0-9_.-]/_/g' <<<"${target%:*}")"
            ca_file="${client_dir}/node-server-ca.crt"
            cert_file="${client_dir}/prometheus-client.crt"
            key_file="${client_dir}/prometheus-client.key"
            result "将使用独立证书目录：${client_dir}"
            break
            ;;
          q|Q)
            RETURN_TO_MAIN=1
            result "正在返回主菜单"
            return 0
            ;;
          *) warn "输入无效：请输入 1、2 或 q" ;;
        esac
      done
    else
      warn "未检测到完整有效的现有证书组，需要先配置证书"
    fi

    if [[ "${reuse_certificates}" == "0" ]]; then
      select_text_editor
      install -d -o root -g "${PROMETHEUS_GROUP}" -m 0750 "${client_dir}"
      touch "${ca_file}" "${cert_file}" "${key_file}"
      chown root:"${PROMETHEUS_GROUP}" "${ca_file}" "${cert_file}" "${key_file}"
      chmod 0640 "${ca_file}" "${cert_file}" "${key_file}"

      edit_pem_file "node_exporter 服务端根 CA 证书（信任锚）" "${ca_file}" certificate \
        "签发 node_exporter 服务端证书的根 CA 公共证书；多个 CA 可按 PEM 格式组成证书包" \
        "node_exporter 服务端证书、客户端证书或根 CA 私钥"
      [[ "${RETURN_TO_MAIN}" == "0" ]] || return 0

      while true; do
        edit_pem_file "Prometheus 抓取探针客户端证书" "${cert_file}" certificate \
          "Prometheus 连接 node_exporter 时出示的 clientAuth 客户端证书" \
          "Prometheus 服务端证书、node_exporter 服务端证书或 CA 私钥"
        [[ "${RETURN_TO_MAIN}" == "0" ]] || return 0
        edit_pem_file "Prometheus 抓取探针客户端私钥" "${key_file}" private_key \
          "与上一步 Prometheus 客户端证书匹配的未加密私钥" \
          "服务端私钥、证书或加密私钥"
        [[ "${RETURN_TO_MAIN}" == "0" ]] || return 0
        if certificate_matches_private_key "${cert_file}" "${key_file}"; then
          result "Prometheus 客户端证书和私钥匹配"
          break
        fi
        warn "Prometheus 客户端证书和私钥不匹配，请重新编辑"
      done
    fi
  fi

  temp_dir="$(mktemp -d -t prometheus-add-target.XXXXXXXX)"
  WORK_DIR="${temp_dir}"
  fragment_file="${temp_dir}/scrape-job.yml"
  candidate_file="${temp_dir}/prometheus.yml"
  backup_file="${temp_dir}/prometheus.yml.backup"
  cp -a "${config_file}" "${backup_file}"

  if [[ "${scrape_scheme}" == "https" && "${reuse_certificates}" == "1" ]]; then
    if append_target_to_reusable_job "${config_file}" "${candidate_file}" "${target}" \
      "${ca_file}" "${cert_file}" "${key_file}"; then
      reused_job=1
      result "已找到使用相同证书配置的 node_exporter 任务，将目标追加到该任务"
    else
      info "未找到引用相同证书路径的兼容任务，将创建新任务并复用现有证书"
    fi
  fi

  if [[ "${reused_job}" == "0" ]]; then
    default_job_name="node_$(sed 's/[^A-Za-z0-9_]/_/g' <<<"${target%:*}")"
    while true; do
      printf '%s任务名称（默认 %s，q 返回主菜单）：%s' "${BLUE}" "${default_job_name}" "${RESET}" >&2
      IFS= read -e -r job_name <"${INTERACTIVE_DEVICE}" || die "读取任务名称失败"
      case "${job_name}" in
        q|Q)
          rm -rf -- "${temp_dir}"
          WORK_DIR=""
          RETURN_TO_MAIN=1
          result "正在返回主菜单"
          return 0
          ;;
      esac
      job_name="${job_name:-${default_job_name}}"
      if [[ "${job_name}" =~ ^[A-Za-z0-9_.-]+$ ]]; then
        break
      fi
      warn "输入无效：任务名称只能包含字母、数字、点、下划线和连字符"
    done

    if [[ "${scrape_scheme}" == "https" ]]; then
      cat >"${fragment_file}" <<EOF
  - job_name: "${job_name}"
    scheme: https
    static_configs:
      - targets: ["${target}"]
    tls_config:
      ca_file: ${ca_file}
      cert_file: ${cert_file}
      key_file: ${key_file}
EOF
    else
      cat >"${fragment_file}" <<EOF
  - job_name: "${job_name}"
    scheme: http
    static_configs:
      - targets: ["${target}"]
EOF
    fi

    awk -v fragment="${fragment_file}" '
      function emit_fragment(line) {
        while ((getline line < fragment) > 0) print line
        close(fragment)
      }
      BEGIN { found = 0; inside = 0; inserted = 0 }
      /^scrape_configs:[[:space:]]*$/ { found = 1; inside = 1; print; next }
      inside && /^[^[:space:]#][^:]*:[[:space:]]*/ {
        emit_fragment()
        inserted = 1
        inside = 0
      }
      { print }
      END {
        if (!found) {
          print ""
          print "scrape_configs:"
          emit_fragment()
        } else if (inside && !inserted) {
          emit_fragment()
        }
      }
    ' "${config_file}" >"${candidate_file}"
  fi

  printf '\n'
  step "校验 Prometheus 配置"
  if ! "${BIN_DIR}/promtool" check config "${candidate_file}"; then
    warn "新增探针后的配置校验失败，原配置未修改"
    rm -rf -- "${temp_dir}"
    WORK_DIR=""
    RETURN_TO_MAIN=1
    result "正在返回主菜单"
    return 0
  fi

  install -o root -g "${PROMETHEUS_GROUP}" -m 0640 "${candidate_file}" "${config_file}"
  if ! systemctl reload prometheus.service; then
    warn "Prometheus 重载失败，正在恢复原配置"
    install -o root -g "${PROMETHEUS_GROUP}" -m 0640 "${backup_file}" "${config_file}"
    systemctl reload prometheus.service >/dev/null 2>&1 || true
    rm -rf -- "${temp_dir}"
    WORK_DIR=""
    die "添加探针失败，原配置已恢复"
  fi

  result "探针已添加：${target}"
  if [[ "${reused_job}" == "1" ]]; then
    info "任务处理：已追加到现有兼容任务"
  else
    info "任务名称：${job_name}"
  fi
  info "连接方式：${scrape_scheme}"
  rm -rf -- "${temp_dir}"
  WORK_DIR=""
  RETURN_TO_MAIN=1
  result "正在返回主菜单"
}

prepare_mtls_settings() {
  case "${PROMETHEUS_MTLS_ENABLED,,}" in
    1|true|yes) PROMETHEUS_MTLS_ENABLED=1 ;;
    0|false|no|"") PROMETHEUS_MTLS_ENABLED=0 ;;
    *) die "PROMETHEUS_MTLS_ENABLED 只支持 1/0、true/false 或 yes/no" ;;
  esac

  if [[ "${PROMETHEUS_MTLS_ENABLED}" != "1" ]]; then
    WEB_CONFIG_ARGUMENT=""
    WEB_SCHEME="http"
    MTLS_STATUS="关闭"
    return
  fi

  require_commands chown openssl touch
  set_interactive_device
  select_text_editor

  local tls_group="root"
  if getent group "${PROMETHEUS_GROUP}" >/dev/null 2>&1; then
    tls_group="${PROMETHEUS_GROUP}"
  fi
  install -d -o root -g root -m 0755 "${CONFIG_DIR}"
  install -d -o root -g "${tls_group}" -m 0750 "${TLS_DIR}"
  touch "${TLS_DIR}/server.crt" "${TLS_DIR}/server.key" "${TLS_DIR}/client-ca.crt"
  chown root:"${tls_group}" "${TLS_DIR}/server.crt" "${TLS_DIR}/server.key" "${TLS_DIR}/client-ca.crt"
  chmod 0640 "${TLS_DIR}/server.crt" "${TLS_DIR}/server.key" "${TLS_DIR}/client-ca.crt"

  while true; do
    edit_pem_file "Prometheus 服务端证书" "${TLS_DIR}/server.crt" certificate \
      "Prometheus 提供 HTTPS 服务使用的服务端证书，证书 SAN 应包含客户端访问时使用的域名或 IP" \
      "客户端证书、私钥或 CA 私钥"
    [[ "${RETURN_TO_MAIN}" == "0" ]] || return 0
    edit_pem_file "Prometheus 服务端私钥" "${TLS_DIR}/server.key" private_key \
      "与上一步 Prometheus 服务端证书匹配的未加密私钥" \
      "证书、客户端私钥或加密私钥"
    [[ "${RETURN_TO_MAIN}" == "0" ]] || return 0
    if certificate_matches_private_key "${TLS_DIR}/server.crt" "${TLS_DIR}/server.key"; then
      result "服务端证书和私钥匹配"
      break
    fi
    warn "服务端证书和私钥不匹配，请重新编辑"
  done
  edit_pem_file "客户端根 CA 证书（信任锚）" "${TLS_DIR}/client-ca.crt" certificate \
    "信任访问 Prometheus 的客户端证书的根 CA 公共证书；如使用中间 CA，可同时包含对应 CA 证书链" \
    "客户端证书、Prometheus 服务端证书或 CA 私钥"
  [[ "${RETURN_TO_MAIN}" == "0" ]] || return 0

  WEB_CONFIG_ARGUMENT=" --web.config.file=${WEB_CONFIG_FILE}"
  WEB_SCHEME="https"
  MTLS_STATUS="启用（强制验证客户端证书）"
}

install_mtls_config() {
  [[ "${PROMETHEUS_MTLS_ENABLED}" == "1" ]] || return 0

  local temp_dir="$1"
  local web_config_tmp="${temp_dir}/mtls-web.yml"

  install -d -o root -g "${PROMETHEUS_GROUP}" -m 0750 "${TLS_DIR}"
  chown root:"${PROMETHEUS_GROUP}" "${TLS_DIR}/server.crt" "${TLS_DIR}/server.key" "${TLS_DIR}/client-ca.crt"
  chmod 0640 "${TLS_DIR}/server.crt" "${TLS_DIR}/server.key" "${TLS_DIR}/client-ca.crt"

  cat >"${web_config_tmp}" <<EOF
tls_server_config:
  cert_file: ${TLS_DIR}/server.crt
  key_file: ${TLS_DIR}/server.key
  client_auth_type: RequireAndVerifyClientCert
  client_ca_file: ${TLS_DIR}/client-ca.crt
  min_version: TLS12
EOF
  install -o root -g "${PROMETHEUS_GROUP}" -m 0640 "${web_config_tmp}" "${WEB_CONFIG_FILE}"
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
  local api_url="https://api.github.com/repos/prometheus/prometheus/releases/latest"
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

  die "无法通过 GitHub Releases API 获取 Prometheus 最新正式版本"
}

installed_version() {
  if [[ -x "${BIN_DIR}/prometheus" ]]; then
    local version_output
    version_output="$("${BIN_DIR}/prometheus" --version 2>/dev/null || true)"
    sed -n '1s/^prometheus, version \([^ ]*\).*/\1/p' <<<"${version_output}"
  fi
}

create_service_account() {
  if ! getent group "${PROMETHEUS_GROUP}" >/dev/null; then
    groupadd --system "${PROMETHEUS_GROUP}"
  fi
  if ! id "${PROMETHEUS_USER}" >/dev/null 2>&1; then
    useradd --system --gid "${PROMETHEUS_GROUP}" --home-dir "${DATA_DIR}" \
      --no-create-home --shell /usr/sbin/nologin "${PROMETHEUS_USER}"
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
      uninstall_prometheus ask
      if [[ "${RETURN_TO_MAIN}" == "0" ]]; then
        exit 0
      fi
      RETURN_TO_MAIN=0
      ;;
    purge|--purge)
      [[ "$#" -eq 1 ]] || die "参数过多，请使用 --help 查看用法"
      uninstall_prometheus purge
      exit 0
      ;;
    install|--install|-i)
      [[ "$#" -le 1 ]] || die "参数过多，请使用 --help 查看用法"
      ;;
    mtls|--mtls)
      [[ "$#" -eq 1 ]] || die "参数过多，请使用 --help 查看用法"
      PROMETHEUS_MTLS_ENABLED=1
      PROMETHEUS_MTLS_MODE_PRESET=1
      ;;
    http|--http)
      [[ "$#" -eq 1 ]] || die "参数过多，请使用 --help 查看用法"
      PROMETHEUS_MTLS_ENABLED=0
      PROMETHEUS_MTLS_MODE_PRESET=1
      ;;
    *)
      die "未知命令：$1，请使用 --help 查看用法"
      ;;
  esac

  print_banner "一键安装、更新与卸载"
  step "检查运行权限"
  require_root
  result "root 权限检查通过"

  local requested_version="${PROMETHEUS_VERSION}"
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
      result "未检测到 Prometheus，将安装最新版本"
      printf '\n'
      step "选择安装方式"
      choose_install_mode
    else
      result "检测到 Prometheus v${current_version}"
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
      PROMETHEUS_MTLS_MODE_PRESET=0
      continue
    fi

    case "${SELECTED_ACTION}" in
      add_probe)
        add_node_exporter_target
        PROMETHEUS_MTLS_MODE_PRESET=0
        continue
        ;;
      uninstall)
        uninstall_prometheus keep
        exit 0
        ;;
      purge)
        uninstall_prometheus purge
        exit 0
        ;;
    esac

    printf '\n'
    step "检查安装依赖"
    require_commands awk cat chmod chown cp getent grep groupadd id install mktemp rm sed sha256sum systemctl tar uname useradd
    result "安装依赖检查通过"

    validate_listen_address "${PROMETHEUS_LISTEN_ADDRESS}"
    if [[ "${MAINTENANCE_ACTION}" == "update" ]]; then
      load_existing_web_mode
    else
      prepare_mtls_settings
    fi
    if [[ "${RETURN_TO_MAIN}" == "1" ]]; then
      PROMETHEUS_MTLS_MODE_PRESET=0
      continue
    fi
    break
  done

  arch="$(detect_arch)"
  work_dir="$(mktemp -d -t prometheus-install.XXXXXXXX)"
  WORK_DIR="${work_dir}"

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
    archive="prometheus-${version}.linux-${arch}.tar.gz"
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
    step "安装 Prometheus"
    tar -xzf "${work_dir}/${archive}" -C "${work_dir}"
    extracted_dir="${work_dir}/prometheus-${version}.linux-${arch}"
    [[ -x "${extracted_dir}/prometheus" && -x "${extracted_dir}/promtool" ]] || die "发布包中缺少 prometheus 或 promtool 程序"
  elif [[ "${MAINTENANCE_ACTION}" == "reconfigure" ]]; then
    printf '\n'
    step "重新配置 Prometheus"
    [[ -x "${BIN_DIR}/prometheus" && -x "${BIN_DIR}/promtool" ]] || die "现有安装缺少 prometheus 或 promtool，无法仅重新配置，请重新运行并选择更新"
  else
    printf '\n'
    step "保留当前程序和配置"
    [[ -x "${BIN_DIR}/prometheus" && -x "${BIN_DIR}/promtool" ]] || die "现有安装缺少 prometheus 或 promtool，请重新运行并选择更新"
    result "无需替换程序文件"
  fi

  create_service_account
  install -d -m 0755 "${CONFIG_DIR}"
  install -d -o "${PROMETHEUS_USER}" -g "${PROMETHEUS_GROUP}" -m 0750 "${DATA_DIR}"
  if [[ "${download_required}" == "1" ]]; then
    install -m 0755 "${extracted_dir}/prometheus" "${BIN_DIR}/prometheus"
    install -m 0755 "${extracted_dir}/promtool" "${BIN_DIR}/promtool"
  fi
  if [[ "${MAINTENANCE_ACTION}" != "update" ]]; then
    install_mtls_config "${work_dir}"
  fi

  if [[ "${download_required}" == "1" ]]; then
    if [[ -d "${extracted_dir}/consoles" ]]; then
      install -d -m 0755 "${CONFIG_DIR}/consoles"
      cp -a "${extracted_dir}/consoles/." "${CONFIG_DIR}/consoles/"
    fi
    if [[ -d "${extracted_dir}/console_libraries" ]]; then
      install -d -m 0755 "${CONFIG_DIR}/console_libraries"
      cp -a "${extracted_dir}/console_libraries/." "${CONFIG_DIR}/console_libraries/"
    fi
  fi

  if [[ ! -e "${CONFIG_DIR}/prometheus.yml" ]]; then
    apply_default_config
    result "已创建默认配置：${CONFIG_DIR}/prometheus.yml"
  else
    info "保留现有配置：${CONFIG_DIR}/prometheus.yml"
  fi
  info "正在检查 Prometheus 配置"
  "${BIN_DIR}/promtool" check config "${CONFIG_DIR}/prometheus.yml"
  if [[ "${MAINTENANCE_ACTION}" == "update" ]]; then
    result "程序更新检查完成，现有配置保持不变"
  else
    result "程序和配置处理完成"
  fi

  printf '\n'
  step "配置并启动系统服务"
  if [[ "${MAINTENANCE_ACTION}" != "update" ]]; then
    write_service_file
  else
    info "保留现有 systemd 服务配置：${SERVICE_FILE}"
  fi
  chown -R root:"${PROMETHEUS_GROUP}" "${CONFIG_DIR}"
  chmod 0750 "${CONFIG_DIR}"

  systemctl daemon-reload
  systemctl enable prometheus.service >/dev/null
  systemctl restart prometheus.service
  if ! systemctl is-active --quiet prometheus.service; then
    systemctl --no-pager --full status prometheus.service || true
    die "Prometheus 服务启动失败"
  fi
  result "Prometheus 服务已启动并设置为开机自启"
  info "查看日志：journalctl -u prometheus -f"
  if [[ "${PROMETHEUS_MTLS_ENABLED}" == "1" ]]; then
    warn "受 mTLS 保护的抓取目标必须在 prometheus.yml 中配置 HTTPS 和客户端证书"
  fi

  rm -rf -- "${work_dir}"
  WORK_DIR=""
  print_completion_card "${action}" "${version}"
}

apply_default_config() {
  local patch
  local listen_port="${PROMETHEUS_LISTEN_ADDRESS##*:}"
  patch="$(mktemp -t prometheus-config.XXXXXXXX)"
  cat >"${patch}" <<EOF
global:
  scrape_interval: 15s
  evaluation_interval: 15s

scrape_configs:
  - job_name: prometheus
    static_configs:
      - targets: ["localhost:${listen_port}"]

  - job_name: node
    static_configs:
      - targets: ["localhost:9100"]
EOF
  install -m 0640 "${patch}" "${CONFIG_DIR}/prometheus.yml"
  rm -f -- "${patch}"
}

write_service_file() {
  local service_tmp
  service_tmp="$(mktemp -t prometheus-service.XXXXXXXX)"
  cat >"${service_tmp}" <<EOF
[Unit]
Description=Prometheus Monitoring System
Documentation=https://prometheus.io/docs/introduction/overview/
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
User=${PROMETHEUS_USER}
Group=${PROMETHEUS_GROUP}
ExecStart=${BIN_DIR}/prometheus --config.file=${CONFIG_DIR}/prometheus.yml --storage.tsdb.path=${DATA_DIR} --web.listen-address=${PROMETHEUS_LISTEN_ADDRESS}${WEB_CONFIG_ARGUMENT}
ExecReload=/bin/kill -HUP \$MAINPID
Restart=on-failure
RestartSec=5s
TimeoutStopSec=20s
LimitNOFILE=65536
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=true
ProtectSystem=full
ReadWritePaths=${DATA_DIR}

[Install]
WantedBy=multi-user.target
EOF
  install -m 0644 "${service_tmp}" "${SERVICE_FILE}"
  rm -f -- "${service_tmp}"
}

uninstall_prometheus() {
  local uninstall_mode="${1:-ask}"
  local current_version

  print_banner "一键卸载"
  step "检查安装状态"
  require_root
  require_commands getent groupdel id sed systemctl userdel

  current_version="$(installed_version)"
  info "当前版本：${current_version:-未知或未安装}"
  info "程序路径：${BIN_DIR}/prometheus"

  printf '\n'
  step "选择卸载方式"
  case "${uninstall_mode}" in
    purge)
      PURGE_MODE=1
      warn "将彻底删除配置、mTLS 证书、监控数据和系统账号"
      ;;
    keep)
      PURGE_MODE=0
      info "将保留配置、mTLS 证书、监控数据和系统账号"
      ;;
    *)
      choose_uninstall_mode
      [[ "${RETURN_TO_MAIN}" == "0" ]] || return 0
      ;;
  esac

  printf '\n'
  step "停止 Prometheus 服务"
  info "正在停止并禁用 prometheus.service"
  systemctl disable --now prometheus.service >/dev/null 2>&1 || true

  printf '\n'
  step "删除服务和程序文件"
  rm -f -- "${SERVICE_FILE}"
  rm -f -- "/etc/systemd/system/multi-user.target.wants/prometheus.service"
  rm -f -- "${BIN_DIR}/prometheus"
  rm -f -- "${BIN_DIR}/promtool"

  systemctl daemon-reload
  systemctl reset-failed prometheus.service >/dev/null 2>&1 || true

  result "Prometheus 服务和程序文件已删除"
  if [[ "${PURGE_MODE}" == "1" ]]; then
    printf '\n'
    step "彻底清理配置、证书、数据和账号"
    rm -rf -- "${CONFIG_DIR}" "${DATA_DIR}"
    if id "${PROMETHEUS_USER}" >/dev/null 2>&1; then
      userdel "${PROMETHEUS_USER}"
    fi
    if getent group "${PROMETHEUS_GROUP}" >/dev/null; then
      groupdel "${PROMETHEUS_GROUP}"
    fi
    result "配置、mTLS 证书、监控数据和系统账号已删除"
    warn "历史监控数据已永久删除，无法通过脚本恢复"
    print_result_card "Prometheus 彻底清理完成"
  else
    info "配置和 mTLS 证书已保留：${CONFIG_DIR}"
    info "监控数据已保留：${DATA_DIR}"
    info "系统账号已保留：${PROMETHEUS_USER}"
    print_result_card "Prometheus 标准卸载完成"
  fi
}

trap 'on_error "$?" "$LINENO"' ERR
trap cleanup EXIT
init_colors
main "$@"
