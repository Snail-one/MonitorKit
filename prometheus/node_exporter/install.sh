#!/usr/bin/env bash
# Install or upgrade Prometheus node_exporter as a systemd service.
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
NODE_EXPORTER_MTLS_ENABLED="${NODE_EXPORTER_MTLS_ENABLED:-0}"

readonly EXPORTER_USER="node_exporter"
readonly EXPORTER_GROUP="node_exporter"
readonly BIN_DIR="/usr/local/bin"
readonly CONFIG_DIR="/etc/node_exporter"
readonly TLS_DIR="${CONFIG_DIR}/tls"
readonly WEB_CONFIG_FILE="${CONFIG_DIR}/web.yml"
readonly SERVICE_FILE="/etc/systemd/system/node_exporter.service"

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
  printf '%s%s%s\n' "${BOLD}${ORANGE}" "╭─ Node Exporter" "${RESET}"
  printf '%s│ %s%s\n' "${ORANGE}" "$1" "${RESET}"
  printf '%s%s%s\n\n' "${ORANGE}" "╰──────────────────────────────────────────────" "${RESET}"
}

print_release_info() {
  printf '\n%s%s%s\n' "${BOLD}${ORANGE}" "╭─ Node Exporter" "${RESET}"
  printf '%s│ %s%s%s\n' "${ORANGE}" "${BOLD}${ORANGE}" "发布信息" "${RESET}"
  printf '%s│ %s平台：%sLinux/%s\n' "${ORANGE}" "${BLUE}" "${RESET}" "$1"
  printf '%s│ %s当前版本：%s%s\n' "${ORANGE}" "${BLUE}" "${RESET}" "$2"
  printf '%s│ %s目标版本：%s%s%s%s\n' "${ORANGE}" "${BLUE}" "${RESET}" "${BOLD}" "$3" "${RESET}"
  printf '%s│ %s执行操作：%s%s%s%s\n' "${ORANGE}" "${BLUE}" "${RESET}" "${BOLD}" "$4" "${RESET}"
  printf '%s│ %s监听地址：%s%s\n' "${ORANGE}" "${BLUE}" "${RESET}" "${NODE_EXPORTER_LISTEN_ADDRESS}"
  printf '%s│ %smTLS：%s%s\n' "${ORANGE}" "${BLUE}" "${RESET}" "${MTLS_STATUS}"
  printf '%s%s%s\n' "${ORANGE}" "╰──────────────────────────────────────────────" "${RESET}"
}

print_completion_card() {
  printf '\n%s%s%s\n' "${BOLD}${ORANGE}" "╭─ Node Exporter" "${RESET}"
  printf '%s│ %s%s完成%s\n' "${ORANGE}" "${BOLD}${GREEN}" "$1" "${RESET}"
  printf '%s│ %s版本：%s%s%s%s\n' "${ORANGE}" "${BLUE}" "${RESET}" "${BOLD}" "$2" "${RESET}"
  printf '%s│ %s服务：%s%s\n' "${ORANGE}" "${BLUE}" "${RESET}" "node_exporter.service"
  printf '%s│ %s指标：%s%s\n' "${ORANGE}" "${BLUE}" "${RESET}" "${WEB_SCHEME}://${NODE_EXPORTER_LISTEN_ADDRESS}/metrics"
  printf '%s%s%s\n' "${ORANGE}" "╰──────────────────────────────────────────────" "${RESET}"
}

print_result_card() {
  printf '\n%s%s%s\n' "${BOLD}${ORANGE}" "╭─ Node Exporter" "${RESET}"
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

cleanup() {
  if [[ -n "${WORK_DIR}" ]]; then
    rm -rf -- "${WORK_DIR}"
    WORK_DIR=""
  fi
}

usage() {
  cat <<'EOF'
node_exporter 探针一键安装、更新与卸载脚本。

用法：
  sudo ./install.sh             # 交互选择 HTTP 或 mTLS
  sudo ./install.sh http        # 直接安装 HTTP 模式
  sudo ./install.sh mtls
  sudo ./install.sh uninstall   # 交互选择标准卸载或彻底清理
  sudo ./install.sh purge       # 直接彻底清理

默认通过 GitHub Releases API 安装最新正式版本，也可以指定固定版本。

可选安装环境变量：
  NODE_EXPORTER_VERSION=latest
  NODE_EXPORTER_LISTEN_ADDRESS=0.0.0.0:9100
  DOWNLOAD_BASE_URL=https://github.com/prometheus/node_exporter/releases/download

mTLS 环境变量：
  NODE_EXPORTER_MTLS_ENABLED=1

mTLS 模式会使用 vim、nano 或 vi 编辑并校验证书文件。
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
  local answer=""

  while true; do
    printf '\n'
    step "配置${content_name}"
    info "文件路径：${file_path}"
    info "手动编辑命令：sudo ${TEXT_EDITOR##*/} ${file_path}"
    printf '%s按回车打开 %s，输入 q 取消：%s' "${BLUE}" "${TEXT_EDITOR##*/}" "${RESET}" >&2
    IFS= read -e -r answer <"${INTERACTIVE_DEVICE}" || die "读取用户输入失败"
    case "${answer}" in
      "") ;;
      q|Q) die "用户取消 mTLS 证书配置" ;;
      *) warn "请直接按回车打开编辑器，或输入 q 取消"; continue ;;
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

  printf '%s  1.%s 安装/更新：普通 HTTP\n' "${GREEN}" "${RESET}" >&2
  printf '%s  2.%s 安装/更新：mTLS（HTTPS，强制验证客户端证书）\n' "${ORANGE}" "${RESET}" >&2
  printf '%s  3.%s 标准卸载（保留配置、证书和账号）\n' "${YELLOW}" "${RESET}" >&2
  printf '%s  4.%s 彻底清理（删除配置、证书和账号）\n' "${RED}" "${RESET}" >&2
  while true; do
    printf '%s请选择操作 [1-4]（默认 1）：%s' "${BLUE}" "${RESET}" >&2
    local choice=""
    IFS= read -e -r choice <"${INTERACTIVE_DEVICE}" || die "读取安装方式失败"
    case "${choice}" in
      ""|1)
        NODE_EXPORTER_MTLS_ENABLED=0
        result "已选择普通 HTTP 模式"
        return
        ;;
      2)
        NODE_EXPORTER_MTLS_ENABLED=1
        result "已选择 mTLS 模式"
        return
        ;;
      3)
        SELECTED_ACTION="uninstall"
        result "已选择标准卸载"
        return
        ;;
      4)
        SELECTED_ACTION="purge"
        warn "已选择彻底清理，mTLS 配置和证书将被永久删除"
        return
        ;;
      *) warn "请输入 1、2、3 或 4" ;;
    esac
  done
}

choose_uninstall_mode() {
  set_interactive_device
  printf '%s  1.%s 标准卸载（保留配置、证书和账号）\n' "${GREEN}" "${RESET}" >&2
  printf '%s  2.%s 彻底清理（删除全部配置、证书和账号）\n' "${RED}" "${RESET}" >&2
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
        warn "已选择彻底清理，mTLS 配置和证书将被永久删除"
        return
        ;;
      *) warn "请输入 1 或 2" ;;
    esac
  done
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

  while true; do
    edit_pem_file "服务端证书" "${TLS_DIR}/server.crt" certificate
    edit_pem_file "服务端私钥" "${TLS_DIR}/server.key" private_key
    if certificate_matches_private_key "${TLS_DIR}/server.crt" "${TLS_DIR}/server.key"; then
      result "服务端证书和私钥匹配"
      break
    fi
    warn "服务端证书和私钥不匹配，请重新编辑"
  done
  edit_pem_file "客户端 CA 证书" "${TLS_DIR}/client-ca.crt" certificate

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
      warn "将彻底删除 mTLS 配置、证书和系统账号"
      ;;
    keep)
      PURGE_MODE=0
      info "将保留 mTLS 配置、证书和系统账号"
      ;;
    *) choose_uninstall_mode ;;
  esac

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
    step "彻底清理 mTLS 配置、证书和账号"
    rm -rf -- "${CONFIG_DIR}"
    if id "${EXPORTER_USER}" >/dev/null 2>&1; then
      userdel "${EXPORTER_USER}"
    fi
    if getent group "${EXPORTER_GROUP}" >/dev/null; then
      groupdel "${EXPORTER_GROUP}"
    fi
    result "mTLS 配置、证书和系统账号已删除"
    print_result_card "node_exporter 彻底清理完成"
  else
    info "mTLS 配置和证书已保留：${CONFIG_DIR}"
    info "系统账号已保留：${EXPORTER_USER}"
    print_result_card "node_exporter 标准卸载完成"
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
      exit 0
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

  printf '\n'
  step "选择操作"
  choose_install_mode
  case "${SELECTED_ACTION}" in
    uninstall)
      uninstall_node_exporter keep
      exit 0
      ;;
    purge)
      uninstall_node_exporter purge
      exit 0
      ;;
  esac

  printf '\n'
  step "检查安装依赖"
  require_commands awk cat chmod getent groupadd id install mktemp rm sed sha256sum systemctl tar uname useradd
  result "安装依赖检查通过"
  prepare_mtls_settings

  local requested_version="${NODE_EXPORTER_VERSION}"
  local version
  local arch
  local current_version
  local action
  local archive
  local release_url
  local work_dir
  local checksum
  local extracted_dir

  validate_listen_address "${NODE_EXPORTER_LISTEN_ADDRESS}"
  arch="$(detect_arch)"
  work_dir="$(mktemp -d -t node-exporter-install.XXXXXXXX)"
  WORK_DIR="${work_dir}"

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

  current_version="$(installed_version)"
  current_version="${current_version:-未安装}"
  if [[ "${current_version}" == "${version}" ]]; then
    action="重新安装/配置"
  elif [[ "${current_version}" == "未安装" ]]; then
    action="安装"
  else
    action="更新"
  fi
  print_release_info "${arch}" "${current_version}" "${version}" "${action}"
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

  create_service_account
  install -m 0755 "${extracted_dir}/node_exporter" "${BIN_DIR}/node_exporter"
  install_mtls_config "${work_dir}"
  write_service_file
  result "程序和服务配置安装完成"

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

trap cleanup EXIT
init_colors
main "$@"
