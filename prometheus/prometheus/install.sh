#!/usr/bin/env bash
# Install or upgrade Prometheus as a systemd service.
#
# Optional environment variables:
#   PROMETHEUS_VERSION=3.13.1
#   PROMETHEUS_LISTEN_ADDRESS=0.0.0.0:9090
#   DOWNLOAD_BASE_URL=https://github.com/prometheus/prometheus/releases/download

set -Eeuo pipefail

PROMETHEUS_VERSION="${PROMETHEUS_VERSION:-3.13.1}"
PROMETHEUS_LISTEN_ADDRESS="${PROMETHEUS_LISTEN_ADDRESS:-0.0.0.0:9090}"
DOWNLOAD_BASE_URL="${DOWNLOAD_BASE_URL:-https://github.com/prometheus/prometheus/releases/download}"

readonly PROMETHEUS_USER="prometheus"
readonly PROMETHEUS_GROUP="prometheus"
readonly CONFIG_DIR="/etc/prometheus"
readonly DATA_DIR="/var/lib/prometheus"
readonly BIN_DIR="/usr/local/bin"
readonly SERVICE_FILE="/etc/systemd/system/prometheus.service"

log() {
  printf '[prometheus-installer] %s\n' "$*"
}

die() {
  printf '[prometheus-installer] ERROR: %s\n' "$*" >&2
  exit 1
}

usage() {
  sed -n '2,8p' "$0" | sed 's/^# \{0,1\}//'
}

require_root() {
  [[ "${EUID}" -eq 0 ]] || die "please run this script as root"
}

require_commands() {
  local command_name
  for command_name in "$@"; do
    command -v "${command_name}" >/dev/null 2>&1 || die "required command not found: ${command_name}"
  done
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) printf 'amd64\n' ;;
    aarch64|arm64) printf 'arm64\n' ;;
    armv7l|armv7) printf 'armv7\n' ;;
    ppc64le) printf 'ppc64le\n' ;;
    s390x) printf 's390x\n' ;;
    *) die "unsupported CPU architecture: $(uname -m)" ;;
  esac
}

validate_version() {
  [[ "$1" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]] || die "invalid PROMETHEUS_VERSION: $1"
}

validate_listen_address() {
  local value="$1"
  local port
  if [[ "${value}" =~ ^[A-Za-z0-9._-]*:([0-9]{1,5})$ ]] || \
     [[ "${value}" =~ ^\[[0-9A-Fa-f:.]+\]:([0-9]{1,5})$ ]]; then
    port="${BASH_REMATCH[1]}"
  else
    die "invalid PROMETHEUS_LISTEN_ADDRESS: ${value}"
  fi
  (( port >= 1 && port <= 65535 )) || die "listen port must be between 1 and 65535"
}

download() {
  local url="$1"
  local destination="$2"
  if command -v curl >/dev/null 2>&1; then
    curl --fail --location --retry 3 --retry-delay 2 --output "${destination}" "${url}"
  elif command -v wget >/dev/null 2>&1; then
    wget --tries=3 --output-document="${destination}" "${url}"
  else
    die "curl or wget is required"
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
  if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
    usage
    exit 0
  fi
  [[ "$#" -eq 0 ]] || die "unknown argument: $1 (try --help)"

  require_root
  require_commands uname tar sha256sum install getent id groupadd useradd systemctl

  local version="${PROMETHEUS_VERSION#v}"
  local arch
  local archive
  local release_url
  local work_dir
  local checksum
  local extracted_dir

  validate_version "${version}"
  validate_listen_address "${PROMETHEUS_LISTEN_ADDRESS}"
  arch="$(detect_arch)"
  archive="prometheus-${version}.linux-${arch}.tar.gz"
  release_url="${DOWNLOAD_BASE_URL%/}/v${version}"
  work_dir="$(mktemp -d -t prometheus-install.XXXXXXXX)"
  trap 'rm -rf -- "${work_dir}"' EXIT

  log "downloading Prometheus ${version} for linux-${arch}"
  download "${release_url}/${archive}" "${work_dir}/${archive}"
  download "${release_url}/sha256sums.txt" "${work_dir}/sha256sums.txt"

  checksum="$(awk -v file="${archive}" '$2 == file || $2 == ("*" file) { print $1; exit }' "${work_dir}/sha256sums.txt")"
  [[ "${checksum}" =~ ^[0-9a-fA-F]{64}$ ]] || die "no valid checksum found for ${archive}"
  printf '%s  %s\n' "${checksum}" "${work_dir}/${archive}" | sha256sum --check --status || die "checksum verification failed"

  tar -xzf "${work_dir}/${archive}" -C "${work_dir}"
  extracted_dir="${work_dir}/prometheus-${version}.linux-${arch}"
  [[ -x "${extracted_dir}/prometheus" && -x "${extracted_dir}/promtool" ]] || die "release archive is missing expected binaries"

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
    log "created default configuration at ${CONFIG_DIR}/prometheus.yml"
  else
    log "keeping existing configuration at ${CONFIG_DIR}/prometheus.yml"
  fi
  "${BIN_DIR}/promtool" check config "${CONFIG_DIR}/prometheus.yml"

  write_service_file
  chown -R root:"${PROMETHEUS_GROUP}" "${CONFIG_DIR}"
  chmod 0750 "${CONFIG_DIR}"

  systemctl daemon-reload
  systemctl enable --now prometheus.service
  systemctl restart prometheus.service
  systemctl --no-pager --full status prometheus.service || die "Prometheus service failed to start"

  log "installation complete: http://${PROMETHEUS_LISTEN_ADDRESS}/"
  log "service logs: journalctl -u prometheus -f"

  trap - EXIT
  rm -rf -- "${work_dir}"
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

main "$@"
