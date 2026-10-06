#!/usr/bin/env bash
set -euo pipefail

# 自动检测脚本所在的 skill 目录
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SKILL_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

ROOT_DIR="${1:-$(pwd)}"
BIN_PATH="${2:-}"

# 按优先级查找二进制：显式参数 > skill 内嵌 > 项目根目录
if [[ -z "${BIN_PATH}" ]]; then
  if [[ -x "${SKILL_DIR}/bin/googlemapd" ]]; then
    BIN_PATH="${SKILL_DIR}/bin/googlemapd"
  elif [[ -x "${ROOT_DIR}/bin/googlemapd" ]]; then
    BIN_PATH="${ROOT_DIR}/bin/googlemapd"
  else
    echo "[ERROR] googlemapd 二进制未找到" >&2
    echo "提示: 在 AutoCartoAI 中运行 './build.sh go'，或在下载器目录单独运行 'make build'" >&2
    exit 1
  fi
fi

echo "检查 GoogleMapD CLI 环境..."

if [[ ! -d "${ROOT_DIR}" ]]; then
  echo "[ERROR] 根目录不存在: ${ROOT_DIR}" >&2
  exit 1
fi

if [[ ! -x "${BIN_PATH}" ]]; then
  echo "[ERROR] CLI 二进制不可执行: ${BIN_PATH}" >&2
  echo "提示: 在 AutoCartoAI 中运行 './build.sh go'，或在下载器目录单独运行 'make build'" >&2
  exit 1
fi

echo "[OK] 根目录: ${ROOT_DIR}"
echo "[OK] 二进制: ${BIN_PATH}"

if command -v go >/dev/null 2>&1; then
  echo "[OK] go: $(go version)"
else
  echo "[WARN] go 不在 PATH 中（仅源码编译需要）"
fi

# 检查配置文件
if [[ -f "${SKILL_DIR}/configs/config.example.yaml" ]]; then
  echo "[OK] 示例配置: ${SKILL_DIR}/configs/config.example.yaml"
elif [[ -f "${ROOT_DIR}/configs/config.example.yaml" ]]; then
  echo "[OK] 示例配置: ${ROOT_DIR}/configs/config.example.yaml"
else
  echo "[WARN] 未找到示例配置文件"
fi

echo "环境检查通过。"
