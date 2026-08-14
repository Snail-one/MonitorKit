#!/usr/bin/env bash

set -Eeuo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
TEST_DIR="$(mktemp -d "${TMPDIR:-/tmp}/monitorkit-installer-test.XXXXXX")"
trap 'rm -rf -- "${TEST_DIR}"' EXIT

FAKE_BIN_DIR="${TEST_DIR}/fake-bin"
FAKE_RELEASE_DIR="${TEST_DIR}/release"
INSTALL_DIR="${TEST_DIR}/install"
mkdir -p "${FAKE_BIN_DIR}" "${FAKE_RELEASE_DIR}" "${INSTALL_DIR}"

case "$(uname -m)" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) printf '跳过：测试架构不受安装器支持\n'; exit 0 ;;
esac

VERSION="v9.9.9-test"
ASSET="snailmon_linux_${ARCH}_${VERSION}"
cat >"${FAKE_RELEASE_DIR}/${ASSET}" <<EOF
#!/bin/sh
if [ "\${1:-}" = "--version" ]; then
  printf 'snailmon ${VERSION}\\ncommit: installer-test\\n'
  exit 0
fi
printf 'fixture binary\\n'
EOF
chmod 0755 "${FAKE_RELEASE_DIR}/${ASSET}"
SHA256="$(sha256sum "${FAKE_RELEASE_DIR}/${ASSET}" | awk '{print $1}')"
printf '%s  %s\n' "${SHA256}" "${ASSET}" >"${FAKE_RELEASE_DIR}/checksums.txt"

cat >"${FAKE_BIN_DIR}/id" <<'EOF'
#!/bin/sh
if [ "${1:-}" = "-u" ]; then
  printf '0\n'
  exit 0
fi
exec /usr/bin/id "$@"
EOF
chmod 0755 "${FAKE_BIN_DIR}/id"

cat >"${FAKE_BIN_DIR}/curl" <<'EOF'
#!/bin/sh
set -eu
output=""
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output|-o) output="$2"; shift 2 ;;
    https://*) url="$1"; shift ;;
    *) shift ;;
  esac
done
[ -n "$output" ] && [ -n "$url" ]
case "$url" in
  */releases/latest) printf '{\n  "tag_name": "%s"\n}\n' "${FAKE_VERSION}" >"$output" ;;
  */checksums.txt) cp "${FAKE_RELEASE_DIR}/checksums.txt" "$output" ;;
  */"${FAKE_ASSET}") cp "${FAKE_RELEASE_DIR}/${FAKE_ASSET}" "$output" ;;
  *) printf 'unexpected URL: %s\n' "$url" >&2; exit 22 ;;
esac
EOF
chmod 0755 "${FAKE_BIN_DIR}/curl"

export FAKE_RELEASE_DIR
export FAKE_ASSET="${ASSET}"
export FAKE_VERSION="${VERSION}"
export PATH="${FAKE_BIN_DIR}:${PATH}"
export NO_COLOR=1

MONITORKIT_INSTALL_DIR="${INSTALL_DIR}" sh "${ROOT_DIR}/scripts/install.sh"
test -x "${INSTALL_DIR}/snailmon"
test "$("${INSTALL_DIR}/snailmon" --version | sed -n '1p')" = "snailmon ${VERSION}"
test "$(sha256sum "${INSTALL_DIR}/snailmon" | awk '{print $1}')" = "${SHA256}"

# 相同版本和哈希再次执行时应安全跳过。
MONITORKIT_INSTALL_DIR="${INSTALL_DIR}" sh "${ROOT_DIR}/scripts/install.sh" "${VERSION}"

MONITORKIT_INSTALL_DIR="${INSTALL_DIR}" sh "${ROOT_DIR}/scripts/install.sh" --uninstall
test ! -e "${INSTALL_DIR}/snailmon"

printf '安装器离线回归测试通过\n'
