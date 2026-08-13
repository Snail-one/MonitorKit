#!/usr/bin/env bash
# Install or upgrade Prometheus as a systemd service.
#
# Optional environment variables:
#   PROMETHEUS_VERSION=latest
#   PROMETHEUS_LISTEN_ADDRESS=0.0.0.0:9090
#   DOWNLOAD_BASE_URL=https://github.com/prometheus/prometheus/releases/download

set -Eeuo pipefail

PROMETHEUS_VERSION="${PROMETHEUS_VERSION:-latest}"
PROMETHEUS_LISTEN_ADDRESS="${PROMETHEUS_LISTEN_ADDRESS:-0.0.0.0:9090}"
DOWNLOAD_BASE_URL="${DOWNLOAD_BASE_URL:-https://github.com/prometheus/prometheus/releases/download}"

readonly PROMETHEUS_USER="prometheus"
readonly PROMETHEUS_GROUP="prometheus"
readonly CONFIG_DIR="/etc/prometheus"
readonly DATA_DIR="/var/lib/prometheus"
readonly BIN_DIR="/usr/local/bin"
readonly SERVICE_FILE="/etc/systemd/system/prometheus.service"

RESET=""
BOLD=""
ORANGE=""
BLUE=""
GREEN=""
YELLOW=""
RED=""
LATEST_VERSION=""

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
  printf '%s%s%s\n' "${ORANGE}" "╰──────────────────────────────────────────────" "${RESET}"
}

print_completion_card() {
  printf '\n%s%s%s\n' "${BOLD}${ORANGE}" "╭─ Prometheus" "${RESET}"
  printf '%s│ %s%s完成%s\n' "${ORANGE}" "${BOLD}${GREEN}" "$1" "${RESET}"
  printf '%s│ %s版本：%s%s%s%s\n' "${ORANGE}" "${BLUE}" "${RESET}" "${BOLD}" "$2" "${RESET}"
  printf '%s│ %s服务：%s%s\n' "${ORANGE}" "${BLUE}" "${RESET}" "prometheus.service"
  printf '%s│ %s访问：%s%s\n' "${ORANGE}" "${BLUE}" "${RESET}" "http://${PROMETHEUS_LISTEN_ADDRESS}/"
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

usage() {
  cat <<'EOF'
Prometheus 一键安装、更新与卸载脚本。

用法：
  sudo ./install.sh [install]
  sudo ./install.sh uninstall

默认通过 GitHub Releases API 安装最新正式版本，也可以指定固定版本。

可选安装环境变量：
  PROMETHEUS_VERSION=latest
  PROMETHEUS_LISTEN_ADDRESS=0.0.0.0:9090
  DOWNLOAD_BASE_URL=https://github.com/prometheus/prometheus/releases/download
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
      uninstall_prometheus
      exit 0
      ;;
    install|--install|-i)
      [[ "$#" -le 1 ]] || die "参数过多，请使用 --help 查看用法"
      ;;
    *)
      die "未知命令：$1，请使用 --help 查看用法"
      ;;
  esac

  print_banner "一键安装与更新"
  step "检查运行环境"
  require_root
  require_commands awk chmod chown cp getent groupadd id install mktemp rm sed sha256sum systemctl tar uname useradd
  result "运行环境检查通过"

  local requested_version="${PROMETHEUS_VERSION}"
  local version
  local arch
  local current_version
  local action
  local archive
  local release_url
  local work_dir
  local checksum
  local extracted_dir

  validate_listen_address "${PROMETHEUS_LISTEN_ADDRESS}"
  arch="$(detect_arch)"
  work_dir="$(mktemp -d -t prometheus-install.XXXXXXXX)"
  trap 'rm -rf -- "${work_dir}"' EXIT

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

  create_service_account
  install -d -m 0755 "${CONFIG_DIR}"
  install -d -o "${PROMETHEUS_USER}" -g "${PROMETHEUS_GROUP}" -m 0750 "${DATA_DIR}"
  install -m 0755 "${extracted_dir}/prometheus" "${BIN_DIR}/prometheus"
  install -m 0755 "${extracted_dir}/promtool" "${BIN_DIR}/promtool"

  if [[ -d "${extracted_dir}/consoles" ]]; then
    install -d -m 0755 "${CONFIG_DIR}/consoles"
    cp -a "${extracted_dir}/consoles/." "${CONFIG_DIR}/consoles/"
  fi
  if [[ -d "${extracted_dir}/console_libraries" ]]; then
    install -d -m 0755 "${CONFIG_DIR}/console_libraries"
    cp -a "${extracted_dir}/console_libraries/." "${CONFIG_DIR}/console_libraries/"
  fi

  if [[ ! -e "${CONFIG_DIR}/prometheus.yml" ]]; then
    apply_default_config
    result "已创建默认配置：${CONFIG_DIR}/prometheus.yml"
  else
    info "保留现有配置：${CONFIG_DIR}/prometheus.yml"
  fi
  info "正在检查 Prometheus 配置"
  "${BIN_DIR}/promtool" check config "${CONFIG_DIR}/prometheus.yml"
  result "程序和配置安装完成"

  printf '\n'
  step "配置并启动系统服务"
  write_service_file
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

  trap - EXIT
  rm -rf -- "${work_dir}"
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
ExecStart=${BIN_DIR}/prometheus --config.file=${CONFIG_DIR}/prometheus.yml --storage.tsdb.path=${DATA_DIR} --web.listen-address=${PROMETHEUS_LISTEN_ADDRESS}
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
  local current_version

  print_banner "一键卸载"
  step "检查安装状态"
  require_root
  require_commands sed systemctl

  current_version="$(installed_version)"
  info "当前版本：${current_version:-未知或未安装}"
  info "程序路径：${BIN_DIR}/prometheus"

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
  info "配置已保留：${CONFIG_DIR}"
  info "监控数据已保留：${DATA_DIR}"
  print_result_card "Prometheus 卸载完成"
}

init_colors
main "$@"
