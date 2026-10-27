#!/bin/bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"
WEB_DIR="$ROOT_DIR/autocarto-web"
TAURI_DIR="$WEB_DIR/src-tauri"
AGENT_DIR="$ROOT_DIR/carto_agent"
PLUGIN_DIR="$ROOT_DIR/autocarto"
BINARIES_DIR="$TAURI_DIR/binaries"
PACKAGE_LOCK_FILE="$WEB_DIR/package-lock.json"
NODE_MODULES_DIR="$WEB_DIR/node_modules"
TAURI_CLI_BIN="$NODE_MODULES_DIR/.bin/tauri"
NEXT_BIN="$NODE_MODULES_DIR/.bin/next"
PLAYWRIGHT_BIN="$NODE_MODULES_DIR/.bin/playwright"
DEPS_STAMP_FILE="$WEB_DIR/.deps-lock.sha256"
DIST_DIR="$ROOT_DIR/dist"

CHECK_EVIDENCE_RELATIVE=".sisyphus/evidence/task-10-build-check.txt"
CHECK_PACKAGED_EVIDENCE_RELATIVE=".sisyphus/evidence/task-10-build-check-packaged.txt"
CHECK_LIVE_EVIDENCE_RELATIVE=".sisyphus/evidence/task-10-build-check-live.txt"
CHECK_FAILURE_ARCHIVE_RELATIVE=".sisyphus/evidence/"
PACKAGED_SMOKE_SCRIPT_PATH="$WEB_DIR/scripts/packaged_runtime_smoke.py"
PACKAGED_SMOKE_EVIDENCE_RELATIVE=".sisyphus/evidence/task-6-packaged-runtime-smoke"
PACKAGED_SMOKE_GATE_EVIDENCE_RELATIVE=".sisyphus/evidence/task-7-release-gate"
PACKAGED_SMOKE_GATE_SUMMARY_NAME="summary.txt"
PACKAGED_SMOKE_RUNTIME_HOME_NAME="runtime-home"
PACKAGED_SMOKE_CONTRACT_VERSION="v1"
PACKAGED_SMOKE_TIMEOUT_SEC="${AUTOCARTO_PACKAGED_SMOKE_TIMEOUT_SEC:-60}"
PACKAGED_SMOKE_PRODUCTION_BUNDLE_NOTE="Existing production bundle Playwright evidence stays separate and does not replace packaged smoke."
CHECK_LIVE_E2E_SPEC="tests/e2e/live-qgis-integration.spec.ts"
declare -a CHECK_HEADLESS_E2E_SPECS=(
    "tests/e2e/backend-reconnect.spec.ts"
    "tests/e2e/diagnostics-page.spec.ts"
    "tests/e2e/services-page.spec.ts"
    "tests/e2e/flow-center.spec.ts"
    "tests/e2e/flow-run.spec.ts"
    "tests/e2e/session-workspace.spec.ts"
)

APP_VERSION=""
PRODUCT_NAME=""
PYTHON_BIN="${PYTHON_BIN:-}"

GO_VERSION_MIN="unknown"
GO_TOOLCHAIN_REQUIRED_RAW=""
GO_TOOLCHAIN_REQUIRED=""
export GOTOOLCHAIN=local
: "${GOPROXY:=https://goproxy.cn,direct}"
: "${GOSUMDB:=sum.golang.google.cn}"
export GOPROXY
export GOSUMDB

ARCH="$(uname -m)"
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
    darwin)
        GOOS_TARGET="darwin"
        QGIS_DEFAULT_DIR="$HOME/Library/Application Support/QGIS/QGIS3/profiles/default/python/plugins"
        DESKTOP_ARTIFACT_HINT="$TAURI_DIR/target/release/bundle/macos/"
        case "$ARCH" in
            x86_64) TARGET_TRIPLE="x86_64-apple-darwin"; GOARCH_TARGET="amd64" ;;
            arm64|aarch64) TARGET_TRIPLE="aarch64-apple-darwin"; GOARCH_TARGET="arm64" ;;
            *) echo "不支持的架构: $ARCH"; exit 1 ;;
        esac
        ;;
    linux)
        GOOS_TARGET="linux"
        QGIS_DEFAULT_DIR="$HOME/.local/share/QGIS/QGIS3/profiles/default/python/plugins"
        DESKTOP_ARTIFACT_HINT="$TAURI_DIR/target/release/bundle/"
        case "$ARCH" in
            x86_64) TARGET_TRIPLE="x86_64-unknown-linux-gnu"; GOARCH_TARGET="amd64" ;;
            arm64|aarch64) TARGET_TRIPLE="aarch64-unknown-linux-gnu"; GOARCH_TARGET="arm64" ;;
            *) echo "不支持的架构: $ARCH"; exit 1 ;;
        esac
        ;;
    *)
        echo "不支持的系统: $OS"
        exit 1
        ;;
esac

if [ "$OS" = "darwin" ]; then
    for rust_bin_dir in "$HOME/.cargo/bin" "/opt/homebrew/opt/rustup/bin" "/usr/local/opt/rustup/bin"; do
        if [ -d "$rust_bin_dir" ]; then
            case ":$PATH:" in
                *":$rust_bin_dir:"*) ;;
                *) export PATH="$rust_bin_dir:$PATH" ;;
            esac
        fi
    done
fi

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

info()  { echo -e "${BLUE}[INFO]${NC} $1"; }
ok()    { echo -e "${GREEN}[OK]${NC} $1"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $1"; }
fail()  { echo -e "${RED}[FAIL]${NC} $1"; exit 1; }
step()  { echo -e "\n${BLUE}━━ $1 ━━${NC}"; }

declare -a PREFLIGHT_ERRORS=()
QGIS_PLUGINS_DIR=""

add_preflight_error() {
    PREFLIGHT_ERRORS+=("$1")
}

detect_python() {
    if [ -n "$PYTHON_BIN" ]; then
        return 0
    fi
    if [ -x "/Applications/QGIS-LTR.app/Contents/MacOS/python" ]; then
        PYTHON_BIN="/Applications/QGIS-LTR.app/Contents/MacOS/python"
    elif [ -x "/Applications/QGIS.app/Contents/MacOS/python" ]; then
        PYTHON_BIN="/Applications/QGIS.app/Contents/MacOS/python"
    elif command -v python3 >/dev/null 2>&1; then
        PYTHON_BIN="python3"
    elif command -v python >/dev/null 2>&1; then
        PYTHON_BIN="python"
    else
        PYTHON_BIN="python3"
    fi
}

load_go_requirements() {
    local go_mod_path="$AGENT_DIR/go.mod"
    GO_VERSION_MIN="unknown"
    GO_TOOLCHAIN_REQUIRED_RAW=""
    GO_TOOLCHAIN_REQUIRED=""

    if [ ! -f "$go_mod_path" ]; then
        return 0
    fi

    GO_VERSION_MIN="$(awk '/^go / {print $2; exit}' "$go_mod_path")"
    GO_TOOLCHAIN_REQUIRED_RAW="$(awk '/^toolchain / {print $2; exit}' "$go_mod_path")"
    GO_TOOLCHAIN_REQUIRED="${GO_TOOLCHAIN_REQUIRED_RAW#go}"

    [ -n "$GO_VERSION_MIN" ] || GO_VERSION_MIN="unknown"
}

load_app_metadata() {
    local tauri_conf="$TAURI_DIR/tauri.conf.json"
    if [ ! -f "$tauri_conf" ]; then
        APP_VERSION="0.0.0"
        PRODUCT_NAME="AutoCartoAI"
        return 0
    fi

    APP_VERSION="$("$PYTHON_BIN" - <<'PY' "$tauri_conf"
import json, sys
with open(sys.argv[1], 'r', encoding='utf-8') as fh:
    data = json.load(fh)
print(str(data.get('version', '0.0.0')).strip())
PY
)"
    PRODUCT_NAME="$("$PYTHON_BIN" - <<'PY' "$tauri_conf"
import json, re, sys
with open(sys.argv[1], 'r', encoding='utf-8') as fh:
    data = json.load(fh)
name = str(data.get('productName', 'AutoCartoAI')).strip() or 'AutoCartoAI'
slug = re.sub(r'[^A-Za-z0-9._-]+', '-', name).strip('-') or 'AutoCartoAI'
print(slug)
PY
)"
    [ -n "$APP_VERSION" ] || APP_VERSION="0.0.0"
    [ -n "$PRODUCT_NAME" ] || PRODUCT_NAME="AutoCartoAI"
}

sync_plugin_metadata_version() {
    local metadata_path="$PLUGIN_DIR/metadata.txt"
    [ -f "$metadata_path" ] || return 0
    "$PYTHON_BIN" - <<'PY' "$metadata_path" "$APP_VERSION"
import sys
path, version = sys.argv[1], sys.argv[2]
with open(path, 'r', encoding='utf-8') as fh:
    lines = fh.readlines()
updated = []
replaced = False
for line in lines:
    if line.startswith('version='):
        updated.append(f'version={version}\n')
        replaced = True
    else:
        updated.append(line)
if not replaced:
    updated.append(f'version={version}\n')
with open(path, 'w', encoding='utf-8') as fh:
    fh.writelines(updated)
PY
}

plugin_archive_name() {
    echo "autocarto-plugin-${APP_VERSION}-${GOOS_TARGET}-${GOARCH_TARGET}.zip"
}

build_plugin_archive() {
    step "打包 QGIS 插件"
    mkdir -p "$DIST_DIR"
    sync_plugin_metadata_version
    local archive_path="$DIST_DIR/$(plugin_archive_name)"
    "$PYTHON_BIN" - <<'PY' "$ROOT_DIR" "$archive_path"
import os, sys, zipfile
from pathlib import Path

root = Path(sys.argv[1])
archive = Path(sys.argv[2])
targets = [root / 'autocarto']
exclude_dirs = {'__pycache__', 'logs', '.session'}
exclude_suffixes = {'.pyc'}

with zipfile.ZipFile(archive, 'w', compression=zipfile.ZIP_DEFLATED) as zf:
    for base in targets:
        for path in base.rglob('*'):
            rel = path.relative_to(root)
            parts = set(rel.parts)
            if parts & exclude_dirs:
                continue
            plugin_rel = path.relative_to(base)
            if plugin_rel.parts[0] == 'data':
                continue
            if path.name in {'.master_key', 'secrets.enc', '.port'}:
                continue
            if plugin_rel.as_posix() == 'config/plugin_config.yaml':
                continue
            if path.suffix in {'.db', '.sqlite'} or path.name.endswith(('.db-wal', '.db-shm')):
                continue
            if path.suffix in exclude_suffixes:
                continue
            if path.is_dir():
                continue
            zf.write(path, rel.as_posix())
print(archive)
PY
    ok "插件压缩包已生成：$archive_path"
}

copy_artifact_if_exists() {
    local source_path="$1"
    local target_path="$2"
    if [ -e "$source_path" ]; then
        mkdir -p "$(dirname "$target_path")"
        rm -rf "$target_path"
        cp -R "$source_path" "$target_path"
        info "已归档产物：$target_path"
    fi
}

collect_dist_artifacts() {
    step "整理 dist 产物"
    mkdir -p "$DIST_DIR"
    local bundle_dir="$TAURI_DIR/target/release/bundle"
    if [ "$OS" = "linux" ]; then
        for artifact in "$bundle_dir"/deb/*.deb; do
            [ -e "$artifact" ] || continue
            copy_artifact_if_exists "$artifact" "$DIST_DIR/${PRODUCT_NAME}-${APP_VERSION}-${GOOS_TARGET}-${GOARCH_TARGET}.deb"
            break
        done
        for artifact in "$bundle_dir"/appimage/*.AppImage; do
            [ -e "$artifact" ] || continue
            copy_artifact_if_exists "$artifact" "$DIST_DIR/${PRODUCT_NAME}-${APP_VERSION}-${GOOS_TARGET}-${GOARCH_TARGET}.AppImage"
            break
        done
    elif [ "$OS" = "darwin" ]; then
        for artifact in "$bundle_dir"/macos/*.app; do
            [ -e "$artifact" ] || continue
            copy_artifact_if_exists "$artifact" "$DIST_DIR/${PRODUCT_NAME}-${APP_VERSION}-${GOOS_TARGET}-${GOARCH_TARGET}.app"
            break
        done
        for artifact in "$bundle_dir"/dmg/*.dmg; do
            [ -e "$artifact" ] || continue
            copy_artifact_if_exists "$artifact" "$DIST_DIR/${PRODUCT_NAME}-${APP_VERSION}-${GOOS_TARGET}-${GOARCH_TARGET}.dmg"
            break
        done
    fi
}

ensure_rust_path() {
    if ! command -v cargo >/dev/null 2>&1 && [ -x "$HOME/.cargo/bin/cargo" ]; then
        export PATH="$HOME/.cargo/bin:$PATH"
    fi
    if ! command -v rustc >/dev/null 2>&1 && [ -x "$HOME/.cargo/bin/rustc" ]; then
        export PATH="$HOME/.cargo/bin:$PATH"
    fi
}

version_ge() {
    local left="$1"
    local right="$2"
    local IFS=.
    local i
    local -a left_parts=($left)
    local -a right_parts=($right)
    local max_len=${#left_parts[@]}

    if [ ${#right_parts[@]} -gt "$max_len" ]; then
        max_len=${#right_parts[@]}
    fi

    for ((i = 0; i < max_len; i++)); do
        local left_num=${left_parts[i]:-0}
        local right_num=${right_parts[i]:-0}
        if ((10#$left_num > 10#$right_num)); then
            return 0
        fi
        if ((10#$left_num < 10#$right_num)); then
            return 1
        fi
    done

    return 0
}

require_dir() {
    local path="$1"
    local guidance="$2"
    if [ ! -d "$path" ]; then
        add_preflight_error "${guidance}（缺少目录: $path）"
    fi
}

require_file() {
    local path="$1"
    local guidance="$2"
    if [ ! -f "$path" ]; then
        add_preflight_error "${guidance}（缺少文件: $path）"
    fi
}

require_command() {
    local name="$1"
    local guidance="$2"
    if ! command -v "$name" >/dev/null 2>&1; then
        add_preflight_error "$guidance"
    fi
    return 0
}

check_repo_layout() {
    local command_name="$1"

    case "$command_name" in
        build|dev|dist|plugin|app|dmg|deb|appimage|check)
            require_dir "$PLUGIN_DIR/flows/system" "缺少内置流程目录，无法完整打包运行资产"
            require_dir "$PLUGIN_DIR/resources/system" "缺少内置资源库目录，无法完整打包运行资产"
            ;;
    esac

    case "$command_name" in
        deps)
            require_dir "$WEB_DIR" "缺少前端目录，无法安装依赖"
            require_file "$PACKAGE_LOCK_FILE" "缺少 package-lock.json，无法安全安装前端依赖；请先在 autocarto-web/ 生成锁文件"
            ;;
        go)
            require_dir "$AGENT_DIR" "缺少 Go 后端目录，无法编�?sidecar"
            require_dir "$TAURI_DIR" "缺少 Tauri 目录，无法放�?sidecar 产物"
            require_file "$AGENT_DIR/go.mod" "缺少 carto_agent/go.mod，无法读�?Go toolchain 要求"
            ;;
        check)
            require_dir "$WEB_DIR" "缺少前端目录，无法执行 Next.js/Tauri 构建检查"
            require_dir "$TAURI_DIR" "缺少 Tauri 目录，无法执行桌面端检查"
            require_dir "$AGENT_DIR" "缺少 Go 后端目录，无法执行 Go 检查"
            require_file "$PACKAGE_LOCK_FILE" "缺少 package-lock.json，无法安全校验前端依赖；请先在 autocarto-web/ 生成锁文件"
            require_file "$AGENT_DIR/go.mod" "缺少 carto_agent/go.mod，无法读�?Go toolchain 要求"
            require_file "$TAURI_DIR/Cargo.toml" "缺少 Cargo.toml，无法执行 Tauri 编译检查"
            ;;
        packaged-smoke)
            require_dir "$WEB_DIR" "缺少前端目录，无法执�?packaged smoke"
            require_dir "$TAURI_DIR" "缺少 Tauri 目录，无法解析打包产物"
            require_file "$TAURI_DIR/tauri.conf.json" "缺少 tauri.conf.json，无法确认打包产物命名"
            require_file "$PACKAGED_SMOKE_SCRIPT_PATH" "缺少 packaged runtime smoke 脚本，无法执行真实产物验证"
            ;;
        build|dev|dist)
            require_dir "$WEB_DIR" "缺少前端目录，无法执行 Next.js/Tauri 构建"
            require_dir "$TAURI_DIR" "缺少 Tauri 目录，无法打包桌面应用"
            require_dir "$AGENT_DIR" "缺少 Go 后端目录，无法编�?sidecar"
            require_dir "$PLUGIN_DIR" "缺少 autocarto 插件目录，无法部署插件"
            require_file "$PACKAGE_LOCK_FILE" "缺少 package-lock.json，无法安全校验前端依赖；请先在 autocarto-web/ 生成锁文件"
            require_file "$AGENT_DIR/go.mod" "缺少 carto_agent/go.mod，无法读�?Go toolchain 要求"
            require_file "$TAURI_DIR/Cargo.toml" "缺少 Cargo.toml，无法执�?Tauri 构建"
            require_file "$TAURI_DIR/tauri.conf.json" "缺少 tauri.conf.json，无法确�?sidecar 打包契约"
            ;;
        app|dmg|deb|appimage)
            require_dir "$WEB_DIR" "缺少前端目录，无法执行 Next.js/Tauri 构建"
            require_dir "$TAURI_DIR" "缺少 Tauri 目录，无法打包桌面应用"
            require_dir "$AGENT_DIR" "缺少 Go 后端目录，无法编�?sidecar"
            require_file "$PACKAGE_LOCK_FILE" "缺少 package-lock.json，无法安全校验前端依赖；请先在 autocarto-web/ 生成锁文件"
            require_file "$AGENT_DIR/go.mod" "缺少 carto_agent/go.mod，无法读�?Go toolchain 要求"
            require_file "$TAURI_DIR/Cargo.toml" "缺少 Cargo.toml，无法执�?Tauri 构建"
            require_file "$TAURI_DIR/tauri.conf.json" "缺少 tauri.conf.json，无法确�?sidecar 打包契约"
            ;;
        plugin)
            require_dir "$PLUGIN_DIR" "缺少 autocarto 插件目录，无法打包插件"
            require_file "$PLUGIN_DIR/metadata.txt" "缺少 autocarto/metadata.txt，无法同步插件版本"
            require_file "$TAURI_DIR/tauri.conf.json" "缺少 tauri.conf.json，无法读取统一版本"
            ;;
    esac
}

check_go_toolchain() {
    local required_go_version="$GO_VERSION_MIN"
    local recommended_go_version="${GO_TOOLCHAIN_REQUIRED:-$GO_VERSION_MIN}"

    if ! command -v go >/dev/null 2>&1; then
        if [ "$required_go_version" = "unknown" ]; then
            add_preflight_error "缺少 Go。请安装仓库要求的 Go 工具链后重试：https://go.dev/dl/"
        else
            add_preflight_error "缺少 Go。请安装 Go >= ${required_go_version}（推荐 ${recommended_go_version}）后重试：https://go.dev/dl/"
        fi
        return
    fi

    local installed_go_version
    installed_go_version="$(go version | awk '{print $3}' | sed 's/^go//')"

    if [ "$required_go_version" != "unknown" ] && ! version_ge "$installed_go_version" "$required_go_version"; then
        add_preflight_error "当前 Go 版本为 ${installed_go_version}，低于仓库要求的 ${required_go_version}。请升级 Go 后重试"
    fi

    if [ -n "$GO_TOOLCHAIN_REQUIRED" ] && ! version_ge "$installed_go_version" "$GO_TOOLCHAIN_REQUIRED"; then
        add_preflight_error "当前 Go 版本为 ${installed_go_version}，低于仓库 toolchain 要求 ${GO_TOOLCHAIN_REQUIRED}。脚本已禁用自动下载工具链，请手动安装后重试"
    fi
}

check_go_cgo_prereqs() {
    if [ "$OS" = "darwin" ]; then
        if ! xcode-select -p >/dev/null 2>&1; then
            add_preflight_error "Go sidecar 使用�?go-sqlite3，macOS 需�?Xcode Command Line Tools。请先运行：xcode-select --install"
        fi
        return 0
    fi

    require_command "cc" "Go sidecar 使用 go-sqlite3，Linux 需要可用的 C 编译器。请先安装 build-essential/gcc（Debian/Ubuntu）、base-devel（Arch）或 gcc（Fedora）后重试"
}

check_node_toolchain() {
    require_command "node" "缺少 Node.js。请先安�?Node.js 后重试：https://nodejs.org/"
    require_command "npm" "缺少 npm。请确认 Node.js 安装完整后重试"
}

check_rust_toolchain() {
    ensure_rust_path
    require_command "rustc" "缺少 Rust 编译�?rustc。请使用 rustup 安装 Rust 后重试：https://www.rust-lang.org/tools/install"
    require_command "cargo" "缺少 Cargo。请使用 rustup 安装 Rust 工具链后重试：https://www.rust-lang.org/tools/install"
}

check_frontend_deps() {
    require_dir "$NODE_MODULES_DIR" "缺少 autocarto-web/node_modules。请先运行：./build.sh deps"
    require_file "$TAURI_CLI_BIN" "缺少本地 Tauri CLI。请先运行：./build.sh deps"
    require_file "$NEXT_BIN" "缺少本地 Next CLI。请先运行：./build.sh deps"
}

check_frontend_check_deps() {
    require_dir "$NODE_MODULES_DIR" "缺少 autocarto-web/node_modules。请先运行：./build.sh deps"
    require_file "$NEXT_BIN" "缺少本地 Next CLI。请先运行：./build.sh deps"
    require_file "$PLAYWRIGHT_BIN" "缺少本地 Playwright CLI。请先运行：./build.sh deps"
}

check_linux_tauri_prereqs() {
    [ "$OS" = "linux" ] || return 0

    if ! command -v pkg-config >/dev/null 2>&1; then
        add_preflight_error "Linux 构建 Tauri 需要 pkg-config。请先安装 pkg-config 以及 Tauri 官方依赖（例如 Debian/Ubuntu: libwebkit2gtk-4.1-dev、libssl-dev、libayatana-appindicator3-dev、librsvg2-dev、libxdo-dev）"
        return 0
    fi

    if ! pkg-config --exists webkit2gtk-4.1; then
        add_preflight_error "缺少 Linux Tauri 依赖 webkit2gtk-4.1。请安装官方依赖包（Debian/Ubuntu: libwebkit2gtk-4.1-dev；Fedora: webkit2gtk4.1-devel；Arch: webkit2gtk-4.1）"
    fi
    if ! pkg-config --exists openssl; then
        add_preflight_error "缺少 OpenSSL 开发库。请安装官方依赖包（Debian/Ubuntu: libssl-dev；Fedora: openssl-devel；Arch: openssl）"
    fi
    if ! pkg-config --exists librsvg-2.0; then
        add_preflight_error "缺少 librsvg 开发库。请安装官方依赖包（Debian/Ubuntu: librsvg2-dev；Fedora: librsvg2-devel；Arch: librsvg）"
    fi
    if ! pkg-config --exists xdo && ! pkg-config --exists libxdo \
        && [ ! -f "/usr/include/xdo.h" ] \
        && [ ! -f "/usr/lib/x86_64-linux-gnu/libxdo.so" ] \
        && [ ! -f "/usr/lib64/libxdo.so" ]; then
        add_preflight_error "缺少 xdo/libxdo 开发库。请安装官方依赖包（Debian/Ubuntu: libxdo-dev；Fedora: libxdo-devel；Arch: xdotool）"
    fi
    if ! pkg-config --exists ayatana-appindicator3-0.1 && ! pkg-config --exists appindicator3-0.1; then
        add_preflight_error "缺少 appindicator 开发库。请安装官方依赖包（Debian/Ubuntu: libayatana-appindicator3-dev；Fedora: libappindicator-gtk3-devel；Arch: libappindicator-gtk3）"
    fi
}

check_macos_tauri_prereqs() {
    [ "$OS" = "darwin" ] || return 0
    if ! xcode-select -p >/dev/null 2>&1; then
        add_preflight_error "macOS 构建 Tauri 需�?Xcode Command Line Tools。请先运行：xcode-select --install"
    fi
}

resolve_qgis_plugins_dir() {
    local candidate
    local parent_dir
    if [ "$OS" = "darwin" ]; then
        candidate="$HOME/Library/Application Support/QGIS/QGIS3/profiles/default/python/plugins"
        parent_dir="$(dirname "$candidate")"
        if [ -d "$candidate" ] || [ -d "$parent_dir" ]; then
            echo "$candidate"
            return 0
        fi
        echo "$candidate"
        return 0
    fi

    for candidate in \
        "$HOME/.local/share/QGIS/QGIS3/profiles/default/python/plugins" \
        "$HOME/.var/app/org.qgis.qgis/data/QGIS/QGIS3/profiles/default/python/plugins"
    do
        parent_dir="$(dirname "$candidate")"
        if [ -d "$candidate" ] || [ -d "$parent_dir" ]; then
            echo "$candidate"
            return 0
        fi
    done

    echo "$QGIS_DEFAULT_DIR"
}

check_qgis_plugins_dir() {
    local resolved_dir
    local parent_dir
    resolved_dir="$(resolve_qgis_plugins_dir)"
    parent_dir="$(dirname "$resolved_dir")"
    if [ ! -d "$resolved_dir" ] && [ -d "$parent_dir" ]; then
        mkdir -p "$resolved_dir"
    fi
    if [ ! -d "$resolved_dir" ]; then
        add_preflight_error "未找到 QGIS 插件目录: $resolved_dir。请先安装并启动 QGIS 创建 profile，或改用 ./build.sh go / ./build.sh check 仅执行非部署步骤"
        return 0
    fi
    QGIS_PLUGINS_DIR="$resolved_dir"
}

print_preflight_result() {
    if [ ${#PREFLIGHT_ERRORS[@]} -eq 0 ]; then
        ok "环境预检通过"
        return 0
    fi

    step "环境预检失败"
    for error_message in "${PREFLIGHT_ERRORS[@]}"; do
        warn "$error_message"
    done
    fail "请按以上指引修复环境后重试"
}

preflight() {
    local command_name="$1"
    PREFLIGHT_ERRORS=()
    QGIS_PLUGINS_DIR=""

    step "环境预检: $command_name"
    check_repo_layout "$command_name"
    load_go_requirements
    detect_python
    load_app_metadata

    case "$command_name" in
        deps)
            check_node_toolchain
            ;;
        go)
            check_go_toolchain
            check_go_cgo_prereqs
            ;;
        check)
            check_go_toolchain
            check_go_cgo_prereqs
            check_node_toolchain
            check_rust_toolchain
            check_frontend_check_deps
            check_linux_tauri_prereqs
            check_macos_tauri_prereqs
            ;;
        packaged-smoke)
            require_command "$PYTHON_BIN" "缺少 Python，无法执行 packaged runtime smoke 脚本"
            ;;
        build)
            check_go_toolchain
            check_go_cgo_prereqs
            check_node_toolchain
            check_rust_toolchain
            check_frontend_deps
            check_linux_tauri_prereqs
            check_macos_tauri_prereqs
            require_command "rsync" "缺少 rsync，无法同步 QGIS 插件目录。请先安装 rsync 后重试"
            check_qgis_plugins_dir
            ;;
        dev)
            check_go_toolchain
            check_go_cgo_prereqs
            check_node_toolchain
            check_rust_toolchain
            check_frontend_deps
            check_linux_tauri_prereqs
            check_macos_tauri_prereqs
            require_command "rsync" "缺少 rsync，无法同步 QGIS 插件目录。请先安装 rsync 后重试"
            check_qgis_plugins_dir
            ;;
        plugin)
            ;;
        dist)
            check_go_toolchain
            check_go_cgo_prereqs
            check_node_toolchain
            check_rust_toolchain
            check_frontend_deps
            check_linux_tauri_prereqs
            check_macos_tauri_prereqs
            ;;
        app|dmg)
            if [ "$OS" != "darwin" ]; then
                add_preflight_error "$command_name 仅支持 macOS。当前系统为 $OS，请改用 ./build.sh build"
                print_preflight_result
                return 0
            fi
            check_go_toolchain
            check_go_cgo_prereqs
            check_node_toolchain
            check_rust_toolchain
            check_frontend_deps
            check_macos_tauri_prereqs
            ;;
        deb|appimage)
            if [ "$OS" != "linux" ]; then
                add_preflight_error "$command_name 仅支持 Linux。当前系统为 $OS"
                print_preflight_result
                return 0
            fi
            check_go_toolchain
            check_go_cgo_prereqs
            check_node_toolchain
            check_rust_toolchain
            check_frontend_deps
            check_linux_tauri_prereqs
            ;;
        clean|help)
            ;;
        *)
            add_preflight_error "未知命令: $command_name"
            ;;
    esac

    print_preflight_result
}

verify_sidecar_artifact() {
    local artifact_path="$BINARIES_DIR/carto-agent-${TARGET_TRIPLE}"
    [ "$GOOS_TARGET" = "windows" ] && artifact_path="${artifact_path}.exe"
    if [ ! -f "$artifact_path" ]; then
        fail "sidecar 构建后未生成预期产物: $artifact_path"
    fi
}

allocate_sidecar_smoke_port() {
    if [ -n "${AUTOCARTO_SIDECAR_SMOKE_PORT:-}" ]; then
        echo "$AUTOCARTO_SIDECAR_SMOKE_PORT"
        return 0
    fi

    local detected_port=""
    detected_port="$("$PYTHON_BIN" - <<'PY' 2>/dev/null || true
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
)"
    detected_port="$(printf '%s' "$detected_port" | tr -d '[:space:]')"
    if [ -n "$detected_port" ]; then
        echo "$detected_port"
        return 0
    fi

    local fallback_port=$((53100 + ($$ % 200)))
    warn "动态探�?sidecar smoke 端口失败，回退到固定候选端�?$fallback_port" >&2
    echo "$fallback_port"
}

verify_sidecar_runtime_contract() {
    local artifact_path="$BINARIES_DIR/carto-agent-${TARGET_TRIPLE}"
    [ "$GOOS_TARGET" = "windows" ] && artifact_path="${artifact_path}.exe"

    local runtime_home
    runtime_home="$(mktemp -d "${TMPDIR:-/tmp}/autocarto-sidecar-smoke.XXXXXX")"
    local log_path="$runtime_home/sidecar-smoke.log"
    local port
    port="$(allocate_sidecar_smoke_port)"

    local sidecar_pid=""
    cleanup_sidecar_runtime_contract() {
        if [ -n "$sidecar_pid" ] && kill -0 "$sidecar_pid" >/dev/null 2>&1; then
            kill "$sidecar_pid" >/dev/null 2>&1 || true
            local attempt=0
            while kill -0 "$sidecar_pid" >/dev/null 2>&1 && [ "$attempt" -lt 20 ]; do
                sleep 0.1
                attempt=$((attempt + 1))
            done
            if kill -0 "$sidecar_pid" >/dev/null 2>&1; then
                warn "sidecar smoke 进程未在预期时间内退出，发�?SIGKILL"
                kill -9 "$sidecar_pid" >/dev/null 2>&1 || true
            fi
            wait "$sidecar_pid" >/dev/null 2>&1 || true
        fi
        rm -rf "$runtime_home"
    }

    AUTOCARTO_HOME="$runtime_home" AUTOCARTO_ASSETS_ROOT="$PLUGIN_DIR" "$artifact_path" -port "$port" >"$log_path" 2>&1 &
    sidecar_pid=$!

    local attempt=0
    local health_ok=0
    while [ "$attempt" -lt 50 ]; do
        if curl -fsS --max-time 2 "http://127.0.0.1:$port/api/health" >/dev/null 2>&1; then
            health_ok=1
            break
        fi
        if ! kill -0 "$sidecar_pid" >/dev/null 2>&1; then
            break
        fi
        sleep 0.2
        attempt=$((attempt + 1))
    done

    if [ "$health_ok" -ne 1 ]; then
        tail -n 80 "$log_path" >&2 || true
        cleanup_sidecar_runtime_contract
        fail "sidecar runtime smoke 失败: api/health 未在超时内就绪"
    fi

    local descriptor_status
    attempt=0
    descriptor_status=""
    while [ "$attempt" -lt 20 ]; do
        descriptor_status="$(curl -sS -o "$runtime_home/descriptor.out" -w "%{http_code}" --max-time 5 -H "X-AutoCarto-Contract-Version: v1" "http://127.0.0.1:$port/api/runtime/descriptor" || true)"
        if [ "$descriptor_status" = "200" ]; then
            break
        fi
        if ! kill -0 "$sidecar_pid" >/dev/null 2>&1; then
            break
        fi
        sleep 0.5
        attempt=$((attempt + 1))
    done
    if [ "$descriptor_status" != "200" ]; then
        cat "$runtime_home/descriptor.out" >&2 2>/dev/null || true
        tail -n 80 "$log_path" >&2 || true
        cleanup_sidecar_runtime_contract
        fail "sidecar runtime smoke 失败�?api/runtime/descriptor 返回异常状�?${descriptor_status:-unknown}"
    fi

    local catalog_status
    attempt=0
    catalog_status=""
    while [ "$attempt" -lt 20 ]; do
        catalog_status="$(curl -sS -o "$runtime_home/catalog.out" -w "%{http_code}" --max-time 5 -H "X-AutoCarto-Contract-Version: v1" "http://127.0.0.1:$port/api/tools/catalog?scope=global" || true)"
        if [ "$catalog_status" = "200" ]; then
            break
        fi
        if ! kill -0 "$sidecar_pid" >/dev/null 2>&1; then
            break
        fi
        sleep 0.5
        attempt=$((attempt + 1))
    done
    if [ "$catalog_status" != "200" ]; then
        cat "$runtime_home/catalog.out" >&2 2>/dev/null || true
        tail -n 80 "$log_path" >&2 || true
        cleanup_sidecar_runtime_contract
        fail "sidecar runtime smoke 失败�?api/tools/catalog 返回异常状�?${catalog_status:-unknown}"
    fi

    cleanup_sidecar_runtime_contract
    ok "sidecar runtime contract smoke 通过"
}

build_go() {
    step "构建 Go sidecar"
    mkdir -p "$BINARIES_DIR"
    sync_plugin_metadata_version
    local ldflags=""
    if [ -n "$APP_VERSION" ]; then
        ldflags="-X main.buildVersion=$APP_VERSION"
    fi
    (
        cd "$AGENT_DIR"
        GOOS="$GOOS_TARGET" GOARCH="$GOARCH_TARGET" go build -ldflags "$ldflags" -o "$BINARIES_DIR/carto-agent-${TARGET_TRIPLE}" ./cmd/agent
    )
    verify_sidecar_artifact
    verify_sidecar_runtime_contract
    ok "sidecar 已生成：$BINARIES_DIR/carto-agent-${TARGET_TRIPLE}"
}

deploy_plugin() {
    step "部署 QGIS 插件"
    [ -n "$QGIS_PLUGINS_DIR" ] || QGIS_PLUGINS_DIR="$(resolve_qgis_plugins_dir)"
    [ -d "$QGIS_PLUGINS_DIR" ] || fail "QGIS 插件目录不存在：$QGIS_PLUGINS_DIR"

    local target_plugin_dir="$QGIS_PLUGINS_DIR/autocarto"
    if [ -d "$target_plugin_dir" ]; then
        info "清理已安�?autocarto 插件目录�?target_plugin_dir"
        rm -rf "$target_plugin_dir"
    fi

    info "同步 autocarto 插件�?$QGIS_PLUGINS_DIR/autocarto"
    rsync -a --delete --exclude='/data/' --exclude='.master_key' --exclude='secrets.enc' --exclude='.port' --exclude='/config/plugin_config.yaml' --exclude='*.db' --exclude='*.db-wal' --exclude='*.db-shm' --exclude='*.sqlite' --exclude='logs' --exclude='.session' --exclude='__pycache__' "$PLUGIN_DIR/" "$QGIS_PLUGINS_DIR/autocarto/"

    ok "QGIS 插件部署完成�?QGIS_PLUGINS_DIR"
}

run_tauri_build() {
    step "构建桌面应用"
    (
        cd "$WEB_DIR"
        if [ "$OS" = "linux" ]; then
            APPIMAGE_EXTRACT_AND_RUN=1 "$TAURI_CLI_BIN" build "$@"
        else
            "$TAURI_CLI_BIN" build "$@"
        fi
    )
    ok "桌面应用构建完成�?DESKTOP_ARTIFACT_HINT"
}

run_tauri_dev() {
    step "启动开发模式"
    info "前端入口：http://localhost:3000"
    info "�?Ctrl+C 停止"
    (
        cd "$WEB_DIR"
        "$TAURI_CLI_BIN" dev
    )
}

is_live_e2e_enabled() {
    [ "${AUTOCARTO_LIVE_E2E:-0}" = "1" ]
}

is_packaged_smoke_enabled() {
    [ "${AUTOCARTO_PACKAGED_SMOKE:-0}" = "1" ]
}

current_check_evidence_relative() {
    if is_packaged_smoke_enabled; then
        printf '%s\n' "$CHECK_PACKAGED_EVIDENCE_RELATIVE"
        return 0
    fi
    if is_live_e2e_enabled; then
        printf '%s\n' "$CHECK_LIVE_EVIDENCE_RELATIVE"
        return 0
    fi
    printf '%s\n' "$CHECK_EVIDENCE_RELATIVE"
}

packaged_smoke_evidence_dir() {
    printf '%s\n' "$ROOT_DIR/$PACKAGED_SMOKE_EVIDENCE_RELATIVE"
}

packaged_smoke_gate_dir() {
    printf '%s\n' "$ROOT_DIR/$PACKAGED_SMOKE_GATE_EVIDENCE_RELATIVE"
}

packaged_smoke_runtime_home_dir() {
    printf '%s\n' "$(packaged_smoke_gate_dir)/$PACKAGED_SMOKE_RUNTIME_HOME_NAME"
}

resolve_packaged_smoke_artifact() {
    local explicit_path="${1:-}"
    local bundle_dir="$TAURI_DIR/target/release/bundle"
    local candidate=""

    if [ -n "$explicit_path" ]; then
        if [ -e "$explicit_path" ]; then
            printf '%s\n' "$explicit_path"
            return 0
        fi
        printf '未找到指定的 packaged smoke 产物�?s\n' "$explicit_path" >&2
        return 1
    fi

    case "$OS" in
        linux)
            candidate="$DIST_DIR/${PRODUCT_NAME}-${APP_VERSION}-${GOOS_TARGET}-${GOARCH_TARGET}.AppImage"
            if [ -f "$candidate" ]; then
                printf '%s\n' "$candidate"
                return 0
            fi
            for candidate in "$bundle_dir"/appimage/*.AppImage; do
                [ -f "$candidate" ] || continue
                printf '%s\n' "$candidate"
                return 0
            done
            printf '%s\n' "未找到 Linux AppImage 打包产物。请先运行 ./build.sh dist 或 ./build.sh appimage，再执行 packaged smoke" >&2
            return 1
            ;;
        darwin)
            candidate="$DIST_DIR/${PRODUCT_NAME}-${APP_VERSION}-${GOOS_TARGET}-${GOARCH_TARGET}.app"
            if [ -d "$candidate" ]; then
                printf '%s\n' "$candidate"
                return 0
            fi
            for candidate in "$bundle_dir"/macos/*.app; do
                [ -d "$candidate" ] || continue
                printf '%s\n' "$candidate"
                return 0
            done
            printf '%s\n' "未找到 macOS .app 打包产物。请先运行 ./build.sh dist 或 ./build.sh app，再执行 packaged smoke" >&2
            return 1
            ;;
    esac

    printf '当前平台不支�?packaged smoke�?s\n' "$OS" >&2
    return 1
}

prepare_packaged_smoke_gate_dirs() {
    local smoke_dir
    local gate_dir
    local runtime_home
    smoke_dir="$(packaged_smoke_evidence_dir)"
    gate_dir="$(packaged_smoke_gate_dir)"
    runtime_home="$(packaged_smoke_runtime_home_dir)"

    rm -rf "$smoke_dir" "$gate_dir"
    mkdir -p "$smoke_dir" "$gate_dir" "$runtime_home"
}

extract_packaged_smoke_failure_reason() {
    local smoke_dir
    smoke_dir="$(packaged_smoke_evidence_dir)"
    "$PYTHON_BIN" - <<'PY' "$smoke_dir"
from pathlib import Path
import re
import sys

smoke_dir = Path(sys.argv[1])
smoke_log = smoke_dir / "smoke.log"
stderr_log = smoke_dir / "stderr.log"

for path in (smoke_log, stderr_log):
    if not path.exists():
        continue
    lines = [line.strip() for line in path.read_text(encoding="utf-8", errors="replace").splitlines() if line.strip()]
    for line in reversed(lines):
        if "packaged runtime smoke 失败:" in line:
            print(line.split("packaged runtime smoke 失败:", 1)[1].strip())
            raise SystemExit(0)
    if lines:
        print(lines[-1])
        raise SystemExit(0)

print("")
PY
}

write_packaged_smoke_gate_summary() {
    local mode="$1"
    local status_label="$2"
    local artifact_path="$3"
    local smoke_status="$4"
    local failure_reason="${5:-}"
    local summary_path="$(packaged_smoke_gate_dir)/$PACKAGED_SMOKE_GATE_SUMMARY_NAME"
    local smoke_dir="$PACKAGED_SMOKE_EVIDENCE_RELATIVE"
    local gate_dir="$PACKAGED_SMOKE_GATE_EVIDENCE_RELATIVE"
    local runtime_home="$gate_dir/$PACKAGED_SMOKE_RUNTIME_HOME_NAME"
    "$PYTHON_BIN" - <<'PY' "$summary_path" "$mode" "$status_label" "$artifact_path" "$smoke_status" "$failure_reason" "$smoke_dir" "$gate_dir" "$runtime_home" "$PACKAGED_SMOKE_PRODUCTION_BUNDLE_NOTE"
from pathlib import Path
import sys

summary_path = Path(sys.argv[1])
mode = sys.argv[2]
status_label = sys.argv[3]
artifact_path = sys.argv[4]
smoke_status = sys.argv[5]
failure_reason = sys.argv[6]
smoke_dir = sys.argv[7]
gate_dir = sys.argv[8]
runtime_home = sys.argv[9]
production_note = sys.argv[10]

content = [
    f"mode={mode}",
    f"status={status_label}",
    f"smoke_status={smoke_status}",
    f"artifact_path={artifact_path}",
    f"smoke_evidence_dir={smoke_dir}",
    f"gate_evidence_dir={gate_dir}",
    f"runtime_home={runtime_home}",
    f"descriptor_json={smoke_dir}/descriptor.json",
    f"legacy_rejection_json={smoke_dir}/legacy_rejection.json",
    f"stdout_log={smoke_dir}/stdout.log",
    f"stderr_log={smoke_dir}/stderr.log",
    f"smoke_log={smoke_dir}/smoke.log",
    f"production_bundle_note={production_note}",
]
if failure_reason:
    content.append(f"failure_reason={failure_reason}")
summary_path.write_text("\n".join(content) + "\n", encoding="utf-8")
PY
}

run_packaged_runtime_smoke_gate() {
    local mode="$1"
    local explicit_artifact_path="${2:-}"
    local artifact_path
    local smoke_dir
    local runtime_home
    local status=0
    local failure_reason=""

    artifact_path="$(resolve_packaged_smoke_artifact "$explicit_artifact_path")" || {
        failure_reason="未找到可用于 packaged smoke 的真实打包产物"
        mkdir -p "$(packaged_smoke_gate_dir)"
        write_packaged_smoke_gate_summary "$mode" "FAIL" "<missing>" "not-run" "$failure_reason"
        return 1
    }

    prepare_packaged_smoke_gate_dirs
    smoke_dir="$(packaged_smoke_evidence_dir)"
    runtime_home="$(packaged_smoke_runtime_home_dir)"

    set +e
    "$PYTHON_BIN" "$PACKAGED_SMOKE_SCRIPT_PATH" \
        --artifact-path "$artifact_path" \
        --runtime-home "$runtime_home" \
        --evidence-dir "$smoke_dir" \
        --expected-contract-version "$PACKAGED_SMOKE_CONTRACT_VERSION" \
        --timeout-sec "$PACKAGED_SMOKE_TIMEOUT_SEC"
    status=$?
    set -e

    if [ "$status" -ne 0 ]; then
        failure_reason="$(extract_packaged_smoke_failure_reason)"
        write_packaged_smoke_gate_summary "$mode" "FAIL" "$artifact_path" "failed" "$failure_reason"
        [ -n "$failure_reason" ] && warn "packaged smoke 失败原因：$failure_reason"
        return "$status"
    fi

    write_packaged_smoke_gate_summary "$mode" "PASS" "$artifact_path" "passed"
    ok "Packaged Runtime Smoke 通过：$artifact_path"
}

run_go_build_gate() {
    (
        cd "$AGENT_DIR"
        go build ./...
    )
}

run_go_vet_gate() {
    (
        cd "$AGENT_DIR"
        go vet ./...
    )
}

run_go_test_gate() {
    (
        cd "$AGENT_DIR"
        # Keep subprocess startup/idle-deadline tests independent of other packages.
        go test -p 1 ./...
    )
}

run_frontend_lint_gate() {
    (
        cd "$WEB_DIR"
        npm run lint
    )
}

run_frontend_unit_gate() {
    (
        cd "$WEB_DIR"
        npm test
    )
}

run_python_gate() {
    (
        cd "$ROOT_DIR"
        "$PYTHON_BIN" "$ROOT_DIR/scripts/refactor/run_python_tests.py"
    )
}

# Check servers own their child processes; occupied ports are never evicted.
cleanup_playwright_ports() { :; }

run_headless_e2e_gate() {
    (
        cd "$WEB_DIR"
        cleanup_playwright_ports
        AUTOCARTO_PLAYWRIGHT_USE_DEV_SERVER=1 "$PLAYWRIGHT_BIN" test "${CHECK_HEADLESS_E2E_SPECS[@]}"
    )
}

run_live_e2e_gate() {
    (
        cd "$WEB_DIR"
        cleanup_playwright_ports
        AUTOCARTO_PLAYWRIGHT_USE_DEV_SERVER=1 "$PLAYWRIGHT_BIN" test "$CHECK_LIVE_E2E_SPEC"
    )
}

run_next_build_gate() {
    (
        cd "$WEB_DIR"
        "$NEXT_BIN" build
    )
}

run_cargo_check_gate() {
    cargo check --release --manifest-path "$TAURI_DIR/Cargo.toml"
}

run_check_step() {
    local title="$1"
    local success_message="$2"
    shift 2

    step "$title"
    "$@"
    local status=$?
    if [ $status -ne 0 ]; then
        printf '%b[FAIL]%b %s 失败（exit %s）\n' "$RED" "$NC" "$title" "$status"
        return $status
    fi

    ok "$success_message"
}

print_check_banner() {
    step "Wave 6 架构门禁（Task 10）"
    info "Wave Gate：Wave 6 / Task 10 tightened verification gate"
    info "本次运行证据：$(current_check_evidence_relative)"
    info "默认 check 证据：$CHECK_EVIDENCE_RELATIVE"
    info "packaged check 证据：$CHECK_PACKAGED_EVIDENCE_RELATIVE"
    info "live check 证据：$CHECK_LIVE_EVIDENCE_RELATIVE"
    info "headless Playwright：ENABLED（默认）"
    info "失败归档目录：$CHECK_FAILURE_ARCHIVE_RELATIVE"
    if is_live_e2e_enabled; then
        info "live-QGIS：ENABLED（AUTOCARTO_LIVE_E2E=1）"
    else
        warn "live-QGIS：SKIPPED（原因：未设置 AUTOCARTO_LIVE_E2E=1）"
    fi
    if is_packaged_smoke_enabled; then
        info "packaged smoke：ENABLED（AUTOCARTO_PACKAGED_SMOKE=1）"
        info "packaged smoke 证据目录：$PACKAGED_SMOKE_EVIDENCE_RELATIVE"
        info "release gate 摘要目录：$PACKAGED_SMOKE_GATE_EVIDENCE_RELATIVE"
    else
        warn "packaged smoke：SKIPPED（原因：未设置 AUTOCARTO_PACKAGED_SMOKE=1）"
    fi
}

print_check_summary() {
    local status="$1"

    step "架构门禁汇总"
    info "本次运行证据：$(current_check_evidence_relative)"
    info "默认 check 证据：$CHECK_EVIDENCE_RELATIVE"
    info "packaged check 证据：$CHECK_PACKAGED_EVIDENCE_RELATIVE"
    info "live check 证据：$CHECK_LIVE_EVIDENCE_RELATIVE"
    info "headless Playwright：ENABLED（默认）"
    info "失败归档目录：$CHECK_FAILURE_ARCHIVE_RELATIVE"
    if is_live_e2e_enabled; then
        info "live-QGIS：ENABLED（AUTOCARTO_LIVE_E2E=1）"
    else
        warn "live-QGIS：SKIPPED（原因：未设置 AUTOCARTO_LIVE_E2E=1）"
    fi
    if is_packaged_smoke_enabled; then
        info "packaged smoke：ENABLED（AUTOCARTO_PACKAGED_SMOKE=1）"
        info "packaged smoke 证据目录：$PACKAGED_SMOKE_EVIDENCE_RELATIVE"
        info "release gate 摘要目录：$PACKAGED_SMOKE_GATE_EVIDENCE_RELATIVE"
    else
        warn "packaged smoke：SKIPPED（原因：未设置 AUTOCARTO_PACKAGED_SMOKE=1）"
    fi

    if [ "$status" -eq 0 ]; then
        ok "最终状态：PASS"
    else
        printf '%b[FAIL]%b 最终状态：FAIL（exit %s）\n' "$RED" "$NC" "$status"
    fi
}

run_check_pipeline() {
    run_check_step "Go 编译检查" "carto_agent 编译通过" run_go_build_gate || return $?
    run_check_step "Go 静态分析" "carto_agent vet 通过" run_go_vet_gate || return $?
    run_check_step "Go 单元测试" "carto_agent 测试通过" run_go_test_gate || return $?
    run_check_step "前端 Lint 检查" "前端 Lint 通过" run_frontend_lint_gate || return $?
    run_check_step "前端单元测试" "前端单元测试通过" run_frontend_unit_gate || return $?
    run_check_step "Python 测试" "Python 测试通过" run_python_gate || return $?
    run_check_step "Headless Playwright E2E" "Headless Playwright E2E 通过" run_headless_e2e_gate || return $?
    run_check_step "Next.js 构建检查" "前端构建通过" run_next_build_gate || return $?
    build_go || return $?
    run_check_step "Rust 编译检查" "Tauri 编译通过" run_cargo_check_gate || return $?
    if is_packaged_smoke_enabled; then
        run_check_step "Packaged Runtime Smoke" "Packaged Runtime Smoke 通过" run_packaged_runtime_smoke_gate check || return $?
    fi

    if is_live_e2e_enabled; then
        run_check_step "Live QGIS Playwright E2E" "Live QGIS Playwright E2E 通过" run_live_e2e_gate || return $?
    fi

    return 0
}

run_check() (
    local status=0
    local check_runtime
    check_runtime="$(mktemp -d "${TMPDIR:-/tmp}/autocarto-check.XXXXXX")"
    trap '"$PYTHON_BIN" -c "import shutil,sys; shutil.rmtree(sys.argv[1])" "$check_runtime"' EXIT
    export AUTOCARTO_HOME="$check_runtime"
    export AUTOCARTO_PROJECT_ROOT="$ROOT_DIR"
    export AUTOCARTO_ASSETS_ROOT="$PLUGIN_DIR"
    export AUTOCARTO_DISABLE_KEYRING=1
    export QT_QPA_PLATFORM="${QT_QPA_PLATFORM:-offscreen}"
    export PYTHONPATH="$PLUGIN_DIR/tests/golden/.runtime/deps:$ROOT_DIR${PYTHONPATH:+:$PYTHONPATH}"
    # Both listeners must be free; the supervisors fail honestly on conflicts.
    export AUTOCARTO_TEST_RUNTIME_PORT="${AUTOCARTO_TEST_RUNTIME_PORT:-50141}"
    export AUTOCARTO_PLAYWRIGHT_FRONTEND_PORT="${AUTOCARTO_PLAYWRIGHT_FRONTEND_PORT:-50142}"

    print_check_banner

    set +e
    run_check_pipeline
    status=$?
    set -e

    print_check_summary "$status"
    return "$status"
)

run_deps() {
    step "Install Frontend Dependencies"
    local lock_hash=""
    local cached_hash=""
    local should_install=1

    lock_hash="$(compute_sha256 "$PACKAGE_LOCK_FILE" 2>/dev/null || true)"
    if [ "${AUTOCARTO_FORCE_DEPS:-0}" = "1" ]; then
        info "AUTOCARTO_FORCE_DEPS=1, forcing npm ci"
    elif [ -n "$lock_hash" ] \
        && [ -d "$NODE_MODULES_DIR" ] \
        && [ -x "$TAURI_CLI_BIN" ] \
        && [ -x "$NEXT_BIN" ] \
        && [ -f "$DEPS_STAMP_FILE" ]; then
        cached_hash="$(tr -d '[:space:]' < "$DEPS_STAMP_FILE" || true)"
        if [ "$cached_hash" = "$lock_hash" ]; then
            should_install=0
        fi
    fi

    if [ "$should_install" = "0" ]; then
        ok "Frontend deps unchanged; skipping npm ci"
        return 0
    fi

    (cd "$WEB_DIR" && npm ci)
    if [ -n "$lock_hash" ]; then
        printf '%s\n' "$lock_hash" > "$DEPS_STAMP_FILE"
    else
        warn "Cannot compute package-lock hash; npm ci ran without updating stamp"
    fi
    ok "Frontend dependencies installed"
}

compute_sha256() {
    local target="$1"
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$target" | awk '{print $1}'
        return 0
    fi
    if command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "$target" | awk '{print $1}'
        return 0
    fi
    return 1
}

clean_local_artifacts() {
    local preserve_release_artifacts="${1:-0}"
    local preserve_runtime_files="${2:-0}"
    local snapshot_dir=""

    if [ "$preserve_release_artifacts" = "1" ]; then
        snapshot_dir="$(mktemp -d)"
        preserve_release_artifacts_snapshot "$snapshot_dir"
    fi

    step "清理构建产物"
    rm -rf "$TAURI_DIR/target" && info "清理 Rust target/"
    rm -rf "$WEB_DIR/out" && info "清理 Next.js out/"
    rm -rf "$WEB_DIR/.next" && info "清理 Next.js .next/"
    rm -rf "$DIST_DIR"
    if [ "$preserve_release_artifacts" = "1" ]; then
        restore_release_artifacts_snapshot "$snapshot_dir"
        rm -rf "$snapshot_dir"
        info "保留 release 产物（dist/ + bundle staging）"
    else
        info "清理 dist/"
    fi
    rm -rf "$BINARIES_DIR" && info "清理 sidecar binaries/"
    rm -rf "$PLUGIN_DIR/bin" && info "清理 autocarto/bin/"
    if [ "$preserve_runtime_files" != "1" ]; then
        rm -rf "$PLUGIN_DIR/logs" && info "清理日志"
        rm -rf "$PLUGIN_DIR/.session" && info "清理会话缓存"
    fi
    ok "清理完成"
}

preserve_release_artifacts_snapshot() {
    local snapshot_dir="$1"
    local dist_snapshot="$snapshot_dir/dist"
    local bundle_snapshot_parent="$snapshot_dir/target/release"
    local artifact=""

    mkdir -p "$dist_snapshot" "$bundle_snapshot_parent"

    for artifact in \
        "$DIST_DIR"/*.deb \
        "$DIST_DIR"/*.AppImage \
        "$DIST_DIR"/*.app \
        "$DIST_DIR"/*.dmg \
        "$DIST_DIR"/autocarto-plugin-*.zip; do
        [ -e "$artifact" ] || continue
        cp -R "$artifact" "$dist_snapshot/"
    done

    if [ -d "$TAURI_DIR/target/release/bundle" ]; then
        cp -R "$TAURI_DIR/target/release/bundle" "$bundle_snapshot_parent/"
    fi
}

restore_release_artifacts_snapshot() {
    local snapshot_dir="$1"
    local dist_snapshot="$snapshot_dir/dist"
    local bundle_snapshot="$snapshot_dir/target/release/bundle"
    local artifact=""
    local restored_dist=0

    for artifact in "$dist_snapshot"/*; do
        [ -e "$artifact" ] || continue
        mkdir -p "$DIST_DIR"
        cp -R "$artifact" "$DIST_DIR/"
        restored_dist=1
    done

    if [ "$restored_dist" = "0" ]; then
        rm -rf "$DIST_DIR"
    fi

    if [ -d "$bundle_snapshot" ]; then
        mkdir -p "$TAURI_DIR/target/release"
        cp -R "$bundle_snapshot" "$TAURI_DIR/target/release/"
    fi
}

clean() {
    clean_local_artifacts
}

full_build() {
    preflight build
    clean_local_artifacts
    build_go
    if [ "$OS" = "darwin" ]; then
        run_tauri_build --bundles app
    else
        run_tauri_build
    fi
    deploy_plugin

    step "构建完成"
    ok "桌面应用输出目录�?DESKTOP_ARTIFACT_HINT"
    ok "Agent sidecar�?BINARIES_DIR/carto-agent-${TARGET_TRIPLE}"
    ok "QGIS 插件目录�?QGIS_PLUGINS_DIR"
}

build_dist() {
    preflight dist
    clean_local_artifacts
    mkdir -p "$DIST_DIR"
    build_go
    if [ "$OS" = "darwin" ]; then
        run_tauri_build --bundles app,dmg
    else
        run_tauri_build --bundles deb,appimage
    fi
    build_plugin_archive
    collect_dist_artifacts
    step "Packaged Runtime Smoke"
    run_packaged_runtime_smoke_gate dist
    ok "dist 产物已整理到 $DIST_DIR"
}

show_help() {
    cat <<'EOF'
AutoCarto AI 构建脚本

用法: ./build.sh [命令]

命令:
  build   一键构建：预检 + Go sidecar + Tauri 桌面应用 + QGIS 插件部署（默认）
  dev     预检 + sidecar + QGIS 插件部署 + Tauri 开发模式
  deps    安装前端依赖（npm ci）
  app     仅在 macOS 构建 .app
  dmg     仅在 macOS 构建 .dmg
  deb     仅在 Linux 构建 .deb
  appimage 仅在 Linux 构建 .AppImage
  plugin  打包 QGIS 插件 zip
  dist    整理桌面端 + 插件完整产物到 dist/
  packaged-smoke  对当前平台已生成的真实打包产物执行 packaged runtime smoke
  go      仅构建 Go sidecar
  check   Wave 6 统一架构门禁（Go/Python/Web/Tauri/E2E，支持 live-QGIS opt-in）
  clean   清理构建产物
  help    显示帮助

说明:
  - 脚本不会自动安装依赖；缺失依赖时会在预检阶段失败并给出修复指引。
  - 前端依赖缺失时，请先运行：./build.sh deps
  - 校验已有发行包：AUTOCARTO_PACKAGED_SMOKE=1 ./build.sh check
  - 发行包检查记录：.sisyphus/evidence/task-10-build-check-packaged.txt
  - build/dev 需要现成的 QGIS 插件目录；若尚未创建，请先启动一次 QGIS。
EOF
}

CMD="${1:-build}"
case "$CMD" in
    build)
        full_build
        ;;
    dev)
        preflight dev
        clean_local_artifacts
        build_go
        deploy_plugin
        run_tauri_dev
        ;;
    app)
        preflight app
        clean_local_artifacts
        build_go
        run_tauri_build --bundles app
        ;;
    dmg)
        preflight dmg
        clean_local_artifacts
        build_go
        run_tauri_build --bundles dmg
        ;;
    deb)
        preflight deb
        clean_local_artifacts
        build_go
        run_tauri_build --bundles deb
        collect_dist_artifacts
        ;;
    appimage)
        preflight appimage
        clean_local_artifacts
        build_go
        run_tauri_build --bundles appimage
        collect_dist_artifacts
        ;;
    plugin)
        preflight plugin
        clean_local_artifacts
        build_plugin_archive
        ;;
    dist)
        build_dist
        ;;
    packaged-smoke)
        preflight packaged-smoke
        load_app_metadata
        step "Packaged Runtime Smoke"
        run_packaged_runtime_smoke_gate packaged-smoke "${2:-}"
        ;;
    go)
        preflight go
        # A sidecar-only rebuild must preserve caches used by the running frontend.
        build_go
        ;;
    check)
        preflight check
        clean_local_artifacts 1 1
        run_check
        ;;
    deps)
        preflight deps
        run_deps
        ;;
    clean)
        clean
        ;;
    help)
        show_help
        ;;
    *)
        warn "未知命令: $CMD"
        show_help
        exit 1
        ;;
esac
