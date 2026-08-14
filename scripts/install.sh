#!/bin/sh

set -eu

REPOSITORY="Snail-one/MonitorKit"
INSTALL_DIR="${MONITORKIT_INSTALL_DIR:-/usr/local/bin}"
BINARY_NAME="${MONITORKIT_BINARY_NAME:-monitorkit}"
RELEASE="${MONITORKIT_VERSION:-latest}"
MODE="install"
TEMP_DIR=""
STAGED_FILE=""

RESET=""
BOLD=""
DIM=""
ORANGE=""
BLUE=""
GREEN=""
YELLOW=""
RED=""

init_colors() {
	if [ "${NO_COLOR+set}" = "set" ]; then
		return
	fi
	if [ "${FORCE_COLOR:-0}" != "1" ]; then
		[ "${TERM:-}" != "dumb" ] || return
		[ -t 1 ] || return
	fi
	ESC="$(printf '\033')"
	RESET="${ESC}[0m"
	BOLD="${ESC}[1m"
	DIM="${ESC}[2m"
	ORANGE="${ESC}[38;5;208m"
	BLUE="${ESC}[34m"
	GREEN="${ESC}[32m"
	YELLOW="${ESC}[33m"
	RED="${ESC}[31m"
}

banner() {
	printf '%s%s%s\n' "$BOLD$ORANGE" "╭─ MonitorKit" "$RESET"
	printf '%s│ %s%s\n' "$ORANGE" "$1" "$RESET"
	printf '%s%s%s\n\n' "$ORANGE" "╰────────────────────────────────────────────────────" "$RESET"
}

step() { printf '%s[步骤]%s %s\n' "$ORANGE" "$RESET" "$*"; }
info() { printf '%s[信息]%s %s\n' "$BLUE" "$RESET" "$*"; }
result() { printf '%s[结果]%s %s\n' "$GREEN" "$RESET" "$*"; }
warn() { printf '%s[警告]%s %s\n' "$YELLOW" "$RESET" "$*"; }
fail() { printf '%s[错误]%s %s\n' "$RED" "$RESET" "$*" >&2; exit 1; }

release_card() {
	printf '\n%s%s%s\n' "$BOLD$ORANGE" "╭─ MonitorKit" "$RESET"
	printf '%s│ %s当前版本：%s%s\n' "$ORANGE" "$BLUE" "$RESET" "$1"
	printf '%s│ %s目标版本：%s%s%s%s\n' "$ORANGE" "$BLUE" "$RESET" "$BOLD" "$2" "$RESET"
	printf '%s│ %s执行操作：%s%s%s%s\n' "$ORANGE" "$BLUE" "$RESET" "$BOLD" "$3" "$RESET"
	printf '%s│ %s平台：%sLinux/%s\n' "$ORANGE" "$BLUE" "$RESET" "$ARCH"
	printf '%s%s%s\n' "$ORANGE" "╰────────────────────────────────────────────────────" "$RESET"
}

completion_card() {
	printf '\n%s%s%s\n' "$BOLD$ORANGE" "╭─ MonitorKit" "$RESET"
	printf '%s│ %s%s完成%s\n' "$ORANGE" "$BOLD$GREEN" "$1" "$RESET"
	printf '%s│ %s版本：%s%s%s%s\n' "$ORANGE" "$BLUE" "$RESET" "$BOLD" "$2" "$RESET"
	printf '%s│ %s命令：%s%s\n' "$ORANGE" "$BLUE" "$RESET" "$3"
	printf '%s%s%s\n' "$ORANGE" "╰────────────────────────────────────────────────────" "$RESET"
}

uninstall_completion_card() {
	printf '\n%s%s%s\n' "$BOLD$ORANGE" "╭─ MonitorKit" "$RESET"
	printf '%s│ %s卸载完成%s\n' "$ORANGE" "$BOLD$GREEN" "$RESET"
	printf '%s│ %s原版本：%s%s%s%s\n' "$ORANGE" "$BLUE" "$RESET" "$BOLD" "$1" "$RESET"
	printf '%s│ %s已删除：%s%s\n' "$ORANGE" "$BLUE" "$RESET" "$2"
	printf '%s│ %s已保留：%s%s\n' "$ORANGE" "$BLUE" "$RESET" "Prometheus、Loki、配置和监控数据"
	printf '%s%s%s\n' "$ORANGE" "╰────────────────────────────────────────────────────" "$RESET"
}

usage() {
	cat <<'EOF'
MonitorKit 在线安装、更新与卸载脚本。

用法：
  sudo sh scripts/install.sh                 # 安装或更新最新正式版
  sudo sh scripts/install.sh v1.2.0          # 安装指定版本
  sudo sh scripts/install.sh --uninstall     # 卸载管理程序

可选环境变量：
  MONITORKIT_VERSION       目标版本，默认为 latest
  MONITORKIT_INSTALL_DIR   安装目录，默认为 /usr/local/bin
  MONITORKIT_BINARY_NAME   命令名，默认为 monitorkit

卸载只删除 MonitorKit 管理程序，不删除 Prometheus、Loki、配置或监控数据。
EOF
}

cleanup() {
	if [ -n "$STAGED_FILE" ]; then
		rm -f -- "$STAGED_FILE"
	fi
	if [ -n "$TEMP_DIR" ]; then
		rm -rf -- "$TEMP_DIR"
	fi
}

trap cleanup 0
trap 'exit 1' HUP INT TERM

case "${1:-}" in
	-h|--help) usage; exit 0 ;;
	uninstall|--uninstall) MODE="uninstall" ;;
	"") ;;
	*) RELEASE="$1" ;;
esac

init_colors
if [ "$MODE" = "uninstall" ]; then
	banner "管理程序卸载"
else
	banner "管理程序安装与更新"
fi

case "$BINARY_NAME" in
	""|*/*) fail "命令名不能为空或包含路径分隔符" ;;
esac
case "$INSTALL_DIR" in
	/*) ;;
	*) fail "安装目录必须是绝对路径：$INSTALL_DIR" ;;
esac
TARGET="${INSTALL_DIR}/${BINARY_NAME}"

[ "$(id -u)" -eq 0 ] || fail "该操作需要 root 权限，请使用 sudo 运行"

if [ "$MODE" = "uninstall" ]; then
	step "检查安装状态"
	if [ ! -e "$TARGET" ] && [ ! -L "$TARGET" ]; then
		result "MonitorKit 当前未安装，无需卸载"
		exit 0
	fi
	CURRENT_RELEASE="未知"
	if [ -x "$TARGET" ]; then
		CURRENT_RELEASE="$("$TARGET" --version 2>/dev/null | awk '$1 == "monitorkit" { print $2; exit }' || true)"
		[ -n "$CURRENT_RELEASE" ] || CURRENT_RELEASE="未知"
	fi
	info "当前版本：$CURRENT_RELEASE"
	info "程序路径：$TARGET"
	rm -f -- "$TARGET"
	[ ! -e "$TARGET" ] && [ ! -L "$TARGET" ] || fail "无法删除程序文件：$TARGET"
	uninstall_completion_card "$CURRENT_RELEASE" "$TARGET"
	exit 0
fi

case "$RELEASE" in
	*[!A-Za-z0-9._-]*) fail "版本号包含不支持的字符：$RELEASE" ;;
esac
[ "$(uname -s)" = "Linux" ] || fail "目前仅支持 Linux"

for REQUIRED_COMMAND in awk chmod install mktemp mv sed uname; do
	command -v "$REQUIRED_COMMAND" >/dev/null 2>&1 || fail "缺少必要命令：$REQUIRED_COMMAND"
done

case "$(uname -m)" in
	x86_64|amd64) ARCH="amd64" ;;
	aarch64|arm64) ARCH="arm64" ;;
	*) fail "不支持的处理器架构：$(uname -m)" ;;
esac

if command -v curl >/dev/null 2>&1; then
	download() { curl --fail --location --silent --show-error --retry 3 --connect-timeout 15 --output "$2" "$1"; }
	download_asset() { curl --fail --location --show-error --retry 3 --connect-timeout 15 --progress-bar --output "$2" "$1"; }
	download_direct() { curl --fail --location --silent --show-error --retry 2 --connect-timeout 15 --noproxy '*' --output "$2" "$1"; }
elif command -v wget >/dev/null 2>&1; then
	download() { wget --quiet --tries=3 --timeout=15 --output-document="$2" "$1"; }
	download_asset() { wget --tries=3 --timeout=15 --output-document="$2" "$1"; }
	download_direct() { wget --no-proxy --quiet --tries=2 --timeout=15 --output-document="$2" "$1"; }
else
	fail "需要 curl 或 wget 才能下载安装包"
fi

proxy_configured() {
	[ -n "${HTTPS_PROXY:-}" ] || [ -n "${https_proxy:-}" ] ||
		[ -n "${HTTP_PROXY:-}" ] || [ -n "${http_proxy:-}" ] ||
		[ -n "${ALL_PROXY:-}" ] || [ -n "${all_proxy:-}" ]
}

download_with_fallback() {
	if download "$1" "$2"; then
		return 0
	fi
	proxy_configured || return 1
	warn "通过代理访问 GitHub 失败，正在尝试直连"
	download_direct "$1" "$2"
}

release_from_checksums() {
	awk '
		{
			name = $2
			sub(/^\*/, "", name)
			if (name ~ /^monitorkit_linux_(amd64|arm64)_/) {
				sub(/^monitorkit_linux_(amd64|arm64)_/, "", name)
				if (version == "") version = name
				else if (version != name) mismatch = 1
			}
		}
		END { if (version == "" || mismatch) exit 1; print version }
	' "$1"
}

if command -v sha256sum >/dev/null 2>&1; then
	file_sha256() { sha256sum "$1" | awk '{print $1}'; }
elif command -v shasum >/dev/null 2>&1; then
	file_sha256() { shasum -a 256 "$1" | awk '{print $1}'; }
else
	fail "需要 sha256sum 或 shasum 才能校验发布文件"
fi

TEMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/monitorkit-install.XXXXXX")"
step "检查发布版本"
if [ "$RELEASE" = "latest" ]; then
	RELEASE_METADATA="${TEMP_DIR}/release.json"
	RELEASE_VERSION=""
	if download_with_fallback "https://api.github.com/repos/${REPOSITORY}/releases/latest" "$RELEASE_METADATA"; then
		RELEASE_VERSION="$(sed -n 's/^[[:space:]]*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' "$RELEASE_METADATA" | sed -n '1p')"
	fi
	if [ -n "$RELEASE_VERSION" ]; then
		info "最新正式版本：$RELEASE_VERSION"
	else
		warn "Releases API 不可用，正在从 checksums.txt 解析版本"
		CHECKSUM_FILE="${TEMP_DIR}/checksums.txt"
		download_with_fallback "https://github.com/${REPOSITORY}/releases/latest/download/checksums.txt" "$CHECKSUM_FILE" || fail "无法获取最新版本"
		RELEASE_VERSION="$(release_from_checksums "$CHECKSUM_FILE")" || fail "checksums.txt 中没有唯一有效版本"
		info "最新正式版本：$RELEASE_VERSION"
	fi
	LATEST_RELEASE="true"
else
	RELEASE_VERSION="$RELEASE"
	LATEST_RELEASE="false"
	info "指定发布版本：$RELEASE_VERSION"
fi
case "$RELEASE_VERSION" in
	*[!A-Za-z0-9._-]*) fail "发布版本包含不支持的字符：$RELEASE_VERSION" ;;
esac

ASSET="monitorkit_linux_${ARCH}_${RELEASE_VERSION}"
DOWNLOAD_BASE="https://github.com/${REPOSITORY}/releases/download/${RELEASE_VERSION}"
ASSET_FILE="${TEMP_DIR}/${ASSET}"
CHECKSUM_FILE="${TEMP_DIR}/checksums.txt"

if [ ! -s "$CHECKSUM_FILE" ]; then
	download_with_fallback "${DOWNLOAD_BASE}/checksums.txt" "$CHECKSUM_FILE" || fail "无法下载 checksums.txt"
fi
EXPECTED_SHA256="$(awk -v asset="$ASSET" '$2 == asset || $2 == "*" asset { print $1; exit }' "$CHECKSUM_FILE")"
case "$EXPECTED_SHA256" in
	????????????????????????????????????????????????????????????????) ;;
	*) fail "checksums.txt 中没有 ${ASSET} 的有效 SHA-256" ;;
esac

CURRENT_DISPLAY="未安装"
CURRENT_RELEASE=""
if [ -x "$TARGET" ]; then
	CURRENT_RELEASE="$("$TARGET" --version 2>/dev/null | awk '$1 == "monitorkit" { print $2; exit }' || true)"
	CURRENT_DISPLAY="${CURRENT_RELEASE:-未知}"
fi

if [ "$CURRENT_RELEASE" = "$RELEASE_VERSION" ]; then
	CURRENT_SHA256="$(file_sha256 "$TARGET")"
	if [ "$CURRENT_SHA256" = "$EXPECTED_SHA256" ]; then
		release_card "$CURRENT_DISPLAY" "$RELEASE_VERSION" "无需更新"
		if [ "$LATEST_RELEASE" = "true" ]; then
			result "当前已是最新正式版本"
		else
			result "当前已是指定版本"
		fi
		exit 0
	fi
	ACTION="修复安装"
	warn "版本一致但本地文件校验失败，将重新下载安装"
elif [ -e "$TARGET" ] || [ -L "$TARGET" ]; then
	ACTION="更新"
else
	ACTION="安装"
fi

release_card "$CURRENT_DISPLAY" "$RELEASE_VERSION" "$ACTION"
printf '\n'
step "下载发布文件"
info "正在下载：$ASSET"
download_asset "${DOWNLOAD_BASE}/${ASSET}" "$ASSET_FILE"

step "校验发布文件"
ACTUAL_SHA256="$(file_sha256 "$ASSET_FILE")"
[ "$ACTUAL_SHA256" = "$EXPECTED_SHA256" ] || fail "发布文件 SHA-256 校验失败"
chmod 0755 "$ASSET_FILE"
DOWNLOADED_RELEASE="$("$ASSET_FILE" --version 2>/dev/null | awk '$1 == "monitorkit" { print $2; exit }' || true)"
[ "$DOWNLOADED_RELEASE" = "$RELEASE_VERSION" ] || fail "下载文件版本不匹配：${DOWNLOADED_RELEASE:-无法识别}"
result "版本与 SHA-256 校验通过"

step "原子安装程序"
install -d -m 0755 "$INSTALL_DIR"
STAGED_FILE="${INSTALL_DIR}/.${BINARY_NAME}.new.$$"
install -m 0755 "$ASSET_FILE" "$STAGED_FILE"
mv -f "$STAGED_FILE" "$TARGET"
STAGED_FILE=""

completion_card "$ACTION" "$RELEASE_VERSION" "$TARGET"
