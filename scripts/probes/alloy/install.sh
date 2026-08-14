#!/usr/bin/env bash
# Install Grafana Alloy as a unified metrics and logs probe.

set -Eeuo pipefail

readonly CONFIG_DIR="/etc/alloy"
readonly CONFIG_FILE="${CONFIG_DIR}/config.alloy"
readonly DATA_DIR="/var/lib/alloy"

ACTION="${1:-install}"
PROMETHEUS_URL="${PROMETHEUS_URL:-}"
LOKI_URL="${LOKI_URL:-}"

info() { printf '[信息] %s\n' "$*"; }
die() { printf '[错误] %s\n' "$*" >&2; exit 1; }

usage() {
  cat <<'EOF'
Grafana Alloy 指标与日志统一探针安装脚本。

用法：
  sudo PROMETHEUS_URL=http://中心服务器:9090 \
       LOKI_URL=http://中心服务器:3100 ./install.sh
  sudo ./install.sh uninstall   # 保留配置和数据
  sudo ./install.sh purge       # 删除配置和数据

PROMETHEUS_URL 和 LOKI_URL 仅填写服务根地址，脚本会自动追加写入路径。
Alloy 已内置 Unix 主机指标采集；不要在同一服务器重复安装 node_exporter。
EOF
}

require_root() {
  [[ "${EUID}" -eq 0 ]] || die "请使用 root 或 sudo 运行"
}

validate_backend_urls() {
  [[ -n "${PROMETHEUS_URL}" ]] || die "缺少 PROMETHEUS_URL，例如：http://10.0.0.10:9090"
  [[ -n "${LOKI_URL}" ]] || die "缺少 LOKI_URL，例如：http://10.0.0.10:3100"
  [[ "${PROMETHEUS_URL}" =~ ^https?://[A-Za-z0-9._-]+(:[0-9]{1,5})?$ ]] || die "PROMETHEUS_URL 格式无效或包含不安全字符"
  [[ "${LOKI_URL}" =~ ^https?://[A-Za-z0-9._-]+(:[0-9]{1,5})?$ ]] || die "LOKI_URL 格式无效或包含不安全字符"
  PROMETHEUS_URL="${PROMETHEUS_URL%/}"
  LOKI_URL="${LOKI_URL%/}"
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
  local temp_file
  temp_file="$(mktemp)"
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
  }
  external_labels = {
    host = constants.hostname,
  }
}
EOF
  install -o root -g alloy -m 0640 "${temp_file}" "${CONFIG_FILE}"
  rm -f -- "${temp_file}"
  chown alloy:alloy "${DATA_DIR}"
}

install_probe() {
  validate_backend_urls
  install_package
  getent group systemd-journal >/dev/null 2>&1 && usermod -aG systemd-journal alloy
  getent group adm >/dev/null 2>&1 && usermod -aG adm alloy
  write_config
  systemctl enable --now alloy.service
  systemctl restart alloy.service
  systemctl is-active --quiet alloy.service || {
    systemctl --no-pager --full status alloy.service || true
    die "alloy.service 启动失败"
  }
  info "Alloy 已安装，指标发送至 ${PROMETHEUS_URL}，日志发送至 ${LOKI_URL}"
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
