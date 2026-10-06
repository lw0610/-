#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SOURCE_DIR="${AUTOCARTO_PLUGIN_SOURCE:-$ROOT_DIR/autocarto}"
INTERVAL_SECONDS="${AUTOCARTO_SYNC_INTERVAL:-1}"

if [ ! -d "$SOURCE_DIR" ]; then
  echo "Source plugin directory not found: $SOURCE_DIR" >&2
  exit 1
fi

snapshot() {
  find "$SOURCE_DIR" \
    -type f \
    ! -path '*/__pycache__/*' \
    ! -path '*/logs/*' \
    ! -path '*/.session/*' \
    -print0 \
    | xargs -0 stat -f '%m %z %N' 2>/dev/null \
    | sort
}

echo "Watching AutoCarto plugin source for changes:"
echo "  source: $SOURCE_DIR"
echo "  interval: ${INTERVAL_SECONDS}s"
echo

"$ROOT_DIR/scripts/sync-autocarto-plugin.sh"
last_snapshot="$(snapshot)"

while true; do
  sleep "$INTERVAL_SECONDS"
  current_snapshot="$(snapshot)"
  if [ "$current_snapshot" != "$last_snapshot" ]; then
    echo
    echo "Change detected at $(date '+%Y-%m-%d %H:%M:%S'), syncing..."
    "$ROOT_DIR/scripts/sync-autocarto-plugin.sh"
    last_snapshot="$current_snapshot"
  fi
done
