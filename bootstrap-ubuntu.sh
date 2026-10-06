#!/bin/bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"
GO_VERSION_REQUIRED="1.24.11"
GO_INSTALL_DIR="$HOME/.local/go${GO_VERSION_REQUIRED}"
LOCAL_BIN_DIR="$HOME/.local/bin"
TMP_DIR="$HOME/.local/tmp"

ARCH="$(uname -m)"
case "$ARCH" in
    x86_64) GO_DOWNLOAD_ARCH="amd64" ;;
    arm64|aarch64) GO_DOWNLOAD_ARCH="arm64" ;;
    *)
        printf '[FAIL] 不支持的架构: %s\n' "$ARCH" >&2
        exit 1
        ;;
esac

GO_ARCHIVE_VERSION="go${GO_VERSION_REQUIRED}.linux-${GO_DOWNLOAD_ARCH}.tar.gz"
: "${GOPROXY:=https://goproxy.cn,direct}"
: "${GOSUMDB:=sum.golang.google.cn}"
export GOPROXY
export GOSUMDB

APT_PACKAGES=(
    build-essential
    pkg-config
    libwebkit2gtk-4.1-dev
    libjavascriptcoregtk-4.1-dev
    libssl-dev
    libayatana-appindicator3-dev
    librsvg2-dev
    libxdo-dev
    rsync
)

info() {
    printf '[INFO] %s\n' "$1"
}

ok() {
    printf '[OK] %s\n' "$1"
}

fail() {
    printf '[FAIL] %s\n' "$1" >&2
    exit 1
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

download_file() {
    local url="$1"
    local target="$2"
    if command -v curl >/dev/null 2>&1; then
        curl -fL "$url" -o "$target"
        return 0
    fi
    if command -v wget >/dev/null 2>&1; then
        wget -O "$target" "$url"
        return 0
    fi
    fail "缺少 curl 或 wget，无法下载依赖"
}

ensure_local_dirs() {
    mkdir -p "$LOCAL_BIN_DIR" "$TMP_DIR"
}

ensure_go() {
    local installed_version=""
    if command -v go >/dev/null 2>&1; then
        installed_version="$(go version | awk '{print $3}' | sed 's/^go//')"
    fi

    if [ -n "$installed_version" ] && version_ge "$installed_version" "$GO_VERSION_REQUIRED"; then
        ok "Go 已满足要求：$installed_version"
        return 0
    fi

    info "安装 Go $GO_VERSION_REQUIRED 到用户目录"
    local archive_path="$TMP_DIR/$GO_ARCHIVE_VERSION"
    local extract_dir="$TMP_DIR/go-extract-$$"
    rm -rf "$extract_dir"
    mkdir -p "$extract_dir"
    download_file "https://go.dev/dl/$GO_ARCHIVE_VERSION" "$archive_path"
    tar -C "$extract_dir" -xzf "$archive_path"
    rm -rf "$GO_INSTALL_DIR"
    mv "$extract_dir/go" "$GO_INSTALL_DIR"
    rm -rf "$extract_dir"
    ln -sf "$GO_INSTALL_DIR/bin/go" "$LOCAL_BIN_DIR/go"
    ln -sf "$GO_INSTALL_DIR/bin/gofmt" "$LOCAL_BIN_DIR/gofmt"
    ok "Go 安装完成：$("$LOCAL_BIN_DIR/go" version)"
}

ensure_rust() {
    export PATH="$HOME/.cargo/bin:$LOCAL_BIN_DIR:$PATH"

    if command -v rustc >/dev/null 2>&1 && command -v cargo >/dev/null 2>&1; then
        ok "Rust 已可用：$(rustc --version)"
        return 0
    fi

    info "安装 Rust stable 到用户目录"
    local installer="$TMP_DIR/rustup-init.sh"
    download_file "https://sh.rustup.rs" "$installer"
    chmod +x "$installer"
    sh "$installer" -y --profile minimal --default-toolchain stable
    export PATH="$HOME/.cargo/bin:$LOCAL_BIN_DIR:$PATH"
    ln -sf "$HOME/.cargo/bin/rustup" "$LOCAL_BIN_DIR/rustup"
    ln -sf "$HOME/.cargo/bin/rustc" "$LOCAL_BIN_DIR/rustc"
    ln -sf "$HOME/.cargo/bin/cargo" "$LOCAL_BIN_DIR/cargo"
    ok "Rust 安装完成：$(rustc --version)"
}

ensure_node() {
    command -v node >/dev/null 2>&1 || fail "缺少 Node.js，请先安装 Node.js LTS"
    command -v npm >/dev/null 2>&1 || fail "缺少 npm，请先安装完整的 Node.js"
    ok "Node.js 已可用：$(node --version)"
}

install_apt_packages() {
    local sudo_cmd="sudo"
    if [ "$(id -u)" -eq 0 ]; then
        sudo_cmd=""
    elif ! command -v sudo >/dev/null 2>&1; then
        fail "缺少 sudo，无法安装 Ubuntu 系统依赖"
    fi

    info "安装 Ubuntu 系统依赖"
    if [ -n "$sudo_cmd" ]; then
        if ! sudo -n true >/dev/null 2>&1; then
            if [ -t 0 ]; then
                sudo -v
            else
                fail "需要 sudo 密码安装 Ubuntu 系统依赖，请在交互式终端运行 ./bootstrap-ubuntu.sh"
            fi
        fi
        repair_ubuntu_sources "$sudo_cmd"
        $sudo_cmd apt update
        $sudo_cmd apt install -y "${APT_PACKAGES[@]}"
    else
        repair_ubuntu_sources ""
        apt update
        apt install -y "${APT_PACKAGES[@]}"
    fi
    ok "Ubuntu 系统依赖安装完成"
}

repair_ubuntu_sources() {
    local sudo_cmd="$1"
    local has_updates_source="0"

    if grep -R "jammy-updates" /etc/apt/sources.list /etc/apt/sources.list.d/* >/dev/null 2>&1; then
        has_updates_source="1"
    fi

    if [ "$has_updates_source" = "1" ] && [ ! -f /etc/apt/sources.list.d/ubuntu.source ]; then
        return 0
    fi

    info "修复 Ubuntu 22.04 APT 源配置"
    if [ -f /etc/apt/sources.list.d/ubuntu.source ]; then
        if [ -n "$sudo_cmd" ]; then
            $sudo_cmd cp /etc/apt/sources.list.d/ubuntu.source /etc/apt/sources.list.d/ubuntu.source.autocarto.bak
            $sudo_cmd rm -f /etc/apt/sources.list.d/ubuntu.source
        else
            cp /etc/apt/sources.list.d/ubuntu.source /etc/apt/sources.list.d/ubuntu.source.autocarto.bak
            rm -f /etc/apt/sources.list.d/ubuntu.source
        fi
    fi

    if [ -n "$sudo_cmd" ]; then
        $sudo_cmd tee /etc/apt/sources.list.d/ubuntu.sources >/dev/null <<'EOF'
Types: deb
URIs: http://mirrors.aliyun.com/ubuntu/
Suites: jammy jammy-updates jammy-backports
Components: main restricted universe multiverse
Signed-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg

Types: deb
URIs: http://mirrors.ustc.edu.cn/ubuntu/
Suites: jammy-security
Components: main restricted universe multiverse
Signed-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg
EOF
    else
        tee /etc/apt/sources.list.d/ubuntu.sources >/dev/null <<'EOF'
Types: deb
URIs: http://mirrors.aliyun.com/ubuntu/
Suites: jammy jammy-updates jammy-backports
Components: main restricted universe multiverse
Signed-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg

Types: deb
URIs: http://mirrors.ustc.edu.cn/ubuntu/
Suites: jammy-security
Components: main restricted universe multiverse
Signed-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg
EOF
    fi
    ok "Ubuntu APT 源已修复"
}

ensure_qgis_plugins_dir() {
    local plugin_dir="$HOME/.local/share/QGIS/QGIS3/profiles/default/python/plugins"
    local parent_dir
    parent_dir="$(dirname "$plugin_dir")"

    if [ -d "$plugin_dir" ]; then
        ok "QGIS 插件目录已存在：$plugin_dir"
        return 0
    fi

    if [ -d "$parent_dir" ]; then
        mkdir -p "$plugin_dir"
        ok "已创建 QGIS 插件目录：$plugin_dir"
        return 0
    fi

    info "尚未检测到 QGIS profile，后续 build 阶段将继续校验"
}

main() {
    [ "$(uname -s)" = "Linux" ] || fail "bootstrap-ubuntu.sh 仅支持 Linux"
    export PATH="$HOME/.cargo/bin:$LOCAL_BIN_DIR:$PATH"

    ensure_local_dirs
    ensure_node
    ensure_go
    ensure_rust
    install_apt_packages
    ensure_qgis_plugins_dir

    "$ROOT_DIR/build.sh" deps
    "$ROOT_DIR/build.sh" build
}

main "$@"
