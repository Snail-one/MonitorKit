#!/usr/bin/env bash
# Install or upgrade Prometheus node_exporter as a systemd service.
#
# Optional environment variables:
#   NODE_EXPORTER_VERSION=1.12.1
#   NODE_EXPORTER_LISTEN_ADDRESS=0.0.0.0:9100
#   DOWNLOAD_BASE_URL=https://github.com/prometheus/node_exporter/releases/download

set -Eeuo pipefail

NODE_EXPORTER_VERSION="${NODE_EXPORTER_VERSION:-1.12.1}"
NODE_EXPORTER_LISTEN_ADDRESS="${NODE_EXPORTER_LISTEN_ADDRESS:-0.0.0.0:9100}"
DOWNLOAD_BASE_URL="${DOWNLOAD_BASE_URL:-https://github.com/prometheus/node_exporter/releases/download}"

readonly EXPORTER_USER="node_exporter"
readonly EXPORTER_GROUP="node_exporter"
readonly BIN_DIR="/usr/local/bin"
readonly SERVICE_FILE="/etc/systemd/system/node_exporter.service"

log() {
  printf '[node-exporter-installer] %s\n' "$*"
}

die() {
  printf '[node-exporter-installer] ERROR: %s\n' "$*" >&2
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
    armv6l|armv6) printf 'armv6\n' ;;
    ppc64le) printf 'ppc64le\n' ;;
    *) die "unsupported CPU architecture: $(uname -m)" ;;
  esac
}

validate_version() {
  [[ "$1" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]] || die "invalid NODE_EXPORTER_VERSION: $1"
}

validate_listen_address() {
  local value="$1"
  local port
  if [[ "${value}" =~ ^[A-Za-z0-9._-]*:([0-9]{1,5})$ ]] || \
     [[ "${value}" =~ ^\[[0-9A-Fa-f:.]+\]:([0-9]{1,5})$ ]]; then
    port="${BASH_REMATCH[1]}"
  else
    die "invalid NODE_EXPORTER_LISTEN_ADDRESS: ${value}"
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
ExecStart=${BIN_DIR}/node_exporter --web.listen-address=${NODE_EXPORTER_LISTEN_ADDRESS}
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

main() {
  if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
    usage
    exit 0
  fi
  [[ "$#" -eq 0 ]] || die "unknown argument: $1 (try --help)"

  require_root
  require_commands uname tar sha256sum install getent id groupadd useradd systemctl

  local version="${NODE_EXPORTER_VERSION#v}"
  local arch
  local archive
  local release_url
  local work_dir
  local checksum
  local extracted_dir

  validate_version "${version}"
  validate_listen_address "${NODE_EXPORTER_LISTEN_ADDRESS}"
  arch="$(detect_arch)"
  archive="node_exporter-${version}.linux-${arch}.tar.gz"
  release_url="${DOWNLOAD_BASE_URL%/}/v${version}"
  work_dir="$(mktemp -d -t node-exporter-install.XXXXXXXX)"
  trap 'rm -rf -- "${work_dir}"' EXIT

  log "downloading node_exporter ${version} for linux-${arch}"
  download "${release_url}/${archive}" "${work_dir}/${archive}"
  download "${release_url}/sha256sums.txt" "${work_dir}/sha256sums.txt"

  checksum="$(awk -v file="${archive}" '$2 == file || $2 == ("*" file) { print $1; exit }' "${work_dir}/sha256sums.txt")"
  [[ "${checksum}" =~ ^[0-9a-fA-F]{64}$ ]] || die "no valid checksum found for ${archive}"
  printf '%s  %s\n' "${checksum}" "${work_dir}/${archive}" | sha256sum --check --status || die "checksum verification failed"

  tar -xzf "${work_dir}/${archive}" -C "${work_dir}"
  extracted_dir="${work_dir}/node_exporter-${version}.linux-${arch}"
  [[ -x "${extracted_dir}/node_exporter" ]] || die "release archive is missing the node_exporter binary"

  create_service_account
  install -m 0755 "${extracted_dir}/node_exporter" "${BIN_DIR}/node_exporter"
  write_service_file

  systemctl daemon-reload
  systemctl enable --now node_exporter.service
  systemctl restart node_exporter.service
  systemctl --no-pager --full status node_exporter.service || die "node_exporter service failed to start"

  log "installation complete: http://${NODE_EXPORTER_LISTEN_ADDRESS}/metrics"
  log "service logs: journalctl -u node_exporter -f"

  trap - EXIT
  rm -rf -- "${work_dir}"
}

main "$@"
