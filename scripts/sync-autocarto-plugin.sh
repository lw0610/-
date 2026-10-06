#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SOURCE_DIR="${AUTOCARTO_PLUGIN_SOURCE:-$ROOT_DIR/autocarto}"
TARGET_DIR="${AUTOCARTO_QGIS_PLUGIN_DIR:-$HOME/Library/Application Support/QGIS/QGIS3/profiles/default/python/plugins/autocarto}"

if [ ! -d "$SOURCE_DIR" ]; then
  echo "Source plugin directory not found: $SOURCE_DIR" >&2
  exit 1
fi

mkdir -p "$TARGET_DIR"

rsync -a --delete \
  --exclude='logs' \
  --exclude='.session' \
  --exclude='__pycache__' \
  "$SOURCE_DIR/" \
  "$TARGET_DIR/"

echo "Synced AutoCarto plugin:"
echo "  source: $SOURCE_DIR"
echo "  target: $TARGET_DIR"
