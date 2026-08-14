#!/usr/bin/env bash
# Install Grafana Alloy as a unified metrics and logs probe.

set -Eeuo pipefail

readonly CONFIG_DIR="/etc/alloy"
readonly CONFIG_FILE="${CONFIG_DIR}/config.alloy"
readonly DATA_DIR="/var/lib/alloy"
readonly TLS_DIR="${CONFIG_DIR}/tls"

ACTION="${1:-install}"
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
PROMETHEUS_MTLS_ENABLED=0
LOKI_MTLS_ENABLED=0
INTERACTIVE_DEVICE=""

info() { printf '[信息] %s\n' "$*"; }
die() { printf '[错误] %s\n' "$*" >&2; exit 1; }

usage() {
  cat <<'EOF'
Grafana Alloy 指标与日志统一探针安装脚本。

用法：
  curl -fsSL https://raw.githubusercontent.com/Snail-one/MonitorKit/main/scripts/probes/alloy/install.sh | sudo bash
  sudo ./install.sh             # 本地执行时使用相同的交互配置
  sudo ./install.sh uninstall   # 保留配置和数据
  sudo ./install.sh purge       # 删除配置和数据

安装时会交互询问 Prometheus、Loki 地址和各自的 mTLS 客户端证书。
URL 仅填写服务根地址，脚本会自动追加写入路径。
Alloy 已内置 Unix 主机指标采集；不要在同一服务器重复安装 node_exporter。

中心端启用 mTLS 时，分别提供客户端证书：
  PROMETHEUS_MTLS_CA_FILE=/root/monitor-ca.crt
  PROMETHEUS_MTLS_CERT_FILE=/root/alloy-client.crt
  PROMETHEUS_MTLS_KEY_FILE=/root/alloy-client.key
  PROMETHEUS_TLS_SERVER_NAME=monitor.example.com
  LOKI_MTLS_CA_FILE=/root/monitor-ca.crt
  LOKI_MTLS_CERT_FILE=/root/alloy-client.crt
  LOKI_MTLS_KEY_FILE=/root/alloy-client.key
  LOKI_TLS_SERVER_NAME=monitor.example.com

mTLS 模式要求对应 URL 使用 https://。证书会复制到 /etc/alloy/tls/，
普通卸载保留证书，purge 才会随 /etc/alloy 一并删除。

无人值守安装仍可预先设置以上变量；变量完整时不会打开交互输入。
EOF
}

require_root() {
  [[ "${EUID}" -eq 0 ]] || die "请使用 root 或 sudo 运行"
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
  detect_interactive_device || die "缺少交互式终端；请在终端运行脚本，或通过环境变量提供完整配置"
}

prompt_required() {
  local variable_name="$1"
  local prompt="$2"
  local value="${!variable_name:-}"
  [[ -n "${value}" ]] && return 0
  if ! detect_interactive_device; then
    printf -v "${variable_name}" '%s' "${default_value}"
    return 0
  fi
  while [[ -z "${value}" ]]; do
    printf '❯ %s： ' "${prompt}" >&2
    IFS= read -r value <"${INTERACTIVE_DEVICE}" || die "读取输入失败"
    value="${value#"${value%%[![:space:]]*}"}"
    value="${value%"${value##*[![:space:]]}"}"
    [[ -n "${value}" ]] || info "该项不能为空，请重新输入"
  done
  printf -v "${variable_name}" '%s' "${value}"
}

prompt_with_default() {
  local variable_name="$1"
  local prompt="$2"
  local default_value="$3"
  local value="${!variable_name:-}"
  [[ -n "${value}" ]] && return 0
  set_interactive_device
  printf '❯ %s [%s]： ' "${prompt}" "${default_value}" >&2
  IFS= read -r value <"${INTERACTIVE_DEVICE}" || die "读取输入失败"
  value="${value:-${default_value}}"
  printf -v "${variable_name}" '%s' "${value}"
}

ask_yes_no() {
  local prompt="$1"
  local answer=""
  set_interactive_device
  while true; do
    printf '❯ %s [y/N]： ' "${prompt}" >&2
    IFS= read -r answer <"${INTERACTIVE_DEVICE}" || die "读取输入失败"
    case "${answer,,}" in
      y|yes) return 0 ;;
      ""|n|no) return 1 ;;
      *) info "请输入 y 或 n" ;;
    esac
  done
}

valid_backend_url() {
  local value="$1"
  local port=""
  if [[ "${value}" =~ ^https?://[A-Za-z0-9._-]+(:([0-9]{1,5}))?$ ]]; then
    port="${BASH_REMATCH[2]:-}"
    [[ -z "${port}" || ("${port}" -ge 1 && "${port}" -le 65535) ]]
    return
  fi
  return 1
}

prompt_backend_url() {
  local variable_name="$1"
  local label="$2"
  local example_port="$3"
  local value="${!variable_name:-}"
  if [[ -n "${value}" ]]; then
    return 0
  fi
  set_interactive_device
  while true; do
    printf '❯ %s 根地址（例如 http://10.0.0.10:%s）： ' "${label}" "${example_port}" >&2
    IFS= read -r value <"${INTERACTIVE_DEVICE}" || die "读取输入失败"
    value="${value%/}"
    if valid_backend_url "${value}"; then
      printf -v "${variable_name}" '%s' "${value}"
      return 0
    fi
    info "地址格式无效，请输入 http://主机:端口 或 https://主机:端口"
  done
}

collect_backend_mtls_settings() {
  local backend="$1"
  local label="$2"
  local url="$3"
  local ca_variable="${backend}_MTLS_CA_FILE"
  local cert_variable="${backend}_MTLS_CERT_FILE"
  local key_variable="${backend}_MTLS_KEY_FILE"
  local server_name_variable="${backend}_TLS_SERVER_NAME"
  local enabled_variable="${backend}_MTLS_ENABLED"
  local configured="${!ca_variable:-}${!cert_variable:-}${!key_variable:-}${!server_name_variable:-}"
  local host="${url#*://}"
  host="${host%%:*}"

  if [[ -z "${configured}" ]]; then
    if ! detect_interactive_device; then
      printf -v "${enabled_variable}" '%s' 0
      return 0
    fi
    if ! ask_yes_no "${label} 是否启用 mTLS 客户端证书认证"; then
      printf -v "${enabled_variable}" '%s' 0
      return 0
    fi
  fi

  printf -v "${enabled_variable}" '%s' 1
  prompt_required "${ca_variable}" "${label} 服务端 CA 证书路径"
  prompt_required "${cert_variable}" "${label} Alloy 客户端证书路径"
  prompt_required "${key_variable}" "${label} Alloy 客户端私钥路径"
  prompt_with_default "${server_name_variable}" "${label} TLS server_name" "${host}"
}

collect_install_settings() {
  info "开始配置 Alloy 数据接收中心"
  prompt_backend_url PROMETHEUS_URL Prometheus 9090
  prompt_backend_url LOKI_URL Loki 3100
  collect_backend_mtls_settings PROMETHEUS Prometheus "${PROMETHEUS_URL}"
  collect_backend_mtls_settings LOKI Loki "${LOKI_URL}"
}

validate_backend_urls() {
  [[ -n "${PROMETHEUS_URL}" ]] || die "缺少 PROMETHEUS_URL，例如：http://10.0.0.10:9090"
  [[ -n "${LOKI_URL}" ]] || die "缺少 LOKI_URL，例如：http://10.0.0.10:3100"
  [[ "${PROMETHEUS_URL}" =~ ^https?://[A-Za-z0-9._-]+(:[0-9]{1,5})?$ ]] || die "PROMETHEUS_URL 格式无效或包含不安全字符"
  [[ "${LOKI_URL}" =~ ^https?://[A-Za-z0-9._-]+(:[0-9]{1,5})?$ ]] || die "LOKI_URL 格式无效或包含不安全字符"
  PROMETHEUS_URL="${PROMETHEUS_URL%/}"
  LOKI_URL="${LOKI_URL%/}"

  validate_mtls_settings prometheus "${PROMETHEUS_URL}" \
    "${PROMETHEUS_MTLS_CA_FILE}" "${PROMETHEUS_MTLS_CERT_FILE}" "${PROMETHEUS_MTLS_KEY_FILE}" "${PROMETHEUS_TLS_SERVER_NAME}"
  validate_mtls_settings loki "${LOKI_URL}" \
    "${LOKI_MTLS_CA_FILE}" "${LOKI_MTLS_CERT_FILE}" "${LOKI_MTLS_KEY_FILE}" "${LOKI_TLS_SERVER_NAME}"
}

validate_mtls_settings() {
  local backend="$1"
  local url="$2"
  local ca_file="$3"
  local cert_file="$4"
  local key_file="$5"
  local server_name="$6"
  local configured=0

  [[ -n "${ca_file}${cert_file}${key_file}${server_name}" ]] && configured=1
  [[ "${configured}" == "1" ]] || return 0
  [[ -n "${ca_file}" && -n "${cert_file}" && -n "${key_file}" ]] || \
    die "${backend} mTLS 必须同时提供 CA、客户端证书和客户端私钥"
  [[ "${url}" == https://* ]] || die "${backend} 启用 mTLS 时 URL 必须使用 https://"
  [[ -r "${ca_file}" ]] || die "无法读取 ${backend} CA 证书：${ca_file}"
  [[ -r "${cert_file}" ]] || die "无法读取 ${backend} 客户端证书：${cert_file}"
  [[ -r "${key_file}" ]] || die "无法读取 ${backend} 客户端私钥：${key_file}"
  if [[ -n "${server_name}" ]]; then
    [[ "${server_name}" =~ ^[A-Za-z0-9._-]+$ ]] || die "${backend} TLS server_name 格式无效"
  fi
  if [[ "${backend}" == "prometheus" ]]; then
    PROMETHEUS_MTLS_ENABLED=1
  else
    LOKI_MTLS_ENABLED=1
  fi
}

validate_certificate_bundle() {
  local backend="$1"
  local ca_file="$2"
  local cert_file="$3"
  local key_file="$4"
  local cert_public_key key_public_key

  command -v openssl >/dev/null 2>&1 || die "${backend} mTLS 需要 openssl"
  openssl x509 -in "${ca_file}" -noout >/dev/null 2>&1 || die "${backend} CA 证书格式无效"
  openssl x509 -in "${cert_file}" -noout >/dev/null 2>&1 || die "${backend} 客户端证书格式无效"
  openssl pkey -in "${key_file}" -noout -passin pass: >/dev/null 2>&1 || die "${backend} 客户端私钥无效或已加密"
  cert_public_key="$(openssl x509 -in "${cert_file}" -pubkey -noout)"
  key_public_key="$(openssl pkey -in "${key_file}" -pubout -passin pass:)"
  [[ "${cert_public_key}" == "${key_public_key}" ]] || die "${backend} 客户端证书与私钥不匹配"
}

install_tls_file() {
  local source="$1"
  local destination="$2"
  if [[ "$(readlink -f "${source}")" != "$(readlink -m "${destination}")" ]]; then
    install -o root -g alloy -m 0640 "${source}" "${destination}"
  else
    chown root:alloy "${destination}"
    chmod 0640 "${destination}"
  fi
}

install_mtls_files() {
  [[ "${PROMETHEUS_MTLS_ENABLED}" == "1" || "${LOKI_MTLS_ENABLED}" == "1" ]] || return 0
  install -d -o root -g alloy -m 0750 "${TLS_DIR}"
  if [[ "${PROMETHEUS_MTLS_ENABLED}" == "1" ]]; then
    validate_certificate_bundle prometheus "${PROMETHEUS_MTLS_CA_FILE}" "${PROMETHEUS_MTLS_CERT_FILE}" "${PROMETHEUS_MTLS_KEY_FILE}"
    install_tls_file "${PROMETHEUS_MTLS_CA_FILE}" "${TLS_DIR}/prometheus-ca.crt"
    install_tls_file "${PROMETHEUS_MTLS_CERT_FILE}" "${TLS_DIR}/prometheus-client.crt"
    install_tls_file "${PROMETHEUS_MTLS_KEY_FILE}" "${TLS_DIR}/prometheus-client.key"
  fi
  if [[ "${LOKI_MTLS_ENABLED}" == "1" ]]; then
    validate_certificate_bundle loki "${LOKI_MTLS_CA_FILE}" "${LOKI_MTLS_CERT_FILE}" "${LOKI_MTLS_KEY_FILE}"
    install_tls_file "${LOKI_MTLS_CA_FILE}" "${TLS_DIR}/loki-ca.crt"
    install_tls_file "${LOKI_MTLS_CERT_FILE}" "${TLS_DIR}/loki-client.crt"
    install_tls_file "${LOKI_MTLS_KEY_FILE}" "${TLS_DIR}/loki-client.key"
  fi
}

install_debian() {
  export DEBIAN_FRONTEND=noninteractive
  apt-get update
  apt-get install -y ca-certificates curl gpg
  install -d -m 0755 /etc/apt/keyrings
  curl -fsSL --proto '=https' https://apt.grafana.com/gpg-full.key -o /etc/apt/keyrings/grafana.asc
  chmod 0644 /etc/apt/keyrings/grafana.asc
  printf '%s\n' 'deb [signed-by=/etc/apt/keyrings/grafana.asc] https://apt.grafana.com stable main' \
    > /etc/apt/sources.list.d/grafana.list
  apt-get update
  apt-get install -y alloy
}

install_rpm() {
  local package_manager="$1"
  local key_file
  command -v curl >/dev/null 2>&1 || "${package_manager}" install -y curl
  key_file="$(mktemp)"
  curl -fsSL --proto '=https' https://rpm.grafana.com/gpg.key -o "${key_file}"
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

write_config() {
  install -d -m 0750 "${CONFIG_DIR}" "${DATA_DIR}"
  local temp_file prometheus_tls_config="" loki_tls_config=""
  temp_file="$(mktemp)"
  if [[ "${PROMETHEUS_MTLS_ENABLED}" == "1" ]]; then
    PROMETHEUS_TLS_SERVER_NAME="${PROMETHEUS_TLS_SERVER_NAME:-${PROMETHEUS_URL#https://}}"
    PROMETHEUS_TLS_SERVER_NAME="${PROMETHEUS_TLS_SERVER_NAME%%:*}"
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
    LOKI_TLS_SERVER_NAME="${LOKI_TLS_SERVER_NAME:-${LOKI_URL#https://}}"
    LOKI_TLS_SERVER_NAME="${LOKI_TLS_SERVER_NAME%%:*}"
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
  alloy validate "${temp_file}" || {
    rm -f -- "${temp_file}"
    die "Alloy 配置校验失败，未覆盖现有配置"
  }
  install -o root -g alloy -m 0640 "${temp_file}" "${CONFIG_FILE}"
  rm -f -- "${temp_file}"
  chown alloy:alloy "${DATA_DIR}"
}

install_probe() {
  collect_install_settings
  validate_backend_urls
  install_package
  getent group systemd-journal >/dev/null 2>&1 && usermod -aG systemd-journal alloy
  getent group adm >/dev/null 2>&1 && usermod -aG adm alloy
  install_mtls_files
  write_config
  systemctl enable --now alloy.service
  systemctl restart alloy.service
  systemctl is-active --quiet alloy.service || {
    systemctl --no-pager --full status alloy.service || true
    die "alloy.service 启动失败"
  }
  info "Alloy 已安装，指标发送至 ${PROMETHEUS_URL}，日志发送至 ${LOKI_URL}"
  [[ "${PROMETHEUS_MTLS_ENABLED}" == "1" ]] && info "Prometheus 连接已启用 mTLS"
  [[ "${LOKI_MTLS_ENABLED}" == "1" ]] && info "Loki 连接已启用 mTLS"
}

uninstall_probe() {
  local purge="$1"
  systemctl disable --now alloy.service >/dev/null 2>&1 || true
  if command -v apt-get >/dev/null 2>&1; then
    apt-get remove -y alloy
  elif command -v dnf >/dev/null 2>&1; then
    dnf remove -y alloy
  elif command -v yum >/dev/null 2>&1; then
    yum remove -y alloy
  elif command -v zypper >/dev/null 2>&1; then
    zypper --non-interactive remove alloy
  fi
  if [[ "${purge}" == "1" ]]; then
    rm -rf -- "${CONFIG_DIR}" "${DATA_DIR}"
  fi
  info "Alloy 已卸载（清理数据：$([[ "${purge}" == "1" ]] && printf 是 || printf 否)）"
}

main() {
  require_root
  case "${ACTION}" in
    install) install_probe ;;
    uninstall) uninstall_probe 0 ;;
    purge) uninstall_probe 1 ;;
    help|-h|--help) usage ;;
    *) usage; die "未知操作：${ACTION}" ;;
  esac
}

main
